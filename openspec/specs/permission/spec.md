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
Every permission request emitted to a frontend SHALL carry the tool call being
requested and a human-readable reason stating what the action is and why approval
is needed. The rich observed permission payload SHALL additionally carry a
non-empty request ID, the correlated tool-call ID and argument revision, the
effective tool call, typed action kind and current mode, the current structured
action preview or typed preview-unavailable reason, the rule that a remember
decision would create, applicable sandbox posture and grant choices, and typed
origin provenance. Those rich fields MUST all describe the same revision so a
frontend does not need to infer safety facts from display strings. The existing
legacy request and reply shape and behavior SHALL remain compatible and MUST NOT
be made to encode observed-only fields in strings.
The rich reply operation SHALL return a typed outcome of `accepted`,
`validation_rejected`, or `already_resolved`. `accepted` consumes the request;
`validation_rejected` MUST leave the same request answerable; and
`already_resolved` MUST NOT reopen or affect execution.

#### Scenario: Prompt names the effective action and reason
- **WHEN** a permission request is emitted after a hook changed the call
- **THEN** it carries the hook-checked effective call and its current preview
- **THEN** the reason describes that effective action and why approval is needed

#### Scenario: Child request retains provenance
- **WHEN** a delegated child action asks the root frontend for permission through the rich observed protocol
- **THEN** the request identifies the stable child, parent, depth, and originating tool call through typed fields
- **THEN** the frontend does not need to parse a task label to attribute the request

#### Scenario: Unpreviewable action is explicit
- **WHEN** an existing custom handler cannot prepare a structured preview for a rich permission interaction
- **THEN** the request carries typed preview unavailability with a reason
- **THEN** it does not fabricate a preview from the tool name or result text

#### Scenario: Remember and grants are reviewable before reply
- **WHEN** a rich request supports remembering or per-call sandbox grants
- **THEN** it states the exact rule that remember would create and the applicable grant dimensions before the frontend replies

#### Scenario: Legacy permission request remains compatible
- **WHEN** an existing frontend consumes a permission request from the legacy run API
- **THEN** it continues to receive the existing tool call, reason, remembered-rule, and one-reply behavior
- **THEN** it is not required to understand rich request identity, revision, preview, provenance, or sandbox posture fields

#### Scenario: Observed adapter limits a caller-owned legacy prompt
- **WHEN** a caller-owned dispatcher emits a legacy permission request into an observed run
- **THEN** its observed request declares protocol `legacy_one_shot` and disables rich revision, schema-aware edit, preview, and grants
- **THEN** allow/deny and an applicable remember decision forward through the legacy reply path exactly once
- **THEN** attempting an unsupported rich decision returns `validation_rejected` and leaves the request answerable

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
into a remembered rule that takes effect immediately, so the next matching action
in the same session is resolved by the rule without prompting again. When a safe
human-readable family scope exists, the existing family rule SHALL remain
compatible. Otherwise the standard permission engine SHALL offer a versioned,
domain-separated HMAC-SHA-256 exact-call rule whose identity includes the typed
action kind, tool name, and canonical effective arguments. An exact-call rule
MUST NOT persist raw arguments, command text, file content, task instructions,
credentials, sandbox grants, or its fingerprint key. Deny rules SHALL continue
to win over allow rules for both selectors.

#### Scenario: Remembered approval silences the next matching action

- **WHEN** an action is approved with remember set
- **AND** the same kind of action recurs later in the same session
- **THEN** it runs without prompting

#### Scenario: Unsafe family generalization uses exact-call fallback

- **WHEN** a standard rich permission request has no safe family scope
- **THEN** allow-and-remember and deny-and-remember remain available for that request
- **THEN** the displayed scope identifies an exact call without exposing its digest or raw arguments

#### Scenario: Exact-call identity is narrow

- **WHEN** an exact-call rule was remembered for one effective call
- **AND** the engine has the same fingerprint key
- **THEN** the same action kind, tool name, and canonical effective arguments match it
- **THEN** a different tool name or different effective arguments do not match it

#### Scenario: Exact-call persistence is secret-free

- **WHEN** a command, task, or custom call containing secret text is remembered exactly
- **THEN** settings contain only the versioned action kind and keyed fingerprint
- **THEN** raw arguments and one-call sandbox grants are not persisted
- **THEN** the settings rule alone cannot verify offline guesses of the original arguments

#### Scenario: Session-only exact remember needs no caller setup

- **WHEN** a direct SDK session has no injected fingerprint key and durable permission persistence is disabled
- **THEN** the engine generates a session-ephemeral fingerprint key
- **THEN** allow-and-remember and deny-and-remember remain available and take effect immediately
- **THEN** an exact rule derived from that ephemeral key is not persisted

#### Scenario: Unsafe legacy exact digest fails safe

- **WHEN** settings contain a legacy `exact-v1` unkeyed SHA-256 rule
- **THEN** startup removes every such allow and deny entry from raw home and project settings before environment placeholders are resolved
- **THEN** the rewrite preserves unrelated raw fields and file permissions, creates no backup containing the old digest, and emits only a path/count/version warning that recommends credential rotation
- **THEN** a direct in-memory legacy rule is still skipped and never automatically allows or denies a call

#### Scenario: Saving cannot resurrect a legacy exact digest

- **WHEN** a remembered rule is appended to a settings file that still contains `exact-v1` entries
- **THEN** the save transaction scrubs all legacy allow and deny entries before appending the new rule
- **THEN** an attempt to append another `exact-v1` rule is refused after the on-disk scrub

### Requirement: Remembered choices persist across sessions

A remembered decision SHALL be saved to settings so it is honored after a restart
and settings reload. Reloadable exact-call rules SHALL use the same persistent
fingerprint key across sessions. Saving SHALL preserve unrelated settings. If
saving fails, the accompanying action SHALL still run; only durability is lost.

#### Scenario: Remembered choice still in effect after restart

- **WHEN** a choice was remembered in a prior session
- **AND** settings are reloaded in a new session
- **AND** an exact-call choice uses the same independently persisted fingerprint key
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

### Requirement: Modes switch live and start from configuration
When the standard permission engine owns control, the permission stage SHALL
begin in the typed mode named by configuration and SHALL expose that ownership
and typed current mode through the public session descriptor. A public typed
mode-change operation SHALL accept only default,
auto-accept-edits, plan, or bypass and SHALL support a mutex-linearized change
during an active run. Every permission decision that begins after the setter
returns SHALL observe the new mode. A request that already snapshotted its mode
and opened a reply path MUST remain pending and MUST NOT be retroactively allowed,
denied, or revised by the mode change. Already executing tools are unaffected.
The existing string mode setter SHALL remain as a compatibility wrapper with the
same live semantics. Hooks and sandbox behavior MUST remain unchanged.

#### Scenario: Configured starting mode governs the first turn
- **WHEN** a valid starting mode is set in configuration
- **THEN** that typed mode appears in the initial descriptor and governs actions from the first turn

#### Scenario: Switching mode before the next decision takes effect
- **WHEN** a different valid mode is selected while the session is idle or running
- **THEN** the change succeeds, a fresh descriptor reports it, and the new mode governs permission decisions begun after the setter returns

#### Scenario: Open prompt is not retroactively resolved
- **WHEN** a permission request is already open and a caller changes mode
- **THEN** the open request retains its snapshotted mode and still requires one explicit reply
- **THEN** a later decision begun after the setter returns observes the new mode

#### Scenario: Existing string setter supports live changes
- **WHEN** an existing caller invokes the string mode setter during a run
- **THEN** the method signature remains unchanged and the live change uses the same linearization semantics as the typed setter

#### Scenario: Unknown typed mode is rejected
- **WHEN** a caller supplies a mode value outside the four defined modes
- **THEN** the change is rejected without altering current state

#### Scenario: Custom permission ownership is explicit
- **WHEN** a session uses a custom dispatcher and does not expose the default permission engine
- **THEN** its descriptor and mode-change result report permission ownership as caller-owned or unsupported rather than inventing a default mode

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
The legacy permission decision SHALL retain its existing allow-with-edited-
arguments behavior for compatibility, but those arguments MUST be revalidated,
run through matching before-tool hard hooks, and reprepared before execution. If
those hooks replace the human-edited arguments, the original approval MUST NOT
authorize the replacement; the harness SHALL issue a fresh legacy permission
request for the final replacement before it can run. The
rich permission protocol SHALL instead expose `revise_arguments` as a
non-approving reply. A malformed, schema-invalid, mismatched, or inapplicable
reply SHALL return typed validation feedback without resolving the current
request. A schema-valid revision SHALL resolve that request and run through hard
hooks. Only when hard hooks allow and preparation succeeds SHALL the harness
produce a new preview and create a new permission request with a new request ID
and incremented revision before allow or deny can be accepted. A hook
block, hook failure, invalid hook replacement, or preparation failure MUST stop
the call without running it or creating a misleading replacement prompt.

#### Scenario: Legacy edited approval remains source-compatible
- **WHEN** an existing frontend answers allow with edited arguments through the legacy reply shape
- **THEN** the harness preserves the reply shape and exactly one honored reply for that request
- **THEN** edited arguments are revalidated, hard-checked, and reprepared
- **THEN** they execute under that approval only when hooks leave them unchanged

#### Scenario: Hook replacement after legacy edit requires reapproval
- **WHEN** a legacy request is allowed with edited arguments and rerun hard hooks replace them
- **THEN** the original request remains resolved exactly once but does not authorize the replacement
- **THEN** a fresh legacy permission request displays the replacement arguments before they can execute

#### Scenario: Rich revision does not approve execution
- **WHEN** the observed TUI submits schema-valid `revise_arguments`, hard hooks allow it, and preparation succeeds
- **THEN** the current request is resolved without allowing the action
- **THEN** the harness hard-checks and prepares the revision and emits a fresh request for an explicit allow or deny

#### Scenario: Revised preview governs final approval
- **WHEN** a user allows the fresh request created for a valid revision
- **THEN** execution uses exactly that request's effective arguments and approved prepared preview

#### Scenario: Schema-invalid revision leaves the request answerable
- **WHEN** `revise_arguments` supplies a schema-invalid value
- **THEN** the interaction returns typed field-level validation feedback and no sandbox or tool mutation begins
- **THEN** the current request remains unresolved so the user can correct the revision or deny it

#### Scenario: Hard gate rejects a schema-valid revision
- **WHEN** a schema-valid revision is accepted and a hard hook blocks, fails, or produces an invalid replacement
- **THEN** the old request remains resolved by the non-approving revision and the call terminates with the typed hook outcome
- **THEN** no replacement permission request, sandbox operation, or tool mutation begins

### Requirement: Unanswered prompts fail safe

A permission request that is never answered SHALL be treated as denied when the
turn's deadline passes or it is cancelled, with a reason naming the timeout, and
SHALL NOT hang.

#### Scenario: No answer resolves to denied on deadline or cancellation

- **WHEN** a prompt is emitted but never answered
- **AND** the turn's deadline passes or the run is cancelled
- **THEN** the action is treated as denied with a reason naming the timeout and the agent does not hang

### Requirement: Exactly one decision honored
Every permission request SHALL honor exactly one valid reply. For a legacy
request, the first decision on its existing reply path SHALL be honored and
additional answers MUST be ignored. For a rich request, the first well-formed
and applicable allow, deny, or `revise_arguments` reply matching its request ID
and revision SHALL resolve that request. A malformed, schema-invalid, mismatched,
or inapplicable reply MUST return typed feedback without consuming the reply;
duplicate or late replies after resolution MUST return an already-resolved
outcome without blocking a sender or affecting execution. A successful rich
argument revision SHALL create a new request ID and revision, so resolving the
old request does not pre-approve the new one.

#### Scenario: Second answer to one prompt is ignored
- **WHEN** a frontend sends two replies for one request ID and revision
- **THEN** the first valid reply returns `accepted` and is honored
- **THEN** the second returns `already_resolved` without a wedge or effect

#### Scenario: Late approval cannot authorize a newer revision
- **WHEN** revision one is resolved by `revise_arguments` and its old reply path later receives allow
- **THEN** that allow is ignored and revision two remains pending until its own request receives one reply

#### Scenario: Reply identifiers must match
- **WHEN** a reply names a different request ID, tool-call ID, or revision than the open request
- **THEN** it does not resolve or authorize the open request

#### Scenario: Invalid grant selection does not consume the request
- **WHEN** an allow reply contains malformed or inapplicable sandbox grants
- **THEN** the reply returns `validation_rejected` with typed feedback and the request remains open
- **THEN** the user can correct the selection or deny the request

### Requirement: Per-call sandbox grants are explicit and ephemeral
A rich permission request for a sandboxed command SHALL expose the effective
baseline sandbox posture and the supported additive grant dimensions. An allow
reply MAY include validated extra read roots, extra write roots, or network
access, and the executor SHALL bind those grants only to the matching approved
call revision. Grants MUST NOT weaken hooks, apply to another call, survive an
argument revision unless resubmitted, or persist through a remembered permission
rule.

#### Scenario: Approved grants bind to one command revision
- **WHEN** the user allows a command revision with an extra read root
- **THEN** that root is added only to that revision's sandbox policy before command execution
- **THEN** a later command receives no such grant unless separately approved

#### Scenario: Revision clears prior grant selection
- **WHEN** a request carrying selected grants is answered with `revise_arguments`
- **THEN** the new permission request recomputes its effective sandbox facts and requires any grants to be selected again

#### Scenario: Remember does not persist a grant
- **WHEN** a command is allowed with both remember and a network grant
- **THEN** only the derived permission rule is eligible for persistence
- **THEN** future matching commands do not inherit network access from that decision

#### Scenario: Invalid or inapplicable grant fails safe
- **WHEN** a reply supplies a malformed grant or grants for an action not governed by the command sandbox
- **THEN** the reply does not execute the action and the interaction reports a typed validation error

#### Scenario: Bypass still obeys sandbox posture
- **WHEN** bypass mode suppresses permission prompts for a command
- **THEN** it adds no per-call grants and the configured sandbox policy still governs execution

### Requirement: Interactive bypass confirmation belongs to the frontend
The typed SDK mode API SHALL remain UI-agnostic and SHALL allow a programmatic
caller to select bypass directly while idle or running. An interactive TUI MUST require
an explicit confirmation before sending a user-initiated transition to bypass,
and cancellation of that confirmation MUST leave the prior mode unchanged. This
frontend guard SHALL be presented as an informed-use safeguard, not as a hard
security boundary; hooks and sandbox remain the unconditional guardrails.

#### Scenario: TUI user confirms bypass
- **WHEN** an idle or running TUI user selects bypass and confirms the warning
- **THEN** the frontend sends the typed mode change and displays bypass only after the SDK accepts it

#### Scenario: TUI user cancels bypass
- **WHEN** the bypass confirmation is dismissed or denied
- **THEN** the frontend sends no bypass mode change and the existing mode remains visible and active

#### Scenario: SDK caller selects bypass without terminal UI
- **WHEN** a programmatic SDK caller selects bypass while idle or during a run
- **THEN** no terminal-specific confirmation is imposed by the permission engine
- **THEN** hard hooks and sandbox behavior remain unchanged
