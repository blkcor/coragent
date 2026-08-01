# Coragent V2 Agent Guidelines

Coragent V2 is a clean-room rebuild of the Coragent coding agent. It is a
product-first terminal agent written in Go. The V2 runtime does not preserve the
V1 public API, wire format, package layout, or session data format.

The current branch contains the V2 source of truth. The `master` branch is
historical evidence only. Do not copy V1 runtime code into V2 without documenting
the behavior being preserved and why a new implementation is not safer or
simpler.

## Read before changing the project

Read these files in order:

1. `docs/product.md`
2. `docs/architecture.md`
3. `docs/roadmap.md`
4. `docs/benchmarks.md`

Product behavior and benchmark evidence override architectural preference. If a
design makes the code cleaner but lowers real task success, the design loses.

## Approved direction

These decisions are settled unless the maintainer explicitly reopens them:

- Product behavior comes before SDK design.
- The public V2 SDK is frozen only in M5, after the runtime passes the product
  benchmark.
- V2 is not source-compatible or wire-compatible with V1.
- The first frontend is a small line-oriented CLI. A focused TUI arrives after
  the change loop and long-task context work.
- A single agent must meet the quality gate before subagents, teams, MCP, or
  parallel tools enter scope.
- Go remains the only implementation language unless the maintainer approves a
  second runtime.

## Product invariants

1. The durable transcript, the context sent to the model, and frontend events
   are different data products. Compaction may change model context but never
   rewrites the transcript.
2. SessionCommands change session state. Events report serializable facts.
   Events never contain channels, callbacks, closures, runtime credentials, or
   internal pointers.
3. Every proposed tool call receives exactly one tool result, including calls
   skipped because of cancellation, steering, denial, or an earlier failure.
4. Every action uses one Action Broker path. Built-in tools, future external
   tools, and delegated tools do not get alternate execution routes.
5. File operations and command execution obey the same workspace policy. A file
   tool is not trusted merely because it does not launch a process.
6. The session Authority Envelope is the immutable maximum authority. Effective
   Policy and Permission may narrow or activate a subset of it, but an approval
   cannot widen it.
7. A displayed patch is an identity-bound prepared action. If its source files
   change before commit, execution fails closed and prepares a new patch.
8. Cancellation propagates through the provider stream, tool execution, child
   process group, and any future child agent.
9. The loop continues when the model produced actual tool calls. Provider finish
   metadata is useful evidence but is not the only continuation signal.
10. Prompt content is assembled from current runtime facts. Do not maintain one
    growing hardcoded system string.
11. Recovery actions are classified, bounded, durably budgeted, and recorded.
    Blind or unlimited retry loops are forbidden.
12. A frontend renders runtime facts and sends SessionCommands. It does not own
    agent state or reconstruct hidden control logic.
13. Every side effect has a durable Action Attempt written before execution.
    Crash recovery reconciles an unfinished attempt and never repeats it
    automatically.
14. A frontend snapshot and live Event subscription use one atomic observation
    operation and a session-wide cursor.
15. Runtime secrets never enter tools, processes, Transcript, Model Context,
    Events, logs, or artifacts. Protected-path and detected secret content uses
    the redacted projections in `docs/architecture.md`.

## Scope discipline

The initial V2 roadmap excludes:

- V1 compatibility adapters
- a stable public SDK before M5
- multiple provider families
- subagents, persistent teammates, and parallel tools
- MCP and plugin systems
- external command hooks and a general lifecycle hook framework
- durable remembered permission rules and multiple permission modes
- a full-screen TUI before M4
- process actions on platforms without an enforcing sandbox backend

Do not add an excluded capability because a seam makes it easy. Reopen scope only
with benchmark evidence and an updated product document.

## Execution model

- One active run per session.
- Tool calls execute sequentially through M4.
- Steering, available from M4, is queued and applied at the next safe boundary.
- Cancellation is separate from steering and interrupts active work.
- Read-only actions inside the workspace do not require approval.
- Workspace mutations and commands require approval before their first side
  effect.
- Optional outside-workspace roots or network endpoints must be present in the
  Authority Envelope when the session starts. A per-call grant can activate only
  a subset already in that envelope.
- Provider transport may reach only the endpoint fixed in session configuration.
  Its credential and connectivity never become tool or process authority.
- Initial GA supports process actions only on macOS with an OS-enforced sandbox.
  Other platforms disable the command tool before preparation or approval.
- The process runner builds a minimal environment. It never forwards the full
  host environment or ambient credential variables.

## Planned repository boundaries

The architecture defines responsibilities before packages. When implementation
starts, keep these dependencies one-way:

```text
frontends -> engine -> context/provider/action -> store and platform adapters
```

The engine must not import a frontend. Tools must not bypass the Action Broker.
Provider adapters must not emit frontend events directly.

Until M5, new runtime packages belong under `internal/`. Do not create a public
facade to make an intermediate milestone look complete.

## Go conventions

- Go 1.22 or newer.
- Every blocking operation takes `context.Context` first.
- Wrap causal errors with `%w`.
- Tool failures become model-visible results. Programmer errors and corrupted
  durable state stop the run.
- Use `log/slog` for diagnostics. Logs never enter the transcript or event
  stream.
- Use concrete names from the product vocabulary: Session, SessionCommand,
  Event, Transcript, Context, ToolCall, ToolResult, PreparedAction, Provider,
  ActionAttempt, RunBudget, and ActionBroker.
- Keep provider-specific wire types inside the provider adapter.
- Never infer a context window from a model name. Obtain it from provider
  capability data or explicit configuration.

Preferred commands:

| Use | Avoid |
| --- | --- |
| `rg` | `grep` |
| `fd` | `find` |
| `eza` | `ls` |
| `sd` | `sed` |
| `golangci-lint` | hand-written lint substitutes |

## Testing

- All state-machine, provider, context, action, permission, and persistence
  behavior must be testable offline.
- Use a scripted fake provider. Unit tests never call a real model.
- Use `t.TempDir()` for repository, session, blob, and tool fixtures.
- Run race tests on cancellation, approval, steering, and process cleanup paths.
- Test invariants, not only examples. The suite must prove tool-call pairing,
  no unapproved side effects, transcript immutability under compaction, monotonic
  event ordering, and cancellation without orphan work.
- Inject crashes before and after every Action Attempt, side effect, ToolResult
  transaction, budget reservation, and observation cursor checkpoint.
- Race atomic snapshot subscription against approval, tool completion, and run
  termination. Prove reconnect sees neither missing nor duplicated state.
- Persist Run Budget counters and prove restart cannot reset recovery, token,
  tool-call, or active-time limits.
- Test runtime credentials, protected files, user-prompt matches, streamed model
  output, tool output, prepared patches, logs, and blobs against the versioned
  secret corpus. Test redaction before every projection boundary.
- Each milestone runs the unlocked subset of `docs/benchmarks.md`. M2 and later
  also run the full 12-task suite.

Baseline verification commands once code exists:

```sh
gofmt -w .
go test ./...
go test -race ./...
go build ./cmd/coragent
golangci-lint run ./...
```

## Milestone delivery

Each roadmap milestone must be runnable and useful if later work never lands.
Do not create an investigation-only milestone or a phase that becomes usable only
after the next phase.

Before implementation of a milestone:

1. Confirm the product behavior and benchmark cases it unlocks.
2. Design only the internal interfaces needed for that milestone.
3. List failure and cancellation paths.
4. Define the rollback boundary.

After implementation:

1. Run offline tests and the milestone benchmark subset.
2. Record evidence, including failures.
3. Update architecture or product docs when behavior changed.
4. Do not advance when a safety gate fails.

The V1 rule that later phases cannot change earlier public contracts does not
apply to V2 internals. Before M5, internal contracts may change when benchmark
evidence justifies the cost. After M5, the published V2 SDK follows semantic
versioning.

## Local and durable data

- Never commit `.coragent/`, credentials, transcripts, benchmark run artifacts,
  or generated model output.
- V2 uses the same settings paths as V1 (`~/.coragent/settings.json` and
  `.coragent/settings.json`). This is a clean-room rebuild; V1 installs are
  expected to be superseded.
- Durable session state lives under `~/.coragent/sessions/`.
- Tests must never read or modify real user state.
- Rollback to V1 must not require a data migration or deletion.
