# Context Manager

## Purpose

Accumulates the conversation across requests, presents it to the model in order,
exposes it only as an uncorruptable read-only snapshot, and warns — using a cheap
estimate — when the assembled conversation exceeds the model's context budget.
Actually shrinking history (compaction) is deferred; only the inert plug-in point
and the over-budget warning ship now.
## Requirements
### Requirement: Accumulate conversation history
The system SHALL accumulate conversation history across requests in the same
conversation and present the full prior history to the model in order.

#### Scenario: Second request sees full prior history
- **WHEN** two requests run in the same conversation and the second runs
- **THEN** the model sees the full prior history in order: system framing, both user
  turns, prior assistant replies, and any tool results

### Requirement: Read-only conversation snapshot
The system SHALL expose the conversation only as a read-only snapshot that callers
can inspect, log, or render but cannot mutate to corrupt the live conversation.

#### Scenario: Snapshot cannot corrupt the live conversation
- **WHEN** a developer reads the conversation during a run
- **THEN** they receive a snapshot copy
- **THEN** mutating the snapshot does not affect the live conversation

### Requirement: Over-budget warning, proceed anyway
The system SHALL, when the assembled conversation exceeds the model's context budget
by a cheap estimate, emit an advisory over-budget warning on the stream and proceed
without dropping anything or failing the run.

#### Scenario: Over-budget conversation warns and proceeds
- **WHEN** the assembled conversation exceeds the model's context budget and the
  agent is about to consult the model
- **THEN** an advisory over-budget warning is emitted on the stream
- **THEN** the run proceeds, nothing is dropped, and the run does not fail

### Requirement: Structured context-usage snapshots
The context manager SHALL produce a structured context-usage snapshot containing
the correlated model round, non-negative used tokens, optional context-window and
remaining tokens, a source of exactly `estimated` or `provider`, and measurement
time. When a context window is known, the snapshot SHALL also make over-budget
state and bounded remaining capacity derivable without parsing warning text.

#### Scenario: Estimated snapshot has typed fields
- **WHEN** the context manager estimates the assembled input for a model round
- **THEN** it produces non-negative used tokens with source `estimated`
- **THEN** the snapshot identifies that model round, measurement time, and the effective context window when one is available

#### Scenario: Provider snapshot preserves measured prompt usage
- **WHEN** a rich provider reports actual prompt tokens for a model round
- **THEN** the context manager produces a snapshot with source `provider`
- **THEN** it uses the measured prompt count as used tokens without combining completion tokens into the context percentage

#### Scenario: Unknown values remain unknown
- **WHEN** a trustworthy context window is unavailable
- **THEN** the corresponding snapshot fields remain unknown
- **THEN** they are not represented as measured zero or a fabricated percentage

### Requirement: Estimate the effective assembled request
An estimated usage snapshot SHALL be computed from the effective provider input
after system framing, stored conversation, prompt-submit injections, transient
context, and advertised tool schemas have been assembled for the round. The
estimator MUST be cheap and deterministic: identical effective input MUST produce
the same estimate.

#### Scenario: Prompt injection is included
- **WHEN** a prompt-submit hook injects context before a model round
- **THEN** the estimate for that round includes the injected context
- **THEN** an estimate taken before the injection is not reused as the effective request estimate

#### Scenario: Advertised tools contribute to the estimate
- **WHEN** the effective provider request advertises tool definitions and schemas
- **THEN** their request payload contributes to the estimated input usage
- **THEN** a frontend is not told that only conversation text consumes context

#### Scenario: Repeated estimate is deterministic
- **WHEN** the same effective conversation, transient context, and advertised tools are estimated repeatedly
- **THEN** every estimate is identical

### Requirement: Stable context-usage lifecycle points
The loop SHALL emit one estimated context-usage snapshot after assembling each
effective model request and before sending that request. If valid provider usage
later arrives for the round, the loop SHALL emit the provider-sourced replacement
after provider usage is known and before dispatching that round's tools, starting
another model round, or emitting the run terminal.

#### Scenario: Estimate precedes provider work
- **WHEN** a model round is about to start
- **THEN** the observed stream receives the effective estimated usage before provider reply content for that round

#### Scenario: Provider usage replaces the round estimate
- **WHEN** the provider reports final usage for a round
- **THEN** the observed stream receives a provider-sourced snapshot correlated to the same round
- **THEN** consumers can replace the estimate rather than add two samples together

#### Scenario: Tool dispatch follows final provider usage
- **WHEN** a model round reports usage and requests a tool
- **THEN** the provider-sourced context snapshot is emitted before that tool starts
- **THEN** the tool still executes through the existing sequential dispatch path

#### Scenario: Provider-free estimate remains useful
- **WHEN** a provider supplies no valid usage for a successful round
- **THEN** no provider-sourced replacement is emitted
- **THEN** the round's estimated snapshot remains the latest truthful context-usage value

### Requirement: Provider usage has explicit precedence and validation
A valid provider prompt-token count SHALL supersede the estimate for the same
round because it describes the backend's measured request. Provider usage MUST
NOT supersede an estimate when the prompt count is missing, negative, belongs to
a different round, or is otherwise invalid; in that case the estimate SHALL
remain active and the invalid sample SHALL be surfaced as a typed warning or
provider error according to severity.

#### Scenario: Valid provider count supersedes estimate
- **WHEN** round three has an estimated used-token count and later receives a valid measured prompt-token count
- **THEN** round three's current usage source becomes `provider`
- **THEN** both samples remain distinguishable in event history

#### Scenario: Provider count belongs to another round
- **WHEN** a usage sample cannot be correlated to the active model round
- **THEN** it does not replace that round's estimate
- **THEN** the mismatch is surfaced without corrupting context history

#### Scenario: Provider omits prompt count
- **WHEN** a provider reports only completion detail with no prompt-token count
- **THEN** the context manager retains the estimated used tokens for the round
- **THEN** the provider completion detail may remain available separately without relabeling the context estimate as provider-measured

### Requirement: Truthful context-window semantics
The context manager SHALL use an explicitly reported trustworthy context window
when one is available. An advisory warning budget MUST NOT be presented as that
window, and the system MUST NOT infer a window from a model name alone. When the
window is known, remaining capacity SHALL floor at zero and over-budget state
SHALL be explicit; when it is unknown, remaining capacity and percentage SHALL
remain unknown.

#### Scenario: Known budget is exceeded
- **WHEN** used context is 12,000 tokens against an effective 10,000-token window
- **THEN** the structured snapshot marks the request over budget
- **THEN** remaining capacity is zero rather than a negative display value

#### Scenario: Model name has no trusted budget metadata
- **WHEN** a model is configured but neither settings nor the provider supplies a trustworthy context window
- **THEN** the context window remains unknown
- **THEN** the system does not guess a percentage from the model name

#### Scenario: Advisory warning budget is not a model window
- **WHEN** `ContextBudgetTokens` is configured but the provider reports no trustworthy context window
- **THEN** estimates and descriptors leave the context window and remaining capacity unknown
- **THEN** the independent legacy over-budget warning still uses the configured threshold

### Requirement: Structured usage preserves legacy warning behavior
Structured context usage SHALL be additive to the existing over-budget warning.
When the cheap estimate exceeds the configured budget, the existing advisory
warning SHALL still be available to legacy callers and the run SHALL still
proceed without dropping conversation content or failing solely because of the
estimate.

#### Scenario: Legacy caller remains warned
- **WHEN** an estimated request exceeds the configured budget during a legacy run
- **THEN** the existing advisory over-budget warning appears on the legacy stream
- **THEN** the run proceeds with the full assembled conversation

#### Scenario: Observed caller receives structured state
- **WHEN** the same over-budget condition occurs during an observed run
- **THEN** the observed stream includes a structured over-budget usage snapshot
- **THEN** any accompanying warning remains advisory rather than a second source of token values

### Requirement: Usage observation never compacts history
Producing estimated or provider-sourced usage SHALL NOT mutate, summarize, drop,
or reorder the stored conversation. V1 MUST NOT emit a `context_compaction`
omission because it does not compact history; an over-budget snapshot and warning
SHALL remain observations only.

#### Scenario: Usage rises above the budget
- **WHEN** a context snapshot reports usage above the effective budget
- **THEN** every existing conversation turn remains present and ordered
- **THEN** no `context_compaction` event is emitted

#### Scenario: Provider usage differs from estimate
- **WHEN** a provider measurement supersedes a local estimate
- **THEN** only the usage observation changes
- **THEN** the conversation snapshot remains byte-for-byte equivalent to the history produced by the run

### Requirement: Offline deterministic usage testing
Structured context-usage behavior SHALL be testable offline. Tests SHALL cover
estimates, provider replacements, lifecycle ordering, invalid-sample handling,
budget semantics, and non-compaction behavior against fixed conversations and
scripted providers with no network or real model.

#### Scenario: Fixed input has fixed estimated snapshot
- **WHEN** an offline test assembles the same conversation, injected context, and tools twice
- **THEN** it receives the same estimated used-token count and context-window state both times

#### Scenario: Scripted provider supersedes estimate
- **WHEN** an offline rich provider reports a valid measured usage sample
- **THEN** the test observes the estimated snapshot followed by the provider snapshot for the same round
- **THEN** neither observation mutates conversation history
