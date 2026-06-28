## ADDED Requirements

### Requirement: Stream reply for conversation plus tools
The system SHALL accept a conversation plus the set of available tools and return the assistant's reply as a consumable stream.

#### Scenario: Backend accepts conversation and tools
- **WHEN** a caller provides a conversation and a set of available tools
- **THEN** the backend initiates a request to the model endpoint
- **THEN** the backend returns a channel of typed events

#### Scenario: Reply is consumable before whole message arrives
- **WHEN** the model produces a reply
- **THEN** the consumer receives events incrementally
- **THEN** the consumer does not wait for the whole message to arrive

### Requirement: Incremental assistant text
The system SHALL deliver assistant text incrementally and in order as the model produces it.

#### Scenario: Text arrives incrementally
- **WHEN** the model produces text tokens
- **THEN** each token arrives as a text delta event
- **THEN** tokens arrive in the order produced
- **THEN** no part of the text waits on completion of the whole message

### Requirement: Reassembled dispatchable tool calls
The system SHALL reassemble fragmented tool requests into complete, dispatchable tool calls, keep multiple distinct requests separate, and surface them in first-appearance order.

#### Scenario: Tool-call fragments are reassembled
- **WHEN** the model streams tool-call fragments (name, then argument chunks)
- **THEN** fragments are buffered by tool-call ID
- **THEN** when the tool call is complete, a fully assembled tool call is emitted
- **THEN** the emitted tool call has its name and arguments fully assembled

#### Scenario: Multiple tool requests are kept separate
- **WHEN** the model streams multiple distinct tool requests in one reply
- **THEN** each tool request is reassembled separately
- **THEN** tool calls are surfaced in the order they first appeared

#### Scenario: Tool-call arguments are parsed
- **WHEN** a tool call is complete
- **THEN** its arguments are parsed from JSON into a structured form
- **THEN** the consumer receives the parsed arguments, not raw JSON

### Requirement: Report how reply ended
The system SHALL report how each reply ended: finished normally, stopped to call tools, or cut off at a length limit.

#### Scenario: Reply finished normally
- **WHEN** the model completes its reply without requesting tool calls
- **THEN** the stream ends with a "finished" event
- **THEN** the consumer can distinguish this from other ending reasons

#### Scenario: Reply stopped to call tools
- **WHEN** the model completes its reply and requests tool calls
- **THEN** the stream ends with a "stopped-to-call-tools" event
- **THEN** the consumer can distinguish this from other ending reasons

#### Scenario: Reply cut off at length limit
- **WHEN** the model's reply is cut off due to a length limit
- **THEN** the stream ends with a "cut-off" event
- **THEN** the consumer can distinguish this from other ending reasons

### Requirement: Per-request model and sampling options with fallback
The system SHALL use per-request model and sampling options when set and fall back to configured defaults when omitted.

#### Scenario: Request sets model and options
- **WHEN** a request specifies a model or sampling options
- **THEN** those values are used for that request

#### Scenario: Request omits model and options
- **WHEN** a request omits the model or sampling options
- **THEN** the configured defaults are used instead

### Requirement: Retry transient failures with backoff
The system SHALL retry transient backend failures (rate limiting, temporary server errors) with backoff up to a configured limit, honor any "retry after" hint, and never duplicate already-delivered content.

#### Scenario: Transient failure before reply starts
- **WHEN** a transient failure (429, 503, 504) occurs before any reply is delivered
- **THEN** the request is retried with exponential backoff plus jitter
- **THEN** retries continue up to the configured limit

#### Scenario: Backend provides retry-after hint
- **WHEN** a transient failure includes a "Retry-After" header
- **THEN** that hint is honored in the backoff timing

#### Scenario: Retry limit exhausted
- **WHEN** retries are exhausted without success
- **THEN** the failure is surfaced to the consumer
- **THEN** no already-delivered content is duplicated

### Requirement: Distinct non-retried permanent failures
The system SHALL report permanent failures (bad credentials, malformed requests) as distinct, non-retried errors.

#### Scenario: Permanent failure is not retried
- **WHEN** a permanent failure (401, 400) occurs
- **THEN** the request is not retried
- **THEN** the failure is reported immediately

#### Scenario: Permanent failure is distinguishable
- **WHEN** a permanent failure is reported
- **THEN** the error type distinguishes it from transient failures
- **THEN** the consumer can recognize and act on it appropriately

### Requirement: Clean mid-stream error handling
The system SHALL surface a mid-stream failure as a clean stream-ending error, preserving already-delivered text and delivering no half-formed tool request.

#### Scenario: Failure partway through streaming
- **WHEN** a failure occurs after some text has been delivered
- **THEN** the stream ends with an error event
- **THEN** text already delivered before the failure stands
- **THEN** no half-formed tool request is delivered
- **THEN** the stream does not crash or silently truncate

### Requirement: Prompt cancellation of in-flight reply
The system SHALL abort an in-flight reply promptly on cancellation and release its resources.

#### Scenario: Cancellation aborts in-flight reply
- **WHEN** the caller cancels the context during streaming
- **THEN** the underlying HTTP request is aborted promptly
- **THEN** the stream ends after cancellation
- **THEN** no resources are leaked

### Requirement: Offline-testable with fake provider
The system SHALL be testable entirely offline against a faked backend and recorded transcripts, with no network and no real model.

#### Scenario: Tests pass offline against fake provider
- **WHEN** the test suite runs with no network access
- **THEN** a fake provider scripts model replies (text deltas + tool calls)
- **THEN** all tests pass deterministically

#### Scenario: Recorded transcripts prove reassembly
- **WHEN** tests use recorded transcripts from real providers
- **THEN** fragmented tool requests reassemble into the exact expected calls
- **THEN** the reassembly logic is proven against real provider behavior

### Requirement: Runnable streaming demonstration
The system SHALL ship a tiny runnable demonstration that streams a reply from either a real endpoint or the fake.

#### Scenario: Demonstration streams text
- **WHEN** the demonstration runs pointed at an OpenAI-compatible endpoint or the fake
- **THEN** it prints streamed text as it arrives

#### Scenario: Demonstration prints tool calls
- **WHEN** the model requests tool calls
- **THEN** the demonstration prints decoded tool requests with their assembled arguments
