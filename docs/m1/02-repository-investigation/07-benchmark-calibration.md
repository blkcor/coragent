# S2.7 I01-I04 Benchmark Calibration and Slice 2 Gate

**Status:** pending acceptance
**Prerequisite:** [S2.6 accepted](06-prompt-loop.md)

## Goal

Prove that the product and benchmark harness can measure the four M1
investigation tasks before the official three-round report.

## Deliverables

- Exact I01-I04 prompts and structured goldens.
- Citation validation, attempt reset, safety inspection, outcome classification,
  and artifact capture.
- Fresh Mercury copy and new V2 session for every attempt.
- One real-model calibration pass per investigation task.

## Acceptance

- [ ] Scripted correct answers pass every scorer.
- [ ] Scorers reject missing facts, bad citations, speculation, wrong grouping,
      and attempted mutation.
- [ ] Each attempt captures Transcript, Event summary, ToolCalls, ToolResults,
      final answer, citation result, workspace diff, and safety result.
- [ ] The workspace diff is empty after every I01-I04 attempt.
- [ ] A pinned-model calibration produces one pass for each of I01-I04.
- [ ] Calibration failures remain in the artifacts.
- [ ] E, F, and R tasks contribute no M1 scored slots.
- [ ] No physical calibration execution triggers `safety_fail`.

## Evidence

Retain scorer fixtures and calibration attempts under
`artifacts/m1/s2/2.7/`.

## Failure and rollback boundary

A safety failure stops Slice 2. A model-quality failure remains evidence and
must be assigned to prompt, loop, tool, fixture, scorer, or adapter before rerun.

## Slice 2 exit

Slice 2 is mergeable only when every S2.1-S2.7 acceptance item passes, Mercury
is frozen, its clean suite is green, and each investigation task has a real-model
calibration pass. At this boundary Coragent is a useful open-ended repository
investigator.
