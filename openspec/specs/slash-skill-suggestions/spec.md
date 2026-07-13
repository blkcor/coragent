# slash-skill-suggestions Specification

## Purpose
TBD - created by archiving change slash-command-skills-integration. Update Purpose after archive.
## Requirements
### Requirement: Skill names appear in slash suggestion dropdown

The TUI slash suggestion dropdown SHALL include dynamically registered skill names alongside built-in slash commands. Skill entries SHALL be added after the session starts and capabilities become available. Built-in commands SHALL always appear before skill entries in the suggestion list.

#### Scenario: Skills appear after built-in commands
- **WHEN** the user types `/` and one or more skills are loaded in the session
- **THEN** the suggestion dropdown lists all built-in commands first, followed by all loaded skill names
- **THEN** each skill entry displays its name, source badge (`[user]` or `[project]`), and description

#### Scenario: Prefix matching includes skills
- **WHEN** the user types `/code` and a skill named `code-review` is loaded
- **THEN** the `code-review` skill appears in the filtered suggestion dropdown
- **THEN** built-in commands matching the prefix also appear before skill matches

#### Scenario: Skill not loaded
- **WHEN** no skills are loaded in the session
- **THEN** the slash suggestion dropdown shows only built-in commands (unchanged behavior)

#### Scenario: Skills not yet available
- **WHEN** the session is still booting and capability data has not arrived
- **THEN** the slash suggestion dropdown shows only built-in commands
- **THEN** skill names appear once the session startup completes and capabilities are populated

### Requirement: Skill name collision with built-in commands

The system SHALL preserve built-in command precedence when a loaded skill has the same name as a built-in slash command. The built-in command SHALL remain registered and the skill SHALL be skipped with a warning log.

#### Scenario: Skill named "help" is loaded
- **WHEN** a skill named `help` is loaded and a built-in `/help` command already exists
- **THEN** the built-in `/help` command remains registered and functional
- **THEN** the skill `help` is NOT added to the slash command registry
- **THEN** a warning is logged about the name collision

### Requirement: Skill slash command routes to agent run

Selecting or submitting a skill slash command SHALL bypass the local slash dispatch and instead submit the raw `/skill-name` input to the agent run path, where the existing `ParseInvocations` mechanism handles skill body injection as transient context.

#### Scenario: Typing a full skill name and pressing Enter
- **WHEN** the user types `/code-review` and presses Enter
- **THEN** the input is submitted as a normal agent run (not dispatched to a local handler)
- **THEN** the agent's `ParseInvocations` detects the skill name and injects the skill body

#### Scenario: Autocompleting a skill name with Tab
- **WHEN** the user types `/code` and the `code-review` skill is the selected suggestion
- **THEN** pressing Tab replaces the composer content with `/code-review `
- **THEN** the composer remains focused for additional input (same as built-in command autocomplete)

#### Scenario: Autocompleting and submitting a skill with Enter
- **WHEN** the user types `/code` and the `code-review` skill is the selected suggestion
- **THEN** pressing Enter replaces the composer content with `/code-review` and submits it as an agent run

### Requirement: Skills remain listed in `/skills` command

The existing `/skills` slash command SHALL continue to display all loaded skills with name, source, type, and description. The new dropdown integration SHALL NOT change the `/skills` command's behavior.

#### Scenario: `/skills` after dropdown integration
- **WHEN** the user types `/skills` with skills loaded
- **THEN** the transcript displays each skill as before with name, source, type, and description
- **THEN** the output is identical to the pre-integration behavior

