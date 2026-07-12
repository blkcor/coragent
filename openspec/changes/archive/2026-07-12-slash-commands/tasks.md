## 1. Slash command infrastructure

- [x] 1.1 Create `tui/slash.go` with `slashCommand` struct (name, aliases, description, handler) and `slashRegistry` type (map-backed, Register method)
- [x] 1.2 Wire `slashRegistry` into `AppModel` as a field, initialize with `newSlashRegistry()` in `NewAppModel`
- [x] 1.3 Modify `submitDraft` in `tui/app.go` to check for `/` prefix, trim whitespace, and call `dispatchSlash` before running the agent

## 2. Command implementations

- [x] 2.1 Implement `/exit` handler: delegates to `beginQuit()` (same path as Ctrl+Q)
- [x] 2.2 Implement `/skills` handler: reads `model.info.Capabilities`, filters `CapabilityKindSkill`, formats notice output
- [x] 2.3 Implement `/context` handler: reads `model.usage`, formats token/window/percentage/source as notice
- [x] 2.4 Implement `/mode` handler: validates mode name against allowed set, calls `setModeCmd` on the port via `model.port.SetMode`
- [x] 2.5 Implement `/clear` handler: resets `model.transcript` to a fresh `TranscriptStore`
- [x] 2.6 Implement `/help` handler: iterates registry, formats name + aliases + description table
- [x] 2.7 Add all commands to the registry in `newSlashRegistry()` with aliases (`/exit`→`/quit`, `/skills`→`/available-skills`, `/context`→`/usage`, `/help`→`/?`)

## 3. Unknown command handling

- [x] 3.1 Unknown `/` input produces a transcript notice: "Unknown command: /foo. Try /help."

## 4. Edge cases

- [x] 4.1 Composer with only `/` (no command name) resets composer silently
- [x] 4.2 `/mode` with no argument shows valid modes list as notice
- [x] 4.3 `/mode` when mode is not changeable (external/unsupported/custom dispatcher) shows appropriate notice
- [x] 4.4 Slash commands are no-ops when `runState` is booting, quitting, or has a startup error (unless command is `/exit`)
- [x] 4.5 Composer resets after every slash command execution so the command text does not persist

## 5. Tests

- [x] 5.1 Add table-driven tests for `slashRegistry.Dispatch` covering known commands, unknown commands, aliases
- [x] 5.2 Add tests for each command handler's output formatting (skills list, context stats, help table, mode validation)
- [x] 5.3 Add integration test in `tui/app_test.go` verifying that `/`-prefixed input dispatches to command handler instead of starting a run
- [x] 5.4 Verify `golangci-lint run ./...` passes with no new diagnostics
- [x] 5.5 Verify `go test ./...` passes all packages
