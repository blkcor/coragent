# Model Backend

## Purpose

OpenAI-compatible streaming model backend — accepts a conversation plus tools, streams incremental text and reassembled tool calls, handles retries, errors, cancellation, and is testable offline against fakes.
## Requirements
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

### Requirement: Optional rich-provider extension
The system SHALL define an optional rich-provider extension for frontend-neutral
reasoning summaries, provider usage, and detailed reply termination. The existing
`Provider` interface, its `StreamReply` method, its `RunEvent` stream protocol,
and its reply-end vocabulary MUST remain source- and behavior-compatible, and a
provider that implements only that interface MUST remain a valid provider.

#### Scenario: Existing provider requires no change
- **WHEN** an SDK caller supplies a provider compiled against the existing `Provider` interface
- **THEN** the session accepts it without a new method, adapter supplied by the caller, or rich event type
- **THEN** its legacy stream is consumed with the existing protocol

#### Scenario: Provider advertises the rich extension
- **WHEN** a provider implements the optional rich-provider extension
- **THEN** the harness can consume its richer typed reply stream
- **THEN** the provider remains usable through the existing `Provider` interface

#### Scenario: One model request uses one provider path
- **WHEN** a provider supports both legacy and rich streaming for a model round
- **THEN** the harness selects one provider method for that round
- **THEN** it does not call both methods or duplicate model work in order to populate two frontend contracts

### Requirement: Ordered rich reply protocol
A successful rich-provider stream SHALL deliver text deltas, complete tool calls,
provider-designated reasoning-summary segments, and usage facts in provider order,
then exactly one typed reply-ended event and closure. A failed stream SHALL end
cleanly with an error, and no event MUST follow a reply-ended event or expose a
half-formed tool call.

#### Scenario: Rich reply completes in protocol order
- **WHEN** a rich provider produces reasoning summary, assistant text, usage, and a normal finish
- **THEN** those non-terminal facts arrive in provider order
- **THEN** exactly one rich reply-ended event follows them and the stream closes

#### Scenario: Rich stream fails while assembling a tool call
- **WHEN** a rich provider fails after receiving only part of a tool-call argument payload
- **THEN** the stream reports the failure cleanly
- **THEN** it does not emit the half-formed tool call or a successful reply-ended event

#### Scenario: Event arrives after rich reply end
- **WHEN** a provider emits any event after its rich reply-ended event
- **THEN** the provider stream is treated as a protocol failure
- **THEN** the trailing event is neither recorded nor dispatched

### Requirement: Provider-designated display-safe reasoning summaries
The rich-provider extension SHALL expose only reasoning content the backend
explicitly designates as a display-safe summary. Provider requests MUST NOT ask
for hidden raw chain-of-thought, and response parsing MUST NOT forward raw
analysis text, encrypted reasoning state, or undifferentiated reasoning fields as
a summary. Every provider-specific summary field MUST be accepted through an
explicit backend adapter rather than a heuristic field-name scan.

#### Scenario: Backend supplies a designated summary
- **WHEN** a backend emits a documented display-safe reasoning-summary field
- **THEN** the rich provider emits that field as a reasoning-summary segment
- **THEN** it preserves the order of summary segments without converting them to assistant answer text

#### Scenario: Backend supplies raw reasoning but no summary
- **WHEN** a backend response includes raw chain-of-thought or opaque reasoning state but no designated display-safe summary
- **THEN** the rich provider emits no reasoning-summary content for that response
- **THEN** it does not log or copy the raw field into a public event

#### Scenario: Backend supports no reasoning summary
- **WHEN** a backend exposes no display-safe reasoning-summary capability
- **THEN** ordinary text and tool streaming continue unchanged
- **THEN** the provider does not synthesize a summary from answer text, timing, or activity status

#### Scenario: Unknown reasoning-shaped field arrives
- **WHEN** a response contains a reasoning-shaped field with no explicit display-summary adapter
- **THEN** the rich provider ignores it for public reasoning output
- **THEN** it does not guess that the field is safe from its name or shape

### Requirement: Structured provider usage facts
When a backend reports token usage, the rich provider SHALL expose a structured
provider-usage fact correlated to the model round. Prompt, completion, total,
cached-prompt, and reasoning-completion counts SHALL be preserved when supplied;
absent counts MUST remain unknown rather than becoming measured zero; and every
exposed count MUST be non-negative and identified as provider-sourced. The
OpenAI-compatible adapter SHALL request streaming usage when the endpoint supports
it and SHALL degrade cleanly when it does not.

#### Scenario: Backend reports final usage
- **WHEN** a streaming backend reports final prompt, completion, and total token counts
- **THEN** the rich provider emits those counts as provider-sourced usage for that model round
- **THEN** the usage is available before the rich reply-ended event

#### Scenario: Backend omits optional usage details
- **WHEN** the backend reports prompt and completion tokens but omits cached-prompt and reasoning-completion details
- **THEN** the rich usage fact preserves the reported prompt and completion counts
- **THEN** the omitted detail fields remain unknown rather than zero

#### Scenario: Backend reports no usage
- **WHEN** a successful backend stream contains no usage payload
- **THEN** the rich provider emits no provider-usage fact
- **THEN** the harness remains free to report a clearly labeled local estimate

#### Scenario: Backend reports invalid usage
- **WHEN** a backend reports a negative or internally invalid usage count
- **THEN** the rich provider does not publish that value as trustworthy provider usage
- **THEN** it surfaces a typed warning or provider error according to whether the reply can still be consumed safely

#### Scenario: Endpoint ignores streaming usage request
- **WHEN** an OpenAI-compatible endpoint returns a valid reply but no requested streaming usage
- **THEN** the rich provider completes the reply without failing solely because usage is absent
- **THEN** it emits no fabricated provider-usage fact

### Requirement: Optional provider context-window metadata
The rich-provider extension SHALL expose the effective model context-window size
when the backend supplies trustworthy metadata. It MUST leave the window unknown
when the backend does not supply it and MUST NOT guess a window from a model name
alone.

#### Scenario: Backend supplies a context window
- **WHEN** the backend reports a trustworthy context-window size for the effective model
- **THEN** the rich provider exposes that non-negative window as provider metadata
- **THEN** observed context usage can use it as the effective window

#### Scenario: Backend supplies no context window
- **WHEN** the backend reports usage but no trustworthy context-window metadata
- **THEN** prompt and completion usage remain available
- **THEN** the context window remains unknown rather than model-name-derived

### Requirement: Distinct rich reply termination reasons
The rich-provider extension SHALL distinguish normal completion, stop for tool
calls, length-limit cutoff, content filtering, provider failure, and an
unrecognized or provider-specific incomplete ending. A provider-specific ending
MUST retain a safe provider reason code when available and MUST NOT be relabeled
as normal completion or a length cutoff merely because the legacy vocabulary is
smaller.

#### Scenario: Length limit and content filter stay distinct
- **WHEN** one backend reply ends for `length` and another ends for `content_filter`
- **THEN** the rich provider reports length-limit cutoff for the first
- **THEN** it reports content filtering for the second
- **THEN** the observed-run adapter maps the length cutoff to `new_user_turn` only as a fresh editable prompt when the session remains usable, while content filtering maps to `unavailable` in Phase 7

#### Scenario: Tool calls remain a distinct ending
- **WHEN** the backend ends a reply after producing complete tool calls
- **THEN** the rich reply-ended event reports stop for tool calls
- **THEN** the tool calls are available for dispatch before the rich stream closes

#### Scenario: Unknown provider reason is not normalized away
- **WHEN** a backend returns a non-empty finish reason unknown to the adapter
- **THEN** the rich reply-ended event reports an unrecognized or provider-specific incomplete ending
- **THEN** it retains the safe reason code for diagnostics without claiming normal completion

#### Scenario: Provider failure is not normal completion
- **WHEN** the provider fails before successfully ending a reply
- **THEN** the rich stream reports provider failure rather than a normal or cutoff ending
- **THEN** no successful reply-ended event follows the failure

### Requirement: Stable legacy reply projection
The OpenAI-compatible provider's existing `StreamReply` behavior SHALL retain the
legacy `ReplyEndReason` vocabulary and mapping. Rich-only reasoning, usage, and
detailed termination facts MUST NOT add fields or enum requirements to legacy
`RunEvent`, and the same recorded backend transcript MUST continue to produce the
same legacy text, tool calls, errors, and reply-end reason as before this change.

#### Scenario: Recorded legacy transcript is unchanged
- **WHEN** an existing recorded backend transcript is consumed through `StreamReply`
- **THEN** its legacy events and reply-end reason match the pre-change expected transcript
- **THEN** no reasoning-summary or provider-usage event appears in that legacy stream

#### Scenario: Rich content-filter detail has a legacy view
- **WHEN** the same provider response is consumed once through each supported provider contract
- **THEN** the rich contract preserves the distinct content-filter detail
- **THEN** the legacy contract retains its established legacy reply-end mapping without a new enum value

### Requirement: Legacy-provider rich fallback
When a provider does not implement the rich extension, the harness SHALL adapt
the existing provider events into the internal rich run path without fabricating
provider-only facts. Existing assistant text, tool calls, errors, and legacy
reply endings SHALL remain available, and a legacy length cutoff SHALL be exposed
to observed callers as a `provider_length` omission.

#### Scenario: Legacy provider powers an observed run
- **WHEN** an observed run uses a provider that implements only `StreamReply`
- **THEN** assistant text, tool calls, errors, and run lifecycle remain observable
- **THEN** no provider reasoning summary or provider-sourced usage is invented

#### Scenario: Legacy provider reports cutoff
- **WHEN** a legacy provider ends a reply with the existing cutoff reason
- **THEN** the observed stream can report a `provider_length` omission
- **THEN** it reports `new_user_turn` only when the session remains able to accept a later user turn, without claiming provider-level resume
- **THEN** the legacy provider interface and event remain unchanged

### Requirement: Offline-testable rich provider
The optional rich-provider protocol SHALL be testable entirely offline with a
scripted fake and recorded OpenAI-compatible transcripts, including interleaved
summary, text, tool fragments, usage, cutoff, error, and cancellation cases.

#### Scenario: Fake rich provider drives deterministic facts
- **WHEN** an offline fake scripts summary segments, text deltas, usage, and a typed ending
- **THEN** the consumer receives the exact expected rich event order and values
- **THEN** no network or real model is required

#### Scenario: Recorded transcript proves distinct cutoff parsing
- **WHEN** recorded transcripts contain length and content-filter finish reasons
- **THEN** tests prove that the rich provider keeps them distinct
- **THEN** tests also prove that the legacy projection remains stable
