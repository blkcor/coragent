## Context

Coragent is a coding-agent harness with a mature Phase 7 codebase: agent loop, tool executor with middleware chain, permission engine, hooks, sandbox, subagents, and a full TUI. The harness currently has no extension mechanism for reusable instruction sets — users must repeat domain knowledge inline in every prompt.

Skills are the established pattern for this: markdown files with frontmatter metadata that the agent loads on demand. Claude Code, Codex, and other coding agents all use this model. For Coragent, skills fill the gap between "one-off prompt" and "hardcoded tool" — they are user-authorable, filesystem-discovered, and injected into context at invocation time.

### Constraints from existing architecture

- Skills must not break the **one execution path** invariant — if skills register as tools, they flow through hooks → permission → sandbox → execution
- Skill content injected into context must be tracked by the **context manager** for budget visibility
- The **TUI is a replaceable client** — skill visibility in the UI is additive, not a harness dependency

## Goals / Non-Goals

**Goals:**
- Discover skills from user and project directories via filesystem walk
- Parse skill definitions (YAML frontmatter + markdown body) with clear error reporting for malformed files
- Register each skill as a tool the model can invoke, following the existing tool-execution chokepoint
- Let users invoke skills with `/skill-name` syntax in the chat input
- Show available skills in the TUI and indicate when one is active
- Track skill content as a distinct context segment for budget attribution

**Non-Goals:**
- Skill marketplace, remote skill fetching, or plugin registry
- Skill hot-reload (session restart required to pick up changes)
- Skill composition or inheritance
- Skill parameter passing (skills are pure context injection, not parameterized templates)
- Automatic skill triggering based on message content (user or model must explicitly invoke)

## Decisions

### Decision 1: Skill file format — YAML frontmatter + markdown body

Each skill lives in a directory under a skill root. The directory contains an `SKILL.md` file:

```markdown
---
name: my-skill
description: Does something useful
type: user
---

Skill body — injected verbatim into context when invoked.
```

**Why:** This is the de facto standard across coding agents. YAML frontmatter is parseable by `gopkg.in/yaml.v3` without custom grammars. The markdown body is already the format the model consumes.

**Alternatives considered:**
- JSON config file + separate body file: more ceremony, worse authoring experience
- Pure markdown with convention-based naming: loses explicit metadata, harder to validate

### Decision 2: Two-root discovery with project override

Skills are discovered from two roots, merged with project priority:

1. `~/.coragent/skills/` — user-level skills (available across all projects)
2. `.coragent/skills/` — project-level skills (override same-named user skills)

Each root is walked recursively. Any directory containing an `SKILL.md` is a skill. The directory name is the default skill name, overridden by the `name` field in frontmatter.

**Why:** Matches the existing config merge pattern (home → project, project overrides). Project skills let teams share conventions; user skills are personal toolkit.

**Alternatives considered:**
- Single flat directory: no namespacing, no project/user separation
- Plugin-style packages: overengineered for v1, the directory-per-skill model is simple and git-friendly

### Decision 3: Skills register as tools

Each loaded skill auto-registers as a tool in the tool catalog. The tool's name is the skill name, its description comes from the frontmatter, and its execution injects the skill body into context.

**Why:** This uses the existing tool-execution chokepoint (hooks → permission → sandbox → execute). The model already knows how to use tools; skills are just tools whose "result" is context enrichment. Permission can gate which skills are usable in which modes.

**Alternatives considered:**
- Separate skill-execution path outside the tool chain: violates the "one execution path" invariant
- Pure prompt preprocessing (no tool registration): model can't autonomously invoke skills, loses the discoverability benefit

### Decision 4: Skill content injection as system-message append

When a skill is invoked (by tool call or `/name`), its body is appended to the conversation as a system-level message, scoped to the current turn. The context manager tracks it as a `skill` segment type.

**Why:** System-message semantics match what skills are — instructions to the model. Turn-scoping prevents skill content from persisting into unrelated turns. Segment tracking lets the context-usage snapshot show how much budget skills consume.

### Decision 5: User-triggered invocation via `/name` prefix

Before the agent loop processes a user turn, the input is scanned for `/skill-name` patterns. Matching skill names trigger injection of the skill body before the user message reaches the model.

**Why:** This is the primary UX for skill use — it mirrors slash-command conventions users already know from chat interfaces. Parsing happens in the harness, not the TUI, so all frontends get it for free.

### Decision 6: Malformed skills are rejected at load time, not silently skipped

If an `SKILL.md` file is missing required frontmatter, has invalid YAML, or duplicates an already-registered name, the skill is rejected and the error is logged. The session continues without that skill.

**Why:** Silent failures hide configuration errors. Loud rejection at load time makes problems visible. The session shouldn't fail entirely because one skill is broken.

## Risks / Trade-offs

- **Skill content blows context budget** → Context manager already emits over-budget warnings; skill segments are tracked distinctly so the user can see which skills are costly
- **Name collisions between skills and built-in tools** → Skills are registered in a separate namespace (prefixed or validated against built-in names at load time) — attempting to register a skill named `read` or `bash` is rejected
- **Malicious project skills** → Skills are inert markdown injected as context; they cannot execute code directly. The permission engine still gates any tool calls the model makes. A malicious skill could only socially engineer the model, which is the same threat model as the user's own prompt
- **Skill directories without SKILL.md create noise** → Directories lacking `SKILL.md` are silently skipped during discovery; only parse errors are loud

## Open Questions

- Should skills support a `tools` field in frontmatter to declare which tools they need? (Deferred — wait for real usage patterns)
- Should the TUI support skill search/filtering? (Deferred — basic list is sufficient for v1)
