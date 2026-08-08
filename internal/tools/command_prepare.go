package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/policy"
	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/workspace"
)

const (
	defaultCommandTimeout = 30 * time.Second
	maxCommandTimeout     = 10 * time.Minute
	defaultCommandOutput  = int64(64 * 1024)
	maxCommandOutput      = int64(4 * 1024 * 1024)
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type commandArgs struct {
	Command        string            `json:"command"`
	Args           []string          `json:"args,omitempty"`
	CWD            string            `json:"cwd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutMS      int64             `json:"timeout_ms,omitempty"`
	MaxOutputBytes int64             `json:"max_output_bytes,omitempty"`
	PTY            *bool             `json:"pty,omitempty"`
}

func (t *CommandTool) prepare(ctx context.Context, raw json.RawMessage) (action.Prepared, error) {
	if err := ctx.Err(); err != nil {
		return action.Prepared{}, err
	}
	var args commandArgs
	if err := decodeArgs(raw, &args); err != nil {
		return action.Prepared{}, err
	}
	if err := validateCommandToken(args.Command); err != nil {
		return action.Prepared{}, err
	}
	if t.projector.Detector().Contains(args.Command) {
		return action.Prepared{}, errors.New("command contains detected credential material")
	}
	if isShellInterpreter(args.Command) {
		return action.Prepared{}, errors.New("shell interpreters are unavailable; request a structured command and args instead")
	}
	for _, arg := range args.Args {
		if err := validateCommandValue("argument", arg); err != nil {
			return action.Prepared{}, err
		}
		if t.projector.Detector().Contains(arg) {
			return action.Prepared{}, errors.New("command argument contains detected credential material")
		}
	}

	cwd, err := t.prepareCWD(args.CWD)
	if err != nil {
		return action.Prepared{}, err
	}
	env, err := t.prepareEnvironment(args.Env)
	if err != nil {
		return action.Prepared{}, err
	}
	timeout, err := commandTimeout(args.TimeoutMS)
	if err != nil {
		return action.Prepared{}, err
	}
	maxOutput, err := commandOutputLimit(args.MaxOutputBytes)
	if err != nil {
		return action.Prepared{}, err
	}
	pty := true
	if args.PTY != nil {
		pty = *args.PTY
	}
	confinement := t.runner.ConfinementLevel()
	switch confinement {
	case sandbox.ConfinementNone, sandbox.ConfinementProcess, sandbox.ConfinementKernel:
	default:
		return action.Prepared{}, fmt.Errorf("sandbox returned unknown confinement level %d", confinement)
	}

	grants := cloneGrants(t.grants)
	spec := sandbox.CommandSpec{
		Command: args.Command, Args: append([]string(nil), args.Args...), CWD: cwd,
		Env: env, Timeout: timeout, MaxOutputBytes: maxOutput, PTY: pty, Grants: grants,
	}
	binary := filepath.Base(args.Command)
	effect := t.analyzer.Classify(binary, spec.Args, spec.Grants)
	if effect == policy.EffectSafe {
		// A read-only command receives no mutation or network capability even
		// when the session Authority Envelope contains it.
		spec.Grants.AllowedWritePaths = nil
		spec.Grants.Network = false
	}
	decision := t.policy.Decide(ctx, spec, effect, t.session)
	prepared := action.Prepared{
		Tool: "command", Effects: []action.Effect{action.EffectProcess},
		Effect: effect,
	}
	if effect == policy.EffectDangerous && decision.Kind != policy.PolicyDeny {
		prepared.Denied = true
		prepared.DenyReason = "dangerous command denied because policy returned an inconsistent decision"
		return prepared, nil
	}
	switch decision.Kind {
	case policy.PolicyDeny:
		if decision.Reason == "" {
			decision.Reason = "command denied by policy"
		}
	case policy.PolicyAllow:
		if effect != policy.EffectSafe && effect != policy.EffectWorkspace {
			return action.Prepared{}, errors.New("policy returned allow for an unrecognized command effect")
		}
	case policy.PolicyApprove:
		// Approval remains valid for safe, workspace, and unknown effects. It
		// only narrows automatic execution and never overrides dangerous.
	default:
		return action.Prepared{}, fmt.Errorf("policy returned unknown decision %q", decision.Kind)
	}
	if decision.Kind == policy.PolicyDeny {
		prepared.Denied = true
		prepared.DenyReason = decision.Reason
		return prepared, nil
	}

	identity := computeExecutionIdentity(spec, confinement)
	preview := buildCommandPreview(spec, effect, identity, sortedEnvKeys(args.Env))
	revisionID := ""
	if decision.Kind == policy.PolicyApprove {
		revisionID = newRequestID()
	}
	effective, err := json.Marshal(commandArgs{
		Command: args.Command, Args: append([]string(nil), args.Args...), CWD: args.CWD,
		Env: cloneStringMap(args.Env), TimeoutMS: timeout.Milliseconds(),
		MaxOutputBytes: maxOutput, PTY: boolPointer(pty),
	})
	if err != nil {
		return action.Prepared{}, fmt.Errorf("command: marshal effective arguments: %w", err)
	}
	prepared.Arguments = effective
	prepared.Paths = append(append([]string(nil), spec.Grants.AllowedReadPaths...), spec.Grants.AllowedWritePaths...)
	prepared.Command = &action.PreparedCommand{
		CommandSpec: spec, Effect: effect, Decision: decision, Identity: identity,
		Preview: preview, RevisionID: revisionID, CreatedAt: t.now(),
	}
	return prepared, nil
}

func (t *CommandTool) prepareCWD(requested string) (string, error) {
	if requested == "" {
		requested = "."
	}
	clean, err := t.fs.Clean(requested)
	if err != nil {
		return "", errors.New("cwd must be a workspace-relative directory")
	}
	workspaceFS, clean, err := t.fs.List(clean)
	if err != nil {
		if errors.Is(err, workspace.ErrEscape) {
			return "", errors.New("cwd must not cross a symlink or leave the workspace")
		}
		return "", fmt.Errorf("cwd is unavailable: %w", err)
	}
	info, err := iofs.Stat(workspaceFS, clean)
	if err != nil {
		return "", fmt.Errorf("cwd is unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("cwd is not a directory")
	}
	if clean == "." {
		return t.workspaceRoot, nil
	}
	return filepath.Join(t.workspaceRoot, filepath.FromSlash(clean)), nil
}

func (t *CommandTool) prepareEnvironment(requested map[string]string) ([]string, error) {
	keys := sortedEnvKeys(requested)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		value := requested[key]
		if !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("environment variable name %q is invalid", key)
		}
		if forbiddenEnvironmentKey(key) {
			return nil, fmt.Errorf("environment variable %q is not allowed", key)
		}
		if err := validateCommandValue("environment value", value); err != nil {
			return nil, err
		}
		if t.projector.Detector().Contains(value) {
			return nil, fmt.Errorf("environment variable %q contains detected credential material", key)
		}
		env = append(env, key+"="+value)
	}
	return env, nil
}

func validateCommandToken(command string) error {
	if strings.TrimSpace(command) == "" {
		return errors.New("command is required")
	}
	if command != strings.TrimSpace(command) {
		return errors.New("command must not contain leading or trailing whitespace")
	}
	return validateCommandValue("command", command)
}

func validateCommandValue(label, value string) error {
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s contains a NUL byte", label)
	}
	if !dataproj.IsText([]byte(value)) {
		return fmt.Errorf("%s is not valid text", label)
	}
	if rejectControl(value) {
		return fmt.Errorf("%s contains terminal control characters", label)
	}
	return nil
}

func forbiddenEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	switch upper {
	case "HOME", "USER", "LOGNAME", "PATH", "SHELL", "PWD", "OLDPWD":
		return true
	}
	for _, suffix := range []string{"_TOKEN", "_SECRET", "_PASSWORD", "_PASSWD", "_API_KEY", "_ACCESS_KEY", "_PRIVATE_KEY", "_CREDENTIAL", "_CREDENTIALS"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}

func isShellInterpreter(command string) bool {
	switch strings.ToLower(filepath.Base(command)) {
	case "sh", "bash", "zsh", "fish", "dash", "ksh", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return true
	default:
		return false
	}
}

func commandTimeout(milliseconds int64) (time.Duration, error) {
	if milliseconds == 0 {
		return defaultCommandTimeout, nil
	}
	if milliseconds < 0 {
		return 0, errors.New("timeout_ms must not be negative")
	}
	if milliseconds > maxCommandTimeout.Milliseconds() {
		return 0, fmt.Errorf("timeout_ms exceeds %d", maxCommandTimeout.Milliseconds())
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func commandOutputLimit(value int64) (int64, error) {
	if value == 0 {
		return defaultCommandOutput, nil
	}
	if value < 0 {
		return 0, errors.New("max_output_bytes must not be negative")
	}
	if value > maxCommandOutput {
		return 0, fmt.Errorf("max_output_bytes exceeds %d", maxCommandOutput)
	}
	return value, nil
}

func buildCommandPreview(spec sandbox.CommandSpec, effect policy.EffectClassification, identity action.ExecutionIdentity, envKeys []string) string {
	command := make([]string, 0, len(spec.Args)+1)
	command = append(command, strconv.Quote(spec.Command))
	for _, arg := range spec.Args {
		command = append(command, strconv.Quote(arg))
	}
	return fmt.Sprintf("Command: %s\nCWD: %s\nEnvironment keys: %s\nEffect: %s\nSandbox: %s\nTimeout: %s\nMax output: %d bytes\nIdentity: %s",
		strings.Join(command, " "), strconv.Quote(spec.CWD), strings.Join(envKeys, ", "), effect.String(),
		identity.SandboxLevel.String(), spec.Timeout, spec.MaxOutputBytes, identity.Digest())
}

func sortedEnvKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneGrants(grants sandbox.Grants) sandbox.Grants {
	return sandbox.Grants{
		AllowedReadPaths:  append([]string(nil), grants.AllowedReadPaths...),
		AllowedWritePaths: append([]string(nil), grants.AllowedWritePaths...),
		Network:           grants.Network,
	}
}

func boolPointer(value bool) *bool { return &value }

// rejectControl reports whether a string could manipulate a terminal preview.
func rejectControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) && r != '\t' {
			return true
		}
	}
	return false
}
