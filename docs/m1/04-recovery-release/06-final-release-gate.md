# S4.6 Final M1 Release Gate

**Status:** pending acceptance
**Prerequisite:** [S4.5 accepted](05-benchmark-report.md)

## Goal

Make the final M1 release decision from a clean checkout and a retained evidence
bundle.

## Acceptance

- [ ] A new user configures the pinned Provider and receives a cited repository
      answer through the CLI in less than five minutes.
- [ ] A saved session resumes after process exit with the same Transcript and
      accepts a follow-up turn.
- [ ] Transcript replay produces the same user-visible semantic conversation.
- [ ] Ctrl-C cancels active work and leaves no Provider or tool work.
- [ ] Restart cannot reset model-call, transport-attempt, or retry-delay counters.
- [ ] The official report passes 10 of 12 slots, every task passes at least two,
      and no physical execution has `safety_fail`.
- [ ] The evidence manifest records Coragent commit, OS/architecture, Go version,
      Provider and model ID, profile/task-pack digests, prompt and detector
      versions, frontend, and every attempt outcome.
- [ ] The bundle contains offline/race/lint/build output, Mercury base output,
      seed red-green evidence, lifecycle and cancellation transcripts, benchmark
      artifacts, safety results, and the release decision.
- [ ] Benchmark and generated-model artifacts are excluded from Git.
- [ ] No TUI, mutation, command, tool network, public SDK, or V1 compatibility
      capability has entered the build.

## Evidence

Retain the signed content-digest manifest and final decision under
`artifacts/m1/release/`; retain physical benchmark attempts under
`artifacts/benchmarks/`.

## Failure and rollback boundary

A safety failure, blocked benchmark report, failed race test, secret-projection
failure, or clean-checkout failure blocks release regardless of aggregate score.
M1 has no repository side effect to undo; session data remains append-only.

## Slice 4 and M1 exit

Slice 4 is mergeable and M1 is complete only when every S4.1-S4.6 acceptance
item passes. The released artifact is the line-oriented read-only companion;
the TUI remains an M4 deliverable.
