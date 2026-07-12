## Why

The TUI composer has no input history — every submitted draft is gone. Pressing Up in the empty composer does nothing, so recalling a previous prompt means re-typing it from scratch. This is a basic terminal UI expectation that hurts usability on every multi-turn exchange.

## What Changes

- Pressing **Up** when the composer is focused and no slash-suggest dropdown is active recalls the previous submission into the composer, replacing whatever draft is currently there.
- Pressing **Down** after navigating up through history walks forward (toward the newest entry), eventually clearing the composer when past the newest entry.
- History is stored in-memory per session — it does not persist across sessions.
- Submitting an empty draft does not add to history. Consecutive identical submissions are not deduplicated against adjacent history entries (each submit is a separate entry).

## Capabilities

### New Capabilities

None. This fits within the existing `tui-frontend` capability.

### Modified Capabilities

- `tui-frontend`: the composer input area gains history recall via Up/Down arrow keys. The composer requirement "Real caret and position-aware composer editing" is extended: Up/Down that would move the cursor within a single-line draft now recall history instead.

## Impact

- `tui/app.go` — `AppModel` struct (new fields), `handleComposerKey` (Intercept Up/Down), `submitDraft` (append to history on submit)
