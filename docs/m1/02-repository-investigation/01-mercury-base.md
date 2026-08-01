# S2.1 Complete and Freeze the Mercury Base Repository

**Status:** pending acceptance
**Prerequisite:** [Slice 1 accepted](../01-safe-grounded-answer/06-cli.md)

## Goal

Create one clean, deterministic repository fixture that supports all 12 core
benchmark tasks without changing its base between M1 and M2.

## Deliverables

- Complete Mercury repository under `testdata/benchmark-repo/`.
- `cmd/mercury/`, `internal/config/`, `internal/jobs/`, `internal/archive/`,
  `internal/worker/`, `internal/discovery/`, user-facing `docs/`, project
  instructions, and deterministic tests.
- Fixture manifest containing a base version and SHA-256 content digest.
- Fresh-copy setup for every benchmark attempt.

The clean base contains all E/F/R target seams but none of the F01/F02 defects.

## Acceptance

- [ ] `go test ./...` passes from the Mercury base root.
- [ ] I01-I04 goldens resolve to existing paths, symbols, and line ranges.
- [ ] E01-E04 begin from the intended pre-feature behavior without partial
      solutions or compatibility aliases.
- [ ] F01/F02 target code and regression seams exist while clean tests stay
      green.
- [ ] R01/R02 target code and trigger seams exist without affecting clean
      behavior.
- [ ] The manifest digest changes if any base file changes.
- [ ] Every attempt copies the immutable base into a fresh temporary workspace.
- [ ] Goldens and seeded-bug descriptions are absent from the copied workspace.

## Evidence

Retain the manifest, base-suite output, task-location audit, and copy-isolation
test under `artifacts/m1/s2/2.1/`.

## Failure and rollback boundary

Do not freeze a failing or incomplete base. After freeze, corrections require a
new fixture version; never edit the frozen version in place.
