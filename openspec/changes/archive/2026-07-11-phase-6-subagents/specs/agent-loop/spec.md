## MODIFIED Requirements

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

## ADDED Requirements

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
