# Design — Phase 2 Tools & Executor

## Context

Phase 1 shipped the `core.Dispatcher` seam — `Dispatch(ctx, ToolCall, emit
func(RunEvent) error) (ToolResult, error)` — and an inert `executor.StandIn` that
fills it, returning a placeholder result. The loop calls this seam once per tool
call; everything else (recoverable vs unrecoverable failure handling,
cancellation precedence, the live `emit` stream carrying a future permission
prompt) is already wired and tested. Phase 2 replaces `StandIn` with the real
chain **behind the same interface**, so the loop is untouched.

Architecture invariants bind this phase: there is exactly **one** tool dispatch
path (no second door); the harness never imports a frontend; `pkg/agent` is the
only public contract and additions are append-only; Provider, Sandbox, and Tool
are seams with first — not only — implementations.

Domain types live in `internal/core` and are re-exported by `pkg/agent` as
aliases. New public names this phase needs (tool registration shape, the
"runs-commands" marker) are added there and re-exported.

## Goals / Non-Goals

**Goals:**
- One ordered chain behind `Dispatcher`: hard pre → permission → sandbox →
  execute → hard post, with the order externally observable and asserted.
- The three unbuilt stages ship as inert pass-through placeholders, each a drop-in
  a later phase replaces without touching the path.
- Sandbox stage routes for command-execution tools only; file ops take the direct
  path.
- Six built-in tools working on a real tree, each failure mode a clean error
  result.
- A catalog: register by name, advertise exactly the set in stable order, reject
  duplicates at wire-up.
- Central output truncation on a clean rune boundary with a legible marker.
- Unknown-tool and argument-shape short-circuits before any stage runs.
- Everything offline-testable against `t.TempDir()` and fakes.

**Non-Goals:**
- Real permission behavior (Phase 3), real hooks (Phase 4), real sandbox
  confinement (Phase 5) — only the inert placeholders here.
- Parallel tool execution — calls run one at a time (architecture §7).
- Crash-atomic write-then-swap — mutations happen in place; deferred.
- The task/subagent tool (Phase 6) — the catalog merely accepts runtime
  registration so it slots in later.
- Rich TUI rendering of tool activity/diffs (Phase 7) — results are plain text.

## Decisions

### D1. The chain is a fixed ordered pipeline of stage functions

The executor composes the call through stages in one fixed order: **resolve +
validate → hard pre-checks → permission → sandbox → execute → hard post-checks →
truncate**. Each stage is a function over a shared per-call context value (the
resolved tool, the live arguments, the `emit` callback). A stage either passes the
call forward, or short-circuits to a terminal `ToolResult`. The order is encoded
in one place (the chain assembly), making it observable: tests inject recording
stand-ins and assert the visit sequence.

*Alternative considered:* a generic middleware list each tool opts into — rejected.
The PRD's invariant is one *fixed* order shared by all calls; a per-tool list
reintroduces the "forgotten call site" hole the chokepoint exists to prevent.

### D2. Stages are seams with inert Phase-2 implementations

`PreToolCheck`, `Permission`, `Sandbox`, `PostToolCheck` are interfaces. Phase 2
ships: `allowAllPermission` (allow, never edit), `neverBlockChecks` (pass), and
`directSandbox` (run the tool's command path directly). Each is a one-method
drop-in. Phase 3/4/5 supply real implementations injected at executor
construction — the chain assembly does not change.

*Alternative:* hard-code the inert behavior inline now and "extract an interface
later" — rejected: it would change the path's shape in a later phase, violating
the invariant that later phases only *fill* a slot.

### D3. Sandbox routing keyed off a tool capability marker

A tool declares whether it runs commands (the shell built-in does; pure file tools
do not). The chain consults this marker to decide whether to enter the sandbox
stage. The marker lives on the tool registration shape so custom command-running
tools opt in identically. File tools skip the stage entirely (not "enter a no-op
sandbox") so the observable order for a file op is genuinely pre → permission →
execute → post.

*Alternative:* route everything through the sandbox stage and let the stage no-op
for file tools — rejected: the PRD requires file ops to *skip* the stage
observably (US-015), and Phase 5's real sandbox must wrap command execution only.

### D4. Tool interface and registration shape

A built-in or custom tool exposes: a `Tool` descriptor (name, description, JSON
parameter schema — the Phase 0 `core.Tool` shape already advertised to the model),
an `Execute(ctx, args) (string, error)` behavior, and a `RunsCommands() bool`
marker for D3. The catalog stores these by name. Argument validation (D6) uses the
descriptor's JSON schema. `pkg/agent` re-exports the registration type so SDK
callers register custom tools against the public surface.

### D5. The catalog: stable order, duplicate rejection

The catalog keeps an insertion-ordered registry. `Advertise()` returns
`[]core.Tool` in registration order — deterministic across runs for the same
registration sequence (US-007). A duplicate name returns a registration error and
leaves the first tool intact (US-008); per architecture, this is a programmer
error surfaced at wire-up, candidate to `panic` only if it happens at static
startup registration. Built-ins register through the same path as custom tools.

### D6. Argument validation against the declared schema

Before the chain runs, the executor validates the call's arguments against the
tool's JSON parameter schema. An unregistered tool short-circuits to an
"unknown tool" error *before* validation or any stage (US-023, FR-9). A
shape mismatch short-circuits to a validation error before execution (FR-10).
After the permission stage returns edited arguments, the executor re-runs this
same validation on the edited args (FR-7) so a correction can never smuggle in a
bad shape.

### D7. Failures are `ToolResult{IsError:true}`, never errors-to-the-loop

Every Phase-2 failure mode — unknown tool, bad args, hard block, permission deny,
post-check veto, I/O error, non-zero exit, timeout, cancellation — is returned as
a `ToolResult` with `IsError:true` and `ToolCallID` set, with a `nil` Go error.
The loop's contract (Phase 1 D3) treats a non-nil error as *unrecoverable*
(StopFailed); Phase 2 reserves that channel for genuine harness breakage, so a
failing tool keeps the run alive (US-004). Cancellation is returned as an error
*result* (FR-21) rather than a Go error, so the loop's own cancellation precedence
governs the run outcome.

### D8. Central truncation on a rune boundary

Truncation is the final stage, applied uniformly to every tool's output string
(D1), so built-ins and custom tools are bounded identically (US-019, US-006). It
clips to the configured budget on a UTF-8 rune boundary (never mid-rune, keeping
the result valid text) and appends a machine-legible marker stating bytes/chars
elided (US-020). One global budget in Phase 2; per-tool budgets are a documented
future refinement.

### D9. Built-in tool implementations

- **read**: reads the file, returns `cat -n`-style line-referenced text; supports
  offset/limit window; detects directory/binary/missing/out-of-range and returns a
  clean error result with no raw bytes.
- **write**: writes whole file, optional `MkdirAll` for parents, concise
  confirmation (path + bytes), never echoes content.
- **edit**: counts occurrences of the target snippet; exactly-one → replace;
  zero → "not found" error unchanged; >1 without replace-all → ambiguous error
  with count, file unchanged; replace-all → replace every occurrence; target ==
  replacement → no-op error unchanged.
- **shell**: runs via the OS shell under the sandbox stage, captures combined
  stdout+stderr, returns output + exit code; non-zero → error result; enforces a
  time budget with `exec.CommandContext` + process-group kill so no orphan
  survives; cancellation stops the child.
- **content-search**: shells out to `rg` with `--vimgrep`-style `file:line:match`
  output, scopable by path/glob/case; exit-code-1 (no matches) → successful
  "no matches" result; `rg` absent → clear actionable error.
- **file-find**: walks the root matching a glob, skips noise dirs (`.git`,
  `node_modules`, …), returns paths in stable (sorted) order; no matches →
  successful empty result; bad root → error.

### D10. Replace `StandIn`, keep the loop untouched

`executor.New(catalog, stages...) core.Dispatcher` returns the real chain. Phase 1
wiring (`SessionConfig.Dispatcher` nil → default) switches its default from
`StandIn` to the real executor seeded with the built-ins and inert stages.
`StandIn` may remain for tests. No change to `core.Dispatcher`, the loop, or any
`pkg/agent` signature — only additive re-exports.

## Risks / Trade-offs

- **Shell process-group kill portability** → Phase 2 targets macOS/Unix
  (`setpgid` + kill the group); the kill strategy is isolated in the shell tool so
  a future OS backend swaps it without touching the chain.
- **`rg` dependency** → absence is detected and degraded to an actionable error
  (FR-18); tests skip rg-dependent assertions when the binary is missing, matching
  the architecture's graceful-degradation rule.
- **In-place mutations not crash-atomic** → accepted for Phase 2; write-then-swap
  is deferred and interacts with Phase 5's filesystem policy.
- **One global truncation budget** → may be too tight for reads or too loose for
  shell; per-tool budgets are an additive future change, no contract impact.
- **Binary-file detection heuristic** (NUL-byte scan) → may misclassify exotic
  text; acceptable, errs toward refusing a raw-byte dump.
- **Inert stages fixed now, armed later** → kept to one-method interfaces so
  Phases 3–5 fill behavior without changing the chain assembly.

## Open Questions

- Default truncation budget value — picked at implementation with surrounding code
  in hand; SDK callers override.
- Exact noise-directory ignore set for file-find — minimal default now, possible
  configurable follow-up (PRD §9).
- Whether the shell tool's time budget is per-call configurable or a single
  default in Phase 2 — lean single default, additive to extend.
- Whether `rg` should be optionally replaced by an in-process Go search when
  absent — deferred; Phase 2 degrades to an error.
