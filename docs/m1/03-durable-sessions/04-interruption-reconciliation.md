# S3.4 Interrupted Read-only Run Reconciliation

**Status:** pending acceptance
**Prerequisite:** [S3.3 accepted](03-atomic-observe.md)

## Goal

Repair an incomplete Provider or read-only tool turn after process exit without
automatically repeating work.

## Deliverables

- Startup detection of incomplete Provider turns and open read-only ToolCalls.
- Durable interrupted terminal outcome.
- Exactly one interrupted ToolResult per open read-only call.
- Idempotent reconciliation before any new Provider request.

M1 has no mutating or process Action Attempts.

## Acceptance

- [ ] An interrupted Provider turn receives one terminal interrupted outcome and
      no fabricated completed assistant block.
- [ ] Every open read-only ToolCall receives exactly one interrupted ToolResult
      before the Transcript becomes model context again.
- [ ] Repeated restart reconciliation is idempotent.
- [ ] No `read`, `list`, `search`, or Provider request repeats automatically.
- [ ] The reconciled session accepts a new explicit user turn.
- [ ] Crash injection before and after Transcript append, ToolResult transaction,
      cursor checkpoint, and terminal outcome preserves all invariants.
- [ ] A corrupt durable state stops instead of inventing recovery facts.

## Evidence

Retain crash-matrix results and reconciled Transcript fixtures under
`artifacts/m1/s3/3.4/`.

## Failure and rollback boundary

When exact reconciliation is impossible, stop with a typed corruption cause.
Never replay an uncertain operation or rewrite prior Transcript records.
