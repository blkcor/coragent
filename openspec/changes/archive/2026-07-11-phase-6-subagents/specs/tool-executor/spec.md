## ADDED Requirements

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
