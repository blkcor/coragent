## ADDED Requirements

### Requirement: Focused task delegation
The system SHALL expose one delegation capability that accepts a non-blank
human-readable label, a non-blank child instruction, and an optional list of tool
names, and SHALL run the accepted instruction through a child instance of the
existing agent loop.

#### Scenario: Delegation starts a child loop
- **WHEN** the model invokes the delegation capability with a valid label and instruction
- **THEN** the system starts one child loop for that instruction and identifies the work by the supplied label

#### Scenario: Invalid delegation arguments do not start a child
- **WHEN** the model omits the label or instruction or supplies either in an invalid shape
- **THEN** the call returns a recoverable argument error and no child loop starts

### Requirement: Fresh isolated child context
Each child's base conversation SHALL begin with its own system framing and the
delegated instruction and MUST NOT contain any turn from the parent's prior
conversation. Inherited lifecycle hooks SHALL retain their existing ability to
inject context produced for the child invocation; such context MUST be
child-scoped rather than copied parent history. All intermediate child turns
SHALL remain private.

#### Scenario: Parent history is absent from child input
- **WHEN** a parent with prior user, assistant, and tool-result turns delegates an instruction and no child lifecycle hook injects context
- **THEN** the first child provider request contains only child framing and that delegated instruction, with none of the parent's prior turns

#### Scenario: Child lifecycle injection stays isolated
- **WHEN** an inherited session-start or prompt-submit hook injects context for a child
- **THEN** the child provider sees that child-scoped injection plus its framing and instruction, and still sees none of the parent's prior turns

#### Scenario: Child intermediate work stays private
- **WHEN** a child completes multiple model rounds and tool calls
- **THEN** none of its intermediate assistant turns, tool calls, or tool results are appended to the parent conversation

### Requirement: Restricted child capabilities
For an explicit non-empty tool list, the child SHALL receive only the intersection
of those names and the parent's effectively advertised-and-executable ordinary
tools. For an omitted or empty tool list, the child SHALL receive only the
available read-only defaults `read_file`, `search_content`, and `find_files`.
The executable child catalog MUST match the advertised child descriptor names
and SHALL preserve the exact effective parent descriptors. Each nested
delegation SHALL derive from its immediate parent's restricted ordinary catalog,
so descendant capabilities MUST narrow monotonically and MUST NOT recover a tool
removed by an earlier generation.

#### Scenario: Explicit subset narrows advertised and executable tools
- **WHEN** a delegation requests `read_file` and `write_file` from a parent that has both plus other tools
- **THEN** the child is advertised and can resolve only those two ordinary tools, apart from the orchestrator-controlled delegation capability

#### Scenario: Omitted tools use safe read-only defaults
- **WHEN** a delegation omits the tool list or supplies an empty list
- **THEN** the child receives only the parent's available read, content-search, and file-find handlers and receives no write, edit, or command handler

#### Scenario: Out-of-set call cannot run
- **WHEN** a child emits a call for a tool outside its restricted executable catalog
- **THEN** the child executor returns a recoverable not-available result and the disallowed handler is not invoked

#### Scenario: Parent-hidden handler does not reappear
- **WHEN** a handler is registered in the parent catalog but omitted from the descriptors the parent session advertises
- **THEN** a child cannot receive that handler through either explicit selection or the safe default

#### Scenario: Descendant cannot recover a tool removed from its parent
- **WHEN** the root can execute a write or command tool, the first child does not receive that tool, and the child requests it for a grandchild
- **THEN** the grandchild neither advertises nor resolves the removed tool, regardless of whether it is requested explicitly or would otherwise match a default name

### Requirement: Depth-governed nested delegation
The orchestrator SHALL control child access to delegation independently of the
requested ordinary-tool list, SHALL permit nesting through at most three
delegation edges from the root, and SHALL refuse the next delegation with a clear
depth-limit error without constructing another child.

#### Scenario: Requested tools cannot grant recursion authority
- **WHEN** a delegation explicitly includes the delegation tool name in its requested tool list
- **THEN** that name is excluded from the ordinary restricted set and recursion capability is determined only by the orchestrator's current depth

#### Scenario: Delegation within the depth budget succeeds
- **WHEN** a child at a depth below three delegates another valid task
- **THEN** the system starts the next child with its depth incremented by one

#### Scenario: Delegation beyond the depth budget is refused
- **WHEN** a child at depth three attempts another delegation
- **THEN** the system returns a recoverable result naming the depth limit, starts no child, and leaves the calling loop able to continue

### Requirement: Final-result-only return
On successful child completion, the delegation SHALL return only the content of
the child's final assistant turn as one tool result. The existing executor output
budget SHALL bound that result with its standard truncation marker; an empty final
answer SHALL remain a successful empty result.

#### Scenario: Final answer returns without intermediate text
- **WHEN** a child performs intermediate model and tool rounds and then completes with a final answer
- **THEN** the parent receives one successful task tool result containing only that final answer

#### Scenario: Oversized answer uses central truncation
- **WHEN** a child's final answer exceeds the executor output budget
- **THEN** the parent receives a non-error bounded result with the standard output-truncated marker

#### Scenario: Empty answer succeeds
- **WHEN** a child completes with an explicitly empty final assistant answer
- **THEN** the parent receives one non-error task result with empty content

### Requirement: Recoverable child terminal failures
The system SHALL convert a child provider failure, missing or malformed final
reply, or step-limit stop into a short recoverable error result for the
originating delegation, and MUST NOT crash or terminate the parent loop. For a
successful reply, a reply-ended event with a non-nil payload and one of the three
defined ending reasons SHALL be the unique final provider event. The system MUST
NOT emit, record, or dispatch anything that follows reply-ended, while still
draining the provider channel through closure. A non-nil trailing provider error
SHALL retain provider-error precedence over the simultaneous protocol failure.

#### Scenario: Child provider failure returns to parent loop
- **WHEN** the child provider ends with a failure
- **THEN** the task call returns an error result describing the child failure and the parent loop may continue

#### Scenario: Child provider closes without a completed reply
- **WHEN** the child provider stream closes without an error or a reply-ended event
- **THEN** the task call returns a recoverable missing-reply error rather than treating the absence of content as an explicit empty answer

#### Scenario: Child provider emits a malformed reply end
- **WHEN** the child provider emits a reply-ended event with no payload or with an undefined ending reason
- **THEN** the task call returns a recoverable protocol error, records no assistant turn, and executes no tool call from that reply
- **THEN** the provider channel is still drained through closure so no producer work is orphaned

#### Scenario: Child provider emits a non-error event after reply end
- **WHEN** the child provider emits text, a tool call, another reply-ended event, or another non-error event after its reply-ended event
- **THEN** the task call returns a recoverable protocol error, no trailing event reaches the parent stream or child conversation, and no trailing tool call executes
- **THEN** the provider channel is still drained through closure so no producer work is orphaned

#### Scenario: Child provider fails after reply end
- **WHEN** the child provider emits a non-nil error event after its reply-ended event
- **THEN** the task call returns a recoverable result describing that provider error rather than the simultaneous protocol error
- **THEN** the error event is not emitted or recorded and the provider channel is still drained through closure

#### Scenario: Child step limit returns a bounded error
- **WHEN** the child reaches its maximum model rounds without completing
- **THEN** the task call returns a recoverable step-limit error rather than treating partial child text as the final answer

### Requirement: Descendant cancellation propagation
The context governing the parent tool call SHALL govern the child loop and every
nested descendant, including their provider streams, tools, and commands. On
parent cancellation the system MUST stop descendant work promptly and MUST NOT
leave an orphaned child running. The parent run SHALL end with its existing
cancelled outcome rather than recording cancellation as a recoverable task result
and continuing.

#### Scenario: Parent cancellation stops a child provider stream
- **WHEN** the parent context is cancelled while a child is waiting on its provider
- **THEN** the child stream stops, the parent run returns its cancelled outcome promptly, and no child work continues

#### Scenario: Parent cancellation stops a grandchild command
- **WHEN** the parent context is cancelled while a grandchild has a command running
- **THEN** cancellation reaches the grandchild command, all intermediate child loops terminate without orphaned work, and the parent run ends cancelled

### Requirement: Inherited safety chokepoint
The delegation call and every child tool call SHALL flow through the existing
ordered executor chain. Children SHALL reuse the parent's effective hook,
permission, and sandbox behavior, while each child mutation remains independently
subject to those gates.

#### Scenario: Delegation itself passes through parent gates
- **WHEN** a parent before-tool hook blocks the task call
- **THEN** no child starts and no downstream permission or handler execution occurs

#### Scenario: Child mutation is gated normally
- **WHEN** an allowed child toolset includes a mutating tool and the child invokes it
- **THEN** the child call passes through hooks, permission, and sandbox routing exactly as the same call would in the parent

#### Scenario: Child lifecycle hooks remain active
- **WHEN** a child starts, submits its delegated prompt, finishes its run, and closes
- **THEN** the inherited session-start, prompt-submit, run-finished, and session-stop hooks execute with the child's isolated conversation and any injected context remains child-scoped

#### Scenario: Child startup hook block prevents provider work
- **WHEN** a child session-start or prompt-submit hook blocks the delegated run
- **THEN** no child provider request occurs and the task returns a recoverable error to the parent loop

#### Scenario: Child run-finished block preserves outcome
- **WHEN** a run-finished hook blocks after the child run outcome is known
- **THEN** the child retains that completed, failed, cancelled, or step-limit outcome exactly as a root session does

#### Scenario: Child cleanup block is surfaced
- **WHEN** a session-stop hook blocks after an otherwise successful child run
- **THEN** cleanup still completes and the task returns a recoverable cleanup error instead of hiding the hard hook outcome behind a successful answer

### Requirement: Sequential child execution
Delegations SHALL obey the existing sequential tool-call order, so a parent run
MUST await one child result before dispatching the next requested tool call.

#### Scenario: Multiple task calls run in model order
- **WHEN** one model round requests two delegations
- **THEN** the second child does not start until the first child has finished and returned its tool result
