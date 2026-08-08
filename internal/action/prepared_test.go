package action

import (
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/sandbox"
)

func TestExecutionIdentityDeterministicAndCollisionResistant(t *testing.T) {
	t.Parallel()
	base := ExecutionIdentity{
		Command: "/usr/bin/go", Args: []string{"test", "./..."}, CWD: "/workspace",
		EnvKeys: []string{"LANG"}, EnvValues: []string{"C"}, Timeout: 30 * time.Second,
		MaxOutputBytes: 64 * 1024, PTY: true,
		ReadPaths: []string{"/workspace"}, WritePaths: []string{"/workspace"},
		SandboxLevel: sandbox.ConfinementKernel, PolicyVersion: "policy-v1",
	}
	baseDigest := base.Digest()
	if repeated := base.Digest(); repeated != baseDigest || len(baseDigest) != 64 {
		t.Fatalf("digest is not deterministic SHA256: %q then %q", baseDigest, repeated)
	}
	tests := []struct {
		name string
		edit func(*ExecutionIdentity)
	}{
		{name: "command", edit: func(v *ExecutionIdentity) { v.Command = "/other/go" }},
		{name: "argument", edit: func(v *ExecutionIdentity) { v.Args = []string{"test", "./pkg"} }},
		{name: "cwd", edit: func(v *ExecutionIdentity) { v.CWD = "/workspace/pkg" }},
		{name: "env key", edit: func(v *ExecutionIdentity) { v.EnvKeys = []string{"LC_ALL"} }},
		{name: "env value", edit: func(v *ExecutionIdentity) { v.EnvValues = []string{"en_US.UTF-8"} }},
		{name: "timeout", edit: func(v *ExecutionIdentity) { v.Timeout++ }},
		{name: "output", edit: func(v *ExecutionIdentity) { v.MaxOutputBytes++ }},
		{name: "pty", edit: func(v *ExecutionIdentity) { v.PTY = false }},
		{name: "read grants", edit: func(v *ExecutionIdentity) { v.ReadPaths = []string{"/workspace", "/outside"} }},
		{name: "write grants", edit: func(v *ExecutionIdentity) { v.WritePaths = []string{"/workspace", "/outside"} }},
		{name: "network", edit: func(v *ExecutionIdentity) { v.Network = true }},
		{name: "sandbox", edit: func(v *ExecutionIdentity) { v.SandboxLevel = sandbox.ConfinementProcess }},
		{name: "policy", edit: func(v *ExecutionIdentity) { v.PolicyVersion = "policy-v2" }},
	}
	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			changed := base
			changed.Args = append([]string(nil), base.Args...)
			changed.EnvKeys = append([]string(nil), base.EnvKeys...)
			changed.EnvValues = append([]string(nil), base.EnvValues...)
			changed.ReadPaths = append([]string(nil), base.ReadPaths...)
			changed.WritePaths = append([]string(nil), base.WritePaths...)
			testCase.edit(&changed)
			if changed.Digest() == base.Digest() {
				t.Fatal("changed execution input collided with base identity")
			}
		})
	}
	invalidThree := base
	invalidThree.SandboxLevel = sandbox.ConfinementLevel(3)
	invalidFour := base
	invalidFour.SandboxLevel = sandbox.ConfinementLevel(4)
	if invalidThree.Digest() == invalidFour.Digest() {
		t.Fatal("distinct invalid confinement levels collided")
	}
}
