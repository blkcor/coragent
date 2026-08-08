//go:build windows

package windows

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/sandbox"
	"golang.org/x/sys/windows"
)

func TestStartCmdEcho(t *testing.T) {
	ctx := context.Background()
	s := New(NewPTYManager())

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "echo", "hello", "world"},
		Env:            os.Environ(),
		Timeout:        10 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d (stderr: %q)", result.ExitCode, string(result.Stderr))
	}
	if !strings.Contains(string(result.Stdout), "hello world") {
		t.Errorf("expected 'hello world' in stdout, got %q", string(result.Stdout))
	}
	if result.Signaled {
		t.Error("process should not be signaled")
	}
}

func TestStartPowerShell(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "powershell",
		Args:           []string{"-Command", "Write-Output", "ps-test"},
		Env:            os.Environ(),
		Timeout:        10 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d (stdout: %q)", result.ExitCode, string(result.Stdout))
	}
	if !strings.Contains(string(result.Stdout), "ps-test") {
		t.Errorf("expected 'ps-test' in stdout, got %q", string(result.Stdout))
	}
}

func TestConfinementLevel(t *testing.T) {
	s := New(NewPTYManager())
	if s.ConfinementLevel() != sandbox.ConfinementProcess {
		t.Errorf("expected ConfinementProcess, got %s", s.ConfinementLevel())
	}
}

func TestTimeoutTerminatesJobObject(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	// Start a process tree: cmd starts two child processes.
	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "start /wait timeout 60 & timeout 60"},
		Env:            os.Environ(),
		Timeout:        500 * time.Millisecond,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if !result.Signaled {
		t.Errorf("process should be signaled on timeout, got exit=%d err=%v", result.ExitCode, result.Error)
	}
	if result.ExitCode == 0 {
		t.Error("process should not exit cleanly on timeout")
	}
}

func TestContextCancelKillsJobObject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "powershell",
		Args:           []string{"-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 60"},
		Env:            os.Environ(),
		Timeout:        30 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	cancel()

	<-proc.Done()
	result := proc.Result()

	if !result.Signaled {
		t.Errorf("process should be signaled on context cancel, got exit=%d err=%v", result.ExitCode, result.Error)
	}
}

func TestWindowsEscapeArg(t *testing.T) {
	for _, value := range []string{"", "plain", "two words", `embedded"quote`, `C:\\path with space\\`} {
		if got, want := windowsEscapeArg(value), windows.EscapeArg(value); got != want {
			t.Errorf("windowsEscapeArg(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestMaxOutputBounding(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	maxBytes := int64(256)
	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "powershell",
		Args:           []string{"-Command", "'A' * 10000"},
		Env:            os.Environ(),
		Timeout:        10 * time.Second,
		MaxOutputBytes: maxBytes,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if int64(len(result.Stdout)) > maxBytes+1024 {
		t.Errorf("output should be bounded near %d, got %d", maxBytes, len(result.Stdout))
	}
}

func TestNonexistentCommand(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	_, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "nonexistent_command_xyz_123",
		Args:           nil,
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}

func TestEmptyCommand(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	_, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "",
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestZeroTimeout(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	_, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "echo", "hi"},
		Timeout:        0,
		MaxOutputBytes: 64 * 1024,
	})
	if err == nil {
		t.Fatal("expected error for zero timeout")
	}
}

func TestPID(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "timeout 1"},
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	pid := proc.PID()
	if pid <= 0 {
		t.Errorf("expected positive PID, got %d", pid)
	}

	<-proc.Done()
	result := proc.Result()
	if result.PID != pid {
		t.Errorf("Result PID %d != live PID %d", result.PID, pid)
	}
}

func TestConPTYBasic(t *testing.T) {
	if !supportsConPTY() {
		t.Skip("ConPTY requires Windows >= 1809 (build 17763)")
	}
	ctx := context.Background()
	s := New(NewPTYManager())

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "echo", "conpty-output"},
		Env:            []string{"SystemRoot=" + os.Getenv("SystemRoot")},
		Timeout:        10 * time.Second,
		MaxOutputBytes: 64 * 1024,
		PTY:            true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Stdout), "conpty-output") {
		t.Errorf("expected 'conpty-output' in stdout, got %q", string(result.Stdout))
	}
}

func TestConPTYResize(t *testing.T) {
	if !supportsConPTY() {
		t.Skip("ConPTY requires Windows >= 1809 (build 17763)")
	}
	ctx := context.Background()
	s := New(NewPTYManager())

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "timeout 2"},
		Env:            []string{"SystemRoot=" + os.Getenv("SystemRoot")},
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
		PTY:            true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := proc.ResizePTY(120, 40); err != nil {
		t.Errorf("ResizePTY: %v", err)
	}

	<-proc.Done()
}

func TestPipeFallback(t *testing.T) {
	// Force the fallback path so a current Windows CI runner can exercise it
	// without requiring an end-of-life pre-1809 virtual machine.
	ctx := context.Background()
	s := New(&ptyManager{useConPTY: false})

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "echo", "pipe-fallback"},
		Env:            []string{"SystemRoot=" + os.Getenv("SystemRoot")},
		Timeout:        10 * time.Second,
		MaxOutputBytes: 64 * 1024,
		PTY:            true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Stdout), "pipe-fallback") {
		t.Errorf("expected 'pipe-fallback' in stdout, got %q", string(result.Stdout))
	}

	// Resize should be a no-op on pipe path.
	if err := proc.ResizePTY(80, 24); err != nil {
		t.Errorf("ResizePTY should be no-op on pipe fallback: %v", err)
	}
}

func TestVersionDetection(t *testing.T) {
	build, err := buildNumber()
	if err != nil {
		t.Fatalf("buildNumber: %v", err)
	}
	if build == 0 {
		t.Fatal("build number should be non-zero")
	}
	t.Logf("Windows build: %d, ConPTY: %v", build, supportsConPTY())

	// buildNumber should be cached — second call returns same value.
	build2, err2 := buildNumber()
	if err2 != nil {
		t.Fatalf("second buildNumber: %v", err2)
	}
	if build2 != build {
		t.Errorf("cached build %d != first build %d", build2, build)
	}
}

func TestConPTYTimeout(t *testing.T) {
	if !supportsConPTY() {
		t.Skip("ConPTY requires Windows >= 1809 (build 17763)")
	}
	ctx := context.Background()
	s := New(NewPTYManager())

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "timeout 60"},
		Env:            []string{"SystemRoot=" + os.Getenv("SystemRoot")},
		Timeout:        500 * time.Millisecond,
		MaxOutputBytes: 64 * 1024,
		PTY:            true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if !result.Signaled {
		t.Errorf("ConPTY process should be signaled on timeout, got exit=%d", result.ExitCode)
	}
}

func TestAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New(nil)
	_, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "echo", "hi"},
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err == nil {
		t.Fatal("expected error for already cancelled context")
	}
}

func TestResizePTYWithoutPTY(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "timeout 1"},
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
		PTY:            false,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := proc.ResizePTY(80, 24); err != nil {
		t.Errorf("ResizePTY should be no-op without PTY: %v", err)
	}

	<-proc.Done()
}

func TestNilPTYManager(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "cmd",
		Args:           []string{"/c", "echo", "nil-pty"},
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
		PTY:            false,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
}

// Cross-compilation guard: ensure the package compiles for windows/amd64.
func TestCrossCompilationGuard(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("test only runs on Windows")
	}
}
