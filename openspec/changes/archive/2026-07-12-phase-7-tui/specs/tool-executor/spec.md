## MODIFIED Requirements

### Requirement: Single ordered execution chain
The executor SHALL route every tool call, built-in or custom, through exactly
one ordered chain: resolve and validate → before-tool hard hooks → action
preparation when supported → human permission → sandbox for command-running
tools → execute or commit → after-tool hard hooks → bounded result. No tool call
SHALL reach the user's machine by any other route. Each argument revision MUST
run through validation and matching before-tool hard hooks before it is prepared
or executed. A rich permission revision SHALL loop within this same dispatch
path through hard checks and preparation, producing a fresh permission request
only when both succeed; it MUST
NOT create a second dispatch path. The executor SHALL fulfil the existing
`Dispatcher` seam without changing its signature.

#### Scenario: Every capability travels the one path
- **WHEN** a read, write, edit, content-search, file-find, shell, task, or custom tool call is dispatched
- **THEN** it passes through the same ordered chain and none has a private bypass route

#### Scenario: Fixed order is observable
- **WHEN** a preview-capable mutating call is dispatched through an instrumented chain
- **THEN** resolution and validation precede before-tool hooks
- **THEN** preparation follows the hooks and precedes the permission request
- **THEN** sandbox when applicable and execution follow final permission
- **THEN** after-tool hooks and bounded result handling run last

#### Scenario: Hook replacement is prepared before permission
- **WHEN** a before-tool hook replaces valid arguments
- **THEN** the replacement is revalidated and prepared
- **THEN** permission sees the replacement arguments and their preview rather than the provider's original action

#### Scenario: Rich permission revision stays on the same chain
- **WHEN** a rich permission request accepts schema-valid revised arguments and hard checks plus preparation succeed
- **THEN** the revision is revalidated and matching before-tool hooks run again
- **THEN** a replacement preview and fresh permission request are produced before any sandbox or mutation

#### Scenario: Revision hard check or preparation fails on the same chain
- **WHEN** an accepted schema-valid revision is blocked by hard hooks or cannot be prepared
- **THEN** the call terminates through the same dispatch path with no fresh permission request
- **THEN** sandbox and tool execution do not begin

#### Scenario: Legacy edited approval retains compatibility without bypassing hooks
- **WHEN** a legacy permission decision allows a call with edited arguments
- **THEN** the executor preserves the legacy reply shape and exactly one honored reply for that request
- **THEN** it revalidates, reruns matching before-tool hooks, and reprepares the effective action
- **THEN** unchanged edited arguments may execute, while a hook replacement requires a fresh legacy request before execution

### Requirement: Central output truncation
Output exceeding the configured budget SHALL be truncated on a clean character
boundary, remain valid text, and retain the existing machine-legible textual
marker stating how much was elided for legacy callers. The observed run path MUST
also emit a structured `output_budget` omission correlated to the tool
call, stating the known original and retained sizes and that the omitted content
is not recoverable from the stream. Truncation SHALL apply uniformly to every
tool's success, error, or after-hook replacement output. Reversible frontend
folding MUST NOT alter the retained result or be reported as truncation.

#### Scenario: Over-budget output is clipped with legacy and observed signals
- **WHEN** a tool returns output exceeding the configured budget
- **THEN** the retained text is clipped to the budget on a clean character boundary and remains valid text
- **THEN** the legacy result carries its existing elision marker
- **THEN** the observed run emits one correlated structured omission with known size and recoverability fields

#### Scenario: Error and replacement output use the same bound
- **WHEN** an error result or an after-tool replacement exceeds the configured budget
- **THEN** it receives the same valid-text truncation, legacy marker, and structured observed omission as a successful result

#### Scenario: In-budget output has no truncation omission
- **WHEN** the complete tool result fits within the configured budget
- **THEN** the result is retained unchanged and no `output_budget` omission is emitted

#### Scenario: Collapsing a result remains presentation only
- **WHEN** a frontend folds an in-budget retained result
- **THEN** the harness records no irreversible omission and expanding the fold can reveal the result

## ADDED Requirements

### Requirement: Correlated effective action facts
The executor SHALL preserve the provider-supplied call as immutable provenance
and SHALL expose each subsequent effective argument revision with its source,
revision, and original tool-call correlation ID to the observed run path. The
latest prepared fact before execution MUST identify the arguments and preview
that will govern that execution; a completion fact MUST correlate to that same
call and approved revision. Frontends MUST NOT need to compare argument prose or
tool names to update one action card.

#### Scenario: Unchanged arguments have one effective revision
- **WHEN** neither a hook nor permission changes a tool call's arguments
- **THEN** the observed prepared fact identifies revision one as effective and correlates it to the provider call ID

#### Scenario: Argument provenance remains distinguishable
- **WHEN** a before-tool hook replaces arguments and a user later revises them
- **THEN** the observed facts distinguish provider, hook, and user revision sources
- **THEN** the latest prepared revision is the one offered for final approval and execution

#### Scenario: Completion identifies the approved revision
- **WHEN** a revised prepared action completes or fails
- **THEN** its observed completion carries the same tool-call ID and approved revision as its prepared fact

#### Scenario: Legacy event projection remains compatible
- **WHEN** an existing frontend consumes the legacy run API
- **THEN** it continues to receive the existing tool-start and tool-finished shapes and ordering
- **THEN** it is not required to understand observed revisions or previews

### Requirement: Preview precedes every mutation
For a preview-capable mutating handler, the executor SHALL obtain a prepared
action and make its latest preview available to the applicable permission flow
before commit. No sandboxed command or file mutation may begin while a rich
permission revision is unapproved. If preparing or delivering the required
permission request fails or is cancelled, the executor MUST fail closed without
executing the action.

#### Scenario: Auto-approved edit is still prepared first
- **WHEN** permission mode or a rule allows a preview-capable edit without a human prompt
- **THEN** the executor prepares the effective action before committing it
- **THEN** the committed candidate is the one represented by the correlated observed preview

#### Scenario: Prompted edit shows the current preview
- **WHEN** a preview-capable edit requires permission through the rich observed protocol
- **THEN** the rich request carries the current effective arguments and prepared preview before the user decides

#### Scenario: Preview delivery failure prevents mutation
- **WHEN** a rich permission request cannot be delivered or its context is cancelled
- **THEN** the executor returns a recoverable denial or cancellation result and does not mutate the target

### Requirement: Tool lifecycle duration is measured consistently
The observed completion for every dispatched tool call SHALL report a
non-negative monotonic elapsed duration correlated to that call. The duration
MUST cover the call's executor lifecycle, including gate and permission wait
time, and MUST be present for success, recoverable error, denial, and
cancellation without relying on wall-clock subtraction.

#### Scenario: Successful tool reports elapsed duration
- **WHEN** a tool passes its gates and completes successfully
- **THEN** its observed completion reports a non-negative duration for the same call ID

#### Scenario: Denied or failed tool still reports elapsed duration
- **WHEN** a call is denied, blocked, cancelled, or returns an error result
- **THEN** its observed completion reports the elapsed executor lifecycle without fabricating a successful execution duration
