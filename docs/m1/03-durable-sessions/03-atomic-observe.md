# S3.3 Atomic Observation and Session-wide Cursors

**Status:** pending acceptance
**Prerequisite:** [S3.2 accepted](02-session-lifecycle.md)

## Goal

Let a frontend reconnect without losing an Event between snapshot and live
subscription.

## Deliverables

- One atomic observe operation.
- Snapshot containing Transcript projection, partial assistant buffer, active
  read-tool state, current run state, and cursor.
- Subscriber registration under the same observation lock as snapshot capture.
- Live release of Events whose cursor is greater than the snapshot cursor.

## Acceptance

- [ ] Event cursors increase across every run in a session and survive restart.
- [ ] Snapshot plus subscription cannot lose ToolResult, cancellation, or
      terminal Events.
- [ ] Races against assistant completion, tool completion, cancellation, and run
      termination produce neither gaps nor duplicate semantic facts.
- [ ] Reconnecting from an old cursor yields every later Event in order.
- [ ] Reconnecting from the current cursor yields no replayed live Event.
- [ ] Frontend deduplication by cursor produces the same conversation as
      Transcript replay.
- [ ] Race tests are stable under repeated execution.

## Evidence

Retain race-test output, cursor traces, and reconnect fixtures under
`artifacts/m1/s3/3.3/`.

## Failure and rollback boundary

Observation failure affects only a subscriber; it cannot mutate Session truth or
advance the durable cursor without a corresponding Event.
