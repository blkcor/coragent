//go:build darwin || linux || freebsd || openbsd || netbsd

package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/creack/pty"
)

const ptyHelperEnvironment = "CORAGENT_PTY_HELPER"

func TestPTYCriticalInteractionAndTerminalRestoration(t *testing.T) {
	child := startPTYHelper(t, "normal", 80, 24)
	defer child.close()
	child.waitFor(t, "coragent", 3*time.Second)

	if err := pty.Setsize(child.terminal, &pty.Winsize{Cols: 100, Rows: 30}); err != nil {
		t.Fatalf("resize PTY: %v", err)
	}
	child.write(t, "\x1b[?1u") // Kitty keyboard capability response.
	child.waitFor(t, "Shift+Enter newline", 2*time.Second)
	// Edit around the real caret, insert the guaranteed Ctrl+J newline, and
	// bracket-paste another line without submitting it.
	child.write(t, "ac\x1b[Db\x0atask\x1b[200~\npasted\x1b[201~\r")
	child.waitFor(t, "Permission required", 4*time.Second)

	// Background history keys and wheel input are routed to the modal, then the
	// argument editor submits a revision without approving it.
	child.write(t, "\x1b[5~\x1b[<64;10;10M")
	child.write(t, "e")
	child.waitFor(t, "Edit arguments", 2*time.Second)
	child.write(t, "\x13") // Ctrl+S
	child.waitFor(t, "preview r2", 3*time.Second)
	child.write(t, "a")
	child.waitFor(t, "succeeded", 3*time.Second)

	// Help exposes the pointer-only history and terminal-native copy fallback.
	child.write(t, "\x1f") // Ctrl+/
	child.waitFor(t, "Keyboard help", 2*time.Second)
	child.waitFor(t, "Shift/Option+drag", 2*time.Second)
	child.write(t, "\x1b")

	// Unmodified drag selects within the current pane and emits OSC 52. The
	// model-supplied hostile OSC 52 payload uses a known marker and must not
	// survive sanitization.
	child.write(t, "\x1b[<0;2;2M\x1b[<32;12;2M\x1b[<0;12;2m")
	child.waitFor(t, "\x1b]52;", 2*time.Second)
	if strings.Contains(child.output(), "c2VjcmV0") {
		t.Fatal("hostile model OSC 52 payload reached the PTY")
	}

	child.write(t, "\x1b[Z") // Shift+Tab
	child.waitFor(t, "AUTO EDIT", 2*time.Second)
	child.write(t, "cancel me\r")
	child.waitFor(t, "assistant output", 2*time.Second)
	child.write(t, "\x03") // Ctrl+C
	child.waitFor(t, "assistant output cancelled", 3*time.Second)
	child.write(t, "\x11") // Ctrl+Q

	if err := child.wait(4 * time.Second); err != nil {
		t.Fatalf("normal PTY helper exit: %v\n%s", err, child.output())
	}
	assertTerminalRestored(t, child.output())
}

func TestPTYForcedShutdownAndPanicRestoreTerminal(t *testing.T) {
	t.Run("forced shutdown", func(t *testing.T) {
		child := startPTYHelper(t, "stuck", 80, 24)
		defer child.close()
		child.waitFor(t, "coragent", 3*time.Second)
		child.write(t, "never settles\r")
		child.waitFor(t, "thinking", 2*time.Second)
		started := time.Now()
		child.write(t, "\x11")
		if err := child.wait(6 * time.Second); err != nil {
			t.Fatalf("forced helper did not exit: %v\n%s", err, child.output())
		}
		elapsed := time.Since(started)
		if elapsed < 3500*time.Millisecond || elapsed > 5500*time.Millisecond {
			t.Fatalf("forced shutdown duration = %v, want bounded drain + close near four seconds", elapsed)
		}
		assertTerminalRestored(t, child.output())
	})

	t.Run("panic", func(t *testing.T) {
		child := startPTYHelper(t, "panic", 80, 24)
		defer child.close()
		child.waitFor(t, "coragent", 3*time.Second)
		child.write(t, "p")
		if err := child.wait(3 * time.Second); err == nil {
			t.Fatal("panic helper exited successfully")
		}
		assertTerminalRestored(t, child.output())
	})
}

// TestPTYHelperProcess is launched as the current test binary under a real PTY.
// It is inert during ordinary test discovery.
func TestPTYHelperProcess(t *testing.T) {
	scenario := os.Getenv(ptyHelperEnvironment)
	if scenario == "" {
		return
	}
	port := &ptyFixturePort{scenario: scenario}
	app := NewAppModel(port, WithVisualMode(NoColorMode()))
	app.clipboardWrite = func(string) error { return nil }
	var model tea.Model = app
	if scenario == "panic" {
		model = &panicOnPModel{app: app}
	}
	_, err := tea.NewProgram(model).Run()
	if err != nil {
		t.Fatalf("PTY helper program: %v", err)
	}
	if scenario == "stuck" && !app.Status().Forced {
		t.Fatal("stuck helper did not report forced shutdown")
	}
}

type panicOnPModel struct{ app *AppModel }

func (model *panicOnPModel) Init() tea.Cmd { return model.app.Init() }

func (model *panicOnPModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyPressMsg); ok && key.String() == "p" {
		panic("intentional PTY restoration fixture")
	}
	_, command := model.app.Update(message)
	return model, command
}

func (model *panicOnPModel) View() tea.View { return model.app.View() }

type ptyFixturePort struct {
	scenario string
	runs     atomic.Int32
}

func (port *ptyFixturePort) Describe(context.Context) (SessionInfo, error) {
	return SessionInfo{
		Project: "pty/fixture", Model: "offline", Provider: "fixture", Mode: ModeDefault,
		ModeChangeable: true, PermissionOwner: "engine", Sandbox: "os",
		ReasoningSummarySupport: SupportSupported, UsageSupport: SupportSupported,
	}, nil
}

func (port *ptyFixturePort) Run(ctx context.Context, input string) (<-chan UIEvent, error) {
	run := port.runs.Add(1)
	if port.scenario == "normal" && run == 1 && input != "ab\ntask\npastedc" {
		return nil, fmt.Errorf("PTY composer produced %q, want position-aware multiline draft", input)
	}
	stream := make(chan UIEvent, 64)
	if port.scenario == "stuck" {
		stream <- UIEvent{Kind: EventRunStarted, RunID: "pty-stuck"}
		stream <- UIEvent{Kind: EventStatusChanged, Activity: ActivityThinking}
		return stream, nil
	}
	if run > 1 {
		go func() {
			defer close(stream)
			stream <- UIEvent{Kind: EventRunStarted, RunID: "pty-cancel"}
			stream <- UIEvent{Kind: EventAssistantStarted, AssistantID: "pty-cancel-assistant"}
			stream <- UIEvent{Kind: EventAssistantTextDelta, AssistantID: "pty-cancel-assistant", Text: "assistant output is still streaming"}
			<-ctx.Done()
			stream <- UIEvent{Kind: EventRunFinished, Terminal: RunCancelled}
		}()
		return stream, nil
	}

	decisions := make(chan PermissionResponse, 2)
	go func() {
		defer close(stream)
		stream <- UIEvent{Kind: EventRunStarted, RunID: "pty-run"}
		stream <- UIEvent{Kind: EventAssistantStarted, AssistantID: "pty-assistant"}
		stream <- UIEvent{Kind: EventAssistantReasoningSummaryDelta, AssistantID: "pty-assistant", Text: "public PTY summary"}
		stream <- UIEvent{Kind: EventAssistantTextDelta, AssistantID: "pty-assistant", Text: strings.Repeat("safe line\n", 45) + "hostile\x1b]52;c;c2VjcmV0\a text"}
		stream <- UIEvent{Kind: EventAssistantFinished, AssistantID: "pty-assistant", Termination: "tool_calls"}
		stream <- UIEvent{Kind: EventToolStarted, CallID: "pty-call", ToolName: "write_file", Arguments: `{"path":"fixture.txt"}`}
		stream <- UIEvent{Kind: EventToolPrepared, CallID: "pty-call", ToolName: "write_file", Arguments: `{"path":"fixture.txt"}`, Revision: 1, Preview: richDiffPreview()}
		stream <- UIEvent{Kind: EventPermissionRequested, CallID: "pty-call", Permission: ptyPrompt("pty-request-1", 1, decisions)}
		decision := <-decisions
		if decision.Decision == DecisionReviseArguments {
			stream <- UIEvent{Kind: EventToolPrepared, CallID: "pty-call", ToolName: "write_file", Arguments: `{"path":"fixture.txt"}`, Revision: 2, Preview: richDiffPreview()}
			stream <- UIEvent{Kind: EventPermissionRequested, CallID: "pty-call", Permission: ptyPrompt("pty-request-2", 2, decisions)}
			decision = <-decisions
		}
		if decision.Decision == DecisionAllowOnce || decision.Decision == DecisionAllowRemember {
			stream <- UIEvent{Kind: EventToolExecuting, CallID: "pty-call", Revision: 2}
			stream <- UIEvent{Kind: EventToolFinished, CallID: "pty-call", Revision: 2, Tool: ToolSucceeded, Result: "fixture succeeded", Duration: 10 * time.Millisecond}
		}
		stream <- UIEvent{Kind: EventRunFinished, Terminal: RunCompleted}
	}()
	return stream, nil
}

func ptyPrompt(requestID string, revision uint64, decisions chan<- PermissionResponse) *PermissionPrompt {
	return &PermissionPrompt{
		RequestID: requestID, CallID: "pty-call", Revision: revision, Tool: "write_file",
		Action: "modify fixture.txt", Arguments: `{"path":"fixture.txt"}`, Reason: "PTY fixture", Origin: "root agent",
		Protocol: "rich", Preview: "modify fixture.txt", StructuredPreview: richDiffPreview(),
		Capabilities: PermissionCapabilities{Allow: true, Deny: true, ReviseArguments: true, SchemaAwareEdit: true, Preview: true},
		RichReply: func(_ context.Context, decision PermissionResponse) (PermissionReplyResult, error) {
			decisions <- decision
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
}

func (port *ptyFixturePort) SetMode(context.Context, SessionMode) error { return nil }

func (port *ptyFixturePort) Close(ctx context.Context) error {
	if port.scenario != "stuck" {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.b.Write(data)
}

func (buffer *synchronizedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.b.String()
}

type ptyChild struct {
	command   *exec.Cmd
	terminal  *os.File
	outputLog *synchronizedBuffer
	done      chan error
	waitOnce  sync.Once
	waitErr   error
}

func startPTYHelper(t *testing.T, scenario string, width, height uint16) *ptyChild {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestPTYHelperProcess$", "-test.count=1")
	command.Env = append(os.Environ(), ptyHelperEnvironment+"="+scenario, "TERM=xterm-256color", "NO_COLOR=1")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: width, Rows: height})
	if err != nil {
		t.Fatalf("start PTY helper: %v", err)
	}
	child := &ptyChild{command: command, terminal: terminal, outputLog: &synchronizedBuffer{}, done: make(chan error, 1)}
	go func() {
		_, _ = io.Copy(child.outputLog, terminal)
	}()
	go func() { child.done <- command.Wait() }()
	return child
}

func (child *ptyChild) write(t *testing.T, value string) {
	t.Helper()
	if _, err := io.WriteString(child.terminal, value); err != nil {
		t.Fatalf("write PTY: %v", err)
	}
}

func (child *ptyChild) waitFor(t *testing.T, value string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(child.output(), value) {
			return
		}
		select {
		case err := <-child.done:
			child.waitErr = err
			t.Fatalf("PTY helper exited before %q: %v\n%s", value, err, child.output())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("PTY output did not contain %q\n%s", value, child.output())
}

func (child *ptyChild) wait(timeout time.Duration) error {
	child.waitOnce.Do(func() {
		select {
		case child.waitErr = <-child.done:
		case <-time.After(timeout):
			child.waitErr = context.DeadlineExceeded
		}
	})
	return child.waitErr
}

func (child *ptyChild) output() string { return child.outputLog.String() }

func (child *ptyChild) close() {
	_ = child.terminal.Close()
	if child.command.Process != nil && child.command.ProcessState == nil {
		_ = child.command.Process.Kill()
	}
}

func assertTerminalRestored(t *testing.T, output string) {
	t.Helper()
	for _, sequence := range []string{"\x1b[?1049l", "\x1b[?1002l", "\x1b[?1006l"} {
		if !strings.Contains(output, sequence) {
			t.Fatalf("PTY output lacks terminal restoration sequence %q\n%s", sequence, output)
		}
	}
}

var _ SessionPort = (*ptyFixturePort)(nil)
var _ tea.Model = (*panicOnPModel)(nil)
