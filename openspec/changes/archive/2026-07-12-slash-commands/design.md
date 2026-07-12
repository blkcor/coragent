## Context

The TUI frontend (`tui/app.go`) routes all non-chord keyboard input through the composer textarea to `submitDraft`, which starts a new agent run. There is no local command surface — every plaintext keystroke goes to the model.

The SDK already provides all the data needed for local commands: `SessionPort` exposes `Describe` (skills via `Capabilities`), `SetMode` (mode switching), and `Close` (shutdown). The `AppModel` already holds `usage` (`*ContextUsage`) from the event stream.

The `/name` convention for skill invocation is already parsed at the harness level (`internal/skill/parse.go` handles it in `Session.Run`), but slash commands for TUI-local actions are absent.

## Goals / Non-Goals

**Goals:**
- Provide a composer-triggered slash-command registry for TUI-local actions
- Support at minimum: `/exit`, `/skills`, `/context`, `/mode`, `/clear`, `/help`
- Each command can emit a transcript notice, change local state, or trigger a Cmd
- Unknown commands produce a helpful notice
- The registry is extensible: adding a command requires one registration call

**Non-Goals:**
- No changing the harness or SDK — slash commands are purely a TUI concern
- No command-line argument parsing (no `--flags`, no sub-sub-commands)
- No plugin/hook integration for slash commands (v1)
- No model involvement in slash command execution
- No changes to `internal/skill/parse.go` skill `/name` parsing

## Decisions

### 1. Slash command registry as a struct with method dispatch

Commands are registered in a `slashRegistry` map keyed by command name (including aliases). Each entry holds a description string and a handler function `func(app *AppModel, args string) tea.Cmd`.

**Why a map over a switch statement:** Adding a command means calling `reg.Register("name", "description", handler)` — a single-line addition. A switch requires modifying the dispatch function body and keeping help text synchronized manually.

**Why not a `[]slashCommand` slice:** The primary access pattern is exact-name lookup, which a map gives in O(1). A slice would need linear scan.

### 2. Interception point: `submitDraft` method

The composer value is checked for a `/` prefix in `submitDraft`, before any agent involvement:

```
submitDraft()
  ├─ draft starts with "/" → dispatchSlash(draft)
  │   ├─ known command → handler(app, args)
  │   └─ unknown → transcript notice "Unknown command: /foo. Try /help."
  └─ otherwise → current behavior (start run)
```

**Why `submitDraft` and not `handleComposerKey`:** The enter key press must commit the current input as a unit. Checking at key-press time would require buffering and partial matching, adding complexity with no benefit.

**Why no prefix character other than `/`:** The `/` prefix is already the skill-invocation convention in this ecosystem. Using it for both model-facing skills (parsed by the harness) and TUI-local commands (parsed by the frontend) is consistent — the frontend commands never reach the harness, so there is no ambiguity.

### 3. Output as transcript notices

Command results are rendered as transcript notices (`model.transcript.AddNotice`). This reuses the existing notice machinery and avoids creating a separate output channel.

**Why not a dedicated "command output" area:** A separate surface would require layout changes and would fight for screen real estate with the transcript. Notices appear inline in the conversation flow, are scrollable, and already handle truncation/rendering.

**Why `AddNotice` and not a blocked "modal" overlay:** Slash commands are fast local lookups, not permission decisions. A modal would block the user from typing the next command while it's visible.

### 4. `/exit` delegates to `beginQuit`

`/exit` calls the same `beginQuit()` path as Ctrl+Q. It does not bypass the quit-drain logic (active runs are cancelled, permission prompts are denied, then the session closes).

### 5. `/skills` reads from `SessionInfo.Capabilities`

Skills are already exposed as `CapabilityCategory` items in the `SessionInfo` struct returned by `Describe`. The `/skills` command filters for `CapabilityKindSkill` entries and formats them for display.

### 6. `/context` reads from `AppModel.usage`

Context usage arrives via the `EventContextUsage` event and is stored in `model.usage`. The `/context` command formats this struct into human-readable output (token count, window size, percentage).

### 7. `/mode <name>` calls `SetMode` on the port

Mode switching is already wired through `cycleMode()` (Shift+Tab) and `setModeCmd`. `/mode` provides an explicit, named alternative: `/mode default`, `/mode auto`, `/mode plan`, `/mode bypass`.

## Risks / Trade-offs

- **Command name collision with future skill names**: A `/skills` command in the TUI will never reach the harness, so it cannot conflict with a hypothetical `skills` SKILL.md. This is correct behavior — TUI commands are intentionally opaque to the agent.
- **Mode change during active permission prompt**: The permission engine already snapshots the mode at request time, so a `/mode` command during a pending request updates the posture for *future* decisions without affecting the open one. The standard `modeChangedMsg` handler applies.
- **Large skill lists**: A project with hundreds of skills could produce a long notice. Mitigation: the notice renderer already handles multi-line notices in the transcript.
