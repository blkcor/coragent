## Why

Coragent currently has no mechanism for users to extend the agent's behavior with reusable, domain-specific instruction sets. Every specialized workflow must be spelled out inline in the prompt, causing repetition and limiting composability. Skills fill this gap — they are the same pattern that makes Claude Code extensible: markdown files with frontmatter metadata that the agent loads on demand and injects into its context. Implementing skills now unblocks a whole family of user-facing customization (project conventions, domain workflows, tool integrations) without changing the harness core.

## What Changes

- New `internal/skill/` package: skill loader, registry, and execution engine
- Skills discovered from two roots (merged): `~/.coragent/skills/` (user) and `.coragent/skills/` (project), with project overriding same-named user skills
- A skill is a directory containing an `SKILL.md` file with YAML frontmatter (`name`, `description`, `type`) and markdown body
- Skills registered at session start and exposed as tools the model can invoke — each skill becomes a tool whose execution injects the skill's body into context
- `/skill-name` syntax in user input triggers skill injection before the agent loop processes the turn
- **Breaking:** none — skills are additive, no existing contract changes
- TUI gains a skill list panel (available skills) and skill-invocation indication in the conversation view

## Capabilities

### New Capabilities

- `skill-registry`: Discovery, loading, validation, and lifecycle of skill definitions from user and project roots. Handles name conflict resolution (project overrides user), hot-reload, and malformed-skill rejection.
- `skill-execution`: Injecting skill content into the agent's context at invocation time — both user-triggered (`/skill-name`) and model-triggered (tool call). Ensures skill content is scoped to the current turn and does not leak across sessions.
- `skill-tui`: TUI surface for listing available skills, showing skill metadata, and indicating when a skill is active in the current turn.

### Modified Capabilities

- `tool-catalog`: Skills register as dynamic tools in the catalog — each skill exposes a tool descriptor so the model can invoke it. The catalog must accept post-initialization registrations without restart.
- `context-manager`: Skill content injected into the system prompt / context must be tracked as a distinct context segment so the context-usage snapshot can attribute tokens to skills.

## Impact

- New package: `internal/skill/`
- Modified: `internal/tools/` (skill-as-tool registration), `internal/context/` (skill-content segment tracking), `tui/` (skill list panel, invocation indicator)
- Public SDK (`pkg/agent/`): new `Skill` type exposed, `Session` gains `RegisterSkill` and `ListSkills` methods
- Config: skill root directories added to settings schema (defaults to `~/.coragent/skills/` and `.coragent/skills/`)
- No new external dependencies
