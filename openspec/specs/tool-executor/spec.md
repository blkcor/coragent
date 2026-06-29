# tool-executor Specification

## Purpose
TBD - created by archiving change phase-2-tools-executor. Update Purpose after archive.
## Requirements
### Requirement: Single ordered execution chain

The executor SHALL route every tool call — built-in or custom — through exactly
one ordered chain of stages: hard pre-checks → human permission → sandbox →
execute → hard post-checks. No tool call SHALL reach the user's machine by any
other route. The executor SHALL fulfil the Phase 1 `Dispatcher` seam without
changing its signature.

#### Scenario: Every capability travels the one path

- **WHEN** a read, write, edit, content-search, file-find, or shell call is dispatched
- **THEN** it passes through the same ordered chain and none has a private bypass route

#### Scenario: Fixed order is observable

- **WHEN** a shell call is dispatched through an instrumented chain
- **THEN** the stages run in exactly this order — hard pre-checks → human permission → sandbox → execute → hard post-checks — and the order is asserted in tests, not merely intended

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

The permission, hard-check, and sandbox stages SHALL ship as pass-through
placeholders — allow-everything permission, never-block hard checks, run-directly
sandbox — that a later phase replaces without altering the path. Each placeholder
SHALL be a drop-in replacement.

#### Scenario: Placeholders produce the tool's own result

- **WHEN** a read, edit, or shell call runs through the chain with all placeholders in place
- **THEN** the result is identical to what the tool would produce on its own

### Requirement: Hard pre-check short-circuit

When a hard pre-check blocks a call, the executor SHALL prevent permission, the
sandbox, and the tool's work from running, and SHALL return an error result
carrying the block's reason. There SHALL be no path for the model to override a
hard block.

#### Scenario: Pre-check block stops all downstream work

- **WHEN** a hard pre-check blocks a call
- **THEN** permission, sandbox, and the tool never run, and the result is an error carrying the block's reason

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

When a hard post-check blocks an otherwise-successful result, the executor SHALL
turn that result into an error carrying the block's reason before handing it back.

#### Scenario: Post-check turns success into a blocked error

- **WHEN** a hard post-check blocks an otherwise-successful result
- **THEN** the result handed back to the model becomes an error carrying the block's reason

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

