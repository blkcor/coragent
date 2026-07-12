## 1. Extend slash command model and registry

- [x] 1.1 Add `Kind` field to `slashCommand` struct with values `"builtin"` and `"skill"`, and add a `Source` field for skill source display
- [x] 1.2 Add `RegisterSkills(items []CapabilityItem)` method to `slashRegistry` that creates skill entries, skips collisions with existing built-in commands (logging a warning), and appends to `ordered`
- [x] 1.3 In `registerCommands()`, mark all existing commands with `Kind: "builtin"`

## 2. Route skill slash commands to agent run

- [x] 2.1 In `submitDraft()` (`tui/app.go`), check the command kind after looking up the first word in the registry: built-in commands dispatch normally, skill commands bypass dispatch and submit the raw input to the agent run path
- [x] 2.2 Wire up `RegisterSkills` call in `handleStartup()` after `app.info.Capabilities` is populated from the session descriptor

## 3. Render skill entries with source badges

- [x] 3.1 In `renderSlashSuggestions()` (`tui/app.go`), render a source badge (`[user]` or `[project]`) for entries with `Kind == "skill"`, using the command's `Source` field
- [x] 3.2 Ensure the 8-entry visible cap and scroll behavior works correctly with mixed built-in/skill entries

## 4. Tests

- [x] 4.1 Add test cases for `RegisterSkills`: skills appended, collision with built-in skipped, idempotent re-registration
- [x] 4.2 Add test cases for `MatchPrefix` with mixed built-in and skill entries
- [x] 4.3 Add test case for `submitDraft` routing: skill entry bypasses dispatch and routes to agent run
- [x] 4.4 Add integration test for the full flow: startup populates skills, typing `/` shows skills in dropdown, selecting a skill submits to agent
- [x] 4.5 Run `golangci-lint run ./...` and `go test ./...` to verify no regressions

## 5. Polish

- [x] 5.1 Update `/help` output to distinguish built-in commands from skills (e.g., group under separate headings or add a visual separator)
- [x] 5.2 Manual smoke test: load a real skill, verify it appears in `/` dropdown, verify selection and submission invokes the skill through the agent
