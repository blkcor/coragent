# Subagents

Phase 6 adds a built-in `task` tool to sessions that use Coragent's default
executor. The tool delegates one focused instruction to an isolated child agent
and returns only that child's final answer.

## Task arguments

The model-facing arguments are:

- `label` — required, non-blank text shown in lifecycle status.
- `instruction` — required, non-blank task for the child.
- `tools` — optional array of ordinary tool names the child may use.

An omitted or empty `tools` array selects the fixed safe default:

- `read_file`
- `search_content`
- `find_files`

An explicit array is intersected with tools that are both advertised and
executable in the parent session. Unknown, hidden, or unavailable names are not
added. Custom read-classified tools are not part of the implicit default and must
be named explicitly.

The same rule applies at every nesting level. A grandchild derives its ordinary
tools from the immediate child's already-restricted set, so delegation can narrow
capabilities further but cannot restore a write, command, or read tool removed by
an earlier generation.

## Isolation and safety

Every child has a fresh conversation containing its focused system framing and
the delegated instruction. Parent user turns, assistant turns, and tool results
are never copied. Child-scoped session-start and prompt-submit hooks may still
inject their normal context.

The delegation call and every child tool call use the existing executor chain:

```text
before-tool hooks -> permission -> sandbox routing -> handler -> after-tool hooks
```

The `task` call itself is read-classified and does not run commands. A child
write, edit, or command receives its own classification, permission decision,
hooks, and sandbox handling. Plan mode can therefore allow delegation to begin
while still refusing a mutating child call.

## Results and events

The parent receives one ordinary tool result containing the child's final
assistant answer. Intermediate child text, tool calls, tool results, ordinary
status, hook outcomes, warnings, errors, and terminal events stay private. The
normal executor output budget truncates an oversized final answer; an explicitly
empty final answer remains a successful empty result.

The outward stream exposes only:

- `subagent_started` and `subagent_finished` status with the task label;
- nested child/grandchild lifecycle status;
- permission requests that require a human reply.

Lifecycle status uses the existing `RunEvent.ToolCall` field. Its `label`
argument identifies the delegated work. If cancellation has already made the
parent stream unwritable, the cancelled run outcome takes precedence over forcing
a final status event.

Child provider failure, step-limit exhaustion, missing final output, and child
cleanup failure become recoverable task errors so the parent model can react.
This includes a provider stream that closes without an explicit reply-ended
event; it is distinct from an explicitly completed empty answer. Reply-ended is
well formed only when it carries one of the defined ending reasons, and it is
terminal: if a custom provider sends a malformed reply-ended event or emits text,
a tool call, or another non-error event after it, the child fails recoverably and
none of the trailing events are emitted, recorded, or dispatched. A non-nil
provider error still remains the reported cause if its event arrives after
reply-ended. The channel is always drained through closure to avoid stranding
provider work.
Cancelling the parent is different: it propagates through every descendant model
stream, tool, and command, then ends the parent run with `StopCancelled` and
leaves no orphan work.

## Recursion and execution order

`task` is not grantable through the requested ordinary `tools` array. The
orchestrator installs it independently in every child and allows at most three
delegation edges from the root. A call beyond that limit returns a recoverable
depth-limit result without starting another child.

Delegations follow the existing sequential tool-call rule. When one model round
requests several task calls, each child finishes and returns its result before
the next child starts.

## SDK compatibility boundaries

Default session construction installs the standard `task` handler only when the
caller does not already provide a custom handler with that name. An existing
caller-owned `task` remains authoritative and is neither replaced nor treated as
a duplicate-registration error.

`SessionConfig.Tools` keeps its existing semantics:

- `nil` derives the advertised descriptors from the default catalog, including
  the standard task when installed;
- a non-nil list, including an empty list, is authoritative for advertisement.

A caller-supplied `Dispatcher` remains fully caller-owned. Coragent does not
modify it or automatically install subagent orchestration in that path.
