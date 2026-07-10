package tools

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/sandbox"
)

func TestShellCommandDarwinSandboxBlocksOutsideWrite(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec enforcement is macOS-only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	wd := t.TempDir()
	scratch := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	p, err := sandbox.DerivePolicy(sandbox.PolicyInputs{WorkingDirectory: wd, ScratchRoot: scratch})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := sandbox.New(p)
	if s.Status().Level != sandbox.ConfinementOSEnforced {
		t.Skipf("OS sandbox unavailable: %+v", s.Status())
	}

	out, err := s.Run(context.Background(), ShellCommand{}, map[string]interface{}{"command": "touch " + outside})
	if err == nil {
		t.Fatalf("outside write should fail, out=%q", out)
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("outside file should not be created, stat=%v", statErr)
	}
}

func TestShellCommandDarwinSandboxAllowsProjectWrite(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec enforcement is macOS-only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	wd := t.TempDir()
	target := filepath.Join(wd, "ok.txt")
	p, err := sandbox.DerivePolicy(sandbox.PolicyInputs{WorkingDirectory: wd, ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := sandbox.New(p)
	if s.Status().Level != sandbox.ConfinementOSEnforced {
		t.Skipf("OS sandbox unavailable: %+v", s.Status())
	}

	out, err := s.Run(context.Background(), ShellCommand{}, map[string]interface{}{"command": "touch " + target})
	if err != nil {
		t.Fatalf("project write should succeed, out=%q err=%v", out, err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("project file should exist: %v", statErr)
	}
}

func TestShellCommandDarwinSandboxBlocksOutsideRead(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec enforcement is macOS-only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	wd := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("classified"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := sandbox.DerivePolicy(sandbox.PolicyInputs{WorkingDirectory: wd, ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := sandbox.New(p)
	if s.Status().Level != sandbox.ConfinementOSEnforced {
		t.Skipf("OS sandbox unavailable: %+v", s.Status())
	}

	out, err := s.Run(context.Background(), ShellCommand{}, map[string]interface{}{"command": "cat " + outside})
	if err == nil {
		t.Fatalf("outside read should fail, out=%q", out)
	}
	if strings.Contains(out, "classified") {
		t.Fatalf("outside file content should not be returned, got %q", out)
	}
	if !strings.Contains(out, "Operation not permitted") && !strings.Contains(out, "exit code") {
		t.Fatalf("expected captured read failure, got %q", out)
	}
}

func TestShellCommandDarwinSandboxDeniesNetworkByDefault(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec enforcement is macOS-only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("nc unavailable")
	}
	ln := listenLocal(t)
	defer ln.Close()
	target := netcatTarget(t, ln)
	p, err := sandbox.DerivePolicy(sandbox.PolicyInputs{WorkingDirectory: t.TempDir(), ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := sandbox.New(p)
	if s.Status().Level != sandbox.ConfinementOSEnforced {
		t.Skipf("OS sandbox unavailable: %+v", s.Status())
	}

	out, err := s.Run(context.Background(), ShellCommand{}, map[string]interface{}{
		"command":    "nc -G 1 -z " + target,
		"timeout_ms": 2000,
	})
	if err == nil {
		t.Fatalf("network should be denied, out=%q", out)
	}
	if !strings.Contains(out, "exit code") && !strings.Contains(out, "Operation not permitted") {
		t.Fatalf("expected captured network failure, got %q", out)
	}
}

func TestShellCommandDarwinSandboxAllowsNetworkWithGrant(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec enforcement is macOS-only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("nc unavailable")
	}
	ln := listenLocal(t)
	defer ln.Close()
	target := netcatTarget(t, ln)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	p, err := sandbox.DerivePolicy(sandbox.PolicyInputs{
		WorkingDirectory: t.TempDir(),
		ScratchRoot:      t.TempDir(),
		Settings:         sandbox.Grants{Network: true},
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := sandbox.New(p)
	if s.Status().Level != sandbox.ConfinementOSEnforced {
		t.Skipf("OS sandbox unavailable: %+v", s.Status())
	}

	out, err := s.Run(context.Background(), ShellCommand{}, map[string]interface{}{
		"command":    "nc -G 1 -z " + target,
		"timeout_ms": 2000,
	})
	if err != nil {
		t.Fatalf("network grant should allow localhost connection, out=%q err=%v", out, err)
	}
}

func TestShellCommandDarwinSandboxRunsGoTestWithDefaults(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec enforcement is macOS-only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go unavailable")
	}
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "go.mod"), []byte("module example.com/sandboxprobe\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testSource := "package sandboxprobe\n\nimport \"testing\"\n\nfunc TestProbe(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(wd, "probe_test.go"), []byte(testSource), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := sandbox.DerivePolicy(sandbox.PolicyInputs{WorkingDirectory: wd, ScratchRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	s := sandbox.New(p)
	if s.Status().Level != sandbox.ConfinementOSEnforced {
		t.Skipf("OS sandbox unavailable: %+v", s.Status())
	}

	out, err := s.Run(context.Background(), ShellCommand{}, map[string]interface{}{
		"command": "cd " + strconv.Quote(wd) + " && go test ./...",
	})
	if err != nil {
		t.Fatalf("ordinary Go test should run with the default policy, out=%q err=%v", out, err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("Go test output should report success, got %q", out)
	}
}

func listenLocal(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listener unavailable: %v", err)
	}
	return ln
}

func netcatTarget(t *testing.T, ln net.Listener) string {
	t.Helper()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil || host == "" || port == "" {
		t.Fatalf("bad listener address: %s", ln.Addr())
	}
	return host + " " + port
}
