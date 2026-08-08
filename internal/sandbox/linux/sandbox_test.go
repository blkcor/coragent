//go:build linux

package linux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/sandbox/nop"
	"golang.org/x/sys/unix"
)

// TestMain checks the sandbox-init marker before running any test: the
// Landlock+seccomp backend re-execs /proc/self/exe (this test binary) as an
// init wrapper, exactly like cmd/coragent/main.go does.
func TestMain(m *testing.M) {
	if HandleInit(os.Args[1:]) {
		return
	}
	os.Exit(m.Run())
}

// requireLinuxSandbox skips the test when Landlock or seccomp is unavailable.
func requireLinuxSandbox(t *testing.T) {
	t.Helper()
	if !isAvailable() {
		t.Skip("Landlock and/or seccomp not available on this kernel")
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
	requireLinuxSandbox(t)

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
	if !strings.Contains(string(r.Stdout), "hello sandbox") {
		t.Errorf("echo output = %q, want 'hello sandbox'", r.Stdout)
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
	// Write to a location that is neither the workspace CWD nor a
	// Landlock-granted path (tmp). The user's HOME is a safe choice —
	// it's outside tmp and outside the test workspace.
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set")
	}
	outsidePath := filepath.Join(home, "coragent-landlock-outside-test")
	_ = os.Remove(outsidePath)

	proc := env.start(t, sandbox.CommandSpec{
		Command: "/bin/sh",
		Args:    []string{"-c", fmt.Sprintf("echo bad > %s", outsidePath)},
		CWD:     env.ws,
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
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
		t.Error("network access should be denied by seccomp filter")
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

func TestLandlockSeccompAvailable(t *testing.T) {
	requireLinuxSandbox(t)
	if !isAvailable() {
		t.Error("Landlock and seccomp should be available when kernel supports both")
	}
	if !landlockAvailable() {
		t.Error("landlockAvailable() should return true")
	}
	if !seccompAvailable() {
		t.Error("seccompAvailable() should return true")
	}
}

func TestProfileCleanup(t *testing.T) {
	env := newSandboxTestEnv(t)
	proc := env.start(t, sandbox.CommandSpec{Command: "/bin/echo", Args: []string{"cleanup"}})
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode != 0 {
		t.Fatalf("echo: exit code %d", r.ExitCode)
	}
}

func TestPTYBasic(t *testing.T) {
	requireLinuxSandbox(t)
	ws := t.TempDir()
	s := New(nop.NewPTYManager())
	proc, err := s.Start(context.Background(), sandbox.CommandSpec{
		Command:        "/bin/echo",
		Args:           []string{"pty-output"},
		CWD:            ws,
		Env:            []string{"PATH=/usr/bin:/bin"},
		Timeout:        10 * time.Second,
		MaxOutputBytes: 64 * 1024,
		PTY:            true,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	<-proc.Done()
	r := proc.Result()
	if r.ExitCode != 0 {
		t.Fatalf("echo under PTY: exit code %d, output: %s", r.ExitCode, r.Stdout)
	}
	if !strings.Contains(string(r.Stdout), "pty-output") {
		t.Errorf("expected 'pty-output' in output, got %q", string(r.Stdout))
	}
}

func TestLandlockRulesetBuilder(t *testing.T) {
	ws := t.TempDir()
	grantDir := t.TempDir()
	spec := sandbox.CommandSpec{
		CWD: ws,
		Grants: sandbox.Grants{
			AllowedWritePaths: []string{grantDir},
		},
	}
	rulesetFD, err := buildLandlockRuleset(spec)
	if err != nil {
		t.Fatalf("buildLandlockRuleset failed: %v", err)
	}
	defer func() { _ = syscall.Close(rulesetFD) }()
	if rulesetFD < 0 {
		t.Error("expected valid ruleset FD")
	}
}

func TestSeccompFilterBuilder(t *testing.T) {
	filter, err := buildSeccompFilter(false)
	if err != nil {
		t.Fatalf("buildSeccompFilter failed: %v", err)
	}
	if len(filter) == 0 {
		t.Error("expected non-empty seccomp filter")
	}
}

func TestSeccompFilterBuilder_NetworkEnabled(t *testing.T) {
	filterNoNet, err := buildSeccompFilter(false)
	if err != nil {
		t.Fatalf("buildSeccompFilter(false) failed: %v", err)
	}
	filterNet, err := buildSeccompFilter(true)
	if err != nil {
		t.Fatalf("buildSeccompFilter(true) failed: %v", err)
	}
	if len(filterNet) <= len(filterNoNet) {
		t.Error("network-enabled filter should contain more instructions than network-denied filter")
	}
}

func TestEncodeDecodeSeccompFilter(t *testing.T) {
	filter, err := buildSeccompFilter(false)
	if err != nil {
		t.Fatalf("buildSeccompFilter failed: %v", err)
	}
	encoded := encodeSeccompFilter(filter)
	decoded, err := decodeSeccompFilter(encoded)
	if err != nil {
		t.Fatalf("decodeSeccompFilter failed: %v", err)
	}
	if len(decoded) != len(filter) {
		t.Errorf("decoded filter length = %d, want %d", len(decoded), len(filter))
	}
	for i := range filter {
		if decoded[i] != filter[i] {
			t.Errorf("filter[%d] = %+v, want %+v", i, decoded[i], filter[i])
		}
	}
}

// runClassicBPF interprets a seccomp BPF filter over seccomp_data{nr, arch}
// following kernel semantics: JEQ uses jt/jf relative jumps, JA jumps by K,
// RET K returns the action. It guards the filter builder against jump
// encoding regressions without needing to install the filter.
func runClassicBPF(t *testing.T, insns []unix.SockFilter, nr, arch uint32) uint32 {
	t.Helper()
	var a uint32
	pc := 0
	for steps := 0; steps < 10000; steps++ {
		if pc < 0 || pc >= len(insns) {
			t.Fatalf("program counter out of range: %d", pc)
		}
		in := insns[pc]
		switch in.Code {
		case bpfLdWAbs:
			if in.K == seccompDataArchOffset {
				a = arch
			} else {
				a = nr
			}
			pc++
		case bpfJeqK:
			if a == in.K {
				pc += int(in.Jt) + 1
			} else {
				pc += int(in.Jf) + 1
			}
		case bpfJa:
			if in.Jt != 0 || in.Jf != 0 {
				t.Fatalf("JA at pc=%d has jt=%d jf=%d, kernel requires jt=jf=0", pc, in.Jt, in.Jf)
			}
			pc += int(in.K) + 1
		case bpfRetK:
			return in.K
		default:
			t.Fatalf("unknown opcode %#x at pc=%d", in.Code, pc)
		}
	}
	t.Fatal("filter did not terminate")
	return 0
}

func TestSeccompFilterSemantics(t *testing.T) {
	var auditArch uint32
	var nrRead, nrWrite, nrSocket, nrMount uint32
	switch runtime.GOARCH {
	case "amd64":
		auditArch = auditArchAMD64
		nrRead, nrWrite, nrSocket, nrMount = 0, 1, 41, 165
	case "arm64":
		auditArch = auditArchARM64
		nrRead, nrWrite, nrSocket, nrMount = 63, 64, 198, 40
	default:
		t.Skip("unsupported architecture")
	}

	filter, err := buildSeccompFilter(false)
	if err != nil {
		t.Fatalf("buildSeccompFilter failed: %v", err)
	}

	cases := []struct {
		name string
		nr   uint32
		want uint32
	}{
		{"read allowed", nrRead, seccompRetAllow},
		{"write allowed", nrWrite, seccompRetAllow},
		{"socket denied", nrSocket, seccompRetKillProcess},
		{"mount denied", nrMount, seccompRetKillProcess},
	}
	for _, c := range cases {
		if got := runClassicBPF(t, filter, c.nr, auditArch); got != c.want {
			t.Errorf("%s: got %#08x, want %#08x", c.name, got, c.want)
		}
	}

	// Wrong-arch seccomp_data must be killed.
	if got := runClassicBPF(t, filter, nrRead, auditArch^0xFF); got != seccompRetKillProcess {
		t.Errorf("foreign arch: got %#08x, want KILL_PROCESS", got)
	}
}

func TestSeccompFilterSemantics_NetworkEnabled(t *testing.T) {
	var auditArch, nrSocket uint32
	switch runtime.GOARCH {
	case "amd64":
		auditArch, nrSocket = auditArchAMD64, 41
	case "arm64":
		auditArch, nrSocket = auditArchARM64, 198
	default:
		t.Skip("unsupported architecture")
	}

	filter, err := buildSeccompFilter(true)
	if err != nil {
		t.Fatalf("buildSeccompFilter(true) failed: %v", err)
	}
	if got := runClassicBPF(t, filter, nrSocket, auditArch); got != seccompRetAllow {
		t.Errorf("socket with network grant: got %#08x, want ALLOW", got)
	}
}
