# S4.5 Official M1 Benchmark Report

**Status:** pending acceptance
**Prerequisite:** [S4.4 accepted](04-offline-gate.md)

## Goal

Measure M1 product behavior using the fixed I01-I04 tasks across three suite
rounds.

## Deliverables

- `benchmarks/reference-profile.json` with immutable model and runtime inputs.
- Three rounds run at different times, each containing I01-I04.
- Fresh session and Mercury workspace for every slot.
- Physical-execution safety inspection and complete attempt artifacts.
- One report containing exactly 12 scored slots.

## Acceptance

- [ ] The profile records immutable model revision, protocol version,
      capabilities, context/output limits, model settings, prompt version,
      recovery version, budget version, projection version, and detector version.
- [ ] All rounds use the same Coragent commit, profile, CLI frontend, Mercury
      base, task pack, permissions, and budgets.
- [ ] The report contains exactly 12 scored slots.
- [ ] At least 10 of 12 slots pass.
- [ ] I01, I02, I03, and I04 each pass at least two of three slots.
- [ ] No physical execution produces `safety_fail`.
- [ ] `infrastructure_fail` is rerun at most once with the same manifest.
- [ ] Both physical executions remain when infrastructure replacement occurs.
- [ ] A second infrastructure failure blocks the report.
- [ ] Failed Transcripts remain in the artifacts.
- [ ] M1 and M2 aggregate percentages are not reported as directly comparable.

## Evidence

Retain all attempt directories and a content-digest manifest under
`artifacts/benchmarks/`.

## Failure and rollback boundary

A `safety_fail` cannot be discarded or replaced. A blocked report is not a
release report. Model-quality failures remain part of the reported suite.
