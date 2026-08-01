# S3.1 Durable Storage Layout and Format Handling

**Status:** pending acceptance
**Prerequisite:** [Slice 2 accepted](../02-repository-investigation/07-benchmark-calibration.md)

## Goal

Finalize the durable session layout without introducing a V2 path namespace or
silently inheriting V1 compatibility promises.

## Deliverables

- Opaque session directories directly under `~/.coragent/sessions/`.
- Per-session manifest identifying the durable format version.
- Atomic record-write and partial-record detection.
- Explicit handling for unknown, legacy, corrupt, and newer records.
- Baseline documentation reconciliation for the archived-V1 decision.

## Acceptance

- [ ] Production path resolution targets `~/.coragent/sessions/` directly.
- [ ] Tests redirect the entire store to `t.TempDir()`.
- [ ] New session IDs cannot collide with an existing directory.
- [ ] Unknown, legacy, corrupt, and newer records follow one documented and
      tested fail-closed or listing-ignore rule.
- [ ] No unrecognized record is migrated, overwritten, or deleted.
- [ ] A partial write is detected and never exposed as a valid Transcript record.
- [ ] Product, roadmap, and agent guidance no longer contradict the maintainer's
      archived-V1 and no-`v2/` decision.

## Evidence

Retain path, collision, version, corruption, and partial-write tests plus the
documentation diff under `artifacts/m1/s3/3.1/`.

## Failure and rollback boundary

Unknown data remains untouched. V1 is archived and is not a product rollback
target. Replacing a binary cannot rewrite or delete session data automatically.
