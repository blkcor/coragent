# S2.5 Instruction Discovery and Precedence

**Status:** pending acceptance
**Prerequisite:** [S2.4 accepted](04-search-tool.md)

## Goal

Load the project instructions applicable to the active working path and preserve
their provenance and precedence.

## Deliverables

- Discovery of `CLAUDE.md` and `AGENTS.md` from repository root through active
  working directory.
- Precedence from user preferences, root instructions, deeper instructions,
  current user request, and hard runtime policy.
- Same-directory ordering of `CLAUDE.md` before `AGENTS.md`.
- Content-hash deduplication with retained source provenance.
- Transcript record of loaded sources and scopes.

## Acceptance

- [ ] Missing optional instruction files are normal.
- [ ] Root instructions apply throughout the repository.
- [ ] Deeper instructions apply only to their subtree and override root conflicts.
- [ ] `AGENTS.md` wins over `CLAUDE.md` at the same directory scope.
- [ ] Identical documents load once while retaining source provenance.
- [ ] The current request overrides project guidance but not hard runtime policy.
- [ ] Source paths, scopes, hashes, and precedence are recorded without protected
      content leakage.
- [ ] Discovery remains inside the workspace and launches no process.

## Evidence

Retain the precedence matrix, deduplication fixtures, and Transcript projection
tests under `artifacts/m1/s2/2.5/`.

## Failure and rollback boundary

Missing optional files do not fail a run. Unreadable required state produces a
typed cause; no instruction file is modified.
