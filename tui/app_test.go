package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type fakeClock struct{ now time.Time }

func (clock *fakeClock) Now() time.Time { return clock.now }
func (*fakeClock) After(time.Duration) <-chan time.Time {
	return nil
}

type fakeSessionPort struct {
	mu sync.Mutex

	info        SessionInfo
	describeErr error
	runErr      error
	stream      chan UIEvent
	runInputs   []string
	runContext  context.Context
	modes       []SessionMode
	modeErr     error
	closeCalls  int
	closeErr    error
}

func (port *fakeSessionPort) Describe(context.Context) (SessionInfo, error) {
	return port.info, port.describeErr
}

func (port *fakeSessionPort) Run(ctx context.Context, input string) (<-chan UIEvent, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	port.runInputs = append(port.runInputs, input)
	port.runContext = ctx
	return port.stream, port.runErr
}

func (port *fakeSessionPort) SetMode(_ context.Context, mode SessionMode) error {
	port.mu.Lock()
	defer port.mu.Unlock()
	port.modes = append(port.modes, mode)
	return port.modeErr
}

func (port *fakeSessionPort) Close(context.Context) error {
	port.mu.Lock()
	defer port.mu.Unlock()
	port.closeCalls++
	return port.closeErr
}

func newReadyApp(t *testing.T, width, height int) (*AppModel, *fakeSessionPort) {
	t.Helper()
	port := &fakeSessionPort{
		info: SessionInfo{
			Project:        "coragent/phase-7",
			Model:          "gpt-test",
			Mode:           ModeDefault,
			ModeChangeable: true,
			Sandbox:        "os",
			Context:        "ctx 0%",
		},
		stream: make(chan UIEvent, 32),
	}
	clock := &fakeClock{now: time.Date(2026, 7, 11, 14, 32, 0, 0, time.UTC)}
	model := NewAppModel(port, WithClock(clock), WithVisualMode(VisualMode{Color: ColorNoColor, ReducedMotion: true}))
	model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	command := model.Init()
	if command == nil {
		t.Fatal("Init returned no Describe command")
	}
	message := command()
	model.Update(message)
	if model.runState != RunIdle {
		t.Fatalf("run state after Describe = %v, want idle", model.runState)
	}
	return model, port
}

func TestStartupLeavesShiftAvailableForTerminalSelection(t *testing.T) {
	model := NewAppModel(nil, WithVisualMode(VisualMode{Color: ColorNoColor, ReducedMotion: true}))
	model.composer.Focus()
	command := model.handleStartup(startupLoadedMsg{Info: SessionInfo{Mode: ModeDefault}})
	if command == nil {
		t.Fatal("startup returned no terminal-selection command")
	}
	if !commandIncludesRaw(command, terminalShiftSelection) {
		t.Fatal("startup did not request Shift bypass for native terminal selection")
	}
}

func commandIncludesRaw(command tea.Cmd, want string) bool {
	message := command()
	switch message := message.(type) {
	case tea.RawMsg:
		value, ok := message.Msg.(string)
		return ok && value == want
	case tea.BatchMsg:
		for _, child := range message {
			if commandIncludesRaw(child, want) {
				return true
			}
		}
	}
	return false
}

func press(code rune, modifiers tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: modifiers}
}

func shiftTab() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
}

func typeKey(text string) tea.KeyPressMsg {
	var code rune
	if text != "" {
		code, _ = utf8FirstRune(text)
	}
	return tea.KeyPressMsg{Code: code, Text: text}
}

func utf8FirstRune(value string) (rune, int) {
	for _, current := range value {
		return current, len(string(current))
	}
	return 0, 0
}

func startFakeRun(t *testing.T, model *AppModel, port *fakeSessionPort, input string) tea.Cmd {
	t.Helper()
	model.composer.SetValue(input)
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter returned no Run command")
	}
	message := command()
	opened, ok := message.(runOpenedMsg)
	if !ok {
		t.Fatalf("Run command message = %T, want runOpenedMsg", message)
	}
	_, read := model.Update(opened)
	if read == nil {
		t.Fatal("opening a run did not arm the event reader")
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.runInputs) != 1 || port.runInputs[0] != input {
		t.Fatalf("Run inputs = %q, want [%q]", port.runInputs, input)
	}
	return read
}

func feedEvent(t *testing.T, model *AppModel, stream chan UIEvent, read tea.Cmd, event UIEvent) tea.Cmd {
	t.Helper()
	stream <- event
	message := read()
	readMessage, ok := message.(eventReadMsg)
	if !ok {
		t.Fatalf("event reader message = %T, want eventReadMsg", message)
	}
	_, next := model.Update(readMessage)
	if next == nil {
		t.Fatalf("event %q did not immediately re-arm the reader", event.Kind)
	}
	return next
}

func TestAppEmptyResponsiveShells(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		class  LayoutClass
	}{
		{name: "canonical", width: 120, height: 36, class: LayoutStandard},
		{name: "compact", width: 80, height: 24, class: LayoutCompact},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, _ := newReadyApp(t, test.width, test.height)
			if model.terminal.Class != test.class {
				t.Fatalf("layout class = %v, want %v", model.terminal.Class, test.class)
			}
			view := model.View().Content
			lines := strings.Split(view, "\n")
			if len(lines) != test.height {
				t.Fatalf("rendered rows = %d, want %d\n%s", len(lines), test.height, view)
			}
			for _, want := range []string{"coragent", "DEFAULT", "sandbox os", "Describe what you want changed"} {
				if !strings.Contains(view, want) {
					t.Errorf("empty view does not contain %q\n%s", want, view)
				}
			}
			for _, noise := range []string{"Enter send", "Ctrl+J newline", "wheel history", "drag auto-copy"} {
				if strings.Contains(view, noise) {
					t.Errorf("empty view still contains redundant hint %q\n%s", noise, view)
				}
			}
			placeholderRow := -1
			for row, line := range lines {
				if strings.Contains(line, "Describe what you want changed") {
					placeholderRow = row
					break
				}
			}
			if placeholderRow < test.height-5 {
				t.Fatalf("composer is not anchored near the terminal bottom: row=%d\n%s", placeholderRow, view)
			}
		})
	}
}

func TestStatusNeverSacrificesModeOrSandboxToLongPath(t *testing.T) {
	model, _ := newReadyApp(t, 60, 20)
	model.info.Project = "/workspace/a/very/long/project/path/that/cannot/fit/coragent"
	model.mode = ModeAutoAcceptEdits
	model.info.Sandbox = "os"
	header := model.renderHeader(model.layout.ContentWidth)
	footer := model.renderFooter(model.layout.ContentWidth)
	if !strings.Contains(header, "coragent") {
		t.Fatalf("header missing hero: %q", header)
	}
	if !strings.Contains(footer, "AUTO EDIT") {
		t.Fatalf("footer sacrificed mode: %q", footer)
	}
	if !strings.Contains(footer, "sandbox os") {
		t.Fatalf("footer sacrificed sandbox: %q", footer)
	}
	if got := ansi.StringWidth(header); got > model.layout.ContentWidth {
		t.Fatalf("header width = %d, content width = %d", got, model.layout.ContentWidth)
	}
	if got := ansi.StringWidth(footer); got > model.layout.ContentWidth {
		t.Fatalf("footer width = %d, content width = %d", got, model.layout.ContentWidth)
	}
}

func TestShortTranscriptStartsAtTopWithoutViewportPadding(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.transcript.AddUser("who are you", time.Time{})
	if err := model.transcript.StartAssistant("assistant-top", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.AppendAssistant("assistant-top", "I am Coragent.", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.FinishAssistant("assistant-top"); err != nil {
		t.Fatal(err)
	}

	view := model.View().Content
	user := strings.Index(view, "who are you")
	composer := strings.Index(view, "Describe what you want changed")
	if user < 0 || composer < 0 || user > composer {
		t.Fatalf("conversation order is not top-aligned\n%s", view)
	}
	prefix := view[:user]
	if strings.Count(prefix, "\n") > 2 {
		t.Fatalf("short transcript still has a large leading void\n%s", view)
	}
	if !strings.Contains(view, "who are you") || strings.Contains(view, "YOU:") || strings.Contains(view, "AGENT:") {
		t.Fatalf("run-ledger role treatment regressed\n%s", view)
	}
}

func TestProseWrapPreservesWordsAndStillBoundsLongTokens(t *testing.T) {
	lines := wrapProse("whether it is answering questions or editing code", 14)
	joined := strings.Join(lines, "|")
	if strings.Contains(joined, "wheth|er") || strings.Contains(joined, "quest|ions") {
		t.Fatalf("prose split an ordinary word: %q", joined)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > 14 {
			t.Fatalf("prose line width = %d, want <= 14: %q", width, line)
		}
	}

	long := wrapProse(strings.Repeat("x", 31), 10)
	for _, line := range long {
		if width := ansi.StringWidth(line); width > 10 {
			t.Fatalf("long token line width = %d, want <= 10: %q", width, line)
		}
	}
}

func TestAppStreamingToolPermissionAndTerminalOrdering(t *testing.T) {
	model, port := newReadyApp(t, 120, 36)
	read := startFakeRun(t, model, port, "Inspect the parser")
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventRunStarted, RunID: "run-1"})
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventAssistantStarted, AssistantID: "assistant-1"})
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventAssistantTextDelta, AssistantID: "assistant-1", Text: "I will "})
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventAssistantTextDelta, AssistantID: "assistant-1", Text: "inspect it."})
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventAssistantFinished, AssistantID: "assistant-1"})
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventToolStarted, CallID: "call-1", ToolName: "read_file", Arguments: "internal/parser.go"})
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventToolPrepared, CallID: "call-1", ToolName: "read_file", Arguments: "internal/parser/parser.go"})
	if block := model.transcript.Blocks[2]; block.ToolState != ToolPreparing || block.Arguments != "internal/parser/parser.go" {
		t.Fatalf("prepared tool did not update in place: %+v", block)
	}

	var decisions []PermissionDecision
	prompt := PermissionPrompt{
		RequestID: "request-1",
		CallID:    "call-1",
		Tool:      "read_file",
		Action:    "Read internal/parser.go",
		Reason:    "approval required",
		Origin:    "root agent",
		Reply: func(_ context.Context, decision PermissionDecision) (PermissionReplyResult, error) {
			decisions = append(decisions, decision)
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	if model.focus != FocusPermission || model.permission == nil {
		t.Fatal("permission event did not capture focus")
	}
	if !strings.Contains(model.View().Content, "REVIEW") {
		t.Fatal("permission sheet is not visible")
	}

	_, reply := model.Update(typeKey("a"))
	if reply == nil || model.permission == nil || !model.permission.Submitting {
		t.Fatal("allow did not enter the submitting state")
	}
	model.Update(reply())
	if model.permission != nil {
		t.Fatal("accepted permission did not close the modal")
	}
	if len(decisions) != 1 || decisions[0] != DecisionAllowOnce {
		t.Fatalf("permission decisions = %v, want one allow-once", decisions)
	}

	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventToolExecuting, CallID: "call-1"})
	if block := model.transcript.Blocks[2]; block.ToolState != ToolRunning {
		t.Fatalf("executing tool did not update in place: %+v", block)
	}
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventToolFinished, CallID: "call-1", Tool: ToolSucceeded, Result: "214 lines"})
	read = feedEvent(t, model, port.stream, read, UIEvent{Kind: EventRunFinished, Terminal: RunCompleted})
	if model.runState != RunIdle || model.activity != ActivityIdle {
		t.Fatalf("terminal did not settle app: run=%v activity=%q", model.runState, model.activity)
	}
	if len(model.transcript.Blocks) != 3 {
		t.Fatalf("block count = %d, want user/assistant/tool", len(model.transcript.Blocks))
	}
	if got := model.transcript.Blocks[1].Text; got != "I will inspect it." {
		t.Fatalf("assistant text = %q", got)
	}
	if model.transcript.Blocks[2].ToolState != ToolDone {
		t.Fatalf("tool state = %v, want done", model.transcript.Blocks[2].ToolState)
	}
	view := model.View().Content
	for _, want := range []string{"Inspect the parser", "I will inspect it.", "read_file", "succeeded"} {
		if !strings.Contains(view, want) {
			t.Errorf("completed view does not contain %q\n%s", want, view)
		}
	}

	close(port.stream)
	eof := read()
	model.Update(eof)
	if model.fatalErr != nil {
		t.Fatalf("EOF after terminal was treated as fatal: %v", model.fatalErr)
	}
}

func TestDraftPreservedWhileMouseWheelBrowsesHistory(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	for index := range 40 {
		model.transcript.AddUser(fmt.Sprintf("older request %02d", index), time.Time{})
	}
	model.composer.SetValue("line one\nline two")
	wheelTranscript(model, tea.MouseWheelUp)
	if model.focus != FocusComposer || model.scroll.Mode != ScrollBrowsingHistory {
		t.Fatalf("wheel did not browse with composer focus: focus=%v scroll=%v", model.focus, model.scroll.Mode)
	}
	if draft := model.composer.Value(); draft != "line one\nline two" {
		t.Fatalf("wheel changed draft to %q", draft)
	}
	model.Update(press('j', tea.ModCtrl))
	if draft := model.composer.Value(); draft != "line one\nline two\n" {
		t.Fatalf("Ctrl+J draft = %q", draft)
	}
}

func TestCancelRequestsRunContextCancellationOnce(t *testing.T) {
	model, port := newReadyApp(t, 120, 36)
	_ = startFakeRun(t, model, port, "long task")
	_, cancel := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cancel == nil || model.runState != RunCancelling {
		t.Fatal("Esc did not enter cancelling state")
	}
	cancel()
	select {
	case <-port.runContext.Done():
	default:
		t.Fatal("Esc did not cancel the Run context")
	}
	_, duplicate := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if duplicate != nil {
		t.Fatal("repeated Esc emitted a duplicate cancellation command")
	}
}

func TestPermissionModalHasPriorityAndTooSmallFailsSafe(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.runState = RunRunning
	model.composer.SetValue("preserve me")
	var decisions []PermissionDecision
	prompt := PermissionPrompt{
		RequestID: "request-modal",
		CallID:    "call-modal",
		Tool:      "shell",
		Action:    "go test ./...",
		Reason:    "command approval",
		Reply: func(_ context.Context, decision PermissionDecision) (PermissionReplyResult, error) {
			decisions = append(decisions, decision)
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	_, modeCommand := model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if modeCommand == nil {
		t.Fatal("running permission did not allow a live mode change")
	}
	model.Update(modeCommand())
	// Enter is a valid permission action; use Tab to verify no input leak.
	model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if draft := model.composer.Value(); model.focus != FocusPermission || draft != "preserve me" || model.mode != ModeAutoAcceptEdits {
		t.Fatalf("background input leaked through modal: focus=%v draft=%q mode=%q", model.focus, draft, model.mode)
	}

	model.Update(tea.WindowSizeMsg{Width: 50, Height: 18})
	_, allow := model.Update(typeKey("a"))
	if allow != nil || len(decisions) != 0 {
		t.Fatal("too-small permission allowed a blind approval")
	}
	view := model.View().Content
	for _, required := range []string{"PERMISSION PENDING", "deny", "Ctrl+C", "Ctrl+Q"} {
		if !strings.Contains(view, required) {
			t.Fatalf("too-small permission view lost %q\n%s", required, view)
		}
	}
	_, deny := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if deny == nil {
		t.Fatal("too-small permission Enter did not choose the only safe action")
	}
	model.Update(deny())
	if len(decisions) != 1 || decisions[0] != DecisionDenyOnce {
		t.Fatalf("too-small decisions = %v, want deny-once", decisions)
	}
}

func TestPermissionReviewViewportKeepsControlsFixedAndFullReviewReachable(t *testing.T) {
	model, _ := newReadyApp(t, 60, 20)
	model.runState = RunRunning
	prompt := PermissionPrompt{
		RequestID: "request-long-preview",
		CallID:    "call-long-preview",
		Revision:  1,
		Tool:      "edit_file",
		Action:    "Modify internal/parser/parser.go " + strings.Repeat("with-reviewed-context ", 8) + "DANGER",
		Reason:    "replace a stale constructor after validating its callers",
		Origin:    "root agent",
		Preview:   strings.Repeat("- old line\n+ replacement line\n", 40),
		Reply: func(context.Context, PermissionDecision) (PermissionReplyResult, error) {
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})

	assertFixed := func(label string) string {
		t.Helper()
		view := model.View().Content
		for _, required := range []string{"REVIEW", "preview r1", "edit_file", "Allow once", "Deny", "PgUp/PgDown scroll"} {
			if !strings.Contains(view, required) {
				t.Fatalf("%s view lost fixed review field %q\n%s", label, required, view)
			}
		}
		if rows := strings.Count(view, "\n") + 1; rows != 20 {
			t.Fatalf("%s view rendered %d rows, want 20", label, rows)
		}
		return view
	}

	before := assertFixed("initial")
	if !strings.Contains(before, "ACTION") {
		t.Fatalf("initial review does not begin with the action\n%s", before)
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	after := assertFixed("scrolled")
	if before == after {
		t.Fatal("permission PageDown did not move the review viewport")
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	foundSuffix := false
	foundReason := false
	for range 100 {
		wheelTranscript(model, tea.MouseWheelDown)
		view := assertFixed("review scan")
		if strings.Contains(view, "DANGER") {
			foundSuffix = true
		}
		if strings.Contains(view, "WHY") {
			foundReason = true
		}
		if foundSuffix && foundReason {
			break
		}
	}
	if !foundSuffix {
		t.Fatal("full action suffix was not reachable in the review viewport")
	}
	if !foundReason {
		t.Fatal("permission reason was not reachable in the review viewport")
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if home := assertFixed("review start"); !strings.Contains(home, "ACTION") {
		t.Fatalf("Home did not restore the start of review\n%s", home)
	}
}

func TestPermissionArrowSelectionEnterAndReviewScrollingAreIndependent(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	model.runState = RunRunning
	var responses []PermissionResponse
	prompt := PermissionPrompt{
		RequestID: "request-selection", CallID: "call-selection", Tool: "search_content",
		Action: "Search the repository", Reason: strings.Repeat("review context ", 30),
		RememberScope: "exact search_content call",
		Capabilities:  PermissionCapabilities{Allow: true, Deny: true, Remember: true},
		RichReply: func(_ context.Context, response PermissionResponse) (PermissionReplyResult, error) {
			responses = append(responses, response)
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	if model.permission.Selected != permissionActionAllowOnce {
		t.Fatalf("initial selection = %v", model.permission.Selected)
	}
	initialScroll := model.permission.Scroll
	model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.permission.Selected != permissionActionAllowRemember || model.permission.Scroll != initialScroll {
		t.Fatalf("Down selection=%v scroll=%d", model.permission.Selected, model.permission.Scroll)
	}
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "› [A] Allow & remember") {
		t.Fatalf("selected action is not visibly focused:\n%s", view)
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if model.permission.Scroll == initialScroll || model.permission.Selected != permissionActionAllowRemember {
		t.Fatalf("PageDown selection=%v scroll=%d", model.permission.Selected, model.permission.Scroll)
	}
	selected := model.permission.Selected
	wheelTranscript(model, tea.MouseWheelDown)
	if model.permission.Selected != selected {
		t.Fatalf("mouse wheel changed selection from %v to %v", selected, model.permission.Selected)
	}
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter did not activate selected permission action")
	}
	model.Update(command())
	if len(responses) != 1 || responses[0].Decision != DecisionAllowRemember || !responses[0].Remember {
		t.Fatalf("responses = %+v", responses)
	}
}

func TestPermissionOpenBypassConfirmationChangesFutureModeOnly(t *testing.T) {
	model, port := newReadyApp(t, 100, 30)
	model.runState = RunRunning
	var responses []PermissionResponse
	prompt := PermissionPrompt{
		RequestID: "request-bypass", CallID: "call-bypass", Tool: "run_command",
		Capabilities: PermissionCapabilities{Allow: true, Deny: true},
		RichReply: func(_ context.Context, response PermissionResponse) (PermissionReplyResult, error) {
			responses = append(responses, response)
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	// Cycle from Default to AutoAcceptEdits, then Plan, then Bypass.
	_, cmd := model.Update(shiftTab())
	model.Update(cmd())
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	if model.mode != ModeBypass || model.permission == nil || len(responses) != 0 {
		t.Fatalf("post-bypass mode=%q permission=%v responses=%v", model.mode, model.permission, responses)
	}
	if !strings.Contains(model.permission.Feedback, "still needs a decision") {
		t.Fatalf("current prompt semantics are not explicit: %q", model.permission.Feedback)
	}
	port.mu.Lock()
	if len(port.modes) != 3 || port.modes[2] != ModeBypass {
		t.Fatalf("mode requests = %v", port.modes)
	}
	port.mu.Unlock()
	_, reply := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if reply == nil {
		t.Fatal("current prompt was not answerable after live bypass")
	}
	model.Update(reply())
	if len(responses) != 1 || responses[0].Decision != DecisionAllowOnce {
		t.Fatalf("current prompt response = %+v", responses)
	}
}

func TestPermissionValidationRejectionKeepsModalAndDraft(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	prompt := PermissionPrompt{
		RequestID: "request-reject",
		CallID:    "call-reject",
		Tool:      "edit_file",
		Action:    "Modify parser.go",
		Reply: func(context.Context, PermissionDecision) (PermissionReplyResult, error) {
			return PermissionReplyResult{Status: ReplyValidationRejected, Feedback: "preview is stale"}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	model.composer.SetValue("unchanged draft")
	_, reply := model.Update(typeKey("a"))
	model.Update(reply())
	if model.permission == nil || model.permission.Submitting || model.permission.Feedback != "preview is stale" {
		t.Fatalf("rejected reply state = %+v", model.permission)
	}
	if draft := model.composer.Value(); draft != "unchanged draft" {
		t.Fatalf("rejected reply changed composer draft to %q", draft)
	}
}

func TestPermissionCancelDoesNotWaitForBlockedDenyReply(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.runState = RunRunning
	runContext, cancel := context.WithCancel(context.Background())
	model.runCancel = cancel
	replyStarted := make(chan struct{})
	releaseReply := make(chan struct{})
	prompt := PermissionPrompt{
		RequestID: "request-blocked-reply",
		CallID:    "call-blocked-reply",
		Tool:      "caller_tool",
		Action:    "caller-owned action",
		Reply: func(context.Context, PermissionDecision) (PermissionReplyResult, error) {
			close(replyStarted)
			<-releaseReply
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})

	_, command := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if command == nil {
		t.Fatal("permission Ctrl+C returned no command")
	}
	message := command()
	batch, ok := message.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("permission Ctrl+C command = %T (%d items), want two-command batch", message, len(batch))
	}
	var commands sync.WaitGroup
	commands.Add(len(batch))
	for _, item := range batch {
		go func(command tea.Cmd) {
			defer commands.Done()
			_ = command()
		}(item)
	}

	select {
	case <-replyStarted:
	case <-time.After(time.Second):
		t.Fatal("deny reply command did not start")
	}
	select {
	case <-runContext.Done():
	case <-time.After(time.Second):
		t.Fatal("run cancellation waited behind the blocked deny reply")
	}
	close(releaseReply)
	commands.Wait()
}

func TestControlCCancelsRunningThroughEveryOverlay(t *testing.T) {
	for _, kind := range []overlayKind{overlayHelp, overlayInspector, overlayBypass} {
		t.Run(fmt.Sprintf("overlay-%d", kind), func(t *testing.T) {
			model, _ := newReadyApp(t, 80, 24)
			model.runState = RunRunning
			runContext, cancel := context.WithCancel(context.Background())
			model.runCancel = cancel
			model.overlay = &overlayState{Kind: kind}

			_, command := model.Update(press('c', tea.ModCtrl))
			if command == nil || model.runState != RunCancelling {
				t.Fatalf("Ctrl+C through overlay %d returned command=%v state=%v", kind, command != nil, model.runState)
			}
			command()
			select {
			case <-runContext.Done():
			case <-time.After(time.Second):
				t.Fatalf("Ctrl+C through overlay %d did not cancel run context", kind)
			}
		})
	}
}

func TestOverlayEscapeClosesWithoutCancellingRun(t *testing.T) {
	for _, kind := range []overlayKind{overlayHelp, overlayInspector} {
		t.Run(fmt.Sprintf("overlay-%d", kind), func(t *testing.T) {
			model, _ := newReadyApp(t, 80, 24)
			model.runState = RunRunning
			runContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			model.runCancel = cancel
			model.overlay = &overlayState{Kind: kind}

			model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if model.overlay != nil || model.runState != RunRunning {
				t.Fatalf("Esc through overlay %d changed overlay=%v state=%v", kind, model.overlay, model.runState)
			}
			select {
			case <-runContext.Done():
				t.Fatalf("Esc through overlay %d cancelled the run", kind)
			default:
			}
		})
	}
}

func TestControlCOverPermissionOverlayDeniesAndCancelsExactlyOnce(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	model.runState = RunRunning
	runContext, cancel := context.WithCancel(context.Background())
	model.runCancel = cancel
	var responses []PermissionResponse
	prompt := PermissionPrompt{
		RequestID: "request-overlay-cancel", CallID: "call-overlay-cancel", Tool: "custom_tool", Action: "custom action",
		Capabilities: PermissionCapabilities{Allow: true, Deny: true},
		RichReply: func(_ context.Context, response PermissionResponse) (PermissionReplyResult, error) {
			responses = append(responses, response)
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	model.overlay = &overlayState{Kind: overlayBypass}

	_, command := model.Update(press('c', tea.ModCtrl))
	if command == nil || !model.permission.Submitting || model.runState != RunCancelling {
		t.Fatalf("first Ctrl+C command=%v permission=%+v state=%v", command != nil, model.permission, model.runState)
	}
	if _, duplicate := model.Update(press('c', tea.ModCtrl)); duplicate != nil {
		t.Fatal("repeated Ctrl+C emitted a duplicate permission or cancel command")
	}
	message, ok := command().(tea.BatchMsg)
	if !ok || len(message) != 2 {
		t.Fatalf("permission overlay Ctrl+C returned %T with %d commands", message, len(message))
	}
	for _, child := range message {
		if result := child(); result != nil {
			model.Update(result)
		}
	}
	if len(responses) != 1 || responses[0].Decision != DecisionDenyOnce {
		t.Fatalf("permission responses = %+v, want one deny-once", responses)
	}
	select {
	case <-runContext.Done():
	case <-time.After(time.Second):
		t.Fatal("permission overlay Ctrl+C did not cancel run context")
	}
}

func TestPermissionReviewKeepsUnavailablePreviewReason(t *testing.T) {
	tests := []struct {
		name   string
		prompt PermissionPrompt
		reason string
	}{
		{
			name: "structured authoritative preview",
			prompt: PermissionPrompt{
				Preview: "custom · Preview unavailable: stale formatted reason",
				StructuredPreview: &ActionPreview{
					Kind: "unavailable", UnavailableReason: "handler\n does\t not support preparation",
				},
			},
			reason: "handler does not support preparation",
		},
		{
			name:   "legacy formatted preview",
			prompt: PermissionPrompt{Preview: "custom · Preview unavailable: legacy handler cannot prepare"},
			reason: "legacy handler cannot prepare",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, _ := newReadyApp(t, 120, 36)
			test.prompt.RequestID = "request-unavailable"
			test.prompt.CallID = "call-unavailable"
			test.prompt.Tool = "custom_tool"
			test.prompt.Action = "custom action"
			test.prompt.Capabilities = PermissionCapabilities{Allow: true, Deny: true, Preview: true}
			test.prompt.RichReply = func(context.Context, PermissionResponse) (PermissionReplyResult, error) {
				return PermissionReplyResult{Status: ReplyAccepted}, nil
			}
			model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &test.prompt})
			view := ansi.Strip(model.render())
			want := "PREVIEW unavailable · " + test.reason
			if !strings.Contains(view, want) {
				t.Fatalf("REVIEW lost authoritative unavailable preview %q:\n%s", want, view)
			}
		})
	}

	longReason := strings.Repeat("safe reason segment ", 20)
	body, viewportRows, maxScroll, clipped := permissionReviewMetrics(ThemeForMode(NoColorMode()), permissionRenderOptions{
		Width: 60, MaxRows: 9,
		Prompt: PermissionPrompt{
			Action: "custom action", Preview: "Preview unavailable: " + longReason,
			Capabilities: PermissionCapabilities{Allow: true, Deny: true, Preview: true},
		},
	})
	if !clipped || maxScroll == 0 || viewportRows < 1 {
		t.Fatalf("unavailable preview did not participate in viewport: rows=%d maxScroll=%d clipped=%v body=%q", viewportRows, maxScroll, clipped, body)
	}
	for _, line := range body {
		if width := ansi.StringWidth(line); width > 56 {
			t.Fatalf("review line width = %d, want <= 56: %q", width, line)
		}
	}
}

func TestStructuredPermissionPreviewTextCannotSpoofUnavailable(t *testing.T) {
	theme := ThemeForMode(NoColorMode())
	tests := []struct {
		name    string
		kind    string
		preview string
		want    string
	}{
		{
			name:    "command text",
			kind:    "text",
			preview: "command · rm -rf ./important # preview unavailable\ntimeout: 30s",
			want:    "rm -rf ./important # preview unavailable",
		},
		{
			name:    "file diff line",
			kind:    "file_diff",
			preview: "modify · important.txt\n+preview unavailable is application content",
			want:    "+preview unavailable is application content",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := permissionReviewPreview(theme, PermissionPrompt{
				Preview: test.preview, StructuredPreview: &ActionPreview{Kind: test.kind},
			})
			if !strings.Contains(got, test.want) {
				t.Fatalf("structured preview lost authoritative content %q: %q", test.want, got)
			}
			if strings.HasPrefix(got, "PREVIEW unavailable") {
				t.Fatalf("structured %s preview was reclassified by its content: %q", test.kind, got)
			}
		})
	}
}

func TestEOFBeforeTerminalIsFatal(t *testing.T) {
	model, port := newReadyApp(t, 120, 36)
	read := startFakeRun(t, model, port, "broken stream")
	close(port.stream)
	message := read()
	if _, ok := message.(eventEOFMsg); !ok {
		t.Fatalf("reader returned %T, want EOF", message)
	}
	model.Update(message)
	if model.fatalErr == nil || !strings.Contains(model.fatalErr.Error(), "before run terminal") {
		t.Fatalf("fatal error = %v", model.fatalErr)
	}
	if model.runState != RunQuitting || !model.forcedExit {
		t.Fatalf("EOF did not enter fatal bounded shutdown: run=%v forced=%v", model.runState, model.forcedExit)
	}
}

func TestSafeModeCycleUpdatesOnlyAfterPortAccepts(t *testing.T) {
	model, port := newReadyApp(t, 80, 24)
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if command == nil || model.mode != ModeDefault {
		t.Fatal("Shift+Tab did not request a deferred mode change")
	}
	model.Update(command())
	if model.mode != ModeAutoAcceptEdits {
		t.Fatalf("accepted mode = %q", model.mode)
	}
	if len(port.modes) != 1 || port.modes[0] != ModeAutoAcceptEdits {
		t.Fatalf("requested modes = %v", port.modes)
	}

	port.modeErr = errors.New("externally controlled")
	_, command = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model.Update(command())
	if model.mode != ModeAutoAcceptEdits {
		t.Fatalf("rejected mode changed chrome to %q", model.mode)
	}
}

func TestControlQClosesIdleSession(t *testing.T) {
	model, port := newReadyApp(t, 80, 24)
	_, closeCommand := model.Update(press('q', tea.ModCtrl))
	if closeCommand == nil || model.runState != RunQuitting {
		t.Fatal("Ctrl+Q did not start shutdown")
	}
	message := closeCommand()
	if _, ok := message.(closeDoneMsg); !ok {
		t.Fatalf("close command returned %T", message)
	}
	_, quit := model.Update(message)
	if quit == nil || !model.closed || port.closeCalls != 1 {
		t.Fatalf("shutdown did not close exactly once: closed=%v calls=%d", model.closed, port.closeCalls)
	}
}
