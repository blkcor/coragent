package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestFocusedComposerHasRealCursorAndEditsAtCaret(t *testing.T) {
	model, port := newReadyApp(t, 80, 24)
	if cursor := model.View().Cursor; cursor == nil {
		t.Fatal("focused idle composer did not expose a real terminal cursor")
	}

	model.Update(typeKey("a"))
	model.Update(typeKey("b"))
	model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model.Update(typeKey("X"))

	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter returned no run command")
	}
	message := command()
	opened, ok := message.(runOpenedMsg)
	if !ok {
		t.Fatalf("run command message = %T, want runOpenedMsg", message)
	}
	model.Update(opened)

	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.runInputs) != 1 || port.runInputs[0] != "aXb" {
		t.Fatalf("submitted input = %q, want [aXb]", port.runInputs)
	}
}

func TestComposerCursorStaysVisibleWhileBrowsingHistory(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	for index := range 60 {
		model.transcript.AddNotice(fmt.Sprintf("older output %02d", index), time.Time{})
	}

	wheelTranscript(model, tea.MouseWheelUp)
	if cursor := model.View().Cursor; cursor == nil {
		t.Fatal("mouse-wheel history browsing hid the composer cursor")
	}
	if model.focus != FocusComposer || model.scroll.Mode != ScrollBrowsingHistory {
		t.Fatalf("wheel browsing coupled focus and scroll: focus=%v scroll=%v", model.focus, model.scroll.Mode)
	}

	model.runState = RunRunning
	if cursor := model.View().Cursor; cursor == nil {
		t.Fatal("composer cursor hidden during run — user cannot type while agent works")
	}
}

func TestComposerEditsWholeUnicodeGraphemesAcrossResize(t *testing.T) {
	model, port := newReadyApp(t, 80, 24)
	for _, input := range []string{"A", "e", "\u0301", "👩", "\u200d", "💻", "B"} {
		model.Update(typeKey(input))
	}
	// Move over B and then over the complete ZWJ emoji. A rune-based cursor
	// would stop inside the emoji and split it when X is inserted.
	model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model.Update(typeKey("X"))

	if got := model.composer.Value(); got != "Ae\u0301X👩\u200d💻B" {
		t.Fatalf("grapheme-aware insertion = %q, want %q", got, "Ae\u0301X👩\u200d💻B")
	}
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	cursor := model.View().Cursor
	if cursor == nil {
		t.Fatal("resize hid the focused Unicode cursor")
	}
	if cursor.X < 0 || cursor.X >= model.layout.Width || cursor.Y < 0 || cursor.Y >= model.layout.Height {
		t.Fatalf("Unicode cursor escaped terminal bounds after resize: %+v in %dx%d", cursor.Position, model.layout.Width, model.layout.Height)
	}

	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("Unicode submit returned no command")
	}
	message := command()
	opened, ok := message.(runOpenedMsg)
	if !ok {
		t.Fatalf("submit command message = %T, want runOpenedMsg", message)
	}
	model.Update(opened)
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.runInputs) != 1 || port.runInputs[0] != "Ae\u0301X👩\u200d💻B" {
		t.Fatalf("submitted Unicode input = %q", port.runInputs)
	}
}

func TestComposerBackspaceAndDeleteRemoveWholeGraphemes(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	model.composer.SetValue("Ae\u0301👩\u200d💻B")

	model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := model.composer.Value(); got != "Ae\u0301B" {
		t.Fatalf("backspace split a ZWJ grapheme: %q", got)
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if got := model.composer.Value(); got != "AB" {
		t.Fatalf("delete split a combining grapheme: %q", got)
	}
}

func TestTranscriptShowsScrollbarAndAcceptsMouseWheel(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	for index := range 60 {
		model.transcript.AddNotice(fmt.Sprintf("history line %02d", index), time.Time{})
	}

	view := model.View()
	if !strings.Contains(view.Content, "┃") {
		t.Fatalf("overflowing transcript has no visible scrollbar thumb\n%s", view.Content)
	}
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("view mouse mode = %v, want cell motion for wheel events", view.MouseMode)
	}

	wheelTranscript(model, tea.MouseWheelUp)
	if model.focus != FocusComposer || model.scroll.Mode != ScrollBrowsingHistory {
		t.Fatalf("wheel did not browse with composer focus: focus=%v scroll=%v", model.focus, model.scroll.Mode)
	}
}

func TestShortTranscriptHasNoFalseScrollState(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	model.transcript.AddNotice("everything fits", time.Time{})

	view := model.View().Content
	if strings.ContainsAny(view, "│┃") {
		t.Fatalf("short transcript exposed a false scrollbar\n%s", view)
	}
	wheelTranscript(model, tea.MouseWheelUp)
	wheelTranscript(model, tea.MouseWheelUp)
	if model.scroll.Mode != ScrollPinnedBottom || model.focus != FocusComposer {
		t.Fatalf("short history entered browsing: scroll=%v focus=%v", model.scroll.Mode, model.focus)
	}
	if cursor := model.View().Cursor; cursor == nil {
		t.Fatal("no-op history navigation hid the composer cursor")
	}
}

func TestPageKeysDoNotControlTranscriptHistory(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	for index := range 60 {
		model.transcript.AddNotice(fmt.Sprintf("history line %02d", index), time.Time{})
	}
	model.composer.SetValue("preserved draft")
	model.pinTranscriptBottom()
	before := model.scroll

	model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if model.focus != FocusComposer || model.scroll != before {
		t.Fatalf("page keys controlled transcript history: focus=%v scroll=%+v", model.focus, model.scroll)
	}
	if got := model.composer.Value(); got != "preserved draft" {
		t.Fatalf("page keys changed draft: %q", got)
	}
	if cursor := model.View().Cursor; cursor == nil {
		t.Fatal("page keys hid the composer cursor")
	}
}

func TestTranscriptScrollClampsAndPreservesAnchorOnAppend(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	for index := range 80 {
		model.transcript.AddNotice(fmt.Sprintf("history line %02d", index), time.Time{})
	}

	wheelTranscript(model, tea.MouseWheelUp)
	before := firstTranscriptContentLine(model.renderTranscript(model.layout.ContentWidth, transcriptRowsForTest(model)))
	if before == "" {
		t.Fatal("wheel up produced an empty transcript")
	}

	model.transcript.AddNotice("new live output", time.Time{})
	model.noteLiveOutput()
	after := firstTranscriptContentLine(model.renderTranscript(model.layout.ContentWidth, transcriptRowsForTest(model)))
	if after != before {
		t.Fatalf("streaming append moved history anchor: before=%q after=%q", before, after)
	}

	for range 200 {
		wheelTranscript(model, tea.MouseWheelUp)
	}
	view := strings.Join(model.renderTranscript(model.layout.ContentWidth, transcriptRowsForTest(model)), "\n")
	if !strings.Contains(view, "history line 00") {
		t.Fatalf("scrolling past the oldest content produced a blank/incorrect viewport\n%s", view)
	}
}

func TestTranscriptScrollPreservesAnchorWhenEarlierBlockChangesHeight(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	if err := model.transcript.StartTool("call-growing", "shell", "go test ./...", time.Time{}); err != nil {
		t.Fatalf("StartTool: %v", err)
	}
	if err := model.transcript.FinishTool("call-growing", "short result", ToolSucceeded, time.Time{}); err != nil {
		t.Fatalf("FinishTool: %v", err)
	}
	for index := range 60 {
		model.transcript.AddNotice(fmt.Sprintf("stable history %02d", index), time.Time{})
	}
	wheelTranscript(model, tea.MouseWheelUp)
	before := firstTranscriptContentLine(model.renderTranscript(model.layout.ContentWidth, transcriptRowsForTest(model)))

	model.transcript.Blocks[0].Result = strings.Repeat("expanded result above viewport ", 40)
	after := firstTranscriptContentLine(model.renderTranscript(model.layout.ContentWidth, transcriptRowsForTest(model)))
	if after != before {
		t.Fatalf("in-place height change moved history anchor: before=%q after=%q", before, after)
	}
}

func TestTranscriptResizePreservesSemanticAnchorAndRepinsWhenHistoryFits(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	for index := range 45 {
		model.transcript.AddNotice(fmt.Sprintf("history %02d %s", index, strings.Repeat("wrapped content ", 5)), time.Time{})
	}
	wheelTranscript(model, tea.MouseWheelUp)
	anchorID, anchorLine := model.scroll.AnchorBlockID, model.scroll.AnchorLine
	if anchorID == "" {
		t.Fatal("history browsing did not establish a semantic anchor")
	}

	model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	rows := model.currentTranscriptRows()
	lines := model.renderedTranscriptRows(model.layout.ContentWidth)
	top := model.resolvedScrollTop(lines, rows)
	end := min(len(lines), top+rows)
	anchoredLine := -2
	for _, line := range lines[top:end] {
		if line.BlockID == anchorID {
			anchoredLine = line.Line
			break
		}
	}
	if anchoredLine == -2 {
		t.Fatalf("resize lost anchored block %q/%d from visible range [%d:%d] of %d rows", anchorID, anchorLine, top, end, len(lines))
	}
	if anchoredLine != anchorLine {
		t.Logf("resize reflow used closest line in anchored block: before=%d after=%d", anchorLine, anchoredLine)
	}

	model.transcript = NewTranscriptStore()
	model.transcript.AddNotice("now everything fits", time.Time{})
	model.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	if model.scroll.Mode != ScrollPinnedBottom || model.scroll.Unread != 0 || model.focus != FocusComposer {
		t.Fatalf("fit-after-resize did not repin: scroll=%+v focus=%v", model.scroll, model.focus)
	}
}

func TestTranscriptWheelRepinsAndPermissionCapturesWheel(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	for index := range 60 {
		model.transcript.AddNotice(fmt.Sprintf("history line %02d", index), time.Time{})
	}
	model.composer.SetValue("draft survives scrolling")
	wheelTranscript(model, tea.MouseWheelUp)
	model.transcript.AddNotice("new live output", time.Time{})
	model.noteLiveOutput()
	if model.scroll.Unread != 1 {
		t.Fatalf("unread after live append = %d, want 1", model.scroll.Unread)
	}

	for model.scroll.Mode == ScrollBrowsingHistory {
		wheelTranscript(model, tea.MouseWheelDown)
	}
	if model.scroll.Unread != 0 {
		t.Fatalf("key return to live bottom kept unread=%d", model.scroll.Unread)
	}

	wheelTranscript(model, tea.MouseWheelUp)
	model.permission = &permissionState{Prompt: PermissionPrompt{RequestID: "request-key", CallID: "call-key"}}
	model.focus = FocusPermission
	before := model.scroll
	wheelTranscript(model, tea.MouseWheelUp)
	if model.scroll != before || model.focus != FocusPermission || model.permission.Scroll != 0 {
		t.Fatalf("permission scroll leaked to background: before=%+v after=%+v focus=%v modal=%d", before, model.scroll, model.focus, model.permission.Scroll)
	}
	if got := model.composer.Value(); got != "draft survives scrolling" {
		t.Fatalf("scrolling changed composer draft: %q", got)
	}
}

func TestUnmodifiedDragSelectsAndCopiesWithoutChangingTranscriptOrComposer(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	var nativeClipboard string
	model.clipboardWrite = func(text string) error {
		nativeClipboard = text
		return nil
	}
	for index := range 60 {
		model.transcript.AddNotice(fmt.Sprintf("history line %02d", index), time.Time{})
	}
	model.composer.SetValue("draft remains editable")
	wheelTranscript(model, tea.MouseWheelUp)
	before := model.scroll
	screen := captureSelectionScreen(model.render(), model.layout.Width, model.layout.Height)
	target := ""
	for index := range 60 {
		candidate := fmt.Sprintf("history line %02d", index)
		for _, line := range screen {
			if strings.Contains(line, candidate) {
				target = candidate
				break
			}
		}
		if target != "" {
			break
		}
	}
	if target == "" {
		t.Fatal("wheel-browsed screen contains no selectable history line")
	}
	start := screenPointForText(t, screen, target)

	model.Update(tea.MouseClickMsg(tea.Mouse{X: start.X, Y: start.Y, Button: tea.MouseLeft}))
	model.Update(tea.MouseMotionMsg(tea.Mouse{
		X:      start.X + CellWidth(target) - 1,
		Y:      start.Y,
		Button: tea.MouseLeft,
	}))
	if got, plain := model.View().Content, model.render(); got == plain {
		t.Fatal("drag selection did not render a visible highlight")
	}
	_, command := model.Update(tea.MouseReleaseMsg(tea.Mouse{
		X:      start.X + CellWidth(target) - 1,
		Y:      start.Y,
		Button: tea.MouseLeft,
	}))
	if command == nil {
		t.Fatal("unmodified drag release returned no clipboard command")
	}
	batch, ok := command().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("clipboard command = %T len=%d, want native + OSC 52 batch", batch, len(batch))
	}
	terminalClipboard := ""
	for _, copyCommand := range batch {
		if message := copyCommand(); message != nil {
			terminalClipboard = fmt.Sprint(message)
		}
	}
	if nativeClipboard != target || terminalClipboard != target {
		t.Fatalf("clipboard text native=%q terminal=%q, want %q", nativeClipboard, terminalClipboard, target)
	}

	if model.scroll != before || model.focus != FocusComposer {
		t.Fatalf("drag selection changed background state: before=%+v after=%+v focus=%v", before, model.scroll, model.focus)
	}
	if got := model.composer.Value(); got != "draft remains editable" {
		t.Fatalf("drag selection changed composer draft: %q", got)
	}
	if cursor := model.View().Cursor; cursor == nil {
		t.Fatal("completed drag selection hid the composer cursor")
	}
}

func TestDragSelectionClampsToCurrentTmuxPane(t *testing.T) {
	const paneWidth = 10
	content := "left      RIGHT-NEIGHBOR\nbottom    OTHER-PANE"
	var selection textSelection
	if !selection.begin(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}, content, paneWidth, 2) {
		t.Fatal("selection did not start inside pane")
	}

	text, ok := selection.finish(
		tea.Mouse{X: paneWidth + 80, Y: 40, Button: tea.MouseLeft},
		content,
		paneWidth,
		2,
	)
	if !ok {
		t.Fatal("selection ending outside pane produced no pane-local copy")
	}
	if text != "left\nbottom" {
		t.Fatalf("pane-local clipboard text = %q, want %q", text, "left\nbottom")
	}
	if strings.Contains(text, "NEIGHBOR") || strings.Contains(text, "OTHER-PANE") {
		t.Fatalf("pane-local selection crossed into adjacent tmux content: %q", text)
	}
}

func TestDragSelectionKeepsWideGraphemeWhole(t *testing.T) {
	const content = "A你B"
	var selection textSelection
	selection.begin(tea.Mouse{X: 1, Y: 0, Button: tea.MouseLeft}, content, 4, 1)
	text, ok := selection.finish(tea.Mouse{X: 2, Y: 0, Button: tea.MouseLeft}, content, 4, 1)
	if !ok || text != "你" {
		t.Fatalf("wide-grapheme selection = %q, ok=%v, want 你", text, ok)
	}

	highlighted := selection.render(content, 4, 1)
	if stripANSI(highlighted) != content {
		t.Fatalf("selection highlight changed visible text: %q", stripANSI(highlighted))
	}
	if CellWidth(highlighted) != CellWidth(content) {
		t.Fatalf("selection highlight changed cell width: got %d want %d", CellWidth(highlighted), CellWidth(content))
	}
}

func TestTranscriptScrollbarUsesShapeInASCIIMode(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	model.theme = ThemeForMode(VisualMode{Color: ColorNoColor, ReducedMotion: true, ASCII: true})
	model.composer.Configure(model.theme, model.layout)
	for index := range 60 {
		model.transcript.AddNotice(fmt.Sprintf("history line %02d", index), time.Time{})
	}
	view := model.View().Content
	if !strings.Contains(view, "#") || !strings.Contains(view, "|") {
		t.Fatalf("ASCII scrollbar track/thumb are not distinguishable by shape\n%s", view)
	}
}

func firstTranscriptContentLine(lines []string) string {
	for _, line := range lines {
		plain := strings.TrimSpace(stripANSI(line))
		if plain != "" {
			return plain
		}
	}
	return ""
}

func transcriptRowsForTest(model *AppModel) int {
	composerRows := model.composerHeight(model.layout.ContentWidth)
	fixedRows := 3 + composerRows
	if model.permission == nil {
		fixedRows++
	}
	return max(1, model.layout.Height-fixedRows)
}

func wheelTranscript(model *AppModel, button tea.MouseButton) {
	model.Update(tea.MouseWheelMsg(tea.Mouse{
		X:      model.layout.Width / 2,
		Y:      model.layout.Height / 2,
		Button: button,
	}))
}

func screenPointForText(t *testing.T, lines []string, target string) cellPoint {
	t.Helper()
	for row, line := range lines {
		if index := strings.Index(line, target); index >= 0 {
			return cellPoint{X: CellWidth(line[:index]), Y: row}
		}
	}
	t.Fatalf("rendered screen does not contain %q", target)
	return cellPoint{}
}

func stripANSI(value string) string {
	var out strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == 0x1b {
			for index < len(value) && value[index] != 'm' {
				index++
			}
			continue
		}
		out.WriteByte(value[index])
	}
	return out.String()
}
