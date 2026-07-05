## ADDED Requirements

### Requirement: Built-ins declare their action classification

Each built-in tool SHALL declare its action classification — read-only,
edits-files, or runs-commands — so the permission stage can gate the correct set
of actions in plan mode and auto-accept-edits mode. The read, content-search, and
file-find tools SHALL classify as read-only; the write and edit tools SHALL
classify as edits-files; the shell tool SHALL classify as runs-commands.

#### Scenario: Read, search, and find classify as read-only

- **WHEN** the action classification of the read, content-search, or file-find tool is inspected
- **THEN** it is read-only

#### Scenario: Write and edit classify as edits-files

- **WHEN** the action classification of the write or edit tool is inspected
- **THEN** it is edits-files

#### Scenario: Shell classifies as runs-commands

- **WHEN** the action classification of the shell tool is inspected
- **THEN** it is runs-commands
