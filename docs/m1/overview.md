# Coragent V2 M1 Delivery Plan

## Status and authority

This directory is the implementation handoff for M1: **Read-only Repo
Companion**. Each numbered document is one mandatory acceptance checkpoint.
The four numbered directories are independently mergeable vertical slices.

The source contracts remain authoritative:

- [`docs/product.md`](../product.md)
- [`docs/architecture.md`](../architecture.md)
- [`docs/roadmap.md`](../roadmap.md)
- [`docs/benchmarks.md`](../benchmarks.md)

This plan records one later maintainer decision: V1 is archived, session data
lives directly under `~/.coragent/sessions/`, and M1 does not create a `v2/`
subdirectory. Older baseline statements about V1 rollback must be reconciled
before the durable-session slice is accepted.

## Product outcome

M1 ships a useful line-oriented terminal product. A developer can enter a
repository, ask an investigation question, let Coragent inspect real files with
read-only tools, receive an answer with file and line citations, cancel active
work, leave the process, resume the same session, and continue the conversation.
Transient Provider failures are retried within durable bounds.

M1 is complete only when this works through the real CLI, not merely package
tests.

## Non-goals

- TUI, Bubble Tea, full-screen layout, panels, themes, mouse input, or rich
  Markdown rendering.
- File mutation, patch preparation, shell commands, process supervision, or an
  OS sandbox.
- Tool network access, approval, argument revision, grants, or permission modes.
- Mutating or process `Action Attempt` recovery.
- Context compaction, continuation, blob offload, or long-task recovery.
- Steering or queued next prompts.
- Public SDK or `pkg/coragent/`.
- V1 API, wire, runtime, or session-data compatibility code.
- Subagents, parallel tools, MCP, plugins, hooks, skills, or remote connectors.
- Scored execution of M2 E01-E04, F01-F02, or R01-R02 tasks.

## Settled decisions

1. M1 uses a line-oriented CLI. The TUI remains an M4 deliverable.
2. Sessions live directly under `~/.coragent/sessions/`; format evolution uses a
   session manifest, not a path-level V2 namespace.
3. All runtime packages remain internal through M4.
4. M1 exposes only pure-Go, workspace-scoped `list`, `read`, and `search` tools.
5. Every tool call uses one `Action Broker` path.
6. Credential isolation and data projection ship in the first usable slice.
7. Mercury is frozen as a complete clean base in M1. F01/F02 defects and
   R01/R02 failures use task-specific seeds or triggers outside the visible
   workspace.
8. The M1 report contains I01-I04 across three rounds: 12 scored slots. Release
   requires at least 10 passes, every task at least two of three, and zero
   `safety_fail` executions.

## Product flow

```text
CLI --SessionCommand--> Session / Engine --> Prompt --> Provider
                              |
                              +--> Action Broker --> scoped WorkspaceFS
                              |
                              +--> Transcript / Store --> Event --> CLI

Credential Source and Data Projector guard every projection boundary.
```

The Engine does not import the CLI. The Provider adapter does not emit frontend
Events. Tools receive no ambient filesystem or network client.

## How to execute this plan

- Complete documents in numeric order.
- A document is accepted only when every checkbox in its `Acceptance` section
  passes and its evidence is retained.
- Do not begin the next numbered document after a failed checkpoint.
- The final document in each directory contains that slice's exit criterion.
- A slice may merge only after its exit criterion passes. Every merged slice
  must remain runnable and useful if later slices never land.
- A failed safety check stops the slice and cannot be waived by a demo or score.
- Offline tests use a scripted fake Provider, fake clock, fake credential
  source, and `t.TempDir()`; they never contact a real model or user state.
- Step evidence lives under the exact `artifacts/m1/` path named in the step.
  Benchmark evidence lives under `artifacts/benchmarks/`. Neither is committed.

M1 will touch more than eight files and several internal packages. The split
below is by product behavior, not package layer.

## Ordered checkpoints

### Slice 1: Safe single-file grounded answer

1. [Serializable commands and Events](01-safe-grounded-answer/01-command-event.md)
2. [Append-only Transcript and initial store](01-safe-grounded-answer/02-transcript-store.md)
3. [Credential isolation and data projection](01-safe-grounded-answer/03-data-projection.md)
4. [Streaming Provider adapter](01-safe-grounded-answer/04-provider-adapter.md)
5. [Action Broker and `read`](01-safe-grounded-answer/05-action-broker-read.md)
6. [Line-oriented CLI](01-safe-grounded-answer/06-cli.md)

### Slice 2: Complete repository investigation

1. [Complete Mercury base](02-repository-investigation/01-mercury-base.md)
2. [Task seeds and recovery triggers](02-repository-investigation/02-task-seeds-triggers.md)
3. [Pure-Go `list`](02-repository-investigation/03-list-tool.md)
4. [Pure-Go `search`](02-repository-investigation/04-search-tool.md)
5. [Instruction discovery](02-repository-investigation/05-instruction-discovery.md)
6. [Prompt assembly and multi-tool loop](02-repository-investigation/06-prompt-loop.md)
7. [I01-I04 calibration](02-repository-investigation/07-benchmark-calibration.md)

### Slice 3: Durable sessions

1. [Storage layout and formats](03-durable-sessions/01-storage-layout.md)
2. [Session lifecycle](03-durable-sessions/02-session-lifecycle.md)
3. [Atomic observation](03-durable-sessions/03-atomic-observe.md)
4. [Interrupted-run reconciliation](03-durable-sessions/04-interruption-reconciliation.md)
5. [CLI lifecycle acceptance](03-durable-sessions/05-cli-lifecycle.md)

### Slice 4: Recovery and release

1. [Classified bounded retry](04-recovery-release/01-bounded-retry.md)
2. [Durable M1 Run Budget](04-recovery-release/02-run-budget.md)
3. [Cancellation across recovery](04-recovery-release/03-cancellation.md)
4. [Offline release invariants](04-recovery-release/04-offline-gate.md)
5. [Official benchmark report](04-recovery-release/05-benchmark-report.md)
6. [Final M1 release gate](04-recovery-release/06-final-release-gate.md)

## Comparability rule

M1's 12-slot investigation report and M2's 36-slot mixed-task report are not
directly comparable. Longitudinal comparison is permitted only for the I01-I04
panel when the Mercury base, task prompt, scorer, and unseeded workspace digest
are unchanged. Model, Provider profile, prompt/runtime, budget, or frontend
changes must be labelled in the comparison manifest.

F01, F02, R01, and R02 have no M1 scored slots and therefore no M1-to-M2 score
trend. If a later task requires a different Mercury base, create a new fixture
version, retain the old base, and establish a new I01-I04 baseline.

## External requirements

Offline work requires no account, API key, MCP server, external service, or
network. The official report requires one fixed OpenAI-compatible endpoint, one
dedicated Provider credential, an immutable model snapshot supporting streaming
ToolCalls and tool-result continuation, at least 32,000 input tokens and 8,000
output tokens, the Go toolchain, and `golangci-lint`.

The credential is runtime-only and never enters examples, tests, logs,
Transcript, Events, Model Context, or benchmark artifacts.

## Rollback and premise risk

M1 tools cannot mutate the repository or launch commands. Session records are
append-only; unknown formats fail closed instead of being rewritten. V1 is
archived and is not an M1 rollback target. Replacing a V2 binary must not rewrite
or delete data under `~/.coragent/sessions/`.

This plan assumes the complete 12-task Mercury topology can be frozen during M1
without weakening M2. If that assumption fails, bump the fixture version and
restart same-fixture longitudinal comparison. Never edit a frozen base in place
or compare the M1 and M2 aggregate percentages as though they had the same task
mix.
