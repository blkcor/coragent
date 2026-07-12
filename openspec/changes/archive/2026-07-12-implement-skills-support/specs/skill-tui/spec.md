# skill-tui Specification

## Purpose

TUI surface for listing available skills, showing skill metadata, and indicating
when a skill is active in the current turn.

## ADDED Requirements

### Requirement: Available skills are listed in the TUI

The TUI SHALL display the list of available skills, showing each skill's name and
description. The list SHALL be accessible from the main interface. Skills SHALL be
ordered as returned by the registry (project skills before user skills,
alphabetical within each group).

#### Scenario: Skills list shows loaded skills

- **WHEN** skills are registered at session start
- **THEN** the TUI displays all registered skills with their names and descriptions

#### Scenario: Empty skills list shows a placeholder

- **WHEN** no skills are registered
- **THEN** the TUI shows a message indicating no skills are available

### Requirement: Skill source is visually distinguishable

The TUI SHALL visually distinguish project skills from user skills in the skill
list, using a label or icon.

#### Scenario: Project and user skills look different

- **WHEN** both project and user skills are registered
- **THEN** the TUI renders them with distinct visual indicators (e.g., label `[project]` vs `[user]`)

### Requirement: Active skill is indicated in the conversation

When a skill is invoked in the current turn (by `/name` or model tool call), the
TUI SHALL indicate which skill is active. The indicator SHALL clear when the turn
ends.

#### Scenario: Skill invocation shows indicator

- **WHEN** the user types `/linter review this`
- **THEN** the TUI shows `linter` as active during that turn

#### Scenario: Indicator clears after turn

- **WHEN** a turn with an active skill completes
- **THEN** the active-skill indicator is cleared

### Requirement: Skill invocation appears in conversation history

When a skill is invoked, the conversation view SHALL show a discrete entry
indicating the skill was loaded, including the skill name. This entry SHALL be
visually distinct from user messages and assistant replies.

#### Scenario: Skill invocation renders as a system entry

- **WHEN** a skill is invoked
- **THEN** the conversation view shows a skill-invocation entry with the skill name
- **THEN** the entry is visually distinct from user and assistant messages
