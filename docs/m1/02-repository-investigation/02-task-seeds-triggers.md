# S2.2 Task-specific Seeds and Recovery Triggers

**Status:** pending acceptance
**Prerequisite:** [S2.1 accepted](01-mercury-base.md)

## Goal

Keep Mercury clean while making future repair and recovery conditions
deterministic and auditable.

## Deliverables

- Versioned F01 and F02 seed patches applied only to temporary workspaces.
- Runner-side R01 and R02 trigger state machines.
- Versioned permission scripts outside the model-visible workspace.
- Separate task-pack version and content digests.

M1 freezes these inputs but does not score E, F, or R tasks.

## Acceptance

- [ ] The clean Mercury suite is green before and after every seed test.
- [ ] F01 deterministically makes only the cancellation/process-group
      regression fail.
- [ ] F02 deterministically makes only the archive path-escape regression fail.
- [ ] Removing either seed restores clean state without editing the frozen base.
- [ ] R01 fails only the first matching content-search call and records one
      trigger activation.
- [ ] R02 waits for a matching prepared revision, changes only the declared
      target state, and delivers approval only for that stale revision.
- [ ] R02's full product flow remains unscored until M2 supplies patch and
      approval behavior.
- [ ] M1 permission scripts deny mutation, commands, external roots, environment
      expansion, and tool network.
- [ ] Task-pack digests are independent of the Mercury base digest.

## Evidence

Retain clean/seeded test matrices, trigger traces, permission tests, and digests
under `artifacts/m1/s2/2.2/`.

## Failure and rollback boundary

Seeds and triggers operate only on attempt-owned temporary state. A trigger
failure invalidates the task pack; it never justifies editing the frozen base.
