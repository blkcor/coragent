## Context

Phase 2 established the executor chokepoint and deliberately left the hard
pre/post stages inert. Phase 3 treats permission as soft and bypassable. Phase 4
fills the hard stages and adds loop-owned lifecycle hook moments without
changing the dispatch seam or importing frontend code.

The current public surface re-exports domain types from `internal/core` through
`pkg/agent`. The executor already accepts `PreToolCheck` and `PostToolCheck`
seams, and the loop already owns the live event stream used by frontends. Hooks
should extend those seams, not create a second execution path.

## Goals / Non-Goals

**Goals:**
- One hook engine supporting external-command and in-process hooks.
- Six v1 moments: session start, prompt submit, before tool, after tool, run
  finished, and session stop.
- Identical scoping and verdict semantics across hook flavors.
- Before-tool hooks integrated into the hard pre-check slot; after-tool hooks
  integrated into the hard post-check slot.
- Prompt/session hooks invoked by the loop and session construction lifecycle.
- Fail-closed behavior for hook failures, including timeout, malformed output,
  panic, oversized output, and cancellation.
- Additive SDK and event-stream surface so frontends can observe outcomes.
- Offline testability with fakes and temp external scripts.

**Non-Goals:**
- Sandboxing external hook programs; v1 treats them as trusted operator config.
- Permission mode/rule behavior; bypass mode affects permission only.
- TUI rendering beyond event payloads.
- Subagent hook inheritance.
- Hot reload of hook definitions.
- Parallel hook execution.

## Decisions

### D1. Model hooks around a shared moment, scope, event, verdict vocabulary

Define one internal vocabulary for hook moments, scopes, event payloads, and
verdicts. External hooks translate JSON stdin/stdout into that vocabulary;
in-process hooks receive and return the vocabulary directly. This keeps parity
real instead of maintaining two subtly different systems.

*Alternative considered:* separate external and in-process APIs with similar
names. Rejected because parity is a Phase 4 requirement and separate APIs invite
drift in edge cases such as injected context, replaced results, and fail-closed
errors.

### D2. Keep tool hooks inside the existing executor stage seams

Before-tool hooks fill the existing `PreToolCheck` slot. A block returns an error
result and short-circuits permission, sandbox, and the tool. A non-blocking
verdict may replace the call arguments; the executor then reuses its existing
argument validation before continuing. After-tool hooks fill the existing
`PostToolCheck` slot. They may replace the result or convert it into a blocked
error before the model sees it.

*Alternative considered:* call hooks from individual tools. Rejected because it
would break the "one execution path" invariant and make custom tools easy to
miswire.

### D3. Loop-owned moments run outside the tool chain

Prompt-submit, run-finished, session-start, and session-stop hooks are not tool
calls. The loop or session builder invokes them at lifecycle boundaries:
session-start before a session accepts work, prompt-submit before provider
streaming begins, run-finished after the terminal outcome is known and before the
terminal event is emitted, and session-stop during shutdown/close. Prompt-submit
can block the turn or inject context into that turn. Run-finished can trigger
post-run side effects such as local notifications; blocks or failures are
surfaced without changing the already-determined run outcome. Session-start can
abort construction/startup or inject standing context. Session-stop attempts
cleanup and records failures without preventing the session from stopping.

*Alternative considered:* represent lifecycle moments as synthetic tools.
Rejected because that would leak internal lifecycle actions into the model-facing
tool set and confuse permission/sandbox semantics.

### D4. Deterministic hook order with first-block-wins

Hooks run sequentially in a stable order: in-process hooks in registration order,
then external hooks in merged settings order for the same moment. The first block
stops the remaining hooks for that moment. Non-blocking verdicts compose in
execution order: before-tool argument edits chain, result replacements chain, and
injected context accumulates.

*Alternative considered:* run external hooks before SDK hooks. Rejected because
SDK hooks represent program-level invariants baked into the embedding
application, while settings hooks are operator-level policy layered above them.

### D5. Scopes are compiled and validated at session construction

A hook scope can name a tool, define a pattern over the moment's relevant detail,
both, or neither. Pattern scopes are compiled during session construction so bad
patterns fail loudly before the first run. When both tool and pattern scopes are
present, both must match. Unscoped hooks fire for every event at their moment.

*Alternative considered:* compile patterns lazily when a hook first fires.
Rejected because malformed definitions should be caught early, not halfway
through a task.

### D6. External hooks use stdin JSON and optional stdout JSON

The harness invokes the configured command with a per-hook timeout and sends the
full hook event as JSON on stdin. Exit status is the default verdict:
zero allows, non-zero blocks. If stdout contains a structured verdict within the
output budget, that verdict overrides the default and can include a reason,
argument replacement, result replacement, or injected context. Malformed or
oversized output fails closed.

*Alternative considered:* pass event fields as command-line arguments. Rejected
because prompt text, tool arguments, and results may be large or structured.

### D7. Hook outcomes surface as normal run events

Add an event payload for hook outcomes and emit it on the existing stream for
blocks, replacements/redactions, and injections. A blocked tool still also
returns a `ToolResult{IsError:true}` so the model can react. Frontends consume
the event stream; they never inspect hook engine state.

*Alternative considered:* only encode hook outcomes inside tool results.
Rejected because prompt-submit/session hooks do not produce tool results, and
frontends need a uniform observable signal.

### D8. Recover from panics and kill process groups on timeout/cancellation

In-process hook panics are recovered and treated as blocks. External hook
commands run with `exec.CommandContext`; on timeout/cancellation the engine kills
the process group, drains bounded output, and returns a blocked verdict. This
matches the shell tool's no-orphan expectation and keeps tests deterministic.

*Alternative considered:* let hook panics bubble as unrecoverable session errors.
Rejected because one bad guardrail should fail closed without crashing the
harness.

## Risks / Trade-offs

- External hook JSON shape may become a public contract quickly -> keep it small,
  documented in tests, and versionable later if needed.
- Session-start injection needs a clear home in conversation history -> model it
  as harness-provided standing context before the first provider call, not as a
  user-authored prompt.
- Settings merge for hook arrays can surprise users -> define replacement or
  append semantics explicitly in the configuration spec and tests.
- Killing child processes is Unix/macOS-specific -> isolate process-group logic
  in the external hook runner, matching current v1 platform assumptions.
- Hook outcome events add public surface -> keep payload focused on moment,
  hook name, action taken, and reason; do not expose internal engine details.

## Migration Plan

1. Add hook domain types and public aliases without removing existing names.
2. Implement the hook engine behind the existing hard pre/post stage interfaces.
3. Extend configuration loading with external hook declarations and validation.
4. Wire prompt/run/session lifecycle hooks into session construction and the run
   lifecycle.
5. Add event payloads and tests proving frontends can observe hook outcomes.
6. Replace inert hard-check defaults with a no-hooks engine that allows when no
   hooks are configured.

Rollback is straightforward during development: keep the engine injectable and
the no-hooks implementation equivalent to the Phase 2 pass-through behavior.

## Open Questions

- Exact field names for external hook JSON input/output should be finalized when
  coding against the current `core` structs.
- Whether project hook arrays replace or append home hook arrays; lean replace
  per field-level project-overrides-home semantics.
- Whether session-stop is exposed through an explicit `Close`/`Stop` API or run
  completion only; implementation should follow the current session lifecycle in
  hand.
