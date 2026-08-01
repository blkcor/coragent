# S4.1 Classified Bounded Provider Retry

**Status:** pending acceptance
**Prerequisite:** [Slice 3 accepted](../03-durable-sessions/05-cli-lifecycle.md)

## Goal

Recover from ordinary transient Provider failures without blind or unlimited
retry.

## Deliverables

- Typed Provider failure classification.
- Retry for rate limit, transient transport error, and Provider overload only.
- At most eight retries after the initial request.
- Computed delay starting at 500 ms, doubling to a 30-second cap, with 20
  percent jitter.
- `Retry-After` replacement delay capped at 120 seconds.
- Fake clock and deterministic fake jitter source.

## Acceptance

- [ ] Fake-clock tests prove the exact computed sequence and 30-second cap.
- [ ] Fake-jitter tests prove every delay stays within the documented 20 percent
      range.
- [ ] Valid `Retry-After` replaces computed delay and never exceeds 120 seconds.
- [ ] A ninth retry opportunity is never started.
- [ ] Authentication, invalid request, malformed stream, cancellation, context
      overflow, and output truncation do not enter this retry path.
- [ ] Failed-attempt partial output is not persisted as a completed assistant
      block.
- [ ] Every path either succeeds or reaches one terminal outcome.
- [ ] No recovery path calls itself without consuming a counter.

## Evidence

Retain clock traces, jitter cases, failure-classification matrix, and retry-bound
tests under `artifacts/m1/s4/4.1/`.

## Failure and rollback boundary

Permanent and protocol failures stop immediately. M1 does not implement M3
context compaction or output continuation as hidden recovery.
