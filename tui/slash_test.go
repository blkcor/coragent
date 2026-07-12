package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func newTestApp(t *testing.T) *AppModel {
	t.Helper()
	port := &fakeSessionPort{
		info: SessionInfo{
			Project:        "coragent",
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
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	command := model.Init()
	if command == nil {
		t.Fatal("Init returned no Describe command")
	}
	message := command()
	model.Update(message)
	return model
}

func dispatchSlash(t *testing.T, model *AppModel, input string) {
	t.Helper()
	model.composer.SetValue(input)
	command := model.slash.Dispatch(model, input)
	model.composer.Reset()
	if command != nil {
		msg := command()
		model.Update(msg)
	}
}

func blockTexts(store *TranscriptStore) []string {
	texts := make([]string, 0, len(store.Blocks))
	for _, block := range store.Blocks {
		if block.Kind == BlockNotice || block.Kind == BlockRichNotice {
			texts = append(texts, block.Text)
		}
	}
	return texts
}

func commandNames(cmds []*slashCommand) []string {
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.Name
	}
	return names
}

// ── registry tests ──────────────────────────────────────────────────────────

func TestSlashRegistry_RegisterAndDispatch(t *testing.T) {
	reg := newSlashRegistry()
	called := false
	reg.Register(slashCommand{
		Name:        "test",
		Description: "a test command",
		Handler: func(_ *AppModel, _ string) tea.Cmd {
			called = true
			return nil
		},
	})

	app := newTestApp(t)

	// Known command.
	called = false
	cmd := reg.Dispatch(app, "/test")
	if cmd != nil {
		cmd()
	}
	if !called {
		t.Error("/test did not invoke handler")
	}

	// Known command via alias.
	reg.Register(slashCommand{
		Name:    "greet",
		Aliases: []string{"hi"},
		Handler: func(_ *AppModel, _ string) tea.Cmd {
			called = true
			return nil
		},
	})
	called = false
	cmd = reg.Dispatch(app, "/hi")
	if cmd != nil {
		cmd()
	}
	if !called {
		t.Error("alias /hi did not invoke handler")
	}
}

func TestSlashRegistry_UnknownCommand(t *testing.T) {
	app := newTestApp(t)
	dispatchSlash(t, app, "/nonexistent")
	texts := blockTexts(&app.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "Unknown command:") && strings.Contains(t, "nonexistent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unknown command notice, got: %v", texts)
	}
}

func TestSlashRegistry_EmptyName(t *testing.T) {
	reg := newSlashRegistry()
	app := newTestApp(t)
	cmd := reg.Dispatch(app, "/")
	if cmd != nil {
		t.Error("/ with no name should return nil")
	}
}

func TestSlashRegistry_CommandsIsOrdered(t *testing.T) {
	reg := newSlashRegistry()
	cmds := reg.Commands()
	if len(cmds) < 6 {
		t.Fatalf("expected at least 6 built-in commands, got %d", len(cmds))
	}
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.Name
	}
	// Verify /help is last (registered last).
	if names[len(names)-1] != "help" {
		t.Errorf("last command should be help, got %s", names[len(names)-1])
	}
}

// ── command handler tests ────────────────────────────────────────────────────

func TestSlashSkills_NoSkills(t *testing.T) {
	app := newTestApp(t)
	// info.Capabilities is empty by default.
	dispatchSlash(t, app, "/skills")
	texts := blockTexts(&app.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "No skills loaded") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'No skills loaded' notice, got: %v", texts)
	}
}

func TestSlashSkills_WithSkills(t *testing.T) {
	app := newTestApp(t)
	app.info.Capabilities = []CapabilityCategory{
		{
			Kind:    "skill",
			Support: SupportSupported,
			Source:  "coragent",
			Items: []CapabilityItem{
				{Name: "review", Source: "project", Detail: "Code review skill"},
				{Name: "refactor", Source: "user", Detail: "Refactoring skill"},
			},
		},
	}
	dispatchSlash(t, app, "/skills")
	texts := blockTexts(&app.transcript)
	foundReview := false
	foundRefactor := false
	for _, t := range texts {
		if strings.Contains(t, "review") && strings.Contains(t, "project") {
			foundReview = true
		}
		if strings.Contains(t, "refactor") && strings.Contains(t, "user") {
			foundRefactor = true
		}
	}
	if !foundReview || !foundRefactor {
		t.Errorf("expected skill entries, got: %v", texts)
	}
}

func TestSlashContext_NoUsage(t *testing.T) {
	app := newTestApp(t)
	app.usage = nil
	dispatchSlash(t, app, "/context")
	texts := blockTexts(&app.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "No context usage data available") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected no-usage notice, got: %v", texts)
	}
}

func TestSlashContext_WithUsage(t *testing.T) {
	app := newTestApp(t)
	app.usage = &ContextUsage{
		Used:   85000,
		Source: "measured",
		Round:  3,
		Window: OptionalCount{Known: true, Value: 200000},
	}
	dispatchSlash(t, app, "/context")
	texts := blockTexts(&app.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "42%") || strings.Contains(t, "85.0k") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected usage stats, got: %v", texts)
	}
}

func TestSlashMode_ValidMode(t *testing.T) {
	app := newTestApp(t)
	dispatchSlash(t, app, "/mode plan")
	port := app.port.(*fakeSessionPort)
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.modes) != 1 || port.modes[0] != ModePlan {
		t.Fatalf("modes = %v, want [plan]", port.modes)
	}
}

func TestSlashMode_NoArg(t *testing.T) {
	app := newTestApp(t)
	dispatchSlash(t, app, "/mode")
	texts := blockTexts(&app.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "Valid modes:") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected valid modes notice, got: %v", texts)
	}
}

func TestSlashMode_InvalidMode(t *testing.T) {
	app := newTestApp(t)
	dispatchSlash(t, app, "/mode invalid")
	texts := blockTexts(&app.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "Unknown mode:") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unknown mode notice, got: %v", texts)
	}
}

func TestSlashMode_NotChangeable(t *testing.T) {
	app := newTestApp(t)
	app.info.ModeChangeable = false
	dispatchSlash(t, app, "/mode plan")
	texts := blockTexts(&app.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "Mode switching is not available") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected not-available notice, got: %v", texts)
	}
}

func TestSlashClear(t *testing.T) {
	app := newTestApp(t)
	app.transcript.AddNotice("some prior notice", app.clock.Now())
	if len(app.transcript.Blocks) == 0 {
		t.Fatal("transcript should have blocks before clear")
	}
	dispatchSlash(t, app, "/clear")
	if len(app.transcript.Blocks) != 0 {
		t.Fatalf("transcript should be empty after clear, got %d blocks", len(app.transcript.Blocks))
	}
}

func TestSlashHelp(t *testing.T) {
	app := newTestApp(t)
	dispatchSlash(t, app, "/help")
	texts := blockTexts(&app.transcript)
	foundExit := false
	foundHelp := false
	for _, t := range texts {
		if strings.Contains(t, "/exit") && strings.Contains(t, "Quit") {
			foundExit = true
		}
		if strings.Contains(t, "/help") && strings.Contains(t, "available commands") {
			foundHelp = true
		}
	}
	if !foundExit || !foundHelp {
		t.Errorf("expected help entries for /exit and /help, got: %v", texts)
	}
}

func TestSlashAliases(t *testing.T) {
	app := newTestApp(t)
	dispatchSlash(t, app, "/quit")
	// /quit is an alias for /exit. beginQuit sets runState to RunQuitting.
	if app.runState != RunQuitting {
		t.Errorf("runState after /quit = %v, want RunQuitting", app.runState)
	}
}

// ── integration test ─────────────────────────────────────────────────────────

func TestSlashInputDoesNotStartRun(t *testing.T) {
	model, port := newReadyApp(t, 120, 36)

	// Submit /help — a slash command should NOT trigger a run.
	model.composer.SetValue("/help")
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command != nil {
		msg := command()
		if _, ok := msg.(runOpenedMsg); ok {
			t.Error("/help triggered a run instead of being dispatched as slash command")
		}
	}

	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.runInputs) > 0 {
		t.Errorf("run was started with input %q, should have been intercepted", port.runInputs)
	}
}

func TestExitCommandAlwaysWorks(t *testing.T) {
	// /exit should work even during booting.
	port := &fakeSessionPort{stream: make(chan UIEvent, 32)}
	clock := &fakeClock{now: time.Date(2026, 7, 11, 14, 32, 0, 0, time.UTC)}
	model := NewAppModel(port, WithClock(clock), WithVisualMode(VisualMode{Color: ColorNoColor, ReducedMotion: true}))
	// Don't call Init/startup — keep state as RunBooting.
	model.composer.SetValue("/exit")
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.runState != RunQuitting {
		t.Errorf("runState after /exit during booting = %v, want RunQuitting", model.runState)
	}
	if command == nil {
		t.Error("/exit during booting should return a quit command")
	}
}

func TestNonExitSlashCommandBlockedDuringBooting(t *testing.T) {
	port := &fakeSessionPort{stream: make(chan UIEvent, 32)}
	clock := &fakeClock{now: time.Date(2026, 7, 11, 14, 32, 0, 0, time.UTC)}
	model := NewAppModel(port, WithClock(clock), WithVisualMode(VisualMode{Color: ColorNoColor, ReducedMotion: true}))
	// Don't call Init/startup — keep state as RunBooting.
	model.composer.SetValue("/help")
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Non-exit commands should be silently ignored during booting.
	if model.runState == RunQuitting {
		t.Error("/help during booting should not quit")
	}
	if command != nil {
		// Verify it's not a run command.
		msg := command()
		if _, ok := msg.(runOpenedMsg); ok {
			t.Error("/help during booting incorrectly triggered a run")
		}
	}
}

// ── suggestion tests ─────────────────────────────────────────────────────────

func TestMatchPrefix_ExactMatch(t *testing.T) {
	reg := newSlashRegistry()
	matches := reg.MatchPrefix("exit")
	if len(matches) != 1 || matches[0].Name != "exit" {
		t.Fatalf("MatchPrefix(exit) = %v, want [exit]", commandNames(matches))
	}
}

func TestMatchPrefix_PartialMatch(t *testing.T) {
	reg := newSlashRegistry()
	matches := reg.MatchPrefix("sk")
	if len(matches) != 1 || matches[0].Name != "skills" {
		t.Fatalf("MatchPrefix(sk) = %v, want [skills]", commandNames(matches))
	}
}

func TestMatchPrefix_AliasMatch(t *testing.T) {
	reg := newSlashRegistry()
	matches := reg.MatchPrefix("qu")
	if len(matches) != 1 || matches[0].Name != "exit" {
		t.Fatalf("MatchPrefix(qu) = %v, want [exit] (via quit alias)", commandNames(matches))
	}
}

func TestMatchPrefix_CaseInsensitive(t *testing.T) {
	reg := newSlashRegistry()
	matches := reg.MatchPrefix("EX")
	if len(matches) != 1 || matches[0].Name != "exit" {
		t.Fatalf("MatchPrefix(EX) = %v, want [exit]", commandNames(matches))
	}
}

func TestMatchPrefix_NoMatches(t *testing.T) {
	reg := newSlashRegistry()
	matches := reg.MatchPrefix("zzz")
	if len(matches) != 0 {
		t.Fatalf("MatchPrefix(zzz) = %v, want empty", commandNames(matches))
	}
}

func TestMatchPrefix_EmptyShowsAll(t *testing.T) {
	reg := newSlashRegistry()
	all := reg.Commands()
	matches := reg.MatchPrefix("")
	if len(matches) != len(all) {
		t.Fatalf("MatchPrefix('') = %d, want %d (all commands)", len(matches), len(all))
	}
}

func TestUpdateSuggestions_SlashActivates(t *testing.T) {
	reg := newSlashRegistry()
	var s slashSuggestState
	s.updateSuggestions(reg, "/")
	if !s.active {
		t.Error("suggestions should be active when composer starts with /")
	}
	if len(s.matches) == 0 {
		t.Error("suggestions should list all commands when only / is typed")
	}
}

func TestUpdateSuggestions_NoSlashDeactivates(t *testing.T) {
	reg := newSlashRegistry()
	var s slashSuggestState
	s.active = true
	s.updateSuggestions(reg, "hello")
	if s.active {
		t.Error("suggestions should deactivate when text does not start with /")
	}
}

func TestUpdateSuggestions_FiltersByPrefix(t *testing.T) {
	reg := newSlashRegistry()
	var s slashSuggestState
	s.updateSuggestions(reg, "/co")
	if !s.active {
		t.Fatal("suggestions should be active for /co")
	}
	names := commandNames(s.matches)
	foundContext := false
	for _, n := range names {
		if n == "context" {
			foundContext = true
			break
		}
	}
	if !foundContext {
		t.Fatalf("expected 'context' in matches for /co, got: %v", names)
	}
}

func TestUpdateSuggestions_AfterSpaceShowsAll(t *testing.T) {
	reg := newSlashRegistry()
	var s slashSuggestState
	s.updateSuggestions(reg, "/exit ")
	if !s.active {
		t.Error("suggestions should stay active after space for arg entry")
	}
}

func TestAcceptSlashSuggestion_CompletesText(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.composer.SetValue("/sk")
	model.slashSuggest.updateSuggestions(model.slash, "/sk")
	if !model.slashSuggest.active {
		t.Fatal("suggestions should be active for /sk")
	}
	if model.slashSuggest.selectedCommand().Name != "skills" {
		t.Fatalf("selected = %s, want skills", model.slashSuggest.selectedCommand().Name)
	}
	_ = model.acceptSlashSuggestion()
	value := model.composer.Value()
	if value != "/skills " {
		t.Fatalf("composer value after Tab = %q, want '/skills '", value)
	}
	if model.slashSuggest.active {
		t.Error("suggestions should be dismissed after Tab completion")
	}
}

func TestAcceptSlashSuggestion_KeepsArguments(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.composer.SetValue("/mo plan")
	model.slashSuggest.updateSuggestions(model.slash, "/mo")
	_ = model.acceptSlashSuggestion()
	value := model.composer.Value()
	if value != "/mode plan " {
		t.Fatalf("composer value = %q, want '/mode plan '", value)
	}
}

func TestEnterWithSuggestionAutocompletes(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.composer.SetValue("/sk")
	model.slashSuggest.updateSuggestions(model.slash, "/sk")
	if !model.slashSuggest.active {
		t.Fatal("suggestions should be active for /sk")
	}
	// Simulate pressing Enter while suggestions are active.
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Suggestions should be dismissed.
	if model.slashSuggest.active {
		t.Error("suggestions should be dismissed after Enter")
	}
	// The composer should have been auto-completed before submission.
	// After submitDraft dispatches, composer is Reset() to empty.
	value := model.composer.Value()
	if value != "" {
		t.Fatalf("composer should be empty after dispatch, got %q", value)
	}
	// Verify the command was dispatched — "No skills loaded" should appear.
	texts := blockTexts(&model.transcript)
	found := false
	for _, t := range texts {
		if strings.Contains(t, "No skills loaded") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'No skills loaded' notice after Enter-autocomplete, got: %v", texts)
	}
	// cmd should NOT be a runOpenedMsg — it should be the slash command result.
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(runOpenedMsg); ok {
			t.Error("Enter with suggestion should not start a run")
		}
	}
}

func TestEnterPreservesArguments(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.composer.SetValue("/mo plan")
	model.slashSuggest.updateSuggestions(model.slash, "/mo")
	if !model.slashSuggest.active {
		t.Fatal("suggestions should be active for /mo")
	}
	// Press Enter — should auto-complete /mo to /mode but keep " plan".
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Execute the returned command so the async mode switch happens.
	if cmd != nil {
		msg := cmd()
		model.Update(msg)
	}
	port := model.port.(*fakeSessionPort)
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.modes) != 1 || port.modes[0] != ModePlan {
		t.Fatalf("modes = %v, want [plan]", port.modes)
	}
}

func TestSuggestionsDismissedOnEsc(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.composer.SetValue("/sk")
	model.slashSuggest.updateSuggestions(model.slash, "/sk")
	if !model.slashSuggest.active {
		t.Fatal("suggestions should be active")
	}
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if model.slashSuggest.active {
		t.Error("suggestions should be dismissed on Esc")
	}
	if cmd != nil {
		t.Error("Esc should return nil command for suggestion dismiss")
	}
}
