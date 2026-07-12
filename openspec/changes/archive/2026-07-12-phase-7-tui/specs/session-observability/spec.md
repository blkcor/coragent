## ADDED Requirements

### Requirement: Opt-in versioned observed runs
The public SDK SHALL expose `Session.RunObserved` as an opt-in observed-run API whose stream carries
versioned observed-event envelopes. Existing callers that use the legacy run API
MUST NOT need to adopt the observed API, and enabling observability for one run
MUST NOT duplicate model work or change tool execution semantics. An observed run
MAY opt into a provider's additive rich-stream method and its documented optional
usage request fields; the legacy run MUST retain the required provider method and
request contract. For providers that expose only the required legacy method, and
for equivalent normalized provider events, both run APIs SHALL produce the same
conversation and terminal outcome.

#### Scenario: Caller opts into the observed contract
- **WHEN** a caller starts a run through the observed-run API
- **THEN** it receives one read-only stream of versioned observed-event envelopes
- **THEN** the run executes through the same harness path as any other run

#### Scenario: Existing caller stays on the legacy contract
- **WHEN** a caller continues to start runs through the existing legacy API
- **THEN** it receives the existing legacy event stream without adopting any observed-event type
- **THEN** its source-level and runtime behavior remain compatible

#### Scenario: Observability is behaviorally inert
- **WHEN** a legacy-only scripted provider input is run once through each public run API under identical session state
- **THEN** both runs make the same provider and tool decisions and produce the same conversation and terminal outcome

#### Scenario: Observed caller opts into provider richness
- **WHEN** a provider implements both required and optional rich stream methods and the caller starts `RunObserved`
- **THEN** the run selects exactly one rich provider request for that round so typed summary, usage, and termination facts can be observed
- **THEN** the same provider used through legacy `Session.Run` still selects only its required legacy method

### Requirement: Stable envelope identity, sequence, and origin
Every observed-event envelope SHALL carry `SchemaVersion`, a non-empty `RunID`,
a `Sequence`, an observation `Timestamp`, a typed `Origin`, a closed event `Kind`,
and exactly one payload matching that kind. Run IDs MUST be unique within a
session; sequence numbers MUST start at one and increase by exactly one in
delivery order; and an origin MUST identify the root session or delegated agent
that caused the fact, including stable parent, depth, and delegation-call
provenance when the origin is delegated.

#### Scenario: One run has gap-free ordering
- **WHEN** an observed run emits several events
- **THEN** every envelope carries the same non-empty run ID
- **THEN** the sequence numbers are `1, 2, 3` and so on with no gaps, duplicates, or reordering
- **THEN** every envelope carries its harness observation timestamp and one payload matching its kind

#### Scenario: Consecutive runs are distinguishable
- **WHEN** two observed runs execute in the same session
- **THEN** their run IDs differ
- **THEN** each run starts its own sequence at one

#### Scenario: Root-origin fact is attributable
- **WHEN** the root agent emits assistant text or starts a tool
- **THEN** the envelope identifies the root session origin
- **THEN** the frontend does not need to infer origin from labels or display text

#### Scenario: Forwarded child interaction retains child provenance
- **WHEN** a permitted child-origin lifecycle fact or permission request is forwarded to the root stream
- **THEN** its envelope retains the child's stable identity, parent identity, and depth
- **THEN** the forwarding does not expose the child's raw text, reasoning, tool log, hook log, or context

### Requirement: Compatible observed-event evolution
The observed-event schema SHALL start with `SchemaVersion` 1 and SHALL define a
closed event-kind set for each supported schema version. Consumers MUST reject an
unsupported schema version cleanly before interpreting its payload. Within one
schema version, existing event meanings and required fields MUST remain stable;
an incompatible envelope, kind, or payload change MUST use a new schema version.
The complete schema-v1 kind set SHALL be `run_started`, `status_changed`,
`assistant_started`, `assistant_text_delta`,
`assistant_reasoning_summary_delta`, `assistant_finished`, `tool_proposed`,
`tool_prepared`, `permission_requested`, `tool_executing`, `tool_finished`,
`context_usage_updated`, `omission_reported`, `hook_outcome`,
`subagent_started`, `subagent_finished`, `warning`, `error`, and
`run_finished`. A kind outside that set while declaring schema v1 MUST be
treated as a protocol error rather than silently skipped.

#### Scenario: Additive field preserves compatibility
- **WHEN** a newer producer adds an optional field within the same schema version
- **THEN** a consumer that does not use that field can continue processing the run in sequence
- **THEN** all previously defined field meanings remain unchanged

#### Scenario: Unsupported schema version is rejected cleanly
- **WHEN** a consumer receives an envelope with an unsupported schema version
- **THEN** it rejects the observed contract cleanly instead of guessing at the kind or payload

#### Scenario: Incompatible change declares a new schema version
- **WHEN** an envelope field or payload meaning cannot remain compatible
- **THEN** the producer uses a new schema version
- **THEN** it does not silently reinterpret the old version

#### Scenario: Unknown kind claims schema version one
- **WHEN** a consumer receives an event kind outside the closed v1 set in an envelope that declares schema version one
- **THEN** it reports a typed protocol error and does not interpret or silently skip that payload

### Requirement: Immutable truthful session descriptor
The public SDK SHALL expose `Session.Describe` as an immutable snapshot of the
effective frontend-relevant state before the first run and between runs. The
descriptor SHALL identify the session, effective model and provider features,
working directory, permission-control ownership and typed mode when owned by the
standard engine, sandbox level and reason, context-window
knowledge, registered tools and hooks, and any optional capability providers such
as skills or MCP. Each capability entry MUST state its kind, name, source,
availability, and safe status detail, and the descriptor MUST distinguish an
unsupported category from a supported category whose effective inventory is
empty.

#### Scenario: Frontend receives effective session facts
- **WHEN** a frontend reads the session descriptor
- **THEN** it can render the effective model and provider features, working directory, permission ownership and engine mode when available, sandbox posture, context-window knowledge, tools, hooks, and reported optional capabilities without importing an internal package

#### Scenario: Supported empty inventory differs from unsupported
- **WHEN** a capability provider truthfully reports that skills are supported but none are loaded
- **THEN** the descriptor reports a supported empty skills inventory with that provider as its source
- **WHEN** no skills capability provider is registered
- **THEN** the descriptor reports skills as unsupported rather than claiming that zero skills were loaded

#### Scenario: Custom capability provider reports skills or MCP
- **WHEN** a custom capability provider reports available skills or MCP servers
- **THEN** the descriptor includes only the reported entries and their availability
- **THEN** the descriptor does not widen tool execution authority

#### Scenario: Previously returned descriptor cannot mutate live state
- **WHEN** a caller mutates its local copy of a descriptor or the session later changes mode
- **THEN** the prior descriptor value and live session state cannot mutate each other
- **THEN** current between-run state is represented by a fresh `Session.Describe` snapshot rather than an event kind outside the closed v1 run schema

### Requirement: Descriptor and event secret hygiene
Session descriptors and observed events MUST NOT expose provider credentials,
authorization headers, system prompts, environment secrets, raw settings values
marked secret, hook command arguments or environment, persisted permission rules,
or other credentials. Frontend-safe identifiers and redacted summaries SHALL
remain available where needed for truthful status.

#### Scenario: Descriptor is built from credentialed settings
- **WHEN** a session uses an API key and secret environment variables
- **THEN** the descriptor contains neither the secret values nor a reversible encoding of them
- **THEN** it can still identify the effective provider and model in frontend-safe form

#### Scenario: Observable error contains secret-bearing backend detail
- **WHEN** a backend error includes an authorization value or configured secret
- **THEN** the observed error payload redacts that value before it reaches the frontend

### Requirement: Typed observed payload vocabulary
The observed stream SHALL represent run start, assistant text, activity,
provider-supplied reasoning summary, context usage, omission, tool lifecycle,
hook outcome, permission interaction, subagent lifecycle, warning or error, and
run termination through the closed schema-v1 kind set when those facts
occur. Correlation identifiers and structured fields MUST carry machine-relevant
meaning; consumers MUST NOT need to parse presentation strings to correlate or
classify events.

#### Scenario: Tool lifecycle is correlated structurally
- **WHEN** a tool starts and later finishes
- **THEN** both observed payloads carry the same tool-call correlation ID
- **THEN** a frontend can update one card without matching tool names or prose

#### Scenario: Permission interaction is correlated structurally
- **WHEN** a tool pauses for permission and later resumes
- **THEN** the permission payload identifies the effective tool call and its origin through typed fields
- **THEN** the reply path remains usable without parsing the reason text

#### Scenario: Caller-owned dispatcher emits a legacy permission event
- **WHEN** a custom dispatcher used by `RunObserved` emits the existing legacy permission event and waits on its reply path
- **THEN** the observed boundary assigns request ID, call correlation, origin, revision one, and protocol `legacy_one_shot`
- **THEN** it marks preview, rich argument revision, schema-aware edit, and per-call grants unsupported instead of inventing them
- **THEN** its exactly-once wrapper can forward allow or deny and remember only when the legacy request supplied a safe remembered rule, so the dispatcher is not stranded

#### Scenario: Unsupported rich fact is absent
- **WHEN** the provider supplies neither reasoning summaries nor usage
- **THEN** the stream emits neither fabricated reasoning-summary payloads nor provider-sourced usage payloads
- **THEN** ordinary activity and any available estimated context usage remain usable

### Requirement: Provider-summary-only reasoning boundary
Observed reasoning SHALL contain only a provider-designated, display-safe
reasoning summary. The harness MUST NOT request, derive, synthesize, persist in
the conversation, log, or emit hidden raw chain-of-thought. Reasoning summaries
MUST remain distinct from assistant answer text and from generic activity
animation.

#### Scenario: Provider supplies a display-safe summary
- **WHEN** a rich provider emits a reasoning-summary segment
- **THEN** the observed stream emits it as a reasoning-summary payload in provider order
- **THEN** it is not appended to the assistant answer or model conversation

#### Scenario: Provider does not support summaries
- **WHEN** a provider exposes no display-safe reasoning summary
- **THEN** the harness emits no reasoning-summary content
- **THEN** it may still emit a typed thinking activity without inventing an explanation

#### Scenario: Backend exposes a raw reasoning field
- **WHEN** a backend response contains hidden chain-of-thought or an undifferentiated raw reasoning field instead of a designated display-safe summary
- **THEN** the harness does not copy that field into an observed event, conversation turn, or log

### Requirement: Structured context-usage source
Observed context usage SHALL use a structured payload that identifies the model
round, non-negative used tokens, optional context-window and remaining tokens, an
exact source of `estimated` or `provider`, and measurement time. Unknown values
MUST remain unknown rather than being represented as measured zero, and a
percentage MUST NOT be reported when the effective context window is unknown.

#### Scenario: Estimated and provider usage are distinguishable
- **WHEN** one context snapshot is computed locally and a later snapshot comes from backend usage for the same round
- **THEN** their sources are `estimated` and `provider` respectively
- **THEN** a frontend can replace an estimate without treating both values as additive consumption

#### Scenario: Context window is unknown
- **WHEN** no trustworthy context window is configured or reported
- **THEN** the payload leaves context window, remaining capacity, and percentage unknown
- **THEN** it still reports available token counts and their source

### Requirement: Structured omission taxonomy
Every irreversible omission known to the harness SHALL be emitted as a structured
omission payload with a typed kind, affected scope and correlation ID,
recoverability, visible and original size information when known, and a typed
continuation mode of `unknown`, `unavailable`, or `new_user_turn`. The
`new_user_turn` value SHALL mean only that the session can accept an editable
follow-up prompt; it MUST NOT promise token-level resume, exact continuation, or
automatic submission. The v1
taxonomy MUST distinguish `output_budget`, `preview_budget`, `provider_length`,
`content_filter`, and `redacted`, and SHALL reserve `context_compaction` for a future implementation
that actually removes context. Frontend-only folding MUST NOT be reported as
harness data loss.

#### Scenario: Tool output is irreversibly truncated
- **WHEN** a tool result exceeds its retained output bound
- **THEN** the observed stream emits an `output_budget` omission correlated to that tool call
- **THEN** the payload states that the omitted bytes are not recoverable from the event stream and includes known retained and original sizes
- **THEN** its continuation mode is `unavailable`

#### Scenario: Action preview is irreversibly bounded
- **WHEN** a prepared action exceeds its retained preview byte or logical-line bound
- **THEN** the observed stream emits a `preview_budget` omission correlated to that tool call and preview revision
- **THEN** the payload states that omitted preview content is not recoverable from the event stream while complete aggregate change counts remain authoritative
- **THEN** its continuation mode is `unavailable`

#### Scenario: Reply stops at a length limit
- **WHEN** a rich provider reports a reply length cutoff
- **THEN** the observed stream emits a `provider_length` omission for that model round
- **THEN** it does not label the cutoff as content filtering
- **THEN** its continuation mode is `new_user_turn` only when the session remains able to accept a later user turn, otherwise `unavailable`

#### Scenario: Reply is content filtered
- **WHEN** a rich provider reports content filtering
- **THEN** the observed stream emits a `content_filter` omission for that model round
- **THEN** it does not label the omission as a token-length cutoff
- **THEN** Phase 7 reports continuation mode as `unavailable`

#### Scenario: Public content is redacted
- **WHEN** the harness intentionally removes secret or unsafe content from an otherwise public payload
- **THEN** the observed stream emits a `redacted` omission correlated to that payload
- **THEN** it does not claim the content can be expanded locally
- **THEN** its continuation mode is `unavailable`

#### Scenario: Frontend folds retained content
- **WHEN** a frontend collapses a long tool result that remains retained in its reducer
- **THEN** no harness omission event is required
- **THEN** expanding the fold can reveal the retained content

#### Scenario: V1 does not claim context compaction
- **WHEN** v1 exceeds its effective context window or configured warning budget but retains the full conversation
- **THEN** it emits no `context_compaction` omission
- **THEN** it continues to use the existing advisory over-budget behavior

### Requirement: Exactly one observed terminal envelope
Every observed run SHALL end with exactly one terminal envelope carrying the
authoritative run outcome. The terminal envelope MUST be the final envelope in
the sequence, the stream MUST close after it, and reply cutoff or omission facts
MUST appear before it without acting as a second run terminal.

#### Scenario: Completed observed run terminates once
- **WHEN** an observed run completes normally
- **THEN** exactly one terminal envelope carries the completed outcome
- **THEN** no event follows it and the stream closes

#### Scenario: Cancelled observed run still terminates once
- **WHEN** an observed run is cancelled while streaming or executing a tool
- **THEN** exactly one terminal envelope carries the cancelled outcome
- **THEN** no reasoning, usage, omission, or lifecycle event appears after it

### Requirement: Offline deterministic observability testing
The observed-run API and its observable contracts SHALL be fully testable
offline. Tests SHALL cover the descriptor, event envelopes, reasoning boundary,
usage sources, omission taxonomy, and terminal rules against scripted legacy and
rich providers with no network or real model.

#### Scenario: Rich transcript is reproducible offline
- **WHEN** a scripted rich provider emits reasoning summary, text, usage, a tool call, and a cutoff in a fixed order
- **THEN** the observed envelope sequence, payload correlations, and terminal outcome match the expected transcript deterministically

#### Scenario: Legacy provider fallback is reproducible offline
- **WHEN** an existing fake provider implements only the legacy provider interface
- **THEN** observed-run tests produce ordinary text and lifecycle facts without fabricated provider-only facts
- **THEN** the legacy fake requires no source change
