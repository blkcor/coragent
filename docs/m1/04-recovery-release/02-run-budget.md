# S4.2 Durable M1 Run Budget

**Status:** pending acceptance
**Prerequisite:** [S4.1 accepted](01-bounded-retry.md)

## Goal

Ensure retry and Provider work remain bounded across process restarts.

## Deliverables

Durable per-run counters with these M1 limits:

- 64 logical model calls;
- 96 Provider transport attempts;
- 10 minutes cumulative retry delay.

The budget is reserved before work. M1 does not add later tool-call, active-time,
token, compaction, or continuation budgets.

## Acceptance

- [ ] Every logical request and transport attempt is durably reserved before it
      begins.
- [ ] Retry delay is reserved before waiting.
- [ ] Crash reconciliation never reduces a reserved counter.
- [ ] Restart cannot reset any counter for the interrupted run.
- [ ] The first exhausted bound prevents new work and records one typed terminal
      outcome.
- [ ] Resume of the interrupted run retains its old budget.
- [ ] A new explicit user turn receives a new run ID and fresh budget.
- [ ] Crash injection before and after each reservation preserves monotonic
      counters.

## Evidence

Retain counter traces, crash matrices, exhausted outcomes, and new-run tests
under `artifacts/m1/s4/4.2/`.

## Failure and rollback boundary

Conservative over-counting after uncertainty is allowed; resetting or reducing
a counter is not. Budget exhaustion performs no side effect.
