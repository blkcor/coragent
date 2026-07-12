package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// slashSuggestState tracks the active slash-command suggestion dropdown.
type slashSuggestState struct {
	active   bool
	matches  []*slashCommand
	selected int
}

// updateSuggestions recomputes matching slash commands from the composer value.
// It activates the dropdown when the text starts with "/" and there are matches.
func (s *slashSuggestState) updateSuggestions(reg *slashRegistry, composerValue string) {
	trimmed := strings.TrimSpace(composerValue)
	if !strings.HasPrefix(trimmed, "/") {
		s.active = false
		s.matches = nil
		s.selected = 0
		return
	}

	query := strings.TrimPrefix(trimmed, "/")
	// Split on space — we only match against the first word (command name).
	if spaceIdx := strings.Index(query, " "); spaceIdx >= 0 {
		query = query[:spaceIdx]
	}

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

// MatchPrefix returns commands whose name or an alias starts with the given
// prefix (case-insensitive). Results are in registration order.
func (r *slashRegistry) MatchPrefix(prefix string) []*slashCommand {
	lower := strings.ToLower(prefix)
	seen := make(map[string]bool)
	var matches []*slashCommand
	for _, cmd := range r.ordered {
		if seen[cmd.Name] {
			continue
		}
		if strings.HasPrefix(strings.ToLower(cmd.Name), lower) {
			seen[cmd.Name] = true
			matches = append(matches, cmd)
			continue
		}
		for _, alias := range cmd.Aliases {
			if strings.HasPrefix(strings.ToLower(alias), lower) {
				seen[cmd.Name] = true
				matches = append(matches, cmd)
				break
			}
		}
	}
	return matches
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
		Handler: func(app *AppModel, _ string) tea.Cmd {
			return app.beginQuit()
		},
	})

	reg.Register(slashCommand{
		Name:        "skills",
		Aliases:     []string{"available-skills"},
		Description: "List loaded skills",
		Handler:     slashSkills,
	})

	reg.Register(slashCommand{
		Name:        "context",
		Aliases:     []string{"usage"},
		Description: "Show context window usage",
		Handler:     slashContext,
	})

	reg.Register(slashCommand{
		Name:        "mode",
		Description: "Switch permission mode (default, auto, plan, bypass)",
		Handler:     slashMode,
	})

	reg.Register(slashCommand{
		Name:        "clear",
		Description: "Clear the transcript",
		Handler: func(app *AppModel, _ string) tea.Cmd {
			app.transcript = NewTranscriptStore()
			return nil
		},
	})

	reg.Register(slashCommand{
		Name:        "help",
		Aliases:     []string{"?"},
		Description: "Show available commands",
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
	for _, cmd := range cmds {
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
