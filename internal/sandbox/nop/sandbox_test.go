package nop

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/sandbox"
)

func TestStartEcho(t *testing.T) {
	ctx := context.Background()
	s := New(NewPTYManager())

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "echo",
		Args:           []string{"hello", "world"},
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Stdout), "hello world") {
		t.Errorf("expected 'hello world' in stdout, got %q", string(result.Stdout))
	}
	if result.Signaled {
		t.Error("process should not be signaled")
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestStartEchoPipe(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "echo",
		Args:           []string{"pipe-test"},
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Stdout), "pipe-test") {
		t.Errorf("expected 'pipe-test' in stdout, got %q", string(result.Stdout))
	}
}

func TestConfinementLevel(t *testing.T) {
	s := New(NewPTYManager())
	if s.ConfinementLevel() != sandbox.ConfinementProcess {
		t.Errorf("expected ConfinementProcess, got %s", s.ConfinementLevel())
	}
}

func TestTimeoutKillsProcess(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "sleep",
		Args:           []string{"60"},
		Env:            os.Environ(),
		Timeout:        100 * time.Millisecond,
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

func TestContextCancelKillsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "sleep",
		Args:           []string{"60"},
		Env:            os.Environ(),
		Timeout:        30 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	<-proc.Done()
	result := proc.Result()

	if !result.Signaled {
		t.Errorf("process should be signaled on context cancel, got exit=%d err=%v", result.ExitCode, result.Error)
	}
}

func TestProcessGroupCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group cleanup uses Unix signals")
	}
	ctx := context.Background()
	s := New(nil)

	script := `sleep 60 & sleep 60`
	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "sh",
		Args:           []string{"-c", script},
		Env:            os.Environ(),
		Timeout:        200 * time.Millisecond,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if !result.Signaled {
		t.Errorf("process group should be signaled on timeout, got exit=%d err=%v", result.ExitCode, result.Error)
	}

	out, _ := exec.Command("pgrep", "-f", "sleep 60").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Logf("possible orphan processes: %s", strings.TrimSpace(string(out)))
	}
}

func TestMaxOutputBounding(t *testing.T) {
	ctx := context.Background()
	s := New(nil)

	maxBytes := int64(128)
	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "sh",
		Args:           []string{"-c", "yes 2>/dev/null | head -c 10240"},
		Env:            os.Environ(),
		Timeout:        5 * time.Second,
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
		Command:        "nonexistent_command_xyz",
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
		Command:        "echo",
		Args:           []string{"hi"},
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
		Command:        "sleep",
		Args:           []string{"1"},
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

func TestPTYBasic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests use Unix PTY")
	}
	ctx := context.Background()
	s := New(NewPTYManager())

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "echo",
		Args:           []string{"pty-output"},
		Env:            []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")},
		Timeout:        5 * time.Second,
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
	if !strings.Contains(string(result.Stdout), "pty-output") {
		t.Errorf("expected 'pty-output' in stdout, got %q", string(result.Stdout))
	}
}

func TestPTYResizeNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests use Unix PTY")
	}
	ctx := context.Background()
	s := New(NewPTYManager())

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "sleep",
		Args:           []string{"1"},
		Env:            []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")},
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
		PTY:            true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := proc.ResizePTY(80, 24); err != nil {
		t.Errorf("ResizePTY: %v", err)
	}

	<-proc.Done()
}

func TestSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal tests use Unix signals")
	}
	ctx := context.Background()
	s := New(nil)

	proc, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "sleep",
		Args:           []string{"60"},
		Env:            os.Environ(),
		Timeout:        30 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		// On some systems the process may already be finished.
		t.Logf("Signal: %v", err)
	}

	<-proc.Done()
	result := proc.Result()

	if !result.Signaled {
		t.Log("Signal sent directly; Signaled flag may not be set by user signals")
	}
	if result.ExitCode == 0 {
		t.Error("process should not exit cleanly after SIGKILL")
	}
}

func TestAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New(nil)
	_, err := s.Start(ctx, sandbox.CommandSpec{
		Command:        "echo",
		Args:           []string{"hi"},
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
		Command:        "sleep",
		Args:           []string{"1"},
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
