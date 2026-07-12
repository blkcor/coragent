## ADDED Requirements

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
