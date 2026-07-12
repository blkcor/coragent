## Context

The TUI has two separate `/`-prefixed systems that don't intersect:

1. **Slash commands** (`tui/slash.go`): A static `slashRegistry` populated in `registerCommands()` at construction time. The `slashSuggestState` watches composer input for `/`, prefix-matches against the registry, and renders a suggestion dropdown. On Enter, `submitDraft()` calls `slash.Dispatch()` which routes to local handler functions. Slash commands never reach the agent.

2. **Skills** (`internal/skill/`, surfaced via `pkg/agent/`): Loaded from disk during `Bootstrap()`, each skill is registered as a tool handler and its metadata injected into `SessionInfo.Capabilities` as a `CapabilityCategory` with `Kind == "skill"`. In raw input (not slash-prefixed), `session.Run()` calls `ParseInvocations()` which detects `/skill-name` tokens, strips them, and injects the skill body as transient context.

The bridge: `app.info.Capabilities` carries skill data after session startup (available via `handleStartup()`). The `/skills` slash command already reads this data for display. Skill invocation via slash needs to: (a) appear in the dropdown, (b) on selection/submission route to an agent run with the skill name so `ParseInvocations` handles it.

## Goals / Non-Goals

**Goals:**
- Skill names appear in the slash suggestion dropdown alongside built-in commands
- Selecting a skill sends it as a normal input (e.g., `/code-review`) through the agent run path where `ParseInvocations` processes it
- Zero changes to `pkg/agent/` or `internal/` — this is a TUI-only integration
- The dropdown remains responsive with any number of skills loaded

**Non-Goals:**
- Skills do NOT get local handler functions — they always route through the agent
- No skill auto-discovery while typing (skills only appear in dropdown after session starts)
- No change to how skills are loaded, parsed, or executed
- No persistence of skill data in the TUI beyond what `SessionInfo` already carries

## Decisions

### Decision 1: Populate slash registry dynamically when capabilities arrive

**Choice:** Add a `RegisterSkills(items []CapabilityItem)` method to `slashRegistry` that creates slash command entries for each skill item. Call it from `handleStartup()` after `app.info.Capabilities` is populated.

**Alternatives considered:**
- *Pre-load skills into the registry at construction* — Rejected because skills aren't known at `NewAppModel()` time. The registry is created before the session starts.
- *Create a separate "skill registry" that slashSuggestState queries independently* — Rejected because it complicates the matching logic (two sources, priority ordering) when a single unified registry already handles prefix matching and deduplication.
- *Replace the registry wholesale with skills included* — Rejected because it loses the built-in command ordering guarantee.

**Rationale:** The `Register` method already exists. Adding a batch variant that handles the skill→slashCommand shape conversion keeps the change minimal. Skills get a sentinel handler that signals "route to agent" rather than executing locally.

### Decision 2: Distinguish skill entries from built-in commands via a flag

**Choice:** Add a `Kind` field to `slashCommand` (`"builtin"` or `"skill"`). In `submitDraft()`, check `Kind`: built-in commands go through `slash.Dispatch()`, skill commands bypass dispatch and submit the raw `/skill-name` input to `port.Run()` (the agent).

**Alternatives considered:**
- *Use a special handler that calls port.Run* — Rejected because slash handlers receive `(app *AppModel, args string)` and the agent run path is tangled with `submitDraft()`'s state transitions (setting `RunRunning`, `runCancel`, `inputHistory`, etc.). Having the skill handler replicate all that state management duplicates logic and creates drift.
- *Make `slash.Dispatch()` return a special "not found but should route to agent" sentinel* — Rejected because it muddies the dispatch contract. The caller already has the composer value; it's cleaner to check before dispatch.

**Rationale:** The routing decision belongs in `submitDraft()` where the full submission context (run state, composer value, port) is available. A simple flag on the command struct makes the branch explicit.

### Decision 3: Skill entries appear after built-in commands in the dropdown

**Choice:** When registering skills, append them to `ordered` after the built-in commands. This keeps `/exit`, `/help`, etc. always at the top regardless of how many skills are loaded.

**Rationale:** Built-in commands are fewer, more critical, and used more frequently. Skills may number in the dozens and should not push safety commands out of the visible suggestion area.

### Decision 4: Deduplicate between skills and built-in commands

**Choice:** If a skill name collides with a built-in command (e.g., a skill named "help"), the built-in takes precedence and the skill is not registered as a slash command. Log a warning.

**Rationale:** Built-in commands have handlers with side effects (mode changes, quit). A skill accidentally named "exit" must not shadow the quit command.

### Decision 5: Show source and description in suggestion rendering

**Choice:** Each suggestion row renders: command name, source badge (only for skills: `[user]` or `[project]`), and description. Built-in commands remain as-is.

**Rationale:** The dropdown already shows command name + description (see `renderSlashSuggestions`). Adding a source badge for skills uses the same `CapabilityItem.Source` field already displayed in `/skills`, helping users distinguish same-named skills from different sources.

## Risks / Trade-offs

- **Large skill sets (>20):** The dropdown caps at 8 visible entries. Users with many skills may need to type a prefix to narrow. This matches the current behavior for commands and is mitigated by prefix matching.
- **Skill name changes after registration:** If skills are reloaded mid-session (not currently supported), the slash registry would be stale. This is a non-issue for v1 since skills are loaded once at startup.
- **Skill names with spaces:** `slashCommand.Name` is the full skill name. The suggestion matching splits on spaces. A skill named "code review" would only match against "code" — `acceptSlashSuggestion()` would still work correctly since it replaces the first word. This edge case is acceptable for v1.

## Open Questions

None — the existing code paths are well-understood and the integration surface is narrow.
