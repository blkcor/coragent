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
The system SHALL expose a single entry point that starts a run from the user's
input and returns one live, read-only event stream the caller drains to completion.

#### Scenario: Start a run and drain its stream
- **WHEN** a caller starts a run through the single entry point with the user's input
- **THEN** a live read-only event stream is returned
- **THEN** the caller can drain that stream to completion

#### Scenario: No second channel for observing a run
- **WHEN** anything observable happens during a run
- **THEN** it arrives on the one returned event stream and nowhere else
- **THEN** the watcher never inspects internal state

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
tool, and idle. Status signals are advisory and carry no control semantics.

#### Scenario: Status marks each transition
- **WHEN** the agent consults the model
- **THEN** a thinking status is emitted
- **WHEN** tools run
- **THEN** a working-a-tool status is emitted
- **WHEN** the run ends
- **THEN** an idle status is emitted

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
