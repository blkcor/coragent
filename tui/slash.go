package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// slashSuggestState tracks the active slash-command suggestion dropdown.
type slashSuggestState struct {
	active    bool
	matches   []*slashCommand
	selected  int
	// suppressed prevents reactivation after Tab completion until the
	// command word changes or the input no longer starts with "/".
	suppressed bool
	lastQuery string
}

// updateSuggestions recomputes matching slash commands from the composer value.
// It activates the dropdown when the text starts with "/" and there are matches,
// unless the dropdown was suppressed by a prior Tab completion and the command
// word has not changed.
func (s *slashSuggestState) updateSuggestions(reg *slashRegistry, composerValue string) {
	trimmed := strings.TrimSpace(composerValue)
	if !strings.HasPrefix(trimmed, "/") {
		s.active = false
		s.matches = nil
		s.selected = 0
		s.suppressed = false
		s.lastQuery = ""
		return
	}

	query := strings.TrimPrefix(trimmed, "/")
	// Split on space — we only match against the first word (command name).
	if spaceIdx := strings.Index(query, " "); spaceIdx >= 0 {
		query = query[:spaceIdx]
	}

	// After Tab completion the dropdown is suppressed. Only reactivate when
	// the user edits the command word (e.g. backspaces to change it).
	if s.suppressed {
		if query == s.lastQuery {
			s.active = false
			return
		}
		s.suppressed = false
	}
	s.lastQuery = query

	if query == "" {
		// Show all commands when only "/" is typed.
		s.matches = reg.Commands()
	} else {
		s.matches = reg.MatchPrefix(query)
	}

	s.active = len(s.matches) > 0
	if s.selected >= len(s.matches) {
		s.selected = 0
	}
}

// selectedCommand returns the currently selected command, or nil.
func (s *slashSuggestState) selectedCommand() *slashCommand {
	if !s.active || len(s.matches) == 0 || s.selected >= len(s.matches) {
		return nil
	}
	return s.matches[s.selected]
}

// ── registry ────────────────────────────────────────────────────────────────

// slashHandler is the function signature for a slash command. It receives the
// AppModel for state access and the whitespace-trimmed argument string (everything
// after the command name). It returns an optional tea.Cmd for side effects.
type slashHandler func(app *AppModel, args string) tea.Cmd

type slashCommand struct {
	Name        string
	Aliases     []string
	Description string
	Handler     slashHandler
	Kind        string // "builtin" or "skill"
	Source      string // "user" or "project" (only meaningful for skill entries)
}

type slashRegistry struct {
	commands map[string]*slashCommand
	ordered  []*slashCommand // stable registration order for /help
}

func newSlashRegistry() *slashRegistry {
	reg := &slashRegistry{
		commands: make(map[string]*slashCommand),
	}
	registerCommands(reg)
	return reg
}

// Register adds a command to the registry. Registering a duplicate name
// overwrites the previous entry.
func (r *slashRegistry) Register(cmd slashCommand) {
	if _, exists := r.commands[cmd.Name]; !exists {
		r.ordered = append(r.ordered, &cmd)
	}
	r.commands[cmd.Name] = &cmd
	for _, alias := range cmd.Aliases {
		r.commands[alias] = &cmd
	}
}

// RegisterSkills adds skill entries to the registry from capability items.
// Skills that collide with an already-registered name are skipped (built-in
// commands take precedence). Duplicate calls with the same skill set are
// idempotent.
func (r *slashRegistry) RegisterSkills(items []CapabilityItem) {
	for _, item := range items {
		name := item.Name
		if _, exists := r.commands[name]; exists {
			// Built-in commands take precedence — skip silently.
			continue
		}
		desc := item.Detail
		if desc == "" {
			desc = item.Name
		}
		cmd := slashCommand{
			Name:        name,
			Description: desc,
			Kind:        "skill",
			Source:      item.Source,
			// No Handler — skill commands route to the agent run, not local dispatch.
		}
		r.commands[name] = &cmd
		r.ordered = append(r.ordered, &cmd)
	}
}

// Lookup returns the registered command for the given name, or nil if no
// command matches.
func (r *slashRegistry) Lookup(name string) *slashCommand {
	return r.commands[name]
}

// Dispatch looks up a command by name and invokes its handler. Returns nil if
// the command is not found, leaving notice output to the caller.
func (r *slashRegistry) Dispatch(app *AppModel, input string) tea.Cmd {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		return nil
	}

	parts := strings.SplitN(trimmed[1:], " ", 2)
	name := strings.TrimSpace(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	if name == "" {
		return nil
	}

	cmd, ok := r.commands[name]
	if !ok {
		app.transcript.AddNotice(fmt.Sprintf("Unknown command: /%s. Try /help.", name), app.clock.Now())
		app.noteLiveOutput()
		return nil
	}

	return cmd.Handler(app, args)
}

// Commands returns the ordered list of registered commands (deduplicated by
// primary name).
func (r *slashRegistry) Commands() []*slashCommand {
	return r.ordered
}

// MatchPrefix returns commands whose name or an alias contains the given
// substring (case-insensitive). Prefix matches appear before other substring
// matches. Results are otherwise in registration order.
func (r *slashRegistry) MatchPrefix(prefix string) []*slashCommand {
	lower := strings.ToLower(prefix)
	if lower == "" {
		return r.Commands()
	}
	seen := make(map[string]bool)
	var prefixMatches, substringMatches []*slashCommand
	for _, cmd := range r.ordered {
		if seen[cmd.Name] {
			continue
		}
		nameLower := strings.ToLower(cmd.Name)
		if strings.HasPrefix(nameLower, lower) {
			seen[cmd.Name] = true
			prefixMatches = append(prefixMatches, cmd)
			continue
		}
		if strings.Contains(nameLower, lower) {
			seen[cmd.Name] = true
			substringMatches = append(substringMatches, cmd)
			continue
		}
		for _, alias := range cmd.Aliases {
			aliasLower := strings.ToLower(alias)
			if strings.HasPrefix(aliasLower, lower) {
				seen[cmd.Name] = true
				prefixMatches = append(prefixMatches, cmd)
				break
			}
			if strings.Contains(aliasLower, lower) {
				seen[cmd.Name] = true
				substringMatches = append(substringMatches, cmd)
				break
			}
		}
	}
	return append(prefixMatches, substringMatches...)
}

// isExitCommand reports whether the given input is an /exit or /quit command.
func isExitCommand(input string) bool {
	trimmed := strings.TrimSpace(input)
	name, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "/"), " ")
	return name == "exit" || name == "quit"
}

// ── command handlers ────────────────────────────────────────────────────────

func registerCommands(reg *slashRegistry) {
	reg.Register(slashCommand{
		Name:        "exit",
		Aliases:     []string{"quit"},
		Description: "Quit coragent",
		Kind:        "builtin",
		Handler: func(app *AppModel, _ string) tea.Cmd {
			return app.beginQuit()
		},
	})

	reg.Register(slashCommand{
		Name:        "skills",
		Aliases:     []string{"available-skills"},
		Description: "List loaded skills",
		Kind:        "builtin",
		Handler:     slashSkills,
	})

	reg.Register(slashCommand{
		Name:        "context",
		Aliases:     []string{"usage"},
		Description: "Show context window usage",
		Kind:        "builtin",
		Handler:     slashContext,
	})

	reg.Register(slashCommand{
		Name:        "mode",
		Description: "Switch permission mode (default, auto, plan, bypass)",
		Kind:        "builtin",
		Handler:     slashMode,
	})

	reg.Register(slashCommand{
		Name:        "clear",
		Description: "Clear the transcript",
		Kind:        "builtin",
		Handler: func(app *AppModel, _ string) tea.Cmd {
			app.transcript = NewTranscriptStore()
			return nil
		},
	})

	reg.Register(slashCommand{
		Name:        "help",
		Aliases:     []string{"?"},
		Description: "Show available commands",
		Kind:        "builtin",
		Handler:     slashHelp,
	})
}

func slashSkills(app *AppModel, _ string) tea.Cmd {
	for _, cat := range app.info.Capabilities {
		if cat.Kind != "skill" {
			continue
		}
		if len(cat.Items) == 0 {
			app.transcript.AddNotice("No skills loaded.", app.clock.Now())
			app.noteLiveOutput()
			return nil
		}
		for _, item := range cat.Items {
			source := item.Source
			if source == "" {
				source = "unknown"
			}
			namePart := app.theme.AccentStyle.Render(item.Name)
			scopePart := app.theme.MutedStyle.Render("[" + source + "]")
			descPart := ""
			if item.Detail != "" {
				desc := strings.Join(strings.Fields(item.Detail), " ")
				descPart = app.theme.TextStyle.Render("  " + desc)
			}
			app.transcript.AddRichNotice(namePart+"  "+scopePart+descPart, app.clock.Now())
		}
		app.noteLiveOutput()
		return nil
	}
	app.transcript.AddNotice("No skills loaded.", app.clock.Now())
	app.noteLiveOutput()
	return nil
}

func slashContext(app *AppModel, _ string) tea.Cmd {
	if app.usage == nil {
		app.transcript.AddNotice("No context usage data available yet. Start a run.", app.clock.Now())
		app.noteLiveOutput()
		return nil
	}
	usage := app.usage
	label, _ := app.contextUsageLabel()
	source := "measured"
	if usage.Source == "estimated" {
		source = "estimated"
	}
	round := ""
	if usage.Round > 0 {
		round = fmt.Sprintf(" · round %d", usage.Round)
	}
	app.transcript.AddNotice(
		fmt.Sprintf("Context: %s (%s%s)", label, source, round),
		app.clock.Now(),
	)
	app.noteLiveOutput()
	return nil
}

func slashMode(app *AppModel, args string) tea.Cmd {
	if app.port == nil {
		return nil
	}
	validModes := map[string]SessionMode{
		"default":          ModeDefault,
		"auto":             ModeAutoAcceptEdits,
		"auto-accept":      ModeAutoAcceptEdits,
		"plan":             ModePlan,
		"bypass":           ModeBypass,
		"auto-accept-edits": ModeAutoAcceptEdits,
	}
	args = strings.TrimSpace(args)
	if args == "" {
		app.transcript.AddNotice("Valid modes: default, auto, plan, bypass", app.clock.Now())
		app.noteLiveOutput()
		return nil
	}
	mode, ok := validModes[args]
	if !ok {
		app.transcript.AddNotice(
			fmt.Sprintf("Unknown mode: %s. Valid modes: default, auto, plan, bypass", args),
			app.clock.Now(),
		)
		app.noteLiveOutput()
		return nil
	}
	if !app.info.ModeChangeable || app.info.Mode == ModeExternal || app.info.Mode == ModeUnsupported || app.modeChangePending {
		app.transcript.AddNotice("Mode switching is not available in this session.", app.clock.Now())
		app.noteLiveOutput()
		return nil
	}
	app.modeChangePending = true
	return setModeCmd(app.port, mode)
}

func slashHelp(app *AppModel, _ string) tea.Cmd {
	cmds := app.slash.Commands()
	if len(cmds) == 0 {
		app.transcript.AddNotice("No slash commands registered.", app.clock.Now())
		app.noteLiveOutput()
		return nil
	}
	addedDivider := false
	for _, cmd := range cmds {
		// Insert a divider before the first skill entry.
		if cmd.Kind == "skill" && !addedDivider {
			app.transcript.AddNotice("— Skills —", app.clock.Now())
			addedDivider = true
		}
		names := "/" + cmd.Name
		if len(cmd.Aliases) > 0 {
			aliasStrs := make([]string, len(cmd.Aliases))
			for i, a := range cmd.Aliases {
				aliasStrs[i] = "/" + a
			}
			names += " (" + strings.Join(aliasStrs, ", ") + ")"
		}
		app.transcript.AddNotice(names+"  —  "+cmd.Description, app.clock.Now())
	}
	app.noteLiveOutput()
	return nil
}
