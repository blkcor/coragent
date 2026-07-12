## MODIFIED Requirements

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

### Requirement: Modes switch between turns and start from configuration
When the standard permission engine owns control, the permission stage SHALL
begin in the typed mode named by configuration and SHALL expose that ownership
and typed current mode through the public session descriptor. A public typed
mode-change operation SHALL accept only default,
auto-accept-edits, plan, or bypass and SHALL apply a change only while no turn is
in flight, so the new mode governs the complete next turn. A mid-turn change
MUST return a stable typed error and leave the current mode unchanged. The
existing string mode setter SHALL remain as a compatibility wrapper over these
rules. Its documented between-turn behavior and signature MUST remain compatible;
a mid-run call that previously had no documented guarantee SHALL now return the
same stable in-flight error as the typed operation and leave mode unchanged.

#### Scenario: Configured starting mode governs the first turn
- **WHEN** a valid starting mode is set in configuration
- **THEN** that typed mode appears in the initial descriptor and governs actions from the first turn

#### Scenario: Switching mode before the next turn takes effect
- **WHEN** a different valid mode is selected while the session is idle
- **THEN** the change succeeds, a fresh descriptor reports it, and the new mode governs every action in the subsequent turn

#### Scenario: Mid-turn switch is rejected atomically
- **WHEN** a caller tries to change mode while a turn or permission interaction is in flight
- **THEN** the operation returns the stable in-flight mode-change error
- **THEN** the governing mode and descriptor state remain unchanged for that turn

#### Scenario: Existing string setter rejects out-of-contract mid-run use
- **WHEN** an existing caller invokes the string mode setter during a run
- **THEN** the method signature remains unchanged but returns the stable in-flight error
- **THEN** valid between-turn string calls retain their prior behavior

#### Scenario: Unknown typed mode is rejected
- **WHEN** a caller supplies a mode value outside the four defined modes
- **THEN** the change is rejected without altering current state

#### Scenario: Custom permission ownership is explicit
- **WHEN** a session uses a custom dispatcher and does not expose the default permission engine
- **THEN** its descriptor and mode-change result report permission ownership as caller-owned or unsupported rather than inventing a default mode

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

## ADDED Requirements

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
caller to select bypass directly between turns. An interactive TUI MUST require
an explicit confirmation before sending a user-initiated transition to bypass,
and cancellation of that confirmation MUST leave the prior mode unchanged. This
frontend guard SHALL be presented as an informed-use safeguard, not as a hard
security boundary; hooks and sandbox remain the unconditional guardrails.

#### Scenario: TUI user confirms bypass
- **WHEN** an idle TUI user selects bypass and confirms the warning
- **THEN** the frontend sends the typed mode change and displays bypass only after the SDK accepts it

#### Scenario: TUI user cancels bypass
- **WHEN** the bypass confirmation is dismissed or denied
- **THEN** the frontend sends no bypass mode change and the existing mode remains visible and active

#### Scenario: SDK caller selects bypass without terminal UI
- **WHEN** a programmatic SDK caller selects bypass between turns
- **THEN** no terminal-specific confirmation is imposed by the permission engine
- **THEN** hard hooks and sandbox behavior remain unchanged
