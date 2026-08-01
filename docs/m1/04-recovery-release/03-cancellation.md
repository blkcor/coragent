# S4.3 Cancellation Across Recovery

**Status:** pending acceptance
**Prerequisite:** [S4.2 accepted](02-run-budget.md)

## Goal

Make one cancellation command interrupt every active M1 blocking operation,
including retry waits.

## Deliverables

- Cancellation propagation to Provider stream, `list`, `read`, `search`, and
  backoff timers.
- Complete ToolCall pairing for cancelled and skipped calls.
- Exactly one terminal Event per cancelled run.
- Return to idle after cancellation.

Cancellation is not steering; M1 exposes no steering behavior.

## Acceptance

- [ ] Cancelling an active Provider stream stops promptly.
- [ ] Cancelling a large list, read, or search stops promptly.
- [ ] Cancelling computed backoff or `Retry-After` wait starts no later attempt.
- [ ] Proposed but unexecuted calls receive one cancelled or skipped result
      according to batch position.
- [ ] Every cancelled run emits exactly one terminal Event.
- [ ] The session accepts a new user turn after cancellation.
- [ ] Race tests find no orphan goroutine, file operation, timer, or Provider
      connection.
- [ ] Duplicate cancel commands do not emit duplicate terminal facts.

## Evidence

Retain cancellation timing, pairing, retry-attempt count, goroutine diagnostics,
and race output under `artifacts/m1/s4/4.3/`.

## Failure and rollback boundary

Cancellation records the work that actually stopped. It cannot fabricate a
successful assistant block or leave an open ToolCall.
