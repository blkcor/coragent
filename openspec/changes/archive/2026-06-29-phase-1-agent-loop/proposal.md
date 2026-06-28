## Why

Phase 0 delivered a streaming `Provider` seam but no agent — only a single
question-and-answer exchange. Phase 1 turns that backend into a **working agent
loop** that keeps going on its own until a task is done, surfacing everything it
does as one ordered, read-only event stream. This is Milestone M1 "It talks": the
heart of the harness, where context handling, tool requests, the wait-for-a-human
moment, and cancellation all meet.

## What Changes

- Add a **single run entry point** that starts a run from the user's input and
  returns one live, read-only event stream.
- Add the **gather → act → verify loop**: assemble the conversation, consult the
  model, run any requested tools, feed results back, repeat until a stop fires.
- Add a **conversation/context manager**: accumulates history, exposes an
  uncorruptable snapshot, and emits an advisory over-budget warning (compaction
  itself is deferred).
- Add the **single tool-dispatch seam** filled in Phase 1 by an inert stand-in
  runner — the sole producer of tool results — so later-phase permission, hooks,
  and sandbox apply universally with no bypass.
- Extend the event vocabulary (additively) with tool-start, tool-finish,
  run-finished (carrying exactly one named stop reason), permission-requested, and
  over-budget-warning events.
- Add **deterministic stop reasons** — completed, reached the step limit (a normal
  stop), cancelled, failed — with full cancellation propagation and recoverable
  tool failures.
- Add a **throwaway readout** frontend driving a `Session` to complete the M1 demo.

No Phase 0 public shape is modified — every addition is purely additive.

## Capabilities

### New Capabilities
- `agent-loop`: the gather→act→verify run lifecycle, the single run entry point,
  the run event stream and its vocabulary, deterministic stop reasons,
  cancellation, recoverable tool failures, the single dispatch seam, and the
  wait-for-a-human pass-through.
- `context-manager`: conversation accumulation across requests, the uncorruptable
  read-only snapshot, and the cheap over-budget estimate and advisory warning.

### Modified Capabilities
<!-- None. Phase 1 additions are purely additive; no Phase 0 requirement changes. -->

## Impact

- **New code:** `pkg/agent/session.go` (Session façade), `internal/loop`
  (loop driver), `internal/context` (conversation manager),
  `internal/executor` (stand-in dispatcher).
- **Additive edits:** `pkg/agent/types.go` (new event types/fields, `StopReason`,
  status constants, `Dispatcher` seam interface).
- **Frontend:** `cmd/demo` gains a Session-driven readout mode.
- **Reuses:** `internal/provider/testutil.FakeProvider` as the offline scripted
  model; the Phase 0 `Provider`, `Conversation`, `RunEvent` contracts unchanged.
- **No new dependencies.** No network in tests.
