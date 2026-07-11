## Why

Phase 6 delivers roadmap milestone M4 by letting the agent delegate focused
investigations without consuming the parent conversation's context budget or
granting every child the parent's full toolset. The existing loop and executor
already provide the reusable runtime and safety chokepoint, so the missing piece
is isolated child-session orchestration with bounded recursion, result-only
return, and cancellation propagation.

## What Changes

- Add a `task` delegation tool that accepts a short label, a child instruction,
  and an optional list of allowed tool names.
- Add a subagent orchestrator that constructs a fresh child conversation over
  the same provider, loop, hooks, permission behavior, and sandbox-backed
  executor wiring as its parent.
- Restrict each child to the requested intersection of available tools, or to a
  fixed read-only default when no tools are requested.
- Return only the child's bounded final answer to the parent as the delegation's
  single tool result; keep child text, tool calls, and tool results out of the
  parent conversation and outward stream.
- Emit labeled subagent started/finished status while forwarding any child
  permission request to the parent's live frontend stream.
- Enforce a fixed recursion-depth ceiling and propagate cancellation through all
  descendants, including provider streams and running tools or commands.
- Surface child failure, unknown tools, and depth refusal as clear recoverable
  tool results so the calling loop can continue; parent cancellation instead
  terminates the whole descendant tree with the existing cancelled run outcome.

## Capabilities

### New Capabilities

- `subagents`: Delegation orchestration, fresh child context, capability
  narrowing, result-only return, depth limits, cancellation propagation, and
  status/permission event routing.

### Modified Capabilities

- `builtin-tools`: Add the model-facing `task` delegation tool and its validated
  label, instruction, and optional tool-list contract to the default toolset.
- `tool-catalog`: Support deterministic restricted child views derived from the
  parent's executable handlers, including the safe read-only default and
  rejection of unavailable requested tools.
- `tool-executor`: Allow the internal task handler to use the live event emitter
  at the existing execution slot without changing the required handler or
  dispatcher contracts or bypassing any executor stage.
- `agent-loop`: Extend the run event behavior with labeled subagent lifecycle
  status and child permission forwarding while preventing child execution noise
  from interleaving with the parent stream.

## Impact

- Affected packages: `internal/subagent`, `internal/tools`, `internal/executor`,
  `internal/loop`, `internal/core`, a reusable internal session runner, and
  `pkg/agent` session composition.
- Affected runtime behavior: default-derived sessions without a caller-owned
  `task` advertise one additional built-in tool; invoking it recursively runs
  the existing loop with an isolated context and a restricted executor/catalog
  view.
- Existing hook, permission, sandbox, provider, and executor contracts remain in
  force. No second tool-execution path and no frontend dependency are introduced.
- Existing caller-owned `task` handlers and custom dispatchers retain ownership;
  the standard task capability is installed only by compatible default-session
  composition.
- Affected tests: offline fake-provider and fake-frontend coverage for isolated
  context, tool filtering, result shaping, depth, cancellation, status routing,
  permission forwarding, and sequential execution.
