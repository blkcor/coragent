package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/policy"
	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/workspace"
)

type commandSandbox struct {
	level  sandbox.ConfinementLevel
	starts int
}

func (s *commandSandbox) Start(context.Context, sandbox.CommandSpec) (sandbox.Process, error) {
	s.starts++
	return nil, errors.New("unexpected command execution during Prepare")
}

func (s *commandSandbox) ConfinementLevel() sandbox.ConfinementLevel { return s.level }

type fixedAnalyzer struct {
	effect policy.EffectClassification
	cmd    string
	args   []string
}

func (a *fixedAnalyzer) Classify(cmd string, args []string, _ sandbox.Grants) policy.EffectClassification {
	a.cmd = cmd
	a.args = append([]string(nil), args...)
	return a.effect
}

type fixedPolicy struct {
	decision policy.PolicyDecision
	spec     sandbox.CommandSpec
	effect   policy.EffectClassification
}

func (p *fixedPolicy) Decide(_ context.Context, spec policy.CommandSpec, effect policy.EffectClassification, _ *policy.SessionState) policy.PolicyDecision {
	p.spec = spec
	p.effect = effect
	return p.decision
}

type commandFixture struct {
	tool     *tools.CommandTool
	root     string
	analyzer *fixedAnalyzer
	policy   *fixedPolicy
	runner   *commandSandbox
}

func newCommandFixture(t *testing.T, effect policy.EffectClassification, decision policy.PolicyDecisionKind) commandFixture {
	t.Helper()
	root := t.TempDir()
	workspaceFS, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceFS.Close() })
	root = workspaceFS.Name()
	analyzer := &fixedAnalyzer{effect: effect}
	policyFake := &fixedPolicy{decision: policy.PolicyDecision{Kind: decision, Reason: "test decision"}}
	runner := &commandSandbox{level: sandbox.ConfinementKernel}
	tool, err := tools.NewCommandTool(tools.CommandToolConfig{
		WorkspaceRoot: root,
		FileService:   workspace.NewFileService(workspaceFS),
		Projector:     dataproj.New(),
		Analyzer:      analyzer,
		Policy:        policyFake,
		Session:       policy.NewSessionState(),
		Sandbox:       runner,
		Grants: sandbox.Grants{
			AllowedReadPaths:  []string{root},
			AllowedWritePaths: []string{root},
		},
		Now: func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewCommandTool: %v", err)
	}
	return commandFixture{tool: tool, root: root, analyzer: analyzer, policy: policyFake, runner: runner}
}

func TestCommandPrepareParsesEffectiveSpecWithoutExecuting(t *testing.T) {
	t.Parallel()
	fixture := newCommandFixture(t, policy.EffectWorkspace, policy.PolicyApprove)
	if err := os.Mkdir(filepath.Join(fixture.root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	prepared, err := fixture.tool.Prepare(context.Background(), json.RawMessage(`{
		"command":"go","args":["test","./..."],"cwd":"pkg",
		"env":{"LANG":"private-normal-value"},"timeout_ms":1500,
		"max_output_bytes":2048,"pty":false
	}`))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prepared.Tool != "command" || !reflect.DeepEqual(prepared.Effects, []action.Effect{action.EffectProcess}) {
		t.Fatalf("prepared envelope = %+v", prepared)
	}
	if prepared.Command == nil {
		t.Fatal("Prepared.Command is nil")
	}
	spec := prepared.Command.CommandSpec
	if spec.Command != "go" || !reflect.DeepEqual(spec.Args, []string{"test", "./..."}) {
		t.Fatalf("command spec = %+v", spec)
	}
	if spec.CWD != filepath.Join(fixture.root, "pkg") {
		t.Fatalf("cwd = %q", spec.CWD)
	}
	if !reflect.DeepEqual(spec.Env, []string{"LANG=private-normal-value"}) || spec.Timeout != 1500*time.Millisecond || spec.MaxOutputBytes != 2048 || spec.PTY {
		t.Fatalf("effective command options = %+v", spec)
	}
	if fixture.analyzer.cmd != "go" || !reflect.DeepEqual(fixture.analyzer.args, spec.Args) {
		t.Fatalf("analyzer input = %q %q", fixture.analyzer.cmd, fixture.analyzer.args)
	}
	if fixture.policy.effect != policy.EffectWorkspace || fixture.policy.spec.CWD != spec.CWD {
		t.Fatalf("policy input = %+v, %s", fixture.policy.spec, fixture.policy.effect)
	}
	if !prepared.NeedsApproval() || prepared.Command.RevisionID == "" {
		t.Fatalf("approval state = needs:%v revision:%q", prepared.NeedsApproval(), prepared.Command.RevisionID)
	}
	if len(prepared.Command.Identity.Digest()) != 64 {
		t.Fatalf("identity digest = %q", prepared.Command.Identity.Digest())
	}
	preview := prepared.Command.Preview
	for _, want := range []string{"Command:", "go", "test", "CWD:", "Effect: workspace", "Sandbox: kernel", "Environment keys: LANG"} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, "private-normal-value") {
		t.Fatal("preview exposed environment value")
	}
	if fixture.runner.starts != 0 {
		t.Fatalf("sandbox starts during Prepare = %d", fixture.runner.starts)
	}
}

func TestCommandPreparePolicyPaths(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name         string
		decision     policy.PolicyDecisionKind
		wantDenied   bool
		wantCommand  bool
		wantApproval bool
		wantRevision bool
	}{
		{name: "deny", decision: policy.PolicyDeny, wantDenied: true},
		{name: "allow", decision: policy.PolicyAllow, wantCommand: true},
		{name: "approve", decision: policy.PolicyApprove, wantCommand: true, wantApproval: true, wantRevision: true},
	} {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newCommandFixture(t, policy.EffectWorkspace, testCase.decision)
			prepared, err := fixture.tool.Prepare(context.Background(), json.RawMessage(`{"command":"go","args":["test"]}`))
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Denied != testCase.wantDenied || (prepared.Command != nil) != testCase.wantCommand || prepared.NeedsApproval() != testCase.wantApproval {
				t.Fatalf("prepared decision = %+v", prepared)
			}
			if prepared.Command != nil && (prepared.Command.RevisionID != "") != testCase.wantRevision {
				t.Fatalf("revision = %q", prepared.Command.RevisionID)
			}
			if prepared.Denied && (prepared.Arguments != nil || prepared.Command != nil) {
				t.Fatal("denied action retained executable identity or arguments")
			}
			if fixture.runner.starts != 0 {
				t.Fatal("Prepare executed a command")
			}
		})
	}
}

func TestCommandPrepareFailsClosedOnInvalidPolicyOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		effect     policy.EffectClassification
		decision   policy.PolicyDecisionKind
		wantDenied bool
	}{
		{name: "zero decision", effect: policy.EffectWorkspace},
		{name: "future decision", effect: policy.EffectWorkspace, decision: policy.PolicyDecisionKind("future")},
		{name: "dangerous allow", effect: policy.EffectDangerous, decision: policy.PolicyAllow, wantDenied: true},
		{name: "unknown effect allow", effect: policy.EffectUnknown, decision: policy.PolicyAllow},
	}
	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newCommandFixture(t, testCase.effect, testCase.decision)
			prepared, err := fixture.tool.Prepare(context.Background(), json.RawMessage(`{"command":"custom-tool"}`))
			if testCase.wantDenied {
				if err != nil {
					t.Fatalf("Prepare: %v", err)
				}
				if !prepared.Denied || prepared.Command != nil || prepared.NeedsApproval() {
					t.Fatalf("dangerous inconsistent policy did not fail closed: %+v", prepared)
				}
				return
			}
			if err == nil {
				t.Fatalf("Prepare = %+v, want policy invariant error", prepared)
			}
		})
	}
}

func TestCommandPrepareRejectsInvalidConfinementLevel(t *testing.T) {
	t.Parallel()
	fixture := newCommandFixture(t, policy.EffectWorkspace, policy.PolicyApprove)
	fixture.runner.level = sandbox.ConfinementLevel(3)
	prepared, err := fixture.tool.Prepare(context.Background(), json.RawMessage(`{"command":"go"}`))
	if err == nil {
		t.Fatalf("Prepare = %+v, want invalid confinement error", prepared)
	}
	if prepared.Command != nil || prepared.NeedsApproval() {
		t.Fatalf("invalid confinement produced executable action: %+v", prepared)
	}
}

func TestCommandPrepareUsesRealEffectAndPolicyMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		command      string
		args         []string
		wantEffect   policy.EffectClassification
		wantDecision policy.PolicyDecisionKind
	}{
		{name: "safe", command: "ls", wantEffect: policy.EffectSafe, wantDecision: policy.PolicyAllow},
		{name: "workspace", command: "go", args: []string{"test", "./..."}, wantEffect: policy.EffectWorkspace, wantDecision: policy.PolicyApprove},
		{name: "dangerous", command: "rm", args: []string{"-rf", "."}, wantEffect: policy.EffectDangerous, wantDecision: policy.PolicyDeny},
		{name: "unknown command", command: "custom-tool", wantEffect: policy.EffectWorkspace, wantDecision: policy.PolicyApprove},
	}
	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			workspaceFS, err := workspace.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspaceFS.Close() }()
			runner := &commandSandbox{level: sandbox.ConfinementKernel}
			state := policy.NewSessionState()
			tool, err := tools.NewCommandTool(tools.CommandToolConfig{
				WorkspaceRoot: root, FileService: workspace.NewFileService(workspaceFS),
				Analyzer: policy.NewEffectAnalyzer(), Policy: policy.NewPolicyEngine(state), Session: state, Sandbox: runner,
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"command": testCase.command, "args": testCase.args})
			prepared, err := tool.Prepare(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Effect != testCase.wantEffect {
				t.Fatalf("effect = %s, want %s", prepared.Effect, testCase.wantEffect)
			}
			gotDecision := policy.PolicyDeny
			if prepared.Command != nil {
				gotDecision = prepared.Command.Decision.Kind
			}
			if gotDecision != testCase.wantDecision {
				t.Fatalf("decision = %s, want %s", gotDecision, testCase.wantDecision)
			}
		})
	}
}

func TestCommandPrepareRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()
	fixture := newCommandFixture(t, policy.EffectWorkspace, policy.PolicyApprove)
	secret := "sk-123456789012345678901234567890"
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty command", raw: `{}`},
		{name: "unknown field", raw: `{"command":"go","extra":true}`},
		{name: "shell", raw: `{"command":"sh","args":["-c","go test ./..."]}`},
		{name: "nul argument", raw: `{"command":"go","args":["bad\u0000arg"]}`},
		{name: "terminal control", raw: `{"command":"go","args":["\u001b[31m"]}`},
		{name: "reserved env", raw: `{"command":"go","env":{"PATH":"/tmp/bin"}}`},
		{name: "credential env name", raw: `{"command":"go","env":{"SERVICE_TOKEN":"normal"}}`},
		{name: "credential argument", raw: `{"command":"go","args":["` + secret + `"]}`},
		{name: "credential env value", raw: `{"command":"go","env":{"LANG":"` + secret + `"}}`},
		{name: "negative timeout", raw: `{"command":"go","timeout_ms":-1}`},
		{name: "large timeout", raw: `{"command":"go","timeout_ms":600001}`},
		{name: "negative output", raw: `{"command":"go","max_output_bytes":-1}`},
		{name: "large output", raw: `{"command":"go","max_output_bytes":4194305}`},
	}
	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fixture.tool.Prepare(context.Background(), json.RawMessage(testCase.raw)); err == nil {
				t.Fatal("Prepare succeeded, want rejection")
			}
		})
	}
}

func TestCommandPrepareConfinesCWD(t *testing.T) {
	t.Parallel()
	fixture := newCommandFixture(t, policy.EffectWorkspace, policy.PolicyApprove)
	filePath := filepath.Join(fixture.root, "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(fixture.root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{"../outside", outside, "escape", "file.txt", "missing"} {
		raw, _ := json.Marshal(map[string]any{"command": "go", "cwd": cwd})
		if _, err := fixture.tool.Prepare(context.Background(), raw); err == nil {
			t.Errorf("Prepare cwd %q succeeded, want rejection", cwd)
		}
	}
}

func TestCommandPrepareDefaultsAndMinimalEnvironment(t *testing.T) {
	fixture := newCommandFixture(t, policy.EffectWorkspace, policy.PolicyApprove)
	prepared, err := fixture.tool.Prepare(context.Background(), json.RawMessage(`{"command":"go"}`))
	if err != nil {
		t.Fatal(err)
	}
	spec := prepared.Command.CommandSpec
	if spec.Timeout != 30*time.Second || spec.MaxOutputBytes != 64*1024 || !spec.PTY {
		t.Fatalf("defaults = %+v", spec)
	}
	if len(spec.Env) != 0 {
		t.Fatalf("ambient environment propagated: %q", spec.Env)
	}
}

func TestSafeCommandDropsWriteAndNetworkGrants(t *testing.T) {
	t.Parallel()
	fixture := newCommandFixture(t, policy.EffectSafe, policy.PolicyAllow)
	fixture.tool, _ = tools.NewCommandTool(tools.CommandToolConfig{
		WorkspaceRoot: fixture.root,
		FileService:   commandFileService(t, fixture.root),
		Analyzer:      fixture.analyzer,
		Policy:        fixture.policy,
		Sandbox:       fixture.runner,
		Grants: sandbox.Grants{
			AllowedReadPaths: []string{fixture.root}, AllowedWritePaths: []string{fixture.root}, Network: true,
		},
	})
	prepared, err := fixture.tool.Prepare(context.Background(), json.RawMessage(`{"command":"ls"}`))
	if err != nil {
		t.Fatal(err)
	}
	grants := prepared.Command.CommandSpec.Grants
	if len(grants.AllowedWritePaths) != 0 || grants.Network {
		t.Fatalf("safe command retained mutation authority: %+v", grants)
	}
}

func TestNewCommandToolRejectsMismatchedWorkspaceCapability(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	other := t.TempDir()
	workspaceFS, err := workspace.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceFS.Close() }()
	_, err = tools.NewCommandTool(tools.CommandToolConfig{
		WorkspaceRoot: root, FileService: workspace.NewFileService(workspaceFS),
		Analyzer: policy.NewEffectAnalyzer(), Policy: policy.NewPolicyEngine(),
		Sandbox: &commandSandbox{level: sandbox.ConfinementKernel},
	})
	if err == nil {
		t.Fatal("NewCommandTool accepted a FileService for another workspace")
	}
}

func commandFileService(t *testing.T, root string) workspace.FileService {
	t.Helper()
	workspaceFS, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceFS.Close() })
	return workspace.NewFileService(workspaceFS)
}
