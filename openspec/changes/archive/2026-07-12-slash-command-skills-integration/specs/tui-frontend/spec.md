## ADDED Requirements

### Requirement: Slash suggestion dropdown includes skill entries

The composer's slash suggestion dropdown SHALL render skill entries with a distinct source badge (`[user]` or `[project]`) alongside their name and description. The rendering SHALL accommodate a larger suggestion list mixing built-in commands and skills while maintaining the existing 8-entry visible cap and prefix-matching behavior.

#### Scenario: Skill entry rendered with source badge
- **WHEN** the slash suggestion dropdown contains a skill named `code-review` from project source
- **THEN** the suggestion row shows the skill name and a `[project]` badge
- **THEN** the skill description is shown alongside the name and badge

#### Scenario: Mixed built-in and skill suggestions
- **WHEN** the user types `/` and both built-in commands and skills are loaded
- **THEN** built-in command rows render without source badges
- **THEN** skill rows render with their source badges
- **THEN** all rows are navigable with Up/Down and selectable with Tab/Enter

#### Scenario: Suggestion count exceeds visible cap
- **WHEN** more than 8 commands and skills match the current prefix
- **THEN** only the first 8 matches are rendered in the dropdown
- **THEN** the scrollable behavior is unchanged from the current implementation
