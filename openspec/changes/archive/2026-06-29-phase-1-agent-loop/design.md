# Design — Phase 1 Agent Loop

## Context

Phase 0 shipped (and froze) these public shapes in `pkg/agent`: `Conversation`,
`Turn`, `Tool`, `ToolCall`, `ToolResult`, `Provider`, `StreamOptions`, `RunEvent`
(+ `RunEventType`), `ReplyEnded`/`ReplyEndReason`, `PermissionRequest`/
`PermissionDecision`. The `Provider.StreamReply(ctx, conv, tools, opts) <-chan RunEvent`
seam already streams text deltas, reassembled tool calls, a reply-ended event, and
errors. `internal/provider/testutil.FakeProvider` scripts replies offline.

Architecture invariants bind this phase: the harness never imports a frontend;
`pkg/agent` is the only public contract; there is exactly **one** tool dispatch path;
a phase must not change a prior phase's public contract (additive only).

## Goals / Non-Goals

**Goals:**
- One run entry point returning one ordered, read-only event stream.
- The gather→act→verify loop with deterministic, exactly-one stop reasons.
- Full cancellation propagation; recoverable vs unrecoverable failures separated.
- The single dispatch seam, filled by an inert stand-in, with the permission
  question carried on the shared stream.
- Conversation manager: accumulate, snapshot (uncorruptable), over-budget warning.
- Backpressure without loss; abandoned-and-cancelled reader never wedges the agent.
- Everything offline-testable against fakes.

**Non-Goals:**
- Real tools and the real middleware chain (Phase 2).
- Permission rules/modes (Phase 3), hooks (Phase 4), sandbox (Phase 5).
- Context compaction/summarization — only the inert plug-in point + warning ship.
- Parallel tool execution; mid-run steering; turn-level retry.

## Decisions

### D1. Reuse `agent.RunEvent` as the single run-stream vocabulary (additive)
The provider already emits `RunEvent`. Rather than introduce a parallel run-event
type, **extend** `RunEvent` so the loop consumes the provider's subset and emits the
richer run-level set on the same type. New `RunEventType` constants are **appended**
(preserving Phase 0 iota values): `ToolStartedEvent`, `ToolFinishedEvent`,
`RunFinishedEvent`, `PermissionRequestedEvent`, `OverBudgetWarningEvent`. New
`RunEvent` fields: `ToolResult *ToolResult`, `RunFinished *RunFinished`,
`Permission *PermissionRequest`, `Warning string`.
*Alternative considered:* a separate `RunStreamEvent` type — rejected: two
vocabularies for one stream violates US-009/US-010 and duplicates the text/toolcall
cases. Reuse keeps the diff append-only and satisfies the additive invariant.

### D2. Run-level stop reason is distinct from provider reply-end reason
`ReplyEndReason` (Phase 0) describes how *one model reply* ended (finished /
stopped-to-call-tools / cut-off). Phase 1 adds `StopReason` describing how the
*whole run* ended: `StopCompleted`, `StopReachedStepLimit`, `StopCancelled`,
`StopFailed`. `RunFinished{ Reason StopReason; Err error }` carries the named cause
(`Err` set only for `StopFailed`). `StopReason.IsError()` returns true only for
`StopFailed` — this is how a developer tells "the guard tripped" (step limit) and
"the user stopped it" (cancelled) apart from "something broke" (US-014/US-017).

### D3. `Dispatcher` is a public seam; Phase 1 ships an inert stand-in
```
Dispatcher interface {
    Dispatch(ctx, call ToolCall, emit func(RunEvent) error) (ToolResult, error)
}
```
- A **recoverable** tool failure → `ToolResult{IsError:true}, nil`: the loop emits
  tool-finish and continues (US-018/019).
- A **truly unrecoverable** condition → non-nil `error`: the loop ends the run with
  `StopFailed`, distinct from a recoverable failure (US-021).
- The `emit` callback hands the dispatcher the **same live stream**, so it can push a
  `PermissionRequestedEvent` (carrying `PermissionRequest.ReplyPath`) and block on
  the reply — the loop never interprets it (US-022/023). `emit` returns an error when
  the context is cancelled so the dispatcher can abort.

`internal/executor.StandIn` implements this, returning a placeholder result — the
sole producer of tool results in Phase 1 (US-005). Phase 2's real executor
implements the same interface, so the loop is untouched.
*Alternative:* keep `Dispatcher` internal — rejected: the seam is the architectural
chokepoint and SDK callers/tests inject fakes; a public, minimal interface is the
stable contract Phase 2 fulfils.

### D4. `Session` is the composition root in `pkg/agent`
`SessionConfig{ Provider; SystemPrompt; Tools; MaxRounds; ContextBudgetTokens;
StreamOptions; Dispatcher }` (`Dispatcher` nil → `StandIn`; zero `MaxRounds`/budget →
documented defaults). `NewSession(cfg) *Session`. `Run(ctx, input) (<-chan RunEvent,
error)` — error return is the clean refusal of a second concurrent run (US-027).
`Conversation() Conversation` returns the manager's snapshot. The Session wires
`internal/loop` + `internal/context` + the dispatcher.

**Cycle correction (decided at implementation):** `internal/{loop,context,executor,
provider}` need the domain types, and `pkg/agent` composes those packages — so
`pkg/agent` importing them while they import `pkg/agent` is an import cycle. The
type *definitions* therefore live in a new leaf package `internal/core`;
`pkg/agent/types.go` re-exports every name as a **type alias** (`type Conversation
= core.Conversation`, …), so the public contract is byte-identical and unchanged.
The internal machinery imports `internal/core`, never `pkg/agent`; `pkg/agent`
imports `internal/core` + `internal/loop` + `internal/context` + `internal/executor`
with no cycle.

### D5. Loop algorithm (`internal/loop.Run(ctx, Deps, emit)`)
Per round, in order:
1. Snapshot the conversation; if `EstimateTokens() > ContextBudgetTokens` emit
   `OverBudgetWarningEvent` and proceed.
2. emit Status **thinking**; call `provider.StreamReply`.
3. Drain provider events: TextDelta → passthrough emit; ToolCallEvent → accumulate;
   ReplyEndedEvent → note; ErrorEvent → capture `provErr`.
4. `ctx.Err()!=nil` → `RunFinished{StopCancelled}` (mid-think). `provErr!=nil` →
   Status idle + `RunFinished{StopFailed, provErr}`; **partial reply not appended**
   (US-020).
5. Append assistant turn. **No tool calls** → Status idle + `RunFinished{StopCompleted}`.
6. emit Status **calling_tool**. For each call in requested order: emit
   `ToolStartedEvent`; `Dispatch(ctx, call, emit)`; then `ctx.Err()!=nil` →
   `StopCancelled` (mid-tool); dispatch `error` → `RunFinished{StopFailed}`; else
   emit `ToolFinishedEvent`. Append all results as one tool turn.
7. If the next round index would reach `MaxRounds` → `RunFinished{StopReachedStepLimit}`
   (normal stop).

Exactly one `RunFinishedEvent` is emitted on every path, then the channel closes.

### D6. Channel buffering & guaranteed terminal delivery (backpressure)
The run channel is **buffered with capacity 1**. Normal `emit`:
`select { case ch<-ev: ...; case <-ctx.Done(): return ctx.Err() }` — a live slow
reader throttles the loop with no loss; a cancel escapes the send (US-029). The
single terminal `RunFinishedEvent` uses a guaranteed-enqueue helper: because it is
the last send and the buffer has room, it enqueues even when `ctx` is already
cancelled — so a live cancelling reader still receives the `StopCancelled` outcome
(US-015) while an abandoned reader cannot wedge the goroutine (US-029). `defer
close(ch)` and `defer` clearing the in-flight guard.
*Alternative:* unbuffered + select-with-ctx on the terminal send — rejected: races
between `ch<-ev` and a closed `ctx.Done()` would drop the cancelled outcome ~50% of
the time, making US-015 flaky.

### D7. Conversation manager (`internal/context.Manager`)
A mutex-guarded `agent.Conversation` seeded with the system turn. `AppendUser`,
`AppendAssistant(content, calls)`, `AppendToolResults(results)`. `Snapshot()`
**deep-copies** turns and nested `ToolCalls`/`ToolResults`/`Arguments` maps so the
returned value cannot mutate the live conversation (US-025). `EstimateTokens()` is a
cheap `sum(len(content))/4` estimate sufficient for the warning (US-026); a precise
tokenizer is a later internal swap. Compaction is an inert documented plug-in point.

### D8. In-flight guard
`Session` holds an `atomic.Bool` (or mutex flag). `Run` compare-and-sets it; a second
concurrent `Run` returns an error without touching history; the run goroutine clears
it via `defer` (US-027). One in-flight turn per session matches the architecture's
concurrency model; subagents (Phase 6) get their own session.

## Risks / Trade-offs

- **Terminal-event delivery races** → D6's buffered-cap-1 guaranteed enqueue makes
  delivery deterministic for a live reader and hang-free for an abandoned one.
- **Reusing `RunEvent` for two roles (provider subset vs run set)** → mildly muddier
  type, but the alternative (two types) is worse for US-009/US-010 and breaks the
  additive invariant. Mitigated by documenting which events each producer emits.
- **Public `Dispatcher` seam fixed now, fully implemented in Phase 2** → kept minimal
  (one method) so Phase 2 fills behavior without changing the signature.
- **Cheap token estimate may fire the warning early/late** → acceptable; it changes
  only *when* the advisory fires, never run behavior; precise counting deferred.
- **Cancellation reported as a failed-style distinguishable outcome** → whether the
  TUI prefers a benign "cancelled" finish is an additive Phase 7 decision.

## Open Questions

- Default values for `MaxRounds` and `ContextBudgetTokens` (pick sane defaults; SDK
  callers override) — resolved at implementation with the surrounding code in hand.
- Whether one tool turn holding all results vs one turn per result serializes best
  for the real provider later — Phase 1 uses one turn with a results slice (matches
  the Phase 0 `Turn.ToolResults` plural field and openai.go's per-result expansion).
