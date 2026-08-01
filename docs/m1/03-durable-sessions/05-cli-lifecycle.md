# S3.5 CLI Lifecycle Acceptance and Slice 3 Gate

**Status:** pending acceptance
**Prerequisite:** [S3.4 accepted](04-interruption-reconciliation.md)

## Goal

Prove durable lifecycle behavior through the supported line-oriented CLI after
a real process restart.

## Deliverables

- `coragent sessions` lists saved sessions.
- `coragent resume <session-id>` resumes an open session.
- `coragent close <session-id>` closes without deleting history.
- CLI rendering from atomic observe and Transcript replay.

## Acceptance

- [ ] Run I02 through the CLI, exit with Ctrl-D, list the session, resume it, and
      ask a follow-up about job ID creation.
- [ ] The resumed view contains the same prior semantic conversation.
- [ ] The follow-up uses existing context plus new repository reads.
- [ ] A cancelled run can be followed by a new turn in the same session.
- [ ] Closing preserves replay and rejects a new prompt clearly.
- [ ] CLI and direct Session drivers observe identical terminal outcomes.
- [ ] I01-I04 remain regression passes; this scenario adds no scored slot.
- [ ] The scenario succeeds after an actual process exit, not only store-object
      reconstruction in one test process.

## Evidence

Retain the two-process transcript, replay comparison, session listing, close
result, and regression output under `artifacts/m1/s3/3.5/`.

## Failure and rollback boundary

Frontend failure cannot alter durable Session truth. Close remains
non-destructive.

## Slice 3 exit

Slice 3 is mergeable only when every S3.1-S3.5 acceptance item passes, atomic
observe race tests show no gap, interrupted calls pair exactly once, and the
two-turn CLI scenario succeeds after a real restart.
