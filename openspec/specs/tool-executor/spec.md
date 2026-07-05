# tool-executor Specification

## Purpose
TBD - created by archiving change phase-2-tools-executor. Update Purpose after archive.
## Requirements
### Requirement: Single ordered execution chain

The executor SHALL route every tool call — built-in or custom — through exactly
one ordered chain of stages: before-tool hard hooks → human permission → sandbox
→ execute → after-tool hard hooks. No tool call SHALL reach the user's machine by
any other route. The executor SHALL fulfil the existing `Dispatcher` seam without
changing its signature.

#### Scenario: Every capability travels the one path

- **WHEN** a read, write, edit, content-search, file-find, shell, or custom tool call is dispatched
- **THEN** it passes through the same ordered chain and none has a private bypass route

#### Scenario: Fixed order is observable

- **WHEN** a shell call is dispatched through an instrumented chain
- **THEN** the stages run in exactly this order — before-tool hard hooks → human permission → sandbox → execute → after-tool hard hooks — and the order is asserted in tests, not merely intended

### Requirement: Sandbox routing for command execution only

The sandbox stage SHALL apply to the shell tool and to any custom tool that
declares it runs commands. The sandbox stage SHALL be skipped for read, write,
edit, content-search, and file-find calls.

#### Scenario: Shell routes through the sandbox stage

- **WHEN** a shell call (or a command-declaring custom tool) is dispatched
- **THEN** the sandbox stage is entered before the tool executes

#### Scenario: File operations skip the sandbox stage

- **WHEN** a read, write, edit, content-search, or file-find call is dispatched
- **THEN** the sandbox stage is skipped and the order is hard pre-checks → permission → execute → post-checks

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
boundary, remain valid text, and carry a machine-legible marker stating how much
was elided. Truncation SHALL apply uniformly to every tool's output.

#### Scenario: Over-budget output is clipped with a marker

- **WHEN** a tool returns output exceeding the configured budget
- **THEN** the output is clipped to the budget on a clean character boundary, remains valid text, and carries a machine-legible marker stating how much was elided

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
