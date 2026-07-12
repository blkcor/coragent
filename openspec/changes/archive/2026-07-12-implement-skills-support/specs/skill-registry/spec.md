# skill-registry Specification

## Purpose

Discovers, loads, validates, and manages skill definitions from user and project
directories. Provides the authoritative set of available skills to the rest of the
harness.

## ADDED Requirements

### Requirement: Skills discovered from user and project roots

The system SHALL discover skills by walking two roots recursively: a user root
(default `~/.coragent/skills/`) and a project root (default `.coragent/skills/`).
Any directory containing an `SKILL.md` file SHALL be recognized as a skill. The
directory name SHALL serve as the default skill name when the frontmatter `name`
field is absent.

#### Scenario: Skill directory with SKILL.md is discovered

- **WHEN** a skill root contains a directory `my-skill/` with an `SKILL.md` file
- **THEN** the loader recognizes `my-skill` as a skill

#### Scenario: Directory without SKILL.md is skipped

- **WHEN** a skill root contains a directory with supporting files but no `SKILL.md`
- **THEN** the loader silently skips that directory and does not treat it as a skill

#### Scenario: Nested directories are discovered

- **WHEN** a skill root contains `category/useful-skill/SKILL.md`
- **THEN** the loader discovers `useful-skill` regardless of nesting depth

### Requirement: Project skills override same-named user skills

When a skill with the same resolved name exists in both the user root and the
project root, the project skill SHALL take precedence and the user skill SHALL be
shadowed. The shadowed skill SHALL NOT appear in the advertised list.

#### Scenario: Project skill shadows user skill

- **WHEN** both `~/.coragent/skills/linter/SKILL.md` and `.coragent/skills/linter/SKILL.md` exist
- **THEN** the project version is registered and the user version is not

#### Scenario: User skill visible when no project override exists

- **WHEN** a skill exists only in the user root with no same-named project skill
- **THEN** the user skill is registered normally

### Requirement: SKILL.md frontmatter is parsed and validated

Each `SKILL.md` file SHALL begin with YAML frontmatter delimited by `---` fences.
The frontmatter MUST contain a `name` field (or the directory name is used) and
MAY contain `description` and `type` fields. The body following the second `---`
fence SHALL be the skill content injected into context.

#### Scenario: Valid SKILL.md with all fields

- **WHEN** an SKILL.md has frontmatter with name, description, and type
- **THEN** all fields are parsed and the body is extracted as skill content

#### Scenario: SKILL.md with only name in frontmatter

- **WHEN** an SKILL.md has frontmatter with only a name field
- **THEN** the skill is valid; description and type default to empty and `user` respectively

#### Scenario: SKILL.md without frontmatter uses directory name

- **WHEN** an SKILL.md has no YAML frontmatter fences
- **THEN** the directory name is used as the skill name and the entire file content is the skill body

### Requirement: Malformed skills are rejected at load time

A skill with invalid YAML frontmatter, a name that collides with a built-in tool
name, or a name that duplicates another skill in the same root SHALL be rejected.
Rejection SHALL produce a structured log warning and SHALL NOT prevent the session
from starting. Other valid skills SHALL still load.

#### Scenario: Invalid YAML is rejected

- **WHEN** an SKILL.md has malformed YAML frontmatter
- **THEN** a warning is logged identifying the file path and the parse error
- **THEN** the skill is excluded from the registry
- **THEN** other valid skills in the same root still load

#### Scenario: Name collision with built-in tool is rejected

- **WHEN** a skill is named `bash` (colliding with the built-in shell tool)
- **THEN** the skill is rejected with a warning naming both the skill path and the conflicting built-in
- **THEN** the built-in tool is unaffected

#### Scenario: Missing SKILL.md content after frontmatter

- **WHEN** an SKILL.md has valid frontmatter but an empty body
- **THEN** the skill is registered with an empty body rather than being rejected

### Requirement: Registry exposes loaded skills as an ordered list

The skill registry SHALL expose loaded skills as a stable-ordered list. Skills from
the project root SHALL appear before skills from the user root. Within each root,
skills SHALL be ordered alphabetically by name.

#### Scenario: Stable order across loads

- **WHEN** the same set of skill files is loaded twice
- **THEN** the advertised skill list has identical order both times

#### Scenario: Project skills precede user skills

- **WHEN** both project and user roots contribute skills
- **THEN** project skills appear first in the list, followed by user skills

### Requirement: Skill roots are configurable

The paths for user and project skill roots SHALL be configurable in settings
(`skill_roots.user` and `skill_roots.project`). When not configured, the defaults
SHALL be `~/.coragent/skills/` and `.coragent/skills/` respectively.

#### Scenario: Default roots when unconfigured

- **WHEN** no skill root settings are present
- **THEN** the loader uses `~/.coragent/skills/` and `.coragent/skills/`

#### Scenario: Custom root paths are respected

- **WHEN** settings specify custom skill root paths
- **THEN** the loader walks the specified paths instead of the defaults

### Requirement: Skill type is an arbitrary string label

The `type` field in frontmatter SHALL be an arbitrary string with no
registry-enforced validation. The registry SHALL default it to `user` when absent.
The value SHALL be passed through to tool descriptors and TUI display without
interpretation.

#### Scenario: Type defaults to user

- **WHEN** a skill has no `type` in its frontmatter
- **THEN** the registry exposes its type as `user`
