## Why

Phase 2 built the one ordered execution path with inert hard-check stages, and
Phase 3 defines permission as a soft human convenience. Phase 4 turns the hard
guardrail layer on: hooks enforce operator and SDK invariants even when human
permission prompts are bypassed.

## What Changes

- Add a `hooks` capability covering v1 lifecycle moments: before tool, after
  tool, prompt submit, run finished, session start, and session stop.
- Support two hook flavors with matching behavior: external commands declared in
  settings, and in-process hooks registered through the SDK.
- Make before-tool hooks enforce the hard pre-check slot in the executor:
  blocking short-circuits permission, sandbox, and tool execution; non-blocking
  hooks may reshape tool input.
- Make after-tool hooks enforce the hard post-check slot in the executor:
  hooks may replace or reject the result before the model sees it.
- Add prompt and session lifecycle hook points owned by the loop/session:
  prompt hooks may block a turn or inject standing context; run-finished hooks
  may trigger post-run side effects such as local notifications; session-start
  hooks may abort startup; session-stop hooks may run teardown and record
  failures.
- Add hook scoping by tool name, pattern over the moment's relevant detail, both
  combined, or no scope for broad rules.
- Define deterministic ordering, first-block-wins, and composition of
  non-blocking outputs.
- Fail closed for all hook failures: external command error, timeout, missing
  program, malformed or oversized output, in-process panic, and cancellation.
- Extend settings with external hook definitions and validate malformed hook
  definitions when the session is built.
- Surface hook outcomes on the existing event stream so any frontend can render
  blocks, redactions, and injections without reading internal state.

No prior phase's public contracts are broken; SDK additions are append-only and
the existing executor chain is filled rather than reshaped.

## Capabilities

### New Capabilities
- `hooks`: hard, unconditional lifecycle rules, including moments, scoping,
  verdicts, external-command hooks, in-process hooks, ordering, fail-closed
  behavior, and hook outcome events.

### Modified Capabilities
- `tool-executor`: replace the inert hard pre/post check behavior with real hook
  execution while preserving the existing ordered chain and short-circuit
  semantics.
- `agent-loop`: invoke prompt-submit, run-finished, session-start, and
  session-stop hooks on the existing run/session lifecycle.
- `configuration`: load, merge, and validate external hook declarations from the
  existing settings file format.

## Impact

- **Public SDK:** append hook registration types and hook outcome/event payloads
  in `pkg/agent` via `internal/core` aliases.
- **Internal machinery:** add real hook evaluation in `internal/hooks`, wire it
  into `internal/executor` stages, and add loop-owned lifecycle hook points in
  `internal/loop` / session construction.
- **Configuration:** extend `internal/config` with external hook settings,
  timeouts, scope validation, and home/project merge behavior.
- **Tool path:** before-tool hooks run before permission; after-tool hooks run
  before the model receives the result; bypass mode never disables hooks.
- **Testing:** cover all behavior offline with in-process fake hooks and
  throwaway external hook scripts using temp directories; no network and no real
  model.
