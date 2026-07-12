## MODIFIED Requirements

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

## ADDED Requirements

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
