## Context

The current `Session` owns a conversation manager, provider, advertised tool
descriptors, and one `Dispatcher`. Default construction builds a catalog from the
six built-ins plus custom handlers, then creates the ordered executor with hooks,
permission, and the Phase 5 sandbox. `loop.Run` already accepts all of these as
dependencies, propagates its context through provider and dispatch calls, and
executes multiple calls sequentially.

Phase 6 must recursively reuse those pieces without copying the parent
conversation or leaking child events. The empty `internal/subagent` package is
the intended orchestration boundary. The `task` capability must remain an
ordinary tool registered in the catalog so its own invocation and every tool the
child later invokes still pass through the one executor path.

There is one wording tension in PRD-06: it says the delegation capability is
withheld from a child's requested toolset while also requiring nested delegation
up to a depth limit. This design interprets that as: callers cannot grant `task`
through the requested ordinary-tool list; the orchestrator installs a
depth-aware `task` handler independently in each child. That preserves capability
narrowing and makes the explicit recursion limit observable.

## Goals / Non-Goals

**Goals:**

- Register a validated `task` tool on default sessions and execute it through the
  existing hooks → permission → sandbox decision → handler → post-hooks chain.
- Run a child over the existing gather → act → verify loop with a fresh
  conversation and a restricted executable catalog.
- Reuse the parent's effective provider, model options, limits, hooks,
  permission behavior, sandbox, and custom handlers without copying its history.
- Return only the final assistant answer as the task tool result and rely on the
  executor's existing central output budget for trimming.
- Forward labeled start/finish status and permission requests while suppressing
  every other child event.
- Enforce a fixed maximum depth and propagate one context cancellation through
  the entire descendant tree.

**Non-Goals:**

- No parallel fan-out; task calls remain sequential like every other tool call.
- No named profiles, shared child memory, configurable depth/result limits, or
  child-output summarization.
- No child-specific permission mode, hook set, sandbox policy, or settings keys.
- No child streaming into the parent model or transcript.
- No automatic modification of caller-supplied custom dispatchers, which retain
  ownership of their advertised and executable tools.
- No divergent child loop and no frontend import from harness packages.

## Decisions

### Decision: Implement delegation as an ordinary event-aware tool handler

Default session composition will register a standard `task` handler after the
existing built-ins and custom handlers when the caller does not already own that
name. The executor will recognize an internal optional handler shape that
accepts the same live `emit` callback already supplied to `Dispatch`.
The executor still resolves, validates, runs pre-hooks and permission, executes
the handler, runs post-hooks, and truncates its output in the existing order.
Only the handler invocation at the execution slot is event-aware.

This internal optional shape does not add a required method to `ToolHandler`, so
existing public custom handlers remain source-compatible. The task handler does
not run OS commands and is classified as a read-only orchestration action; any
mutating child tool is independently classified and gated inside the child.

The task handler and orchestration live in `internal/subagent`; that package may
depend on `internal/loop`, `internal/executor`, `internal/tools`,
`internal/context`, and `internal/core`. `internal/tools` MUST NOT import
`internal/subagent`, and `internal/subagent` MUST NOT import `pkg/agent`. The
composition root in `pkg/agent` installs the handler, preserving an acyclic
dependency direction.

Alternatives considered:

- Intercept `task` in the loop or wrap the dispatcher before the executor.
  Rejected because either creates a second execution door that can bypass hooks
  or permission.
- Store a global event sink on the task handler. Rejected because it is unsafe
  across sessions and obscures cancellation/backpressure ownership.
- Change the required `ToolHandler.Execute` signature. Rejected because prior
  phase public contracts must remain intact.

### Decision: Preserve caller ownership of the `task` name

Adding a standard tool name must not turn a previously valid custom handler into
a duplicate-registration panic. Default construction first registers the six
existing built-ins and caller handlers. If a caller handler named `task` exists,
it remains the executable owner and the standard subagent handler is not
installed. Otherwise the standard handler is registered. An explicit
`SessionConfig.Tools` list continues to control what is advertised, including
the existing distinction between `nil` (derive descriptors from the catalog) and
a non-nil empty slice (advertise none).

A caller-supplied `Dispatcher` remains wholly caller-owned: default catalog,
executor, and subagent wiring are all skipped, exactly as before. This preserves
prior SDK configurations while making the standard capability the default when
the default composition can install it safely.

Alternatives considered:

- Reserve `task` unconditionally and panic or fail startup on a caller handler
  with that name. Rejected because the name was valid before Phase 6 and the
  phase must not invalidate prior public configurations.
- Silently replace a caller's `task` handler. Rejected because it changes
  executable behavior and weakens explicit SDK ownership.

### Decision: Share one internal session runtime between roots and children

The session-scoped composition currently embedded in `pkg/agent.Session` will be
factored into a frontend-agnostic internal runtime that owns a conversation and
drives session-start, prompt-submit, `loop.Run`, run-finished, and session-stop
lifecycle points. `pkg/agent.Session` keeps the public concurrency guard, event
channel, and SDK methods while delegating the run mechanics to that runtime. The
subagent orchestrator constructs the same runtime with child dependencies and
drains it internally.

This gives roots and children one lifecycle implementation without letting an
internal package import `pkg/agent` and without copying `Session.Run` into the
orchestrator.

Alternatives considered:

- Duplicate the session wrapper inside `internal/subagent`. Rejected because
  lifecycle ordering, cancellation, and future loop fixes would drift.
- Import `pkg/agent` from `internal/subagent` and recursively call `NewSession`.
  Rejected because `pkg/agent` is the composition root and already imports the
  orchestrator, creating an import cycle as well as losing resolved handlers.

### Decision: Build children from an immutable runtime blueprint

Default session construction will retain an internal blueprint containing the
effective provider, stream options, max-round/context limits, lifecycle hooks,
executor safety collaborators, and the parent's effective tool set. That tool
set pairs each exact descriptor actually advertised by the parent with the
registered handler of the same name, in parent advertisement order. Names that
are only advertised or only executable are excluded, so a built-in hidden by an
SDK caller's explicit descriptor list cannot reappear in a child and a custom
descriptor is not regenerated from its handler. The subagent orchestrator uses
the blueprint to create a fresh conversation manager, a restricted catalog, and
a new executor, then invokes the same session-run composition around `loop.Run`
used by the parent.

An explicit parent descriptor list can contain the same name more than once even
though the executable catalog cannot. The blueprint keeps the first advertised
descriptor for that name and ignores later duplicates for child derivation only;
the root session's existing advertised list remains unchanged.

The child receives a fixed focused-subagent system framing plus the delegated
instruction as its sole user turn. No parent turn, tool result, or assistant text
is copied. Child-scoped session-start and prompt-submit hooks may add their normal
injected context, but that content is produced by the child's own lifecycle and
is not inherited parent history. Run-finished and session-stop hooks also run for
the child, while before/after-tool hooks, permission, and sandbox behavior are
inherited in the child executor. Their resulting raw hook events stay on the
child stream unless they are human permission requests.

A child session-start or prompt-submit block prevents any provider call and
becomes a recoverable task error. A run-finished block retains the already chosen
child run outcome, matching the root contract. The orchestrator always performs
session-stop cleanup; if that cleanup blocks after an otherwise successful child,
the task returns a recoverable cleanup error so the hard outcome is not hidden.
If the child already failed, its primary error remains primary and the bounded
cleanup reason is attached as context.

For nested delegation, the newly installed depth-aware `task` handler retains a
new blueprint whose catalog and advertised descriptors are the current child's
already-restricted ordinary set. It inherits the same provider and safety
collaborators, but never points back to the root capability set. Each generation
can therefore narrow capabilities further without recovering an ancestor tool.

Alternatives considered:

- Call public `NewSession` recursively with a copied `SessionConfig`. Rejected
  because it loses the already-resolved executable catalog and risks configuration
  drift between parent and child construction.
- Clone the parent conversation and trim it. Rejected because even a trimmed
  copy violates the isolation contract and spends child context on irrelevant
  history.
- Implement a smaller bespoke child loop. Rejected because cancellation, tool
  continuation, stop reasons, and future loop fixes would diverge.

### Decision: Derive a restricted executable catalog in parent advertisement order

The catalog will support an internal restricted view that preserves handler
identity, the exact effective parent descriptor, and parent advertisement order.
For an explicit non-empty tool list, the view contains only requested names that
exist in the parent's effective ordinary tool set. Unknown or parent-hidden
names are not advertised or registered. For an omitted or empty list, the view
contains only `read_file`, `search_content`, and `find_files` when those handlers
are effective for the parent.

The restriction applies to executable handlers as well as descriptors. A model
that nevertheless emits an out-of-set name therefore reaches the child executor
and receives its existing recoverable unknown-tool result; the disallowed handler
cannot run.

Alternatives considered:

- Filter only the descriptors sent to the model. Rejected because a hallucinated
  or injected call could still resolve to a hidden executable handler.
- Build a separate allow-list check outside the catalog. Rejected because the
  catalog already owns name-to-handler resolution and can enforce both discovery
  and execution with one deterministic view.
- Treat an explicit list containing unknown names as a fatal task error. Rejected
  because the PRD specifies intersection semantics and clean refusal at call time.

### Decision: Keep recursion capability separate from requested tools

`task` is never accepted from the delegation's requested ordinary-tool list and
is never part of the read-only default. Instead, each child catalog receives a
new depth-aware task handler from the orchestrator. The root depth is zero, each
successful spawn increments it, and a private v1 maximum of three delegation
edges is enforced before constructing a child.

At the maximum depth the handler remains resolvable so an attempted delegation
returns a specific recoverable depth-limit result and starts no child. Requested
tool names therefore cannot widen recursion authority, while nesting inside the
fixed budget remains possible.

Alternatives considered:

- Remove `task` from every child. Rejected because that makes the PRD's nested
  depth and grandchild cancellation requirements impossible to exercise.
- Remove `task` only at the limit. Rejected because a model call would produce a
  generic unknown-tool result instead of the required depth-limit reason.
- Let callers request `task` explicitly. Rejected because a tool subset must not
  grant control-plane capability and recursion is governed only by depth.

### Decision: Drain child events and return the final conversation answer

The orchestrator drains the child run stream to completion so provider and tool
goroutines cannot be stranded. It does not concatenate `TextDelta` events,
because those include text from intermediate rounds. On a completed run it reads
the final assistant turn from the child's private conversation and returns that
content from the task handler. The outer executor then applies the same central
30,000-byte default budget and truncation marker as every other tool result.

An empty final answer is a successful empty result. Step-limit and failed child
outcomes become short handler errors, which the outer executor converts to
recoverable error results. Cancellation uses the existing context precedence and
ends the surrounding parent run promptly with `StopCancelled`, without recording
a recoverable task result or inventing a second terminal outcome.

The shared loop distinguishes an explicit reply-ended event with empty content
from a provider channel that closes without completing a reply. Only the former
creates a successful empty assistant turn; a silent close is a provider failure
and therefore a recoverable task error.

The reply-ended event is well formed only when it carries a non-nil payload whose
reason is `Finished`, `StoppedToCallTools`, or `CutOff`, and it is the unique final
event for a successful provider reply. The loop still drains the provider channel
through closure so a producer cannot be stranded, but a malformed reply-ended
event or any second terminal event, text, tool call, or other non-error event after
reply-ended is a protocol failure. Trailing content is not emitted or recorded and
trailing tool calls are never dispatched. Cancellation or an outward send failure
still wins over provider failure, and a non-nil provider failure still wins over
this protocol failure even when its error event arrives after reply-ended.

Alternatives considered:

- Concatenate child text deltas. Rejected because it would mix intermediate
  reasoning with the final answer.
- Add a subagent-only truncation algorithm. Rejected because central executor
  truncation already supplies one consistent bounded-result contract.
- Insert the child answer directly into the parent context manager. Rejected
  because the parent loop already records one tool result for the originating
  task call; direct insertion would duplicate or bypass normal conversation flow.

### Decision: Filter events by type at the orchestrator boundary

Immediately before starting the child, the task handler emits a status-change
event with `subagent_started` and the label. It emits `subagent_finished` with the
same label when the child reaches a completed or failed terminal path while the
parent stream remains writable. A depth refusal occurs before child start and
emits neither lifecycle status. Parent cancellation remains authoritative: the
handler attempts the finished status but MUST NOT delay cancellation to force an
ordinary event onto a stream that can no longer accept it.

To preserve the prior public struct shape, the two status events use stable new
status values and carry a minimal copy of the originating task call in the
existing `RunEvent.ToolCall` field; its `label` argument is the frontend label.
No `RunEvent` field or new public payload type is added.

While draining the child stream, the orchestrator forwards
`PermissionRequestedEvent` and nested `subagent_started` / `subagent_finished`
status through the parent's live `emit` callback. Permission forwarding preserves
the child's reply path so the frontend can answer it, and lifecycle forwarding
keeps grandchildren visible without exposing their raw work. Text deltas, child
tool lifecycle, hook outcomes, warnings, ordinary statuses, errors, and the
child's terminal event remain private. If forwarding fails, the derived child
context is cancelled through its cancel function and normal cancellation
propagation takes over.

Alternatives considered:

- Merge the full child stream into the parent stream. Rejected because it breaks
  context/UI isolation and interleaves nested work with the parent.
- Suppress permission requests with all other child events. Rejected because a
  child could deadlock waiting for a human who was never prompted.
- Log child activity instead of typing it. Rejected because logs are not the
  frontend event stream and cannot carry a permission reply path.

### Decision: Derive every child context from the parent call context

For each accepted delegation, the orchestrator creates
`childCtx, childCancel := context.WithCancel(parentCallCtx)`. That derived context
flows into the child loop, provider stream, child dispatch calls, sandbox runner,
and any nested task handler. Parent cancellation therefore reaches the entire
tree; if forwarding a child permission request fails, the orchestrator can call
`childCancel` without attempting to cancel its caller. No background-rooted
context or detached goroutine is introduced. The orchestrator always cancels the
derived context on return and drains until the child channel closes, so no
orphan work remains.

Alternatives considered:

- Create a fresh background context per child. Rejected because parent cancel
  would not reach descendants.
- Return immediately and collect the child asynchronously. Rejected because v1
  requires sequential tool execution and a single result before the parent loop
  continues.

## Risks / Trade-offs

- [Risk] Reusing mutable permission and hook collaborators across nested sessions
  could expose concurrency assumptions. → Mitigation: v1 runs task calls and all
  child tools sequentially; add race-focused tests and document shared session
  policy state.
- [Risk] An event-aware optional handler path could accidentally skip executor
  stages. → Mitigation: branch only at the final handler invocation and assert
  full pre/permission/post ordering in executor tests.
- [Risk] A child permission request can block while all child text is hidden. →
  Mitigation: forward the typed request and reply path unchanged and keep the
  labeled started status visible until completion.
- [Risk] Custom handlers may be safe to advertise but unsafe as read-only
  defaults. → Mitigation: the default is a fixed allow-list of the three known
  read/search built-ins; custom tools require explicit selection.
- [Risk] The fixed depth of three may be too shallow or expensive for some uses.
  → Mitigation: keep it private and well-tested in v1; configuration is an
  explicitly deferred additive change.
- [Risk] A child final turn may be missing or followed by malformed trailing
  provider events on unusual terminal paths. → Mitigation: require one final
  reply-ended event, reject anything after it while draining the channel, and
  accept empty content only when that explicit completion is well formed.
- [Risk] A caller may already own a custom handler named `task`. → Mitigation:
  preserve that handler and skip only the standard registration, with regression
  tests for both implicit and explicit advertised descriptor lists.
- [Risk] A cancelled parent stream may reject the matching subagent-finished
  status. → Mitigation: make the cancelled run outcome authoritative and never
  delay tree cancellation to force a non-terminal status event.

## Migration Plan

1. Extract the shared internal session runtime and immutable child blueprint
   inputs without changing the public `Session` behavior.
2. Add the event-aware handler invocation at the existing executor execution
   slot and add labeled lifecycle status values using existing `RunEvent` fields.
3. Add deterministic catalog restriction while preserving existing registration,
   lookup, duplicate, and advertisement behavior.
4. Implement the `task` descriptor, argument parsing, depth-aware handler, and
   subagent orchestrator over the existing loop.
5. Register the standard handler only when the caller does not own `task`; leave
   explicit tool advertisement and caller-supplied dispatchers unchanged.
6. Add offline fake-provider/frontend tests for isolation, filtering, result
   shaping, safety inheritance, depth, permission routing, and cancellation.
7. Document the task arguments, safe default toolset, event visibility, depth
   limit, and custom-dispatcher ownership boundary.

Rollback during implementation is to stop registering `task` in the default
catalog; the prior six built-ins and executor path remain unchanged. No settings
or persisted data migration is required.

## Open Questions

- Should a later phase expose the fixed depth and result limits through the
  existing settings file?
- Should custom-dispatcher SDK users receive an additive public helper for
  installing the standard task capability, or continue to own delegation
  dispatch entirely?
- Should a future additive event API replace the v1 `ToolCall`-carried status
  label when the TUI needs richer progress such as depth or elapsed time?
