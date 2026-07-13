## Why

Skills are loaded into the agent with discoverable names, but typing `/skill-name` in the composer shows "Unknown command" instead of invoking the skill. Users must remember exact skill names and type them blind — the slash suggestion dropdown only surfaces built-in commands, missing the primary extensibility mechanism. This gap makes skills feel second-class.

## What Changes

- The slash suggestion dropdown dynamically includes loaded skill names alongside built-in commands when the user types `/`
- Each skill appears as a selectable entry showing its name, source (user/project), and description — matching the information already displayed by `/skills`
- Selecting a skill via Tab/Enter inserts the skill invocation (`/skill-name`) and submits it as an agent run, where the existing `ParseInvocations` path handles injection of the skill body as transient context
- Skill entries are registered lazily when the session starts (capabilities become available), and the dropdown always reflects the currently loaded skill set
- The static `/skills` list command continues to work unchanged

## Capabilities

### New Capabilities
- `slash-skill-suggestions`: The TUI slash suggestion dropdown includes dynamically registered skill names from the session's capability inventory, with prefix matching, selection, and invocation that routes through the existing skill-invocation path.

### Modified Capabilities
- `slash-commands`: The slash command registry gains a mechanism for dynamic registration after initial construction, so skills can be added when capability data arrives without replacing or restarting the registry.
- `tui-frontend`: The composer's slash suggestion rendering must handle a potentially larger list mixing built-in commands and skills, and `submitDraft` must distinguish skill invocations from built-in slash commands so skills route to the agent run rather than the local dispatcher.

## Impact

- Affected code: `tui/slash.go` (registry, suggestion state, dispatch), `tui/app.go` (handleStartup integration, submitDraft routing), `tui/slash_test.go` (new test cases for dynamic registration and skill invocation)
- No changes to `pkg/agent/`, `internal/skill/`, or the public SDK surface
- No breaking changes to existing slash commands or the `/skills` listing
- No new dependencies
