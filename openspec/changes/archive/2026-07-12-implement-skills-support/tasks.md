## 1. Skill data model and file parsing

- [x] 1.1 Create `internal/skill/` package with `Skill` struct (name, description, type, body, source path)
- [x] 1.2 Implement `SKILL.md` parser: extract YAML frontmatter and markdown body, handle missing frontmatter gracefully
- [x] 1.3 Validate parsed skills: reject empty names, names colliding with reserved built-in tool names

## 2. Skill registry (discovery and loading)

- [x] 2.1 Implement `Loader` that walks a root directory recursively, finds directories containing `SKILL.md`, and parses them
- [x] 2.2 Implement two-root merge: load project root first, then user root, with project overriding same-named user skills
- [x] 2.3 Implement `Registry` that holds loaded skills in stable order (project skills before user skills, alphabetical within each group)
- [x] 2.4 Wire skill root paths from settings (`skill_roots.user`, `skill_roots.project`) with defaults to `~/.coragent/skills/` and `.coragent/skills/`

## 3. Skill-as-tool registration

- [x] 3.1 Add post-initialization registration support to tool catalog (`RegisterAfterInit` or similar)
- [x] 3.2 Implement skill-to-tool adapter: each skill becomes a tool whose execution injects the skill body into context
- [x] 3.3 Register all loaded skills as tools at session startup, after built-in tools are registered
- [x] 3.4 Ensure skill tools carry type metadata in their descriptors so frontends can distinguish them from built-ins

## 4. Skill execution (invocation and context injection)

- [x] 4.1 Implement `/name` parser: scan user input for `/skill-name` patterns, extract matching skill names, strip tokens from visible message
- [x] 4.2 Implement skill body injection as a system-level message scoped to the current turn
- [x] 4.3 Wire skill execution into the agent loop: parse `/name` patterns before the model round, inject matching skill content
- [x] 4.4 Ensure no recursive skill expansion (skill body text containing `/other-skill` is treated as literal text)

## 5. Context manager integration

- [x] 5.1 Add `skill` segment type to context-usage snapshot data model
- [x] 5.2 Track skill body token contributions in the usage estimate for each round
- [x] 5.3 Ensure skill segments do not persist across turns (each round's snapshot is independent)

## 6. Public SDK surface

- [x] 6.1 Define `Skill` type in `pkg/agent/` (public counterpart to internal skill type)
- [x] 6.2 Add `RegisterSkill` and `ListSkills` methods to `Session`
- [x] 6.3 Expose skill invocation events on the event stream (skill loaded, skill execution result)

## 7. TUI integration

- [x] 7.1 Add skill list panel showing available skills with name, description, and source (project/user)
- [x] 7.2 Add active-skill indicator that shows which skills are loaded in the current turn
- [x] 7.3 Render skill invocation as a distinct entry in the conversation view (visually separate from user/assistant messages)
- [x] 7.4 Handle empty skill list gracefully with a placeholder message

## 8. Testing

- [x] 8.1 Table-driven tests for SKILL.md parsing (valid, missing frontmatter, invalid YAML, empty body, name collision)
- [x] 8.2 Table-driven tests for loader/registry (empty roots, nested dirs, project-override-user, malformed files don't break other skills)
- [x] 8.3 Offline tests for `/name` parsing and skill injection against a fake provider
- [x] 8.4 Offline tests for skill segment tracking in context-usage snapshots
- [x] 8.5 Offline tests for skill-as-tool execution through the tool-executor middleware chain
