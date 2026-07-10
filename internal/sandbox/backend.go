package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/blkcor/coragent/internal/core"
)

const defaultCommandTimeout = 30 * time.Second

type sandboxExecution struct {
	Profile         string
	SandboxExecPath string
	Environment     []string
}

// Option configures a Sandbox.
type Option func(*Sandbox)

// WithSandboxExecPath overrides the sandbox-exec lookup, primarily for tests.
func WithSandboxExecPath(path string) Option {
	return func(s *Sandbox) {
		s.sandboxExecPath = path
		s.lookedUp = true
	}
}

// WithForceFallback forces policy fallback, primarily for tests and unsupported hosts.
func WithForceFallback(reason string) Option {
	return func(s *Sandbox) {
		s.forceFallback = true
		s.forceReason = reason
	}
}

// Sandbox is the real command confinement stage.
type Sandbox struct {
	policy          Policy
	inputs          PolicyInputs
	hasInputs       bool
	status          Status
	sandboxExecPath string
	lookedUp        bool
	forceFallback   bool
	forceReason     string
}

// New creates a sandbox stage for a derived policy.
func New(policy Policy, opts ...Option) *Sandbox {
	s := &Sandbox{policy: policy}
	for _, opt := range opts {
		opt(s)
	}
	s.selectBackend()
	return s
}

// NewFromInputs derives policy from inputs and keeps those inputs so per-call
// permission grants can widen the policy deterministically.
func NewFromInputs(inputs PolicyInputs, opts ...Option) (*Sandbox, error) {
	policy, err := DerivePolicy(inputs)
	if err != nil {
		return nil, err
	}
	s := &Sandbox{policy: policy, inputs: inputs, hasInputs: true}
	for _, opt := range opts {
		opt(s)
	}
	s.selectBackend()
	return s, nil
}

// Status reports the active confinement level.
func (s *Sandbox) Status() Status {
	return s.status
}

// Run executes a command-running tool under the active sandbox backend.
func (s *Sandbox) Run(ctx context.Context, handler core.ToolHandler, args map[string]interface{}) (string, error) {
	return s.run(ctx, handler, args, s.policy)
}

// RunWithGrants applies per-call permission grants before running the command.
func (s *Sandbox) RunWithGrants(ctx context.Context, handler core.ToolHandler, args map[string]interface{}, grants core.SandboxGrants) (string, error) {
	policy := s.policy
	if hasCoreGrants(grants) && s.hasInputs {
		inputs := s.inputs
		inputs.Permission = mergePermissionGrants(inputs.Permission, grants)
		derived, err := DerivePolicy(inputs)
		if err != nil {
			return "", fmt.Errorf("sandbox: derive permission policy: %w", err)
		}
		policy = derived
	} else if hasCoreGrants(grants) {
		wide, err := widenPolicy(policy, grants)
		if err != nil {
			return "", fmt.Errorf("sandbox: apply permission grants: %w", err)
		}
		policy = wide
	}
	return s.run(ctx, handler, args, policy)
}

func (s *Sandbox) run(ctx context.Context, handler core.ToolHandler, args map[string]interface{}, policy Policy) (string, error) {
	commandHandler, ok := handler.(core.CommandToolHandler)
	if !ok {
		return "", fmt.Errorf("sandbox: command-running tool %q must implement core.CommandToolHandler", handler.Descriptor().Name)
	}
	return commandHandler.ExecuteCommand(ctx, args, policyCommandRunner{sandbox: s, policy: policy})
}

type policyCommandRunner struct {
	sandbox *Sandbox
	policy  Policy
}

func (r policyCommandRunner) Run(ctx context.Context, spec core.CommandSpec) (string, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return "", fmt.Errorf("sandbox: command is required")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	environment, err := commandEnvironment(r.policy)
	if err != nil {
		return "", err
	}
	if r.sandbox.status.Level == ConfinementOSEnforced {
		return runSandboxedCommand(ctx, spec.Command, timeout, sandboxExecution{
			Profile:         DarwinProfile(r.policy),
			SandboxExecPath: r.sandbox.sandboxExecPath,
			Environment:     environment,
		})
	}
	if denied := fallbackDenied(r.policy, spec.Command); denied != "" {
		return "sandbox blocked command: " + denied, fmt.Errorf("sandbox: blocked command: %s", denied)
	}
	return runCommandProcess(ctx, "sh", []string{"-c", spec.Command}, timeout, environment)
}

func (s *Sandbox) selectBackend() {
	if s.forceFallback {
		reason := s.forceReason
		if reason == "" {
			reason = "forced fallback"
		}
		s.status = Status{Level: ConfinementPolicyFallback, Reason: reason}
		return
	}
	if runtime.GOOS != "darwin" {
		s.status = Status{Level: ConfinementPolicyFallback, Reason: "OS sandbox backend is only available on macOS"}
		return
	}
	if !s.lookedUp {
		path, err := exec.LookPath("sandbox-exec")
		if err != nil {
			s.status = Status{Level: ConfinementPolicyFallback, Reason: "sandbox-exec not found"}
			return
		}
		s.sandboxExecPath = path
	}
	if s.sandboxExecPath == "" {
		s.status = Status{Level: ConfinementPolicyFallback, Reason: "sandbox-exec not found"}
		return
	}
	if err := smokeTestSandboxExec(s.sandboxExecPath); err != nil {
		s.status = Status{Level: ConfinementPolicyFallback, Reason: "sandbox-exec unavailable: " + err.Error()}
		return
	}
	s.status = Status{Level: ConfinementOSEnforced}
}

func smokeTestSandboxExec(path string) error {
	return exec.Command(path, "-p", "(version 1)\n(allow default)\n", "/usr/bin/true").Run()
}

func runSandboxedCommand(ctx context.Context, command string, timeout time.Duration, execCfg sandboxExecution) (string, error) {
	name := "sandbox-exec"
	if execCfg.SandboxExecPath != "" {
		name = execCfg.SandboxExecPath
	}
	return runCommandProcess(ctx, name, []string{"-p", execCfg.Profile, "sh", "-c", command}, timeout, execCfg.Environment)
}

func runCommandProcess(ctx context.Context, name string, args []string, timeout time.Duration, environment []string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, name, args...)
	if environment != nil {
		cmd.Env = environment
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	output := buf.String()

	if ctx.Err() != nil {
		return composeCommandOutput(output, "[cancelled]"), fmt.Errorf("sandbox: command cancelled: %w", ctx.Err())
	}
	if cctx.Err() == context.DeadlineExceeded {
		note := fmt.Sprintf("[timed out after %s; exit code: -1]", timeout)
		return composeCommandOutput(output, note), fmt.Errorf("sandbox: command timed out after %s", timeout)
	}

	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if runErr != nil {
		return composeCommandOutput(output, "[error starting command]"), fmt.Errorf("sandbox: start command: %w", runErr)
	}

	text := composeCommandOutput(output, fmt.Sprintf("[exit code: %d]", exitCode))
	if exitCode != 0 {
		return text, fmt.Errorf("sandbox: command exited with code %d", exitCode)
	}
	return text, nil
}

func commandEnvironment(policy Policy) ([]string, error) {
	if policy.ScratchRoot == "" {
		return os.Environ(), nil
	}
	if err := os.MkdirAll(policy.ScratchRoot, 0o755); err != nil {
		return nil, fmt.Errorf("sandbox: create scratch root: %w", err)
	}
	overrides := []string{
		"TMPDIR=" + policy.ScratchRoot,
		"TMP=" + policy.ScratchRoot,
		"TEMP=" + policy.ScratchRoot,
		"GOTMPDIR=" + policy.ScratchRoot,
		"GOCACHE=" + filepath.Join(policy.ScratchRoot, "go-build-cache"),
	}
	replaced := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, _ := strings.Cut(entry, "=")
		replaced[key] = struct{}{}
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := replaced[key]; !ok {
			environment = append(environment, entry)
		}
	}
	return append(environment, overrides...), nil
}

func composeCommandOutput(output, note string) string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return note
	}
	return output + "\n" + note
}

func fallbackDenied(policy Policy, command string) string {
	for _, target := range writeTargets(command) {
		if !policy.CanWrite(target) {
			return fmt.Sprintf("write to %s is outside allowed sandbox write roots", target)
		}
	}
	return ""
}

func hasCoreGrants(grants core.SandboxGrants) bool {
	return len(grants.ExtraReadRoots) > 0 || len(grants.ExtraWriteRoots) > 0 || grants.Network
}

func mergePermissionGrants(base Grants, extra core.SandboxGrants) Grants {
	return Grants{
		ExtraReadRoots:  append(append([]string(nil), base.ExtraReadRoots...), extra.ExtraReadRoots...),
		ExtraWriteRoots: append(append([]string(nil), base.ExtraWriteRoots...), extra.ExtraWriteRoots...),
		Network:         base.Network || extra.Network,
	}
}

func widenPolicy(policy Policy, grants core.SandboxGrants) (Policy, error) {
	var err error
	policy.ReadRoots, err = appendCanonical(policy.ReadRoots, grants.ExtraReadRoots...)
	if err != nil {
		return Policy{}, err
	}
	policy.WriteRoots, err = appendCanonical(policy.WriteRoots, grants.ExtraWriteRoots...)
	if err != nil {
		return Policy{}, err
	}
	policy.ReadRoots = stableRoots(policy.ReadRoots)
	policy.WriteRoots = stableRoots(policy.WriteRoots)
	if grants.Network {
		policy.Network = NetworkAllowed
	}
	return policy, nil
}

func writeTargets(command string) []string {
	tokens := shellishFields(command)
	var out []string
	for i, tok := range tokens {
		switch tok {
		case ">", ">>":
			if i+1 < len(tokens) {
				out = append(out, tokens[i+1])
			}
		case "touch", "mkdir", "rm", "rmdir":
			out = append(out, commandArgsUntilOperator(tokens[i+1:])...)
		case "cp", "mv":
			args := commandArgsUntilOperator(tokens[i+1:])
			if len(args) > 0 {
				out = append(out, args[len(args)-1])
			}
		default:
			if strings.HasPrefix(tok, ">") && len(tok) > 1 {
				out = append(out, strings.TrimPrefix(strings.TrimPrefix(tok, ">"), ">"))
			}
		}
	}
	return out
}

func commandArgsUntilOperator(tokens []string) []string {
	var out []string
	for _, tok := range tokens {
		if tok == "&&" || tok == "||" || tok == ";" || tok == "|" {
			break
		}
		if strings.HasPrefix(tok, "-") {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func shellishFields(s string) []string {
	raw := strings.Fields(s)
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		out = append(out, strings.Trim(tok, `"'`))
	}
	return out
}

var _ core.Sandbox = (*Sandbox)(nil)
