# tool-executor Specification

## Purpose
TBD - created by archiving change phase-2-tools-executor. Update Purpose after archive.
## Requirements
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

### Requirement: Sandbox routing for command execution only

The sandbox stage SHALL apply to the shell tool and to any custom tool that
declares it runs commands. For those command-running tools, the sandbox stage
SHALL enforce the active sandbox policy before the tool's command execution is
allowed to affect the host. The sandbox stage SHALL be skipped for read, write,
edit, content-search, and file-find calls.

#### Scenario: Shell routes through the sandbox stage

- **WHEN** a shell call (or a command-declaring custom tool) is dispatched
- **THEN** the sandbox stage is entered before the tool executes
- **THEN** the active sandbox policy is applied to the command execution

#### Scenario: File operations skip the sandbox stage

- **WHEN** a read, write, edit, content-search, or file-find call is dispatched
- **THEN** the sandbox stage is skipped and the order is hard pre-checks → permission → execute → post-checks

#### Scenario: Sandbox block prevents command execution

- **WHEN** the sandbox stage blocks a shell call
- **THEN** the command does not run outside the sandbox policy
- **THEN** the executor returns an error result tied to the originating tool call

### Requirement: Inert placeholder stages

The sandbox stage SHALL remain a drop-in stage supplied by its own phase, the
permission stage SHALL be a real human-in-the-loop gate, and the hard pre-check
and hard post-check stage slots SHALL be filled by the hooks capability without
altering the executor path. A session with no matching hooks SHALL pass through
the hard stages exactly as the Phase 2 inert placeholders did.

#### Scenario: No configured hooks preserves pass-through behavior

- **WHEN** a read, edit, or shell call runs through the chain with no matching hooks configured and the permission stage allowing
- **THEN** the hard hook stages allow the call and the result is identical to what the tool would produce on its own

### Requirement: Hard pre-check short-circuit

When a before-tool hard hook blocks a call, the executor SHALL prevent
permission, the sandbox, and the tool's work from running, and SHALL return an
error result carrying the block's reason. There SHALL be no path for the model or
permission bypass mode to override a hard block.

#### Scenario: Pre-check block stops all downstream work

- **WHEN** a before-tool hard hook blocks a call
- **THEN** permission, sandbox, and the tool never run, and the result is an error carrying the block's reason

#### Scenario: Permission bypass does not affect hard pre-check

- **WHEN** permission is configured to bypass human prompts
- **WHEN** a before-tool hard hook blocks a call
- **THEN** the call is still blocked before permission, sandbox, or tool execution

### Requirement: Permission denial short-circuit

When the permission stage denies a call, the executor SHALL prevent the sandbox
and the tool's work from running and SHALL return a "permission denied" error
result carrying the reason.

#### Scenario: Denial stops sandbox and tool

- **WHEN** the permission stage denies a call
- **THEN** the sandbox and the tool never run, and the result is a clear permission-denied error carrying the reason

### Requirement: Edited-argument re-validation

When the permission stage returns edited arguments, the executor SHALL re-validate
them against the tool's declared shape and run the tool on the corrected
arguments, not the originals.

#### Scenario: Tool runs on corrected arguments

- **WHEN** the permission stage returns edited arguments that fit the tool's declared shape
- **THEN** the edited arguments are re-validated and the tool runs on them, not on the originals

### Requirement: Hard post-check veto

The executor SHALL apply after-tool hard hook verdicts before handing a tool
result back to the model. A blocking verdict SHALL turn the result into an error
carrying the block's reason, and a replacement verdict SHALL replace the result
content the model receives.

#### Scenario: Post-check turns success into a blocked error

- **WHEN** an after-tool hard hook blocks an otherwise-successful result
- **THEN** the result handed back to the model becomes an error carrying the block's reason

#### Scenario: Post-check replacement reaches the model

- **WHEN** an after-tool hard hook replaces an otherwise-successful result
- **THEN** the result handed back to the model contains the replacement content rather than the original content

### Requirement: Unknown tool and argument validation short-circuit

A call naming an unregistered tool SHALL short-circuit to an "unknown tool" error
before any stage runs. Arguments that do not fit the tool's declared shape SHALL
be rejected with a validation error before the tool's work runs.

#### Scenario: Unknown tool fails before any stage

- **WHEN** a call names a tool that is not registered
- **THEN** it short-circuits to a clear "unknown tool" error before any stage runs

#### Scenario: Malformed arguments rejected before execution

- **WHEN** a call's arguments do not fit the tool's declared shape
- **THEN** it is rejected with a validation error and the tool's work is never invoked with bad input

### Requirement: Failures are results, not crashes

Any tool-, input-, or I/O-level failure anywhere on the path SHALL surface as an
error result the model can read, never a crash. Each result SHALL be tied back to
its originating call.

#### Scenario: A failing tool returns a readable error result

- **WHEN** a tool fails on a missing file, a bad command, or a denied action
- **THEN** an error result the model can read is returned, tied to its originating call, and the session does not crash

### Requirement: Custom tools are first-class

A custom tool, when invoked, SHALL travel the identical ordered path as a built-in
with no special-casing — gated, sandboxed where it declares command execution, and
truncated by the same budget.

#### Scenario: Custom tool gets identical treatment

- **WHEN** a registered custom tool is invoked
- **THEN** it travels the same ordered path as a built-in, is sandboxed only if it declares command execution, and its output is truncated by the same budget

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

### Requirement: Cancellation on the path

Every blocking operation on the path SHALL be cancellable. When the surrounding
work is cancelled, the call SHALL return a cancellation error result and stop any
child work.

#### Scenario: Cancellation returns an error result and stops child work

- **WHEN** the context is cancelled while a call is on the path
- **THEN** the call returns a cancellation error result and any in-flight child work is stopped

### Requirement: Permission stage shares the live event stream

The permission stage SHALL be given the same live event stream the frontend is
draining, so a future permission prompt can reach the human and its answer can
flow back without the loop mediating.

#### Scenario: Permission stage can emit on the live stream

- **WHEN** the executor invokes the permission stage
- **THEN** the stage receives the same `emit` stream the frontend drains, able to raise a permission request and block on its reply path

### Requirement: Hook-edited arguments are revalidated
When a before-tool hard hook returns edited arguments, the executor SHALL
revalidate those arguments against the tool's declared shape before consulting
permission or running the tool.

#### Scenario: Invalid hook edit blocks the call
- **WHEN** a before-tool hook returns edited arguments that do not fit the tool's declared shape
- **THEN** the call is blocked with a validation error result
- **THEN** permission, sandbox, and the tool do not run

#### Scenario: Valid hook edit proceeds
- **WHEN** a before-tool hook returns edited arguments that fit the tool's declared shape
- **THEN** the edited arguments are the arguments passed to later stages

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

### Requirement: Command handlers use the sandbox runner

A command-running tool SHALL preserve its handler-owned argument validation and
result semantics while launching each child process only through the command
runner supplied by the sandbox stage. A tool that declares command execution but
does not implement the command-handler contract MUST fail closed before its
ordinary `Execute` path can launch an unrestricted process.

#### Scenario: Custom command handler keeps its semantics

- **WHEN** a custom command handler validates or transforms non-standard arguments
- **THEN** the sandbox invokes that handler rather than executing a raw argument directly
- **THEN** each resulting child process is launched through the active sandbox runner
- **THEN** the handler may post-process the runner output

#### Scenario: Unadapted command handler fails closed

- **WHEN** a tool declares that it runs commands but does not implement the command-handler contract
- **THEN** the sandbox returns a readable error result
- **THEN** the tool's ordinary execution method is not called

### Requirement: Real sandbox stage preserves the ordered chain
The executor SHALL fill the existing sandbox stage with the real sandbox
implementation without changing the chain order, the dispatcher seam, or the
surrounding permission and hook semantics.

#### Scenario: Hook and permission order is unchanged
- **WHEN** a command-running tool call is dispatched with real sandboxing enabled
- **THEN** before-tool hard hooks run before permission
- **THEN** permission runs before the sandbox
- **THEN** the sandbox runs before command execution
- **THEN** after-tool hard hooks run after a successful command result

#### Scenario: Permission denial still stops sandbox
- **WHEN** the permission stage denies a command-running tool call
- **THEN** the sandbox stage does not run
- **THEN** the command does not execute

### Requirement: Sandbox failures are tool failures
Sandbox-level failures SHALL surface as recoverable tool error results rather
than harness crashes. The executor MUST attach the error result to the
originating tool call and allow the loop to continue according to normal
recoverable tool failure behavior.

#### Scenario: Sandbox denial returns error result
- **WHEN** the sandbox denies a command-running tool call
- **THEN** the executor returns a `ToolResult` for that call
- **THEN** the result has `IsError` true
- **THEN** the run does not crash

#### Scenario: Sandbox backend error returns error result
- **WHEN** the active sandbox backend fails while preparing or running a command
- **THEN** the executor returns a readable error result tied to the call
- **THEN** the run does not crash

### Requirement: Event-aware handlers remain inside the ordered chain
The executor SHALL support an internal optional handler form that can receive the
live event emitter only at the existing tool-execution slot. Resolution and
argument validation, before-tool hooks, permission, sandbox routing when the
handler runs commands, after-tool hooks, output truncation, and cancellation
MUST retain their existing order and semantics. The optional form MUST NOT change
the required public `ToolHandler` or `Dispatcher` contracts and MUST NOT create a
second dispatch path.

#### Scenario: Event-aware task observes the full executor order
- **WHEN** the `task` handler is dispatched with instrumented validation, hooks, permission, and output truncation
- **THEN** every existing stage runs in its defined order and only the final handler invocation receives the live emitter

#### Scenario: Pre-hook block prevents child construction
- **WHEN** a before-tool hook blocks an event-aware task call
- **THEN** permission and the task handler do not run, no child starts, and the executor returns the normal blocked error result

#### Scenario: Permission denial prevents child construction
- **WHEN** permission denies an event-aware task call
- **THEN** the task handler does not run, no child starts, and the executor returns the normal permission-denied result

#### Scenario: Post-hook and truncation still govern task output
- **WHEN** an event-aware task handler returns a child result
- **THEN** after-tool hooks inspect or replace it and the central output budget bounds the final tool result exactly as for an ordinary handler

#### Scenario: Existing handlers require no changes
- **WHEN** a built-in or custom handler implements only the existing required `ToolHandler` methods
- **THEN** it compiles and executes through the unchanged ordinary invocation path

### Requirement: Event-aware handlers share the dispatch emitter
The executor SHALL pass an event-aware handler the same live emitter received by
`Dispatch`. The handler MUST NOT use a global event sink or a second outward
channel, and an emitter failure MUST remain subject to the surrounding context's
cancellation and backpressure behavior.

#### Scenario: Task lifecycle uses the parent run stream
- **WHEN** an event-aware task handler emits subagent lifecycle status or forwards a child permission request
- **THEN** the event travels through the same emitter used by the parent dispatch and no side channel is created

#### Scenario: Abandoned parent stream does not strand the handler
- **WHEN** the shared emitter fails because the parent stream is no longer accepting events
- **THEN** the task handler cancels its derived child work and returns without leaving a blocked descendant

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
