package policy

import (
	"context"
	"testing"

	"github.com/blkcor/coragent/internal/sandbox"
)

func TestPolicyEngineDecisionMatrix(t *testing.T) {
	t.Parallel()
	engine := NewPolicyEngine()
	tests := []struct {
		name   string
		spec   CommandSpec
		effect EffectClassification
		want   PolicyDecisionKind
		reason string
	}{
		{name: "safe", spec: CommandSpec{Command: "git", Args: []string{"status"}}, effect: EffectSafe, want: PolicyAllow, reason: "safe read-only command"},
		{name: "dangerous", spec: CommandSpec{Command: "rm", Args: []string{"-rf", "."}}, effect: EffectDangerous, want: PolicyDeny, reason: "dangerous command"},
		{name: "workspace", spec: CommandSpec{Command: "go", Args: []string{"test", "./..."}}, effect: EffectWorkspace, want: PolicyApprove, reason: "workspace mutation requires approval"},
		{name: "zero-value unknown effect", spec: CommandSpec{Command: "mystery"}, effect: EffectUnknown, want: PolicyApprove, reason: "unrecognized command, requires approval"},
		{name: "out-of-range effect", spec: CommandSpec{Command: "mystery"}, effect: EffectClassification(99), want: PolicyApprove, reason: "unrecognized command, requires approval"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := engine.Decide(context.Background(), tt.spec, tt.effect, nil)
			if got.Kind != tt.want {
				t.Fatalf("Decide kind = %q, want %q", got.Kind, tt.want)
			}
			if got.Reason == "" || !contains(got.Reason, tt.reason) {
				t.Fatalf("Decide reason = %q, want substring %q", got.Reason, tt.reason)
			}
		})
	}
}

func TestPolicyEngineApprovalMemoryIsSessionScoped(t *testing.T) {
	t.Parallel()
	engine := NewPolicyEngine()
	sessionA := NewSessionState()
	sessionB := NewSessionState()
	specA := sandbox.CommandSpec{Command: "go", Args: []string{"test", "./pkg/a"}}
	specB := sandbox.CommandSpec{Command: "go", Args: []string{"test", "./pkg/b"}}

	first := engine.Decide(context.Background(), specA, EffectWorkspace, sessionA)
	if first.Kind != PolicyApprove {
		t.Fatalf("first decision = %q, want approve", first.Kind)
	}
	engine.RecordApproval(specA, first)

	if got := engine.Decide(context.Background(), specB, EffectWorkspace, sessionA); got.Kind != PolicyAllow {
		t.Fatalf("same-session matching prefix = %q, want allow", got.Kind)
	}
	if got := engine.Decide(context.Background(), specA, EffectWorkspace, sessionB); got.Kind != PolicyApprove {
		t.Fatalf("new-session decision = %q, want approve", got.Kind)
	}
}

func TestPolicyEngineZeroValueSessionStateIsIsolated(t *testing.T) {
	t.Parallel()
	engine := NewPolicyEngine()
	sessionA := &SessionState{}
	sessionB := &SessionState{}
	spec := CommandSpec{Command: "go", Args: []string{"test", "./..."}}
	decision := engine.Decide(context.Background(), spec, EffectWorkspace, sessionA)
	engine.RecordApproval(spec, decision)
	if got := engine.Decide(context.Background(), spec, EffectWorkspace, sessionA); got.Kind != PolicyAllow {
		t.Fatalf("approved zero-value session = %q, want allow", got.Kind)
	}
	if got := engine.Decide(context.Background(), spec, EffectWorkspace, sessionB); got.Kind != PolicyApprove {
		t.Fatalf("distinct zero-value session = %q, want approve", got.Kind)
	}
}

func TestPolicyEngineMemoryBindsAuthoritySensitiveScope(t *testing.T) {
	t.Parallel()
	state := NewSessionState()
	engine := NewPolicyEngine(state)
	base := CommandSpec{
		Command: "go", Args: []string{"test", "./pkg/a"}, CWD: "/workspace",
		Env: []string{"LANG=C", "TMPDIR=/tmp/session"}, Timeout: 30,
		MaxOutputBytes: 4096, PTY: true,
		Grants: sandbox.Grants{AllowedReadPaths: []string{"/workspace"}, AllowedWritePaths: []string{"/workspace"}},
	}
	decision := engine.Decide(context.Background(), base, EffectWorkspace, state)
	engine.RecordApproval(base, decision)

	tailChanged := base
	tailChanged.Args = []string{"test", "./pkg/b"}
	if got := engine.Decide(context.Background(), tailChanged, EffectWorkspace, state); got.Kind != PolicyAllow {
		t.Fatalf("documented prefix reuse = %q, want allow", got.Kind)
	}

	tests := []struct {
		name string
		edit func(*CommandSpec)
	}{
		{name: "executable", edit: func(spec *CommandSpec) { spec.Command = "/opt/go/bin/go" }},
		{name: "cwd", edit: func(spec *CommandSpec) { spec.CWD = "/workspace/subdir" }},
		{name: "environment", edit: func(spec *CommandSpec) { spec.Env = []string{"LANG=C", "TMPDIR=/tmp/other"} }},
		{name: "environment order", edit: func(spec *CommandSpec) { spec.Env = []string{"TMPDIR=/tmp/session", "LANG=C"} }},
		{name: "duplicate environment order", edit: func(spec *CommandSpec) { spec.Env = []string{"MODE=safe", "MODE=unsafe"} }},
		{name: "read grant", edit: func(spec *CommandSpec) { spec.Grants.AllowedReadPaths = []string{"/workspace", "/outside"} }},
		{name: "write grant", edit: func(spec *CommandSpec) { spec.Grants.AllowedWritePaths = []string{"/workspace", "/outside"} }},
		{name: "network", edit: func(spec *CommandSpec) { spec.Grants.Network = true }},
		{name: "timeout", edit: func(spec *CommandSpec) { spec.Timeout++ }},
		{name: "output bound", edit: func(spec *CommandSpec) { spec.MaxOutputBytes++ }},
		{name: "pty", edit: func(spec *CommandSpec) { spec.PTY = false }},
	}
	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			changed := base
			changed.Args = append([]string(nil), base.Args...)
			changed.Env = append([]string(nil), base.Env...)
			changed.Grants.AllowedReadPaths = append([]string(nil), base.Grants.AllowedReadPaths...)
			changed.Grants.AllowedWritePaths = append([]string(nil), base.Grants.AllowedWritePaths...)
			testCase.edit(&changed)
			if got := engine.Decide(context.Background(), changed, EffectWorkspace, state); got.Kind != PolicyApprove {
				t.Fatalf("scope-changed decision = %q, want approve", got.Kind)
			}
		})
	}
}

func TestCommandIdentityPreservesDuplicateEnvironmentOrder(t *testing.T) {
	t.Parallel()
	first := CommandSpec{Command: "go", Args: []string{"test"}, Env: []string{"MODE=unsafe", "MODE=safe"}}
	second := first
	second.Env = []string{"MODE=safe", "MODE=unsafe"}
	if CommandIdentityDigest(first) == CommandIdentityDigest(second) {
		t.Fatal("environment order with duplicate keys must change command identity")
	}
}

func TestPolicyEngineStoresAndValidatesIdentityDigest(t *testing.T) {
	t.Parallel()
	state := NewSessionState()
	engine := NewPolicyEngine(state)
	spec := CommandSpec{Command: "npm", Args: []string{"install", "one"}, CWD: "/workspace"}
	decision := engine.Decide(context.Background(), spec, EffectWorkspace, state)
	if decision.IdentityDigest == "" || len(decision.IdentityDigest) != 64 {
		t.Fatalf("identity digest = %q, want SHA256", decision.IdentityDigest)
	}
	engine.RecordApproval(spec, decision)
	got, ok := state.Memory.identityDigest(spec.Command, spec.Args)
	if !ok || got != decision.IdentityDigest {
		t.Fatalf("stored identity = %q, %v; want %q, true", got, ok, decision.IdentityDigest)
	}

	changed := spec
	changed.Args = []string{"install", "two"}
	engine.RecordApproval(changed, decision)
	if got, _ := state.Memory.identityDigest(spec.Command, spec.Args); got != decision.IdentityDigest {
		t.Fatalf("mismatched decision replaced stored identity: %q", got)
	}
}

func TestPolicyEngineMemoryCannotOverrideDangerousClassification(t *testing.T) {
	t.Parallel()
	state := NewSessionState()
	state.Memory.MarkApproved("rm", []string{"-rf", "."})
	engine := NewPolicyEngine(state)
	got := engine.Decide(context.Background(), CommandSpec{Command: "rm", Args: []string{"-rf", "."}}, EffectDangerous, state)
	if got.Kind != PolicyDeny {
		t.Fatalf("dangerous remembered decision = %q, want deny", got.Kind)
	}
}

func TestPolicyEngineRecordsOnlyMatchingApproveDecision(t *testing.T) {
	t.Parallel()
	engine := NewPolicyEngine()
	approved := CommandSpec{Command: "npm", Args: []string{"install", "example"}}
	other := CommandSpec{Command: "go", Args: []string{"test", "./..."}}
	decision := engine.Decide(context.Background(), approved, EffectWorkspace, nil)

	engine.RecordApproval(other, decision)
	if got := engine.Decide(context.Background(), other, EffectWorkspace, nil); got.Kind != PolicyApprove {
		t.Fatalf("mismatched approval was recorded: %q", got.Kind)
	}
	engine.RecordApproval(approved, decision)
	if got := engine.Decide(context.Background(), approved, EffectWorkspace, nil); got.Kind != PolicyAllow {
		t.Fatalf("matching approval was not recorded: %q", got.Kind)
	}
}

func TestApprovalPrefixScopesCommandFamilies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cmd  string
		args []string
		want string
	}{
		{cmd: "/usr/local/bin/go", args: []string{"test", "./pkg/a"}, want: "go:test"},
		{cmd: "npm", args: []string{"install", "one"}, want: "npm:install"},
		{cmd: "git", args: []string{"checkout", "path/a"}, want: "git:checkout:path/a"},
		{cmd: "git", args: []string{"status"}, want: "git:status"},
		{cmd: "pwd", want: "pwd"},
	}
	for _, tt := range tests {
		if got := ApprovalPrefix(tt.cmd, tt.args); got != tt.want {
			t.Errorf("ApprovalPrefix(%q, %q) = %q, want %q", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestPolicyEngineCancelledContextFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := NewPolicyEngine().Decide(ctx, CommandSpec{Command: "ls"}, EffectSafe, nil)
	if got.Kind != PolicyDeny {
		t.Fatalf("cancelled decision = %q, want deny", got.Kind)
	}
}

func contains(value, substring string) bool {
	if len(substring) > len(value) {
		return false
	}
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
