# hooks Specification

## Purpose
TBD - created by archiving change phase-4-hooks. Update Purpose after archive.
## Requirements
### Requirement: Hook moments
The system SHALL support hooks at exactly six v1 lifecycle moments: session
start, prompt submit, before tool execution, after tool execution, run finished,
and session stop.

#### Scenario: Hooks fire at each v1 moment
- **WHEN** hooks are registered for session start, prompt submit, before tool, after tool, run finished, and session stop
- **THEN** each hook fires at its matching lifecycle moment

#### Scenario: Unsupported moments are rejected
- **WHEN** a hook definition names a lifecycle moment outside the v1 set
- **THEN** session construction fails with an error naming the offending hook

### Requirement: Hard blocking verdicts
A hook that returns a blocking verdict SHALL stop the gated action and SHALL NOT
be bypassable by permission mode or model retry.

#### Scenario: Blocking verdict stops action
- **WHEN** a hook returns a blocking verdict for an action
- **THEN** the action is stopped before any downstream work for that action runs
- **THEN** the model receives a readable error result or run error explaining the block

#### Scenario: Bypass mode does not bypass hooks
- **WHEN** permission is configured to bypass human prompts
- **THEN** a blocking hook still stops the action

### Requirement: Hook scopes
The system SHALL allow a hook to be scoped by tool name, by pattern over the
moment's relevant detail, by both together, or by no scope.

#### Scenario: Tool scope matches one tool
- **WHEN** a hook is scoped to a named tool
- **THEN** it fires for that tool and does not fire for other tools

#### Scenario: Pattern scope matches relevant detail
- **WHEN** a hook has a pattern scope over command text, file path, prompt text, or result text
- **THEN** it fires only when that moment's relevant detail matches the pattern

#### Scenario: Combined scopes require both matches
- **WHEN** a hook has both a tool-name scope and a pattern scope
- **THEN** it fires only when both scopes match

#### Scenario: Unscoped hook fires broadly
- **WHEN** a hook has no scope
- **THEN** it fires for every action at its configured moment

### Requirement: External command hooks
The system SHALL run external command hooks declared in settings by sending the
full hook event as JSON on stdin and consuming the command's exit status and
optional structured stdout as the hook verdict.

#### Scenario: Exit status is default verdict
- **WHEN** an external hook exits successfully without structured output
- **THEN** the action is allowed

#### Scenario: Failed exit blocks by default
- **WHEN** an external hook exits with a non-zero status without structured output
- **THEN** the action is blocked with a readable reason

#### Scenario: Structured output overrides exit status
- **WHEN** an external hook emits a valid structured verdict
- **THEN** the structured verdict determines block, reason, replacement, or injection behavior

### Requirement: In-process hooks
The system SHALL let SDK callers register in-process hooks that receive the same
hook event information and return the same verdict kinds as external hooks.

#### Scenario: In-process hook fires without a child process
- **WHEN** an SDK caller registers an in-process hook
- **THEN** the hook fires at its configured moment without spawning a separate process

#### Scenario: In-process and external parity
- **WHEN** equivalent in-process and external hooks return equivalent verdicts
- **THEN** the gated action observes equivalent behavior

### Requirement: Hook verdict composition
When multiple hooks match one moment, the system SHALL run them in deterministic
order, stop at the first blocking verdict, and compose non-blocking verdicts in
that order.

#### Scenario: First block wins
- **WHEN** multiple matching hooks run and one returns a blocking verdict
- **THEN** later hooks for that moment do not run
- **THEN** the first block's reason is the reason surfaced

#### Scenario: Non-blocking outputs compose
- **WHEN** multiple matching hooks return non-blocking argument edits, result replacements, or injected context
- **THEN** those outputs are applied in hook execution order

### Requirement: Before-tool input shaping
A before-tool hook SHALL be able to allow, block, or replace tool arguments
before permission, sandbox, or tool execution occurs. Matching before-tool hooks
MUST run again whenever permission supplies edited or revised arguments, including
the legacy allow-with-edited-arguments path and the rich `revise_arguments` path.
Each hook replacement SHALL be revalidated before later hooks or stages consume
it, and a block, hook failure, or invalid replacement on any revision MUST fail
closed before preparation, permission approval of that revision, sandbox, or tool
execution. A successful rich revision SHALL be prepared and presented in a fresh
permission request before it can run.

#### Scenario: Before-tool block short-circuits downstream stages
- **WHEN** a before-tool hook blocks a tool call
- **THEN** permission is not consulted for that revision
- **THEN** preparation, sandbox, and the tool body do not run

#### Scenario: Before-tool replacement is revalidated
- **WHEN** a before-tool hook replaces a tool call's arguments
- **THEN** the replacement arguments are validated against the tool's declared shape before the call proceeds

#### Scenario: Permission-edited arguments cannot bypass a matching hook
- **WHEN** a legacy permission decision allows a call with edited arguments
- **THEN** matching before-tool hooks inspect those edited arguments before execution
- **THEN** a hook block or invalid hook replacement prevents sandbox and tool execution

#### Scenario: Legacy hook replacement invalidates the old approval
- **WHEN** rerun hooks validly replace human-edited arguments from a legacy approval
- **THEN** the replacement is revalidated and prepared but does not execute under the old approval
- **THEN** a fresh legacy permission request presents the replacement before any sandbox or tool execution

#### Scenario: Rich revision is rechecked and reapproved
- **WHEN** a rich permission request receives revised arguments
- **THEN** matching before-tool hooks run against the revised effective call
- **THEN** a valid allowed hook result is revalidated and prepared
- **THEN** the harness issues a fresh permission request for that revision before execution

#### Scenario: Hook replacement composition remains deterministic on revision
- **WHEN** several matching before-tool hooks replace a permission revision
- **THEN** replacements compose in configured hook order with validation between replacements
- **THEN** the final valid replacement is the one prepared for the next permission request

### Requirement: After-tool result shaping
An after-tool hook SHALL be able to allow, block, or replace the tool result
before that result is recorded for the model.

#### Scenario: After-tool replacement reaches model
- **WHEN** an after-tool hook replaces a tool result
- **THEN** the model receives the replacement result rather than the original result

#### Scenario: After-tool block becomes error result
- **WHEN** an after-tool hook blocks a tool result
- **THEN** the model receives an error result carrying the block reason

### Requirement: Prompt-submit hooks
A prompt-submit hook SHALL run before the user's prompt is sent to the provider
and SHALL be able to block the turn or inject context into that turn.

#### Scenario: Prompt block prevents provider call
- **WHEN** a prompt-submit hook blocks a prompt
- **THEN** the prompt is not sent to the provider
- **THEN** the run ends or reports failure with the hook's readable reason

#### Scenario: Prompt injection precedes provider call
- **WHEN** a prompt-submit hook injects context
- **THEN** the injected context is present in the conversation before the provider is called

### Requirement: Session lifecycle hooks
Session-start hooks SHALL run before a session accepts work, and session-stop
hooks SHALL run during shutdown or completion cleanup.

#### Scenario: Session-start block aborts startup
- **WHEN** a session-start hook blocks
- **THEN** the session does not accept runs
- **THEN** the caller receives an error naming the block reason

#### Scenario: Session-stop failure cannot un-stop
- **WHEN** a session-stop hook blocks or fails while the session is stopping
- **THEN** the stop is recorded and surfaced
- **THEN** the session still stops

### Requirement: Run-finished hooks
A run-finished hook SHALL run after a run's terminal outcome has been determined
and before the terminal run-finished event is emitted.

#### Scenario: Run-finished hook observes terminal outcome
- **WHEN** a run reaches a terminal outcome
- **THEN** matching run-finished hooks run with the terminal outcome and current conversation snapshot
- **THEN** the terminal run-finished event is emitted after those hooks finish

#### Scenario: Run-finished failure cannot change terminal outcome
- **WHEN** a run-finished hook blocks or fails
- **THEN** the hook failure is surfaced as a hook outcome
- **THEN** the already-determined terminal outcome is preserved

### Requirement: Fail-closed hook failures
The system SHALL treat hook errors, missing external programs, timeouts,
malformed output, oversized output, panics, and cancellation as blocking
verdicts.

#### Scenario: Broken external hook blocks
- **WHEN** an external hook errors, times out, is missing, emits malformed output, or emits oversized output
- **THEN** the gated action is blocked with a readable reason

#### Scenario: Panicking in-process hook blocks
- **WHEN** an in-process hook panics
- **THEN** the panic is recovered
- **THEN** the gated action is blocked with a readable reason

#### Scenario: Cancelled hook stops child work
- **WHEN** cancellation reaches an in-flight external hook
- **THEN** the hook process and its children are stopped
- **THEN** no process or goroutine is leaked

### Requirement: Hook outcome events
Hook blocks, replacements, and injections SHALL surface on the existing run event
stream with enough information for any frontend to render what happened.

#### Scenario: Block outcome is observable
- **WHEN** a hook blocks an action
- **THEN** a hook outcome event appears on the run stream with the moment, hook name, action, and reason

#### Scenario: Replacement and injection outcomes are observable
- **WHEN** a hook replaces content or injects context
- **THEN** a hook outcome event appears on the run stream describing the action without requiring frontend access to internal hook state

