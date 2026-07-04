# permission Specification

## Purpose
TBD - created by syncing change phase-3-permission. Update Purpose after archive.

## Requirements

### Requirement: Three outcomes with ask as the safe default

The permission stage SHALL resolve every tool call reaching it to exactly one
outcome — allow, deny, or ask the human — and SHALL ask the human whenever no
mode and no rule decides the call. There SHALL be no silent allow of an uncovered
action.

#### Scenario: Uncovered action in default mode asks once

- **WHEN** a call in default mode is covered by no rule
- **THEN** the human is prompted exactly once for that call

#### Scenario: Nothing deciding means ask, never silent allow

- **WHEN** no mode and no rule resolves a call
- **THEN** the outcome is to ask the human and never a silent allow

### Requirement: Permission request states what and why

A permission request emitted to the frontend SHALL carry the tool call being
requested and a human-readable reason stating what the action is and why approval
is needed.

#### Scenario: Prompt names the action and the reason

- **WHEN** a permission request is emitted
- **THEN** it carries the tool call and a reason describing the action and why approval is needed

### Requirement: Allow proceeds; deny stops and informs the agent

An allowed call SHALL proceed through the rest of the execution path. A denied
call SHALL NOT run; the agent SHALL receive a permission-denied result carrying a
reason, and the run SHALL continue without a crash.

#### Scenario: Allow lets the action proceed

- **WHEN** a showing prompt is answered allow
- **THEN** the call proceeds to the next stage of the execution path

#### Scenario: Deny prevents the action and the agent adapts

- **WHEN** a showing prompt is answered deny
- **THEN** the call does not run and the agent receives a denial result it can react to, with no crash

### Requirement: Any frontend can answer a request

A permission request SHALL be answerable by any frontend — human or code-driven —
over its embedded reply path, producing the matching outcome with no human
present.

#### Scenario: Code-driven answer produces the matching outcome

- **WHEN** a code-driven frontend answers allow or deny with no human present
- **THEN** the resolved outcome matches the scripted answer

### Requirement: Remember a decision within the session

When a decision is answered with remember set, the permission stage SHALL turn it
into a durable rule that takes effect immediately, so the next matching action in
the same session is resolved by the rule without prompting again.

#### Scenario: Remembered approval silences the next matching action

- **WHEN** an action is approved with remember set
- **AND** the same kind of action recurs later in the same session
- **THEN** it runs without prompting

### Requirement: Remembered choices persist across sessions

A remembered decision SHALL be saved to settings so it is honored after a restart
and settings reload. Saving SHALL preserve unrelated settings. If saving fails,
the accompanying action SHALL still run — only durability is lost.

#### Scenario: Remembered choice still in effect after restart

- **WHEN** a choice was remembered in a prior session
- **AND** settings are reloaded in a new session
- **THEN** the choice is still in effect

#### Scenario: Saving preserves unrelated settings

- **WHEN** a remembered choice is saved
- **THEN** unrelated settings in the file are preserved

#### Scenario: Failed save does not block the action

- **WHEN** saving a remembered choice fails
- **THEN** the approved action still runs and only durability is lost

### Requirement: Default mode

In default mode the permission stage SHALL consult the configured rules and prompt
the human for any action not covered by a rule.

#### Scenario: Default mode consults rules and asks otherwise

- **WHEN** an action runs in default mode
- **THEN** matching rules decide it and any uncovered action prompts the human

### Requirement: Auto-accept-edits mode

In auto-accept-edits mode the permission stage SHALL allow file-edit actions
without prompting while still subjecting non-edit actions to asking and to deny
rules.

#### Scenario: Edits run without asking

- **WHEN** a file-edit action runs in auto-accept-edits mode
- **THEN** it runs without prompting

#### Scenario: Non-edit action still asks

- **WHEN** a command (non-edit) action runs in auto-accept-edits mode
- **THEN** it is still asked about

#### Scenario: Deny rule still blocks in this mode

- **WHEN** an action covered by a deny rule runs in auto-accept-edits mode
- **THEN** it is refused

### Requirement: Plan mode blocks every mutation with a reason

In plan mode the permission stage SHALL block every state-changing action with a
stated reason and SHALL let read actions proceed. An action whose state-change
classification is unknown SHALL be treated as state-changing and blocked. A
plan-mode block SHALL NOT be defeatable by any allow rule.

#### Scenario: Write, edit, or command is blocked with a reason

- **WHEN** a write, edit, or command action runs in plan mode
- **THEN** it is blocked with a clear reason naming plan mode

#### Scenario: Read proceeds in plan mode

- **WHEN** a read action runs in plan mode
- **THEN** it proceeds normally

#### Scenario: Unknown action is treated as state-changing

- **WHEN** an action of unknown classification runs in plan mode
- **THEN** it is blocked, erring on the safe side

#### Scenario: Allow rule does not defeat a plan-mode block

- **WHEN** an allow rule would cover a write
- **AND** plan mode is active
- **THEN** the write is still blocked

### Requirement: Bypass mode disables only the asking

In bypass mode the permission stage SHALL allow any requested action with no
prompt and SHALL NOT consult rules. Bypass SHALL NOT affect the hard guardrails
(hooks and sandbox) that run elsewhere on the path.

#### Scenario: Bypass allows with no prompt and ignores rules

- **WHEN** an action runs in bypass mode
- **THEN** it runs with no prompt and rules are not consulted

#### Scenario: Hard guardrail still stops a forbidden action in bypass

- **WHEN** a hard guardrail forbids an action
- **AND** bypass mode is active
- **THEN** the action is still stopped by that guardrail

### Requirement: Modes switch between turns and start from configuration

The permission stage SHALL begin in the mode named by configuration and SHALL
allow the mode to be changed between turns so the new mode governs subsequent
actions.

#### Scenario: Configured starting mode governs the first turn

- **WHEN** a starting mode is set in configuration
- **THEN** it governs actions from the first turn

#### Scenario: Switching mode before the next turn takes effect

- **WHEN** a different mode is selected before the next turn
- **THEN** the new mode governs subsequent actions

### Requirement: Allow and deny rules with command-family matching

The permission stage SHALL maintain a per-action-type allow list and deny list. An
allow rule for a command SHALL cover that command and its argument variations
without matching an unrelated command.

#### Scenario: Allow rule runs a covered action without asking

- **WHEN** an action is covered by an allow rule
- **THEN** it runs without asking

#### Scenario: Deny rule refuses a covered action without asking

- **WHEN** an action is covered by a deny rule
- **THEN** it is refused without asking

#### Scenario: Command rule covers the family but not unrelated commands

- **WHEN** an allow rule for `git status` is active
- **THEN** `git status --short` runs without asking
- **AND** `git stash` and `git statusfoo` are not covered by that rule

### Requirement: Deny wins over allow

The permission stage SHALL resolve an action as deny whenever both an allow rule
and a deny rule could match it.

#### Scenario: Deny prevails when both could match

- **WHEN** an allow rule and a deny rule both match an action
- **THEN** the action is denied

### Requirement: Rules merged home-then-project

The permission stage SHALL read rules from the settings file merged home-then-
project, with project rules layering over home and a project's stricter deny
honored over a home allow.

#### Scenario: Both layers apply with project precedence

- **WHEN** home and project settings both define rules
- **THEN** both layers apply with project rules taking precedence

#### Scenario: Project deny beats home allow

- **WHEN** a project deny and a home allow both match an action
- **THEN** the action is denied

### Requirement: Deterministic resolution order

The permission stage SHALL resolve modes and rules in a fixed order such that
bypass overrides everything soft, a plan-mode block beats any allow, and a deny
beats an allow.

#### Scenario: The user-visible precedence holds

- **WHEN** modes and rules could conflict for one action
- **THEN** bypass overrides everything soft, plan-mode block beats any allow, and deny beats allow

### Requirement: Edit arguments before approving with re-validation

The permission stage SHALL let a decision carry edited arguments that replace the
originals on approval. Edited arguments SHALL be re-validated against the tool's
declared shape before running; an invalid edit SHALL NOT run and the agent SHALL
be told the edited arguments were rejected.

#### Scenario: Approving with edited arguments runs the edited ones

- **WHEN** an action is approved with edited arguments
- **THEN** it runs with the edited arguments, not the originals

#### Scenario: Invalid edited arguments are rejected

- **WHEN** an action is approved with arguments edited into an invalid shape
- **THEN** the action does not run and the agent is told the edit was rejected

### Requirement: Unanswered prompts fail safe

A permission request that is never answered SHALL be treated as denied when the
turn's deadline passes or it is cancelled, with a reason naming the timeout, and
SHALL NOT hang.

#### Scenario: No answer resolves to denied on deadline or cancellation

- **WHEN** a prompt is emitted but never answered
- **AND** the turn's deadline passes or the run is cancelled
- **THEN** the action is treated as denied with a reason naming the timeout and the agent does not hang

### Requirement: Exactly one decision honored

For a single permission request, the permission stage SHALL honor exactly one
decision; additional answers SHALL be ignored.

#### Scenario: Second answer to one prompt is ignored

- **WHEN** a frontend sends two answers to one prompt
- **THEN** the first decision is honored and the second is ignored, with no wedge
