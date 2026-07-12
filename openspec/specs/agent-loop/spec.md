# Agent Loop

## Purpose

The gather→act→verify run lifecycle: a single run entry point drives the model
through repeated rounds of consulting the model and running requested tools until a
deterministic stop fires, surfacing everything as one ordered, read-only event
stream. Every tool request flows through a single dispatch seam so later-phase
permission, hooks, and sandbox apply universally with no bypass. Fully testable
offline against a scripted model and a stand-in dispatcher.
## Requirements
### Requirement: Single run entry point
The system SHALL preserve `Session.Run` as the existing legacy run entry point
and SHALL add `Session.RunObserved` as one opt-in observed-run entry point. A
caller MUST choose exactly one entry point for
an invocation, and either entry point SHALL start one run and return one live,
read-only event stream that the caller drains to completion. Both entry points
MUST drive the same single internal run lifecycle; neither entry point may create
a mirrored observation side channel or a second execution path.

#### Scenario: Start a legacy run and drain its stream
- **WHEN** a caller starts a run through the existing legacy entry point with the user's input
- **THEN** a live read-only legacy event stream is returned
- **THEN** the caller can drain that stream to completion

#### Scenario: Start an observed run and drain its stream
- **WHEN** a caller opts into the observed-run entry point with the user's input
- **THEN** a live read-only observed-event stream is returned
- **THEN** the caller can drain that stream to completion

#### Scenario: No second channel for one run
- **WHEN** anything observable happens during a run started through either entry point
- **THEN** it arrives on the one stream returned for that invocation and nowhere else
- **THEN** the watcher never inspects internal state or subscribes to a mirrored side channel

#### Scenario: Entry points share the in-flight guard
- **WHEN** either kind of run is already in flight and a caller starts another kind on the same session
- **THEN** the second start is cleanly refused
- **THEN** the first run and conversation history are unaffected

### Requirement: Gather-act-verify loop
The system SHALL drive a repeating cycle that assembles the conversation, consults
the model, runs any requested tools, feeds the results back, and repeats until a
stop fires.

#### Scenario: Continue automatically after a tool returns
- **WHEN** a tool returns its result to the loop
- **THEN** the next round begins with that result available to the model
- **THEN** no user prompting is required between rounds

#### Scenario: Continuation reflects the tool result
- **WHEN** a tool result returns and the next round begins
- **THEN** the model's continuation reflects that result with no extra prompting

### Requirement: Sequential ordered tool execution
The system SHALL execute multiple tool requests from one round sequentially, in the
order requested, each emitting a start then a finish before the next begins.

#### Scenario: Tools run one at a time in order
- **WHEN** the model requests several tools in one round
- **THEN** each tool produces a start then a finish before the next begins
- **THEN** the tools run in the order requested

### Requirement: Single dispatch path
The system SHALL route every tool request through one and only one dispatch path,
so later-phase stages apply to every tool with no bypass.

#### Scenario: Stand-in runner is the sole result producer
- **WHEN** any tool request is dispatched in Phase 1
- **THEN** it travels the single dispatch path
- **THEN** the stand-in runner is the only thing that ever produces a tool result

### Requirement: Incremental assistant text on the stream
The system SHALL emit each fragment of assistant text on the stream as it is
produced rather than buffering to the end of the reply.

#### Scenario: Text fragments appear as produced
- **WHEN** the model is producing text
- **THEN** each fragment appears on the stream as it is produced
- **THEN** no fragment waits on completion of the whole reply

### Requirement: Distinct tool-start and tool-finish events
The system SHALL emit a distinct tool-start carrying the request (its name and
arguments) and a distinct tool-finish carrying the result, bracketing each tool
invocation.

#### Scenario: Start and finish bracket a tool
- **WHEN** a tool runs
- **THEN** a tool-start event carrying the request, its name and arguments appears
- **THEN** a tool-finish event carrying the result appears after it

### Requirement: Status signals
The system SHALL emit status signals marking transitions among thinking, working a
tool, idle, subagent started, and subagent finished. Subagent lifecycle status
SHALL carry the delegation label. For a child that has started, a matching
finished status SHALL precede the task result on completed or failed paths while
the parent stream accepts events. A depth refusal MUST NOT emit child lifecycle
status because no child starts. Status signals are advisory and carry no control
semantics; parent cancellation MUST take precedence over forcing a finished
status onto a stream that no longer accepts ordinary events.

#### Scenario: Status marks each transition
- **WHEN** the agent consults the model
- **THEN** a thinking status is emitted
- **WHEN** tools run
- **THEN** a working-a-tool status is emitted
- **WHEN** the run ends
- **THEN** an idle status is emitted

#### Scenario: Frontend observes labeled child lifecycle
- **WHEN** a valid delegation labeled `find config defaults` starts and completes
- **THEN** the parent stream emits subagent-started and subagent-finished status signals carrying `find config defaults` in that order

#### Scenario: Failed child still finishes visibly
- **WHEN** a child starts and later fails while the parent stream remains writable
- **THEN** the parent stream emits the matching finished status before the task tool's error result completes

#### Scenario: Depth refusal emits no child lifecycle status
- **WHEN** a delegation is refused before construction because it would exceed the depth limit
- **THEN** the parent stream emits neither subagent-started nor subagent-finished for that refused delegation

#### Scenario: Cancellation remains authoritative
- **WHEN** parent cancellation makes the event stream reject ordinary events after a child has started
- **THEN** the run ends promptly with the existing cancelled outcome without waiting to force a subagent-finished status

### Requirement: Small named stable event vocabulary
The system SHALL deliver every observable event from a small, named, stable
vocabulary interpreted identically across different frontends.

#### Scenario: Frontends interpret a run identically
- **WHEN** the same run is consumed by different frontends
- **THEN** each interprets it identically from the same named events

### Requirement: Finish on its own when complete
The system SHALL finish on its own with the reason "completed" when the model
returns a reply with no tool request, then close the stream.

#### Scenario: Reply without tool request completes the run
- **WHEN** the model produces a reply with no tool request and that reply completes
- **THEN** the run finishes with reason "completed"
- **THEN** the stream closes

### Requirement: Step limit as a normal stop
The system SHALL enforce a configured maximum number of model rounds; on reaching
it the run ends with reason "reached the step limit", classified as a normal stop
(not an error), then the stream closes.

#### Scenario: A looping model is halted
- **WHEN** the model requests a tool on every round and the run reaches the
  configured maximum number of model rounds
- **THEN** the run ends with reason "reached the step limit"
- **THEN** that reason is classified as a normal stop, not an error
- **THEN** the stream closes

### Requirement: Exactly one named stop reason
The system SHALL end every run with exactly one outcome from the named set —
completed, reached the step limit, cancelled, failed — followed by the stream
closing; never two outcomes and never none.

#### Scenario: Every run ends with exactly one outcome
- **WHEN** any run ends
- **THEN** it ends with exactly one outcome from the named set
- **THEN** the stream closes afterward
- **THEN** there are never two endings and never none

#### Scenario: Step limit is distinguishable from failure
- **WHEN** a developer inspects the outcome of a run that reached the step limit
- **THEN** it is distinguishable from a failed outcome and classified as a normal stop

### Requirement: Cancellation propagates all the way down
The system SHALL propagate cancellation to the live model stream and any running
tool, ending promptly with a cancelled outcome and leaving no background work alive.

#### Scenario: Cancel mid-think
- **WHEN** the user cancels while the model is streaming
- **THEN** the live model stream is abandoned
- **THEN** the run ends promptly with a cancelled outcome and the stream closes
- **THEN** no further events follow

#### Scenario: Cancel mid-tool
- **WHEN** the user cancels while a tool is running
- **THEN** the cancellation reaches the running tool
- **THEN** the run ends promptly with a cancelled outcome
- **THEN** the agent does not hang on a stuck tool

#### Scenario: No background work survives cancellation
- **WHEN** a cancelled run ends
- **THEN** the live model stream and any running tool have been signalled to stop
- **THEN** no background work remains alive

### Requirement: Distinguishable cancellation outcome
The system SHALL make the cancelled outcome distinguishable from both a clean finish
and a backend failure.

#### Scenario: Cancellation is its own outcome
- **WHEN** a developer inspects the outcome of a cancelled run
- **THEN** it is distinguishable from a clean finish and from a backend failure

### Requirement: Recoverable tool failure
The system SHALL surface a tool failure as a failed result the model can see on the
next round and continue the run; it SHALL NOT crash and SHALL NOT end the run.

#### Scenario: A failed tool keeps the run going
- **WHEN** a tool fails and its failure comes back
- **THEN** it is surfaced on the stream as a finished-but-failed step
- **THEN** the run continues so the model can recover
- **THEN** the failed result is recorded for the model to see on the next round
- **THEN** no crash occurs

### Requirement: Backend failure ends the run with a named cause
The system SHALL end the run with a failed outcome that names the cause when the
model backend errors, treating no partial reply as a real turn.

#### Scenario: Provider error fails the run
- **WHEN** the model backend errors before or during a reply
- **THEN** the run ends with a failed outcome that names the cause
- **THEN** the stream closes
- **THEN** no partial reply is recorded as a real turn

### Requirement: Unrecoverable conditions distinguishable from tool failures
The system SHALL make a truly unrecoverable condition during dispatch end the run
with a failed outcome distinct from an ordinary, recoverable tool failure.

#### Scenario: Unrecoverable dispatch condition ends the run
- **WHEN** a truly unrecoverable condition occurs during dispatch
- **THEN** the run ends with a failed outcome
- **THEN** that outcome is distinct from an ordinary tool failure that would have
  continued the run

### Requirement: Wait-for-a-human pass-through
The system SHALL carry a wait-for-a-human question outward on the shared stream and
the answer back, without the loop interpreting it, positioned after a tool's start
and before its finish.

#### Scenario: Question rides the shared stream and resumes on answer
- **WHEN** the dispatch path pauses to ask a question
- **THEN** the question appears on the same stream the watcher is draining
- **WHEN** the watcher answers
- **THEN** the agent resumes and the tool completes, with no change to the loop

#### Scenario: The question is positioned in context
- **WHEN** a tool raises an approval question
- **THEN** the question appears after the tool starts and before it finishes

### Requirement: One in-flight run per conversation
The system SHALL allow only one run in flight per conversation at a time and SHALL
cleanly refuse a second concurrent start with no effect on the first and no change
to history.

#### Scenario: Second concurrent run is refused
- **WHEN** a run is already in flight and a second run is started on the same
  conversation
- **THEN** the second is cleanly refused
- **THEN** the first run is unaffected and history is unchanged

### Requirement: Backpressure without loss
The system SHALL apply backpressure to a slow reader without losing events, and
SHALL unblock and exit cleanly when an abandoned reader cancels.

#### Scenario: Slow reader throttles the agent
- **WHEN** events pile up faster than a slow reader consumes them
- **THEN** the agent throttles itself to the reader's pace
- **THEN** no events are lost

#### Scenario: Abandoned reader that cancels unblocks the agent
- **WHEN** an abandoned reader cancels while the agent is blocked
- **THEN** the agent unblocks and exits
- **THEN** no leftover background work remains

### Requirement: Fully exercisable offline against fakes
The system SHALL be fully exercisable against a scripted model and a stand-in
frontend, with no network and no real model, producing reproducible deterministic
scenarios.

#### Scenario: The headline scenario runs offline
- **WHEN** a scripted model whose first round emits a little text then requests one
  tool, and whose second round emits a little more text then stops, is driven with
  the stand-in tool runner
- **THEN** the watcher observes, in order: status thinking; the first text; status
  working-a-tool; the tool start with its name and arguments; the tool finish with
  its result; status thinking; the second text; status idle; run finished with
  reason "completed"; the stream closes
- **THEN** the conversation afterward reads, in order: the system framing, the
  user's request, the assistant's first reply carrying the tool request, the tool's
  result, and the assistant's final reply

### Requirement: Prompt-submit hook gate
The loop SHALL invoke prompt-submit hooks before sending the user's prompt to the
provider. A blocking prompt-submit hook SHALL prevent provider work for that turn.

#### Scenario: Prompt-submit block stops provider call
- **WHEN** a run starts from a user prompt and a prompt-submit hook blocks that prompt
- **THEN** the provider is not called
- **THEN** the run stream surfaces the hook block reason

#### Scenario: Prompt-submit injection is visible to provider
- **WHEN** a prompt-submit hook injects context for a prompt
- **THEN** the injected context is included in the conversation assembled for the provider call

### Requirement: Session-start hook gate
The session lifecycle SHALL invoke session-start hooks before a session accepts
work. A blocking session-start hook SHALL prevent the session from running turns.

#### Scenario: Session-start block prevents runs
- **WHEN** a session-start hook blocks session startup
- **THEN** the session refuses to run user prompts
- **THEN** the caller receives the hook block reason

#### Scenario: Session-start injection seeds context
- **WHEN** a session-start hook injects standing context
- **THEN** that context is present before the first provider call in the session

### Requirement: Session-stop hook cleanup
The session lifecycle SHALL invoke session-stop hooks when the session stops or
closes. A blocking or failing session-stop hook SHALL be surfaced but SHALL NOT
prevent the session from stopping.

#### Scenario: Session-stop hook runs during cleanup
- **WHEN** a session is stopped or closed
- **THEN** matching session-stop hooks run during cleanup

#### Scenario: Session-stop block is recorded
- **WHEN** a session-stop hook blocks or fails
- **THEN** the block or failure is surfaced on the event stream or returned cleanup result
- **THEN** the session still stops

### Requirement: Run-finished hook notification point
The loop SHALL invoke run-finished hooks after computing a run's terminal
outcome and before emitting the terminal run-finished event. A blocking or
failing run-finished hook SHALL be surfaced but SHALL NOT change the run's
terminal outcome.

#### Scenario: Run-finished hook runs before terminal event
- **WHEN** a run reaches completed, failed, cancelled, or step-limit outcome
- **THEN** matching run-finished hooks run with that outcome
- **THEN** the terminal run-finished event is emitted after those hooks finish

#### Scenario: Run-finished hook failure preserves outcome
- **WHEN** a run-finished hook blocks or fails after the run outcome is known
- **THEN** the hook failure is surfaced on the run stream
- **THEN** the terminal run-finished event still carries the original outcome

### Requirement: Hook outcome events on run stream
The loop SHALL carry hook outcome events on the same run stream used for
assistant text, tool lifecycle, permission requests, status, and run completion.

#### Scenario: Frontend observes hook outcomes from stream
- **WHEN** a hook blocks, replaces content, or injects context during a run
- **THEN** the outcome appears on the run stream
- **THEN** a frontend can render it without reading internal hook state

### Requirement: Child event isolation with permission pass-through
The orchestrator SHALL drain the child run stream internally and MUST NOT forward
child text deltas, tool lifecycle events, ordinary statuses, hook outcomes,
warnings, errors, or terminal events onto the parent stream. It SHALL forward a
child permission-request event with its original reply path and SHALL forward
nested subagent-started/subagent-finished status so grandchildren remain visible
without exposing their raw work.

#### Scenario: Raw child work does not interleave
- **WHEN** a child streams text, invokes tools, emits hook outcomes, and completes
- **THEN** none of those raw child events appear on the parent stream beyond the labeled subagent lifecycle status

#### Scenario: Child permission request reaches the frontend
- **WHEN** a child tool call requires human permission
- **THEN** the same typed permission request and reply path appear on the parent stream, and the child resumes after the frontend answers

#### Scenario: Nested lifecycle remains visible
- **WHEN** a child delegates work to a grandchild
- **THEN** the grandchild's labeled subagent-started and subagent-finished status reach the root frontend while its raw text and tool events remain private

#### Scenario: Failed permission forwarding cancels child work
- **WHEN** the parent stream can no longer accept a forwarded child permission request
- **THEN** the derived child context is cancelled and the delegation does not remain blocked on an unreachable human reply

### Requirement: One authoritative rich internal lifecycle
The loop SHALL produce all frontend-neutral lifecycle facts through one rich
internal event path. The selected public stream SHALL be a projection of that
path, and selecting legacy or observed output MUST NOT duplicate provider calls,
hook execution, permission decisions, tool dispatch, context mutation, or
terminal computation.

#### Scenario: Observed run executes each action once
- **WHEN** an observed run consults the provider, requests permission, and invokes a tool
- **THEN** the provider call, permission interaction, and tool dispatch each occur exactly once
- **THEN** their observed payloads come from the same execution rather than replayed work

#### Scenario: Legacy run uses the same internal path
- **WHEN** a legacy run executes the same scripted lifecycle
- **THEN** it uses the rich internal path and projects only legacy-representable events
- **THEN** no separate legacy loop or executor path exists

### Requirement: Deterministic unchanged legacy projection
The loop SHALL project its rich internal facts onto the existing `Session.Run`,
`Provider`, and `RunEvent` contract without changing their signatures, existing
event meanings, relative ordering, backpressure, cancellation, or single-terminal
rules. Observed-only facts such as reasoning summaries, provider usage, detailed
preparation, omissions, provenance, and capability facts MUST be omitted from the
legacy stream rather than encoded into status, warning, error, result, or JSON
strings or new fields on `RunEvent`.

#### Scenario: Existing fake produces the same legacy transcript
- **WHEN** an existing fake provider drives a legacy run after observability is added
- **THEN** the legacy event types, payload meanings, and order match the pre-change expected transcript
- **THEN** the run still ends with one existing `RunFinishedEvent`

#### Scenario: Rich-only facts do not leak into legacy strings
- **WHEN** a rich provider supplies reasoning summary, usage, and a content-filter detail during a legacy run
- **THEN** those facts are not serialized into legacy status, warning, error, tool-result, or assistant-text fields
- **THEN** every representable legacy event retains its existing payload semantics

#### Scenario: Legacy projection preserves backpressure
- **WHEN** a legacy reader is slow while the rich internal path is producing events
- **THEN** the existing backpressure behavior throttles the run without losing projected events
- **THEN** the projection does not accumulate an unbounded shadow transcript

### Requirement: Complete ordered observed projection
For an observed run, the loop SHALL project every allowed rich internal fact into
one observed envelope in occurrence order, assign its run sequence at the public
stream boundary, and preserve existing sequential tool ordering. An allowed fact
MUST NOT be silently lost merely because it has no legacy representation.

#### Scenario: Interleaved rich facts retain order
- **WHEN** a provider emits reasoning summary, assistant text, usage, and a reply ending before a tool runs
- **THEN** the observed envelopes preserve that occurrence order
- **THEN** the subsequent tool start and finish follow in execution order

#### Scenario: Slow observed reader applies backpressure
- **WHEN** observed envelopes accumulate faster than the caller consumes them
- **THEN** the run throttles to the reader without dropping envelopes or sequence numbers
- **THEN** cancellation still unblocks the producer promptly

### Requirement: Optional rich-provider selection and legacy adaptation
For each observed model round, the loop SHALL consume the optional rich-provider
extension when available and otherwise SHALL adapt the unchanged legacy provider
stream to the rich internal lifecycle. The legacy `Session.Run` entry point SHALL
continue to call the required `Provider.StreamReply` method even when that same
provider also implements the optional extension, preserving compatible backend
request behavior. Provider capability detection MUST NOT require a probe request,
and unsupported reasoning or provider usage MUST remain absent.

#### Scenario: Rich provider is selected without a probe
- **WHEN** an observed run uses a provider that implements the optional rich extension
- **THEN** the loop selects it by capability for the model round
- **THEN** it performs one real provider request and no probe request

#### Scenario: Legacy entry point retains its provider protocol
- **WHEN** `Session.Run` uses a provider that implements both required and optional stream methods
- **THEN** the loop calls only the required legacy stream method
- **THEN** adding rich support to that provider does not add request fields or stricter response requirements to the legacy entry point

#### Scenario: Legacy provider is adapted
- **WHEN** the configured provider implements only the existing provider interface
- **THEN** the loop adapts its text, tool-call, error, and reply-ended events into the internal lifecycle
- **THEN** ordinary run behavior remains available without fabricated rich facts

### Requirement: Reasoning summary is observation only
The loop SHALL forward provider-designated display-safe reasoning summaries only
to the observed stream. It MUST NOT append a reasoning summary to the assistant
turn, feed it back to the provider as conversation history, reinterpret it as an
answer, or use it to authorize or dispatch a tool.

#### Scenario: Summary precedes an answer
- **WHEN** a rich provider emits a reasoning summary followed by assistant text
- **THEN** the observed stream shows both in provider order as distinct payload kinds
- **THEN** only the assistant text is recorded as assistant conversation content

#### Scenario: Summary mentions an action
- **WHEN** a reasoning summary mentions editing a file but the provider emits no tool call
- **THEN** the loop does not dispatch an edit from the summary
- **THEN** the normal provider reply-ending rules determine whether the run continues or stops

### Requirement: Usage and omission facts do not change control flow
Context-usage and omission facts SHALL be observational. They MUST NOT themselves
create a tool call, bypass hooks or permission, mutate the conversation, or add a
second stop reason. Existing over-budget warnings, provider failures, reply
endings, tool requests, step limits, and cancellation SHALL remain the
authoritative control inputs.

#### Scenario: Provider usage arrives before a tool request
- **WHEN** a model round reports provider usage and then requests a tool
- **THEN** the usage is observed and the requested tool still follows the single execution chokepoint
- **THEN** usage does not approve, deny, or modify the call

#### Scenario: Reply omission is reported before run completion
- **WHEN** a model reply ends with a structured length or content-filter omission and no tool call
- **THEN** the omission appears before the one run terminal event
- **THEN** it does not become an additional run terminal outcome

### Requirement: Stable origin propagation on the root stream
The loop SHALL assign root origin to root-produced facts and SHALL preserve the
delegated origin on child lifecycle and permission facts that the subagent
orchestrator is allowed to forward. The root stream SHALL assign its own gap-free
sequence to delivery order without replacing child identity, and it MUST retain
the existing raw-child-event isolation rule.

#### Scenario: Root and child facts are distinguishable
- **WHEN** the root starts a delegation and a child later requests permission
- **THEN** the lifecycle and permission envelopes identify their effective origins and parent relationship
- **THEN** sequence numbers still describe their delivery order on the root stream

#### Scenario: Raw child fact remains isolated
- **WHEN** a child emits assistant text, reasoning summary, provider usage, or tool logs
- **THEN** the root loop does not forward those raw facts
- **THEN** adding origin metadata does not weaken child isolation

### Requirement: One terminal computation across public projections
The loop SHALL compute one authoritative run outcome, invoke run-finished hooks
once with that outcome, and then project exactly one terminal event as the final
event of the selected public stream. Legacy and observed projections MUST use the
same stop-reason semantics, and projection logic MUST NOT replace or duplicate the
outcome. Advisory idle or subagent-finished status MUST NOT be a prerequisite for
terminal delivery on a cancelled stream.

#### Scenario: Completed run has one terminal
- **WHEN** a run completes after its run-finished hooks execute
- **THEN** the selected public stream emits exactly one completed terminal event
- **THEN** the stream closes with no later event

#### Scenario: Run-finished hook fails during an observed run
- **WHEN** a run-finished hook blocks or fails after the authoritative outcome is known
- **THEN** its outcome is observed before the terminal envelope
- **THEN** the original run outcome is emitted once and remains unchanged

#### Scenario: Cancellation wins during projection
- **WHEN** cancellation reaches a running provider or tool while rich facts are pending
- **THEN** background work stops promptly
- **THEN** exactly one cancelled terminal event is delivered under the selected contract
- **THEN** no pending rich fact is emitted after that terminal
- **THEN** terminal delivery does not wait to force an advisory idle or subagent-finished status

### Requirement: Projection compatibility is tested offline
The loop's rich path, observed projection, and legacy projection SHALL be covered
by deterministic offline contract tests using the same scripted execution. Tests
MUST prove that legacy transcripts remain stable, observed sequences are complete,
and no action or terminal is duplicated.

#### Scenario: Dual contract fixture verifies projections
- **WHEN** one scripted fixture is run independently through legacy and observed entry points
- **THEN** the legacy transcript matches its existing golden sequence
- **THEN** the observed transcript contains the corresponding rich facts with gap-free envelopes
- **THEN** instrumentation shows one provider call and one dispatch per scripted action in each run

### Requirement: Provider-channel waits remain cancellation-aware
The loop SHALL select provider-event receives against the governing context
rather than ranging indefinitely on the provider channel. When cancellation wins
before a non-cooperative provider closes its channel, the loop MUST stop waiting,
ignore later provider events, and compute exactly one cancelled run terminal.
Tool and dispatcher implementations remain required to honor their blocking
context contract; a frontend MAY use a bounded forced process shutdown when a
caller-owned implementation violates that contract.

#### Scenario: Legacy provider channel never closes
- **WHEN** a custom legacy provider returns a channel that never closes and the run context is cancelled
- **THEN** the loop stops receiving without waiting for channel closure
- **THEN** exactly one cancelled terminal event is emitted and no later provider event is accepted

#### Scenario: Rich provider channel never closes
- **WHEN** a rich provider ignores cancellation and leaves its event channel open forever
- **THEN** the same cancellation-aware receive path settles the run once as cancelled

