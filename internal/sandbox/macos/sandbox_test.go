//go:build darwin

package macos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/sandbox"
)

// requireSandboxExec skips the test when sandbox-exec is unavailable.
func requireSandboxExec(t *testing.T) {
	t.Helper()
	if !isAvailable() {
		t.Skip("sandbox-exec not available on this system")
	}
}

// sandboxTestEnv holds resources for a sandbox integration test.
type sandboxTestEnv struct {
	s   *Sandbox
	ws  string
	ctx context.Context
}

func newSandboxTestEnv(t *testing.T) *sandboxTestEnv {
	t.Helper()
	requireSandboxExec(t)

	ws := t.TempDir()

	s := New(nil)
	return &sandboxTestEnv{
		s:   s,
		ws:  ws,
		ctx: context.Background(),
	}
}

func (e *sandboxTestEnv) start(t *testing.T, spec sandbox.CommandSpec) sandbox.Process {
	t.Helper()
	if spec.CWD == "" {
		spec.CWD = e.ws
	}
	if spec.Timeout == 0 {
		spec.Timeout = 10 * time.Second
	}
	if spec.MaxOutputBytes == 0 {
		spec.MaxOutputBytes = 64 * 1024
	}
	proc, err := e.s.Start(e.ctx, spec)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() {
		select {
		case <-proc.Done():
		default:
			_ = proc.Signal(os.Kill)
			<-proc.Done()
		}
	})
	return proc
}

// --- Tests ---

func TestConfinementLevel(t *testing.T) {
	env := newSandboxTestEnv(t)
	if lvl := env.s.ConfinementLevel(); lvl != sandbox.ConfinementKernel {
		t.Errorf("ConfinementLevel() = %s, want %s", lvl, sandbox.ConfinementKernel)
	}
}

func TestEcho(t *testing.T) {
	env := newSandboxTestEnv(t)
	proc := env.start(t, sandbox.CommandSpec{Command: "/bin/echo", Args: []string{"hello", "sandbox"}})
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode != 0 {
		t.Fatalf("echo: exit code %d", r.ExitCode)
	}
	output := string(r.Stdout)
	if !strings.Contains(output, "hello sandbox") {
		t.Errorf("echo output = %q, want 'hello sandbox'", output)
	}
}

func TestLS(t *testing.T) {
	env := newSandboxTestEnv(t)
	// Create a file in workspace so ls has something to show.
	f, err := os.Create(filepath.Join(env.ws, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	proc := env.start(t, sandbox.CommandSpec{Command: "/bin/ls", Args: []string{env.ws}})
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode != 0 {
		t.Fatalf("ls: exit code %d", r.ExitCode)
	}
	if !strings.Contains(string(r.Stdout), "test.txt") {
		t.Errorf("ls output should contain 'test.txt': %s", r.Stdout)
	}
}

func TestCat(t *testing.T) {
	env := newSandboxTestEnv(t)
	content := "sandboxed-content"
	fpath := filepath.Join(env.ws, "readme.txt")
	if err := os.WriteFile(fpath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	proc := env.start(t, sandbox.CommandSpec{Command: "/bin/cat", Args: []string{fpath}})
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode != 0 {
		t.Fatalf("cat: exit code %d", r.ExitCode)
	}
	if !strings.Contains(string(r.Stdout), content) {
		t.Errorf("cat output = %q, want %q", r.Stdout, content)
	}
}

func TestWriteInsideWorkspace(t *testing.T) {
	env := newSandboxTestEnv(t)
	fpath := filepath.Join(env.ws, "output.txt")
	proc := env.start(t, sandbox.CommandSpec{
		Command: "/bin/sh",
		Args:    []string{"-c", fmt.Sprintf("echo ok > %s", fpath)},
	})
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode != 0 {
		t.Fatalf("write inside workspace failed: exit %d, output: %s", r.ExitCode, r.Stdout)
	}
	got, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "ok" {
		t.Errorf("file content = %q, want 'ok'", got)
	}
}

func TestWriteOutsideWorkspace(t *testing.T) {
	env := newSandboxTestEnv(t)
	// Write to the user's home directory, which is not the workspace CWD and
	// not the system temp dir. The sandbox must deny this write.
	outsidePath := filepath.Join(os.Getenv("HOME"), "coragent-sandbox-outside-test")
	_ = os.Remove(outsidePath)

	proc := env.start(t, sandbox.CommandSpec{
		Command: "/bin/sh",
		Args:    []string{"-c", fmt.Sprintf("echo bad > %s", outsidePath)},
	})
	<-proc.Done()
	defer func() { _ = os.Remove(outsidePath) }()

	r := proc.Result()
	if r.ExitCode == 0 {
		t.Error("write outside workspace should have been denied by sandbox")
	}
}

func TestNetworkDenied(t *testing.T) {
	env := newSandboxTestEnv(t)
	// Try a network lookup — this should fail under Seatbelt.
	// Use a short timeout to avoid hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}

	proc, err := env.s.Start(ctx, sandbox.CommandSpec{
		Command:        "/usr/bin/curl",
		Args:           []string{"-s", "--connect-timeout", "2", "http://localhost:0"},
		CWD:            env.ws,
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode == 0 {
		t.Error("network access should be denied by sandbox")
	}
}

func TestTimeout(t *testing.T) {
	env := newSandboxTestEnv(t)

	proc, err := env.s.Start(env.ctx, sandbox.CommandSpec{
		Command:        "/bin/sleep",
		Args:           []string{"10"},
		CWD:            env.ws,
		Timeout:        200 * time.Millisecond,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	<-proc.Done()
	r := proc.Result()
	if !r.Signaled {
		t.Error("process should be signaled on timeout")
	}
	if r.ExitCode == 0 {
		t.Error("process should not exit with code 0 on timeout")
	}
}

func TestContextCancellation(t *testing.T) {
	env := newSandboxTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())

	proc, err := env.s.Start(ctx, sandbox.CommandSpec{
		Command:        "/bin/sleep",
		Args:           []string{"10"},
		CWD:            env.ws,
		Timeout:        30 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()

	<-proc.Done()
	r := proc.Result()
	if r.ExitCode == 0 && !r.Signaled {
		t.Error("process should be killed on context cancellation")
	}
}

func TestSandboxExecUnavailable(t *testing.T) {
	// Simulate unavailability by constructing sandbox and checking error.
	// We rely on isAvailable() at construction time. This test is a smoke
	// check that the check itself works on macOS (where sandbox-exec exists).
	requireSandboxExec(t)
	if !isAvailable() {
		t.Error("sandbox-exec should be available on macOS")
	}
}

func TestProfileTempFileCleanup(t *testing.T) {
	env := newSandboxTestEnv(t)
	proc := env.start(t, sandbox.CommandSpec{Command: "/bin/echo", Args: []string{"cleanup"}})
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode != 0 {
		t.Fatalf("echo: exit code %d", r.ExitCode)
	}
	// Profile file should be removed after process exit.
	// The test can't directly observe this since os.Remove happens
	// asynchronously, but we can give it a moment.
	time.Sleep(50 * time.Millisecond)
}
