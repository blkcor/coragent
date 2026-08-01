# S3.2 Create, List, Load, Resume, and Close

**Status:** pending acceptance
**Prerequisite:** [S3.1 accepted](01-storage-layout.md)

## Goal

Complete the M1 lifecycle so saved work can be found, replayed, resumed, and
closed without deleting history.

## Deliverables

- Session create, list, load, resume, and close behavior.
- Stable open/closed state.
- Non-destructive, idempotent close.
- Duplicate, late, and session-mismatched command rejection.

Exiting the CLI while idle does not close the session. A closed session remains
available for replay but rejects new submit commands.

## Acceptance

- [ ] Creation records workspace identity, immutable M1 authority, projection
      version, and initial Event cursor.
- [ ] Listing returns stable session ID, workspace, last activity, and open or
      closed state without contacting the Provider.
- [ ] Loading reproduces Transcript and current state without a Provider call.
- [ ] Resuming an open idle session accepts a new user turn.
- [ ] Closing appends a fact, deletes nothing, is idempotent, and rejects later
      submit commands.
- [ ] Duplicate, late, and session-mismatched commands change no state.
- [ ] One session still permits at most one active run.

## Evidence

Retain lifecycle state-machine and store-reopen tests under
`artifacts/m1/s3/3.2/`.

## Failure and rollback boundary

Lifecycle failures append or expose no false state. Close is not deletion, so no
data restoration step is required.
