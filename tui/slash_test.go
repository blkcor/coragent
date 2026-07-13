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
	if len(matches) < 1 {
		t.Fatalf("MatchPrefix(EX) = %v, want at least [exit]", commandNames(matches))
	}
	// exit should appear first (prefix match) before other substring matches.
	if matches[0].Name != "exit" {
		t.Errorf("MatchPrefix(EX): first match = %q, want exit (prefix before substring)", matches[0].Name)
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

// ── RegisterSkills tests ────────────────────────────────────────────────────

func TestRegisterSkills_AppendsToRegistry(t *testing.T) {
	reg := newSlashRegistry()
	builtinCount := len(reg.Commands())

	items := []CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code changes"},
		{Name: "refactor", Source: "user", Detail: "Refactoring helper"},
	}
	reg.RegisterSkills(items)

	all := reg.Commands()
	if len(all) != builtinCount+2 {
		t.Fatalf("expected %d commands after RegisterSkills, got %d", builtinCount+2, len(all))
	}

	// Skills should be after built-in commands.
	lastTwo := all[len(all)-2:]
	if lastTwo[0].Name != "code-review" || lastTwo[1].Name != "refactor" {
		t.Errorf("skills not appended in order: %v", commandNames(lastTwo))
	}

	// Verify skill entry fields.
	cr := reg.Lookup("code-review")
	if cr == nil || cr.Kind != "skill" || cr.Source != "project" {
		t.Errorf("code-review entry: kind=%q source=%q, want skill/project", cr.Kind, cr.Source)
	}
}

func TestRegisterSkills_CollisionSkipped(t *testing.T) {
	reg := newSlashRegistry()

	// Try to register a skill named "help" — should be skipped.
	items := []CapabilityItem{
		{Name: "help", Source: "project", Detail: "A skill that collides"},
	}
	reg.RegisterSkills(items)

	cmd := reg.Lookup("help")
	if cmd == nil {
		t.Fatal("help should still exist in registry")
	}
	if cmd.Kind != "builtin" {
		t.Errorf("help command kind = %q, want builtin (skill must not overwrite)", cmd.Kind)
	}
	if cmd.Source != "" {
		t.Errorf("help command source = %q, want empty (should not be skill source)", cmd.Source)
	}
}

func TestRegisterSkills_Idempotent(t *testing.T) {
	reg := newSlashRegistry()
	builtinCount := len(reg.Commands())

	items := []CapabilityItem{
		{Name: "review", Source: "project", Detail: "Code review"},
	}
	reg.RegisterSkills(items)
	firstCount := len(reg.Commands())
	if firstCount != builtinCount+1 {
		t.Fatalf("after first call: %d commands, want %d", firstCount, builtinCount+1)
	}

	// Second call with same items should not add duplicates.
	reg.RegisterSkills(items)
	secondCount := len(reg.Commands())
	if secondCount != firstCount {
		t.Errorf("after second call: %d commands, want %d (duplicates)", secondCount, firstCount)
	}
}

// ── MatchPrefix with mixed entries ──────────────────────────────────────────

func TestMatchPrefix_MixedBuiltinAndSkill(t *testing.T) {
	reg := newSlashRegistry()
	reg.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code"},
		{Name: "compile", Source: "user", Detail: "Compile project"},
	})

	// Prefix "co" should match both built-in "context" and skill "code-review".
	matches := reg.MatchPrefix("co")
	names := commandNames(matches)
	if len(names) < 2 {
		t.Fatalf("MatchPrefix(co) = %v, want at least [context, code-review]", names)
	}
	foundContext := false
	foundCodeReview := false
	for _, n := range names {
		if n == "context" {
			foundContext = true
		}
		if n == "code-review" {
			foundCodeReview = true
		}
	}
	if !foundContext || !foundCodeReview {
		t.Errorf("MatchPrefix(co) missing expected: context=%v code-review=%v", foundContext, foundCodeReview)
	}
	// Prefix matches appear before substring matches.
	for _, n := range names[:2] {
		if !strings.HasPrefix(n, "co") {
			t.Errorf("first matches should be prefix matches, got %q", n)
		}
	}
}

func TestMatchPrefix_SkillOnly(t *testing.T) {
	reg := newSlashRegistry()
	reg.RegisterSkills([]CapabilityItem{
		{Name: "refactor", Source: "user", Detail: "Refactoring"},
	})

	matches := reg.MatchPrefix("ref")
	names := commandNames(matches)
	if len(names) != 1 || names[0] != "refactor" {
		t.Fatalf("MatchPrefix(ref) = %v, want [refactor]", names)
	}
}

func TestMatchPrefix_SubstringMatch(t *testing.T) {
	reg := newSlashRegistry()
	reg.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code changes"},
	})

	// /review should match code-review via substring matching.
	matches := reg.MatchPrefix("review")
	names := commandNames(matches)
	if len(names) != 1 || names[0] != "code-review" {
		t.Fatalf("MatchPrefix(review) = %v, want [code-review]", names)
	}
}

func TestMatchPrefix_PrefixBeforeSubstring(t *testing.T) {
	reg := newSlashRegistry()
	reg.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code"},
	})

	// /re should match both context (substring, "context" contains "re")
	// and code-review (substring). But prefix matches come first.
	matches := reg.MatchPrefix("re")
	names := commandNames(matches)
	foundCodeReview := false
	for _, n := range names {
		if n == "code-review" {
			foundCodeReview = true
		}
	}
	if !foundCodeReview {
		t.Errorf("MatchPrefix(re) should include code-review via substring match, got %v", names)
	}
}


// ── submitDraft routing tests ───────────────────────────────────────────────

func TestSubmitDraft_SkillRoutesToAgent(t *testing.T) {
	model, port := newReadyApp(t, 120, 36)

	// Register a skill in the slash registry (simulating handleStartup).
	model.slash.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code changes"},
	})

	// Submit a skill name via the composer.
	model.composer.SetValue("/code-review")
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("submitDraft returned nil for skill command")
	}
	// Execute the command to trigger port.Run().
	cmd()

	port.mu.Lock()
	defer port.mu.Unlock()

	// The skill should have been submitted as an agent run, not dispatched.
	if len(port.runInputs) != 1 {
		t.Fatalf("expected 1 run input, got %d", len(port.runInputs))
	}
	if port.runInputs[0] != "/code-review" {
		t.Errorf("run input = %q, want '/code-review'", port.runInputs[0])
	}

	// Verify the "Loaded skill" notice was added to the transcript.
	texts := blockTexts(&model.transcript)
	found := false
	for _, txt := range texts {
		if strings.Contains(txt, "Loaded skill:") && strings.Contains(txt, "code-review") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Loaded skill: code-review' notice, got: %v", texts)
	}
}

func TestSubmitDraft_BuiltinStillDispatched(t *testing.T) {
	model, port := newReadyApp(t, 120, 36)

	// Even with skills registered, built-in commands should still dispatch locally.
	model.slash.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code"},
	})

	model.composer.SetValue("/help")
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	port.mu.Lock()
	defer port.mu.Unlock()

	// /help should NOT trigger a run.
	if len(port.runInputs) > 0 {
		t.Errorf("/help triggered a run: inputs = %v", port.runInputs)
	}
	// Execute command if present so side effects (notice rendering) complete.
	if cmd != nil {
		cmd()
	}
	// Verify that /help produced output in the transcript.
	texts := blockTexts(&model.transcript)
	foundHelp := false
	for _, txt := range texts {
		if strings.Contains(txt, "/help") && strings.Contains(txt, "available commands") {
			foundHelp = true
			break
		}
	}
	if !foundHelp {
		t.Errorf("expected help notice in transcript, got: %v", texts)
	}
}

func TestSubmitDraft_UnknownStillShowsNotice(t *testing.T) {
	model, port := newReadyApp(t, 120, 36)

	model.composer.SetValue("/nonexistent-cmd")
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}

	port.mu.Lock()
	defer port.mu.Unlock()

	// Unknown commands should not trigger a run.
	if len(port.runInputs) > 0 {
		t.Errorf("unknown command triggered a run: inputs = %v", port.runInputs)
	}

	// Should show "Unknown command" notice.
	texts := blockTexts(&model.transcript)
	found := false
	for _, txt := range texts {
		if strings.Contains(txt, "Unknown command:") && strings.Contains(txt, "nonexistent-cmd") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unknown command notice, got: %v", texts)
	}
}

// ── integration test ────────────────────────────────────────────────────────

func TestSlashStartupRegistersSkills(t *testing.T) {
	port := &fakeSessionPort{
		info: SessionInfo{
			Project:        "coragent",
			Model:          "gpt-test",
			Mode:           ModeDefault,
			ModeChangeable: true,
			Sandbox:        "os",
			Context:        "ctx 0%",
			Capabilities: []CapabilityCategory{
				{
					Kind:    "skill",
					Support: SupportSupported,
					Source:  "coragent",
					Items: []CapabilityItem{
						{Name: "review", Source: "project", Detail: "Code review skill"},
						{Name: "refactor", Source: "user", Detail: "Refactoring skill"},
					},
				},
			},
		},
		stream: make(chan UIEvent, 32),
	}
	clock := &fakeClock{now: time.Date(2026, 7, 11, 14, 32, 0, 0, time.UTC)}
	model := NewAppModel(port, WithClock(clock), WithVisualMode(VisualMode{Color: ColorNoColor, ReducedMotion: true}))
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	// Simulate startup completion (same flow as newTestApp).
	initCmd := model.Init()
	if initCmd == nil {
		t.Fatal("Init returned nil")
	}
	msg := initCmd()
	model.Update(msg)

	// After startup, skills should be registered as slash commands.
	review := model.slash.Lookup("review")
	if review == nil {
		t.Fatal("review skill not registered in slash registry")
	}
	if review.Kind != "skill" || review.Source != "project" {
		t.Errorf("review entry: kind=%q source=%q", review.Kind, review.Source)
	}

	refactor := model.slash.Lookup("refactor")
	if refactor == nil {
		t.Fatal("refactor skill not registered in slash registry")
	}
	if refactor.Kind != "skill" || refactor.Source != "user" {
		t.Errorf("refactor entry: kind=%q source=%q", refactor.Kind, refactor.Source)
	}

	// Typing "/" should show both built-in commands and skills.
	model.composer.SetValue("/")
	model.slashSuggest.updateSuggestions(model.slash, "/")
	if !model.slashSuggest.active {
		t.Fatal("suggestions should be active for /")
	}
	names := commandNames(model.slashSuggest.matches)
	if len(names) < 8 {
		t.Errorf("expected at least 8 matches (6 builtins + 2 skills), got %d: %v", len(names), names)
	}

	// Clear suggestion state so the Enter key bypasses the suggestion handler
	// and goes to the default submitDraft path.
	model.slashSuggest.active = false
	model.slashSuggest.matches = nil
	model.slashSuggest.selected = 0

	// Submitting /review should route to agent run.
	model.composer.SetValue("/review")
	_, submitCmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if submitCmd == nil {
		t.Fatal("submitDraft returned nil for skill command")
	}
	// Execute the command to trigger port.Run().
	submitCmd()

	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.runInputs) != 1 || port.runInputs[0] != "/review" {
		t.Errorf("run inputs = %v, want [/review]", port.runInputs)
	}
}

// ── suppression after Tab tests ──────────────────────────────────────────────────────

func TestTabSuppressesSuggestionsAfterCompletion(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.slash.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code changes"},
	})

	// Type /co and Tab-complete to /code-review.
	model.composer.SetValue("/co")
	model.slashSuggest.updateSuggestions(model.slash, "/co")
	if !model.slashSuggest.active {
		t.Fatal("suggestions should be active for /co")
	}

	// Navigate to the code-review skill entry (after built-in commands).
	for model.slashSuggest.selectedCommand() != nil && model.slashSuggest.selectedCommand().Name != "code-review" {
		model.slashSuggest.selected++
	}

	_ = model.acceptSlashSuggestion()
	value := model.composer.Value()
	if value != "/code-review " {
		t.Fatalf("after Tab: composer value = %q, want '/code-review '", value)
	}
	if model.slashSuggest.active {
		t.Error("suggestions should be dismissed after Tab")
	}

	// Typing more text after Tab should NOT reactivate the dropdown.
	model.composer.SetValue("/code-review fix the bug")
	model.slashSuggest.updateSuggestions(model.slash, "/code-review fix the bug")
	if model.slashSuggest.active {
		t.Error("suggestions should NOT reactivate when appending after Tab completion")
	}
}

func TestTabSuppressionClearsWhenCommandEdited(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.slash.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code changes"},
	})

	// Tab-complete /co to /code-review.
	model.composer.SetValue("/co")
	model.slashSuggest.updateSuggestions(model.slash, "/co")
	// Navigate to the code-review skill entry.
	for model.slashSuggest.selectedCommand() != nil && model.slashSuggest.selectedCommand().Name != "code-review" {
		model.slashSuggest.selected++
	}
	_ = model.acceptSlashSuggestion()
	if model.slashSuggest.active {
		t.Error("suggestions should be dismissed after Tab")
	}

	// Backspacing to change the command word should allow reactivation.
	model.composer.SetValue("/code-revie")
	model.slashSuggest.updateSuggestions(model.slash, "/code-revie")
	if !model.slashSuggest.active {
		t.Error("suggestions should reactivate when the command word is edited")
	}
}

func TestTabSuppressionClearsOnNewSlash(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	model.slash.RegisterSkills([]CapabilityItem{
		{Name: "code-review", Source: "project", Detail: "Review code changes"},
	})

	// Tab-complete /co to /code-review.
	model.composer.SetValue("/co")
	model.slashSuggest.updateSuggestions(model.slash, "/co")
	// Navigate to the code-review skill entry.
	for model.slashSuggest.selectedCommand() != nil && model.slashSuggest.selectedCommand().Name != "code-review" {
		model.slashSuggest.selected++
	}
	_ = model.acceptSlashSuggestion()

	// Clear and start a new slash.
	model.composer.SetValue("hello")
	model.slashSuggest.updateSuggestions(model.slash, "hello")
	if model.slashSuggest.active {
		t.Error("suggestions should not be active for non-slash input")
	}

	// New slash should show suggestions again.
	model.composer.SetValue("/")
	model.slashSuggest.updateSuggestions(model.slash, "/")
	if !model.slashSuggest.active {
		t.Error("suggestions should reactivate for a fresh '/' input after Tab suppression")
	}
}
