## Why

The TUI frontend exposes no local command-surface — every keystroke that isn't a Ctrl/Alt chord goes to the model. Users need a fast, keyboard-first vocabulary for TUI-local actions (quit, inspect state, list skills, switch modes) without involving the agent loop. Implementing this as slash-prefixed composer input mirrors the `/name` convention already used for skill invocation and gives the TUI parity with other agent harnesses.

## What Changes

- Intercept composer input that begins with `/` before it reaches `submitDraft`
- Maintain a local registry of slash commands, each with a name, description, and handler
- **New commands:**
  - `/exit` (alias `/quit`) — cleanly quit the TUI (same as Ctrl+Q)
  - `/skills` (alias `/available-skills`) — list loaded skills with source and description
  - `/context` (alias `/usage`) — show current context-window usage statistics
  - `/mode <name>` — switch permission mode (default, auto-accept-edits, plan, bypass)
  - `/clear` — clear the transcript view locally
  - `/help` (alias `/?`) — list all available slash commands with short descriptions
- Unknown slash commands are rendered as a notice in the transcript
- The slash command registry is extensible — adding a command is a single registration

## Capabilities

### New Capabilities
- `slash-commands`: TUI-local command dispatch triggered by `/`-prefixed composer input, supporting exit, skill listing, context stats, mode switching, transcript clearing, and help

### Modified Capabilities
<!-- None: all existing capabilities remain unchanged. -->

## Impact

- `tui/app.go` — intercept `/` input in `submitDraft`, new command dispatch methods
- `tui/port.go` — skills are already available in `SessionInfo.Capabilities`; context is already on `AppModel.usage`
- `tui/agent_adapter.go` — no changes needed (SessionPort already exposes SetMode, Describe)
- `cmd/coragent/main.go` — no changes needed (AppModel wires through existing ports)
