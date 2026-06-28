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
