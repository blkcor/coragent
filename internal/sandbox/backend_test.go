package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/core"
)

func TestDarwinProfileReflectsPolicy(t *testing.T) {
	p := Policy{
		ReadRoots:  []string{"/project", "/usr/bin"},
		WriteRoots: []string{"/project", "/tmp/scratch"},
		Network:    NetworkDenied,
	}
	profile := DarwinProfile(p)
	for _, want := range []string{
		`(allow default)`,
		`(deny file-read* (require-all`,
		`(deny file-write*)`,
		`(allow file-read* (subpath "/project"))`,
		`(allow file-write* (subpath "/tmp/scratch"))`,
		`(deny network*)`,
	} {
		if !strings.Contains(profile, want) {
			t.Fatalf("profile missing %q:\n%s", want, profile)
		}
	}
}

func TestOSEnforcedPreservesCustomCommandHandlerSemantics(t *testing.T) {
	fakeSandboxExec := filepath.Join(t.TempDir(), "sandbox-exec")
	if err := os.WriteFile(fakeSandboxExec, []byte("#!/bin/sh\nprintf 'wrapped:%s\\n' \"$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: t.TempDir(), ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := New(p, WithSandboxExecPath(fakeSandboxExec))
	if s.Status().Level != ConfinementOSEnforced {
		t.Fatalf("fake sandbox-exec should select OS enforcement, got %+v", s.Status())
	}
	tool := &fakeCommandTool{output: "handler output"}

	out, err := s.Run(context.Background(), tool, map[string]interface{}{"script": "echo custom"})
	if err != nil {
		t.Fatalf("run: out=%q err=%v", out, err)
	}
	if !tool.executed {
		t.Fatalf("OS backend must preserve the custom handler's validation and result semantics")
	}
	if out != "handler output" {
		t.Fatalf("custom handler output should be preserved, got %q", out)
	}
	if !strings.Contains(tool.runnerOutput, "wrapped:-p") || !strings.Contains(tool.runnerOutput, "echo custom") {
		t.Fatalf("custom handler must launch through sandbox-exec, runner output=%q", tool.runnerOutput)
	}
}

func TestCommandToolWithoutRunnerContractFailsClosed(t *testing.T) {
	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: t.TempDir(), ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := New(p, WithForceFallback("test fallback"))
	tool := &legacyCommandTool{}

	out, err := s.Run(context.Background(), tool, map[string]interface{}{"command": "echo unsafe"})
	if err == nil || !strings.Contains(err.Error(), "CommandToolHandler") {
		t.Fatalf("legacy command tool should fail closed, out=%q err=%v", out, err)
	}
	if tool.executed {
		t.Fatal("legacy command tool must not execute outside the sandbox runner")
	}
}

func TestFallbackDeniesIdentifiedOutsideWrite(t *testing.T) {
	wd := t.TempDir()
	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: wd, ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := New(p, WithForceFallback("test fallback"))
	tool := &fakeCommandTool{}

	out, err := s.Run(context.Background(), tool, map[string]interface{}{"command": "touch /not-allowed"})
	if err == nil || !strings.Contains(out, "sandbox blocked command") {
		t.Fatalf("want sandbox block, out=%q err=%v", out, err)
	}
	if tool.executed {
		t.Fatalf("blocked command must not execute")
	}
}

func TestFallbackAllowsInProjectWrite(t *testing.T) {
	wd := t.TempDir()
	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: wd, ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := New(p, WithForceFallback("test fallback"))
	tool := &fakeCommandTool{output: "ok"}

	out, err := s.Run(context.Background(), tool, map[string]interface{}{"command": "touch " + wd + "/ok.txt"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "ok" || !tool.executed {
		t.Fatalf("tool should execute, out=%q executed=%v", out, tool.executed)
	}
}

func TestRunWithGrantsWidensFallbackPolicy(t *testing.T) {
	wd := t.TempDir()
	extra := t.TempDir()
	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: wd, ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := New(p, WithForceFallback("test fallback"))
	tool := &fakeCommandTool{output: "ok"}

	_, err = s.Run(context.Background(), tool, map[string]interface{}{"command": "touch " + extra + "/denied.txt"})
	if err == nil {
		t.Fatalf("write should be denied before grant")
	}
	tool.executed = false
	out, err := s.RunWithGrants(context.Background(), tool, map[string]interface{}{"command": "touch " + extra + "/allowed.txt"}, core.SandboxGrants{ExtraWriteRoots: []string{extra}})
	if err != nil {
		t.Fatalf("write should be allowed by per-call grant, out=%q err=%v", out, err)
	}
	if !tool.executed {
		t.Fatalf("tool should execute after grant")
	}
}

func TestRunWithGrantsFromInputsWidensFallbackPolicy(t *testing.T) {
	wd := t.TempDir()
	extra := t.TempDir()
	s, err := NewFromInputs(PolicyInputs{WorkingDirectory: wd, ScratchRoot: t.TempDir()}, WithForceFallback("test fallback"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tool := &fakeCommandTool{output: "ok"}

	out, err := s.RunWithGrants(context.Background(), tool, map[string]interface{}{"command": "touch " + extra + "/allowed.txt"}, core.SandboxGrants{ExtraWriteRoots: []string{extra}})
	if err != nil {
		t.Fatalf("write should be allowed by per-call grant, out=%q err=%v", out, err)
	}
}

func TestRunWithGrantsPreservesExistingPermissionContext(t *testing.T) {
	wd := t.TempDir()
	initialWriteRoot := t.TempDir()
	s, err := NewFromInputs(PolicyInputs{
		WorkingDirectory: wd,
		ScratchRoot:      t.TempDir(),
		Permission:       Grants{ExtraWriteRoots: []string{initialWriteRoot}},
	}, WithForceFallback("test fallback"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tool := &fakeCommandTool{output: "ok"}
	target := filepath.Join(initialWriteRoot, "still-allowed.txt")

	out, err := s.RunWithGrants(
		context.Background(),
		tool,
		map[string]interface{}{"command": "touch " + target},
		core.SandboxGrants{Network: true},
	)
	if err != nil {
		t.Fatalf("per-call grant must preserve initial permission roots, out=%q err=%v", out, err)
	}
	if !tool.executed {
		t.Fatalf("tool should execute with both initial and per-call grants")
	}
}

func TestForcedFallbackStatus(t *testing.T) {
	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: t.TempDir(), ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := New(p, WithForceFallback("because test"))
	status := s.Status()
	if status.Level != ConfinementPolicyFallback || !strings.Contains(status.Reason, "because test") {
		t.Fatalf("bad status: %+v", status)
	}
}

func TestSandboxedCommandTimeoutPreservesPartialOutput(t *testing.T) {
	fakeSandboxExec := passthroughSandboxExec(t)
	started := time.Now()

	out, err := runSandboxedCommand(
		context.Background(),
		"echo ready; kill -STOP $$",
		2*time.Second,
		sandboxExecution{SandboxExecPath: fakeSandboxExec},
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, out=%q err=%v", out, err)
	}
	if !strings.Contains(out, "ready") || !strings.Contains(out, "timed out") {
		t.Fatalf("partial output and timeout note must be preserved, got %q", out)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("timed-out process group was not stopped promptly: %s", elapsed)
	}
}

func TestSandboxedCommandCancellationPreservesPartialOutput(t *testing.T) {
	fakeSandboxExec := passthroughSandboxExec(t)
	ctx, cancel := context.WithCancel(context.Background())
	marker := filepath.Join(t.TempDir(), "command-started")
	started := time.Now()
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := runSandboxedCommand(
			ctx,
			"echo ready; touch "+strconv.Quote(marker)+"; kill -STOP $$",
			5*time.Second,
			sandboxExecution{SandboxExecPath: fakeSandboxExec},
		)
		done <- result{out: out, err: err}
	}()
	waitForPath(t, marker)
	cancel()
	var got result
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled command did not stop promptly")
	}
	out, err := got.out, got.err
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, out=%q err=%v", out, err)
	}
	if !strings.Contains(out, "ready") || !strings.Contains(out, "cancelled") {
		t.Fatalf("partial output and cancellation note must be preserved, got %q", out)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancelled process group was not stopped promptly: %s", elapsed)
	}
}

func TestSandboxedCommandCancellationKillsChildProcessGroup(t *testing.T) {
	fakeSandboxExec := passthroughSandboxExec(t)
	ctx, cancel := context.WithCancel(context.Background())
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		command := "sleep 30 & child=$!; echo $child > " + strconv.Quote(pidFile) + "; wait"
		out, err := runSandboxedCommand(
			ctx,
			command,
			10*time.Second,
			sandboxExecution{SandboxExecPath: fakeSandboxExec},
		)
		done <- result{out: out, err: err}
	}()
	waitForPath(t, pidFile)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}

	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("want cancellation error, out=%q err=%v", got.out, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled process group did not stop promptly")
	}
	waitForProcessExit(t, pid)
}

func TestSandboxedCommandBackendStartErrorIsRecoverable(t *testing.T) {
	out, err := runSandboxedCommand(
		context.Background(),
		"echo never-runs",
		time.Second,
		sandboxExecution{SandboxExecPath: filepath.Join(t.TempDir(), "missing-sandbox-exec")},
	)
	if err == nil || !strings.Contains(err.Error(), "start command") {
		t.Fatalf("want readable backend start error, out=%q err=%v", out, err)
	}
	if !strings.Contains(out, "error starting command") {
		t.Fatalf("tool-visible output should explain the start failure, got %q", out)
	}
}

func TestCommandEnvironmentUsesScratchForTemporaryWrites(t *testing.T) {
	scratch := t.TempDir()
	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: t.TempDir(), ScratchRoot: scratch})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	environment, err := commandEnvironment(p)
	if err != nil {
		t.Fatalf("environment: %v", err)
	}
	values := make(map[string]string)
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if values["TMPDIR"] != p.ScratchRoot || values["GOTMPDIR"] != p.ScratchRoot {
		t.Fatalf("temporary roots must use scratch, got TMPDIR=%q GOTMPDIR=%q", values["TMPDIR"], values["GOTMPDIR"])
	}
	if !p.CanWrite(values["GOCACHE"]) {
		t.Fatalf("Go cache must be redirected to an allowed write root, got %q", values["GOCACHE"])
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived sandbox cancellation", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func passthroughSandboxExec(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sandbox-exec")
	script := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = sh ]; then\n    exec \"$@\"\n  fi\n  shift\ndone\nexit 64\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type fakeCommandTool struct {
	output       string
	runnerOutput string
	executed     bool
}

func (f *fakeCommandTool) Descriptor() core.Tool {
	return core.Tool{Name: "run_command", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (f *fakeCommandTool) Execute(context.Context, map[string]interface{}) (string, error) {
	f.executed = true
	return f.output, nil
}

func (f *fakeCommandTool) ExecuteCommand(ctx context.Context, args map[string]interface{}, runner core.CommandRunner) (string, error) {
	command, _ := args["command"].(string)
	if command == "" {
		command, _ = args["script"].(string)
	}
	out, err := runner.Run(ctx, core.CommandSpec{Command: command})
	f.runnerOutput = out
	if err != nil {
		return out, err
	}
	f.executed = true
	if f.output != "" {
		return f.output, nil
	}
	return out, nil
}

func (f *fakeCommandTool) RunsCommands() bool { return true }

type legacyCommandTool struct {
	executed bool
}

func (l *legacyCommandTool) Descriptor() core.Tool {
	return core.Tool{Name: "legacy_command", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (l *legacyCommandTool) Execute(context.Context, map[string]interface{}) (string, error) {
	l.executed = true
	return "unsafe", nil
}

func (l *legacyCommandTool) RunsCommands() bool { return true }
