## MODIFIED Requirements

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

## ADDED Requirements

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
