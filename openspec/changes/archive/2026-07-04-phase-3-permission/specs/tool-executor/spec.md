## ADDED Requirements

### Requirement: Action classification handed to the permission stage

The executor SHALL classify each resolved call's action kind — read-only,
edits-files, or runs-commands — and SHALL hand that classification to the
permission stage so the soft gate can apply mode-aware decisions. A call whose
classification cannot be determined SHALL be handed an unknown classification so
the permission stage can err safe.

#### Scenario: Permission stage receives the call's action kind

- **WHEN** a call is dispatched through the chain
- **THEN** the permission stage is given the call's action classification before it decides

#### Scenario: Unclassifiable call is handed an unknown classification

- **WHEN** a dispatched call's action kind cannot be determined
- **THEN** the permission stage receives an unknown classification rather than a guessed one

## MODIFIED Requirements

### Requirement: Inert placeholder stages

The hard-check and sandbox stages SHALL ship as pass-through placeholders —
never-block hard checks, run-directly sandbox — that a later phase replaces
without altering the path. Each placeholder SHALL be a drop-in replacement. The
permission stage SHALL be a real human-in-the-loop gate rather than an
allow-everything placeholder.

#### Scenario: Placeholders produce the tool's own result

- **WHEN** a read, edit, or shell call runs through the chain with the hard-check and sandbox placeholders in place and the permission stage allowing
- **THEN** the result is identical to what the tool would produce on its own
