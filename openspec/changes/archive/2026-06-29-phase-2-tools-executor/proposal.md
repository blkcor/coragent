## Why

Phase 1 gave the agent a voice and a single dispatch seam (`core.Dispatcher`),
but that seam is filled by an inert stand-in — the agent can *ask* to use a tool,
yet nothing real happens. Phase 2 gives the agent hands: the one ordered
execution path every action travels through, and the six built-in tools that do
real work on a project. Building the path first — with its safety stages present
but inert — is what guarantees Phases 3–5 can arm those stages without ever
adding a bypass.

## What Changes

- Add the **single ordered execution chain** behind the existing `Dispatcher`
  seam: hard pre-checks → human permission → sandbox → execute → hard post-checks.
  The loop is untouched; the `StandIn` is replaced by the real executor.
- Ship the three not-yet-built stages (permission, hard checks, sandbox) as
  **inert pass-through placeholders** a later phase replaces without altering the
  path.
- Route the **sandbox stage to command-execution tools only**; pure file
  operations take the direct path.
- Add a **tool catalog**: register tools by name, advertise exactly the
  registered set in a stable order, reject duplicate names loudly at wire-up.
- Add **six built-in tools**: read file, write file, precise edit, run shell
  command, content search, file-pattern search.
- Add **central output truncation**: any tool output over the configured budget
  is clipped on a clean character boundary with a machine-legible elision marker.
- Surface **every failure as an error result** tied to its originating call —
  unknown tool, bad arguments, I/O error, denial, timeout, cancellation — never a
  crash.

No prior phase's public contract changes; the `Dispatcher` interface is fulfilled
as-is. All behavior is offline-testable against temp dirs and fakes.

## Capabilities

### New Capabilities
- `tool-executor`: the single ordered middleware chain behind the `Dispatcher`
  seam — fixed observable stage order, inert placeholder stages, the
  shell-only sandbox routing, short-circuit semantics (unknown tool, argument
  validation, hard block, permission deny, edited-args re-validation, post-check
  veto), error-results-not-crashes, cancellation, and central output truncation.
- `tool-catalog`: registration by name, advertising exactly the registered set in
  a stable cross-run order, and duplicate-name rejection at registration time.
- `builtin-tools`: the six built-in capabilities (read, write, edit, shell,
  content-search, file-find) and their per-tool behaviors and failure modes.

### Modified Capabilities
<!-- None. Phase 2 fulfils the Phase 1 Dispatcher seam as-is; all additions are new. -->

## Impact

- **New code:** `internal/executor` (real chain replacing `StandIn`, catalog,
  truncation), `internal/tools` (the six built-ins + registration helpers).
- **Additive edits:** `internal/core/types.go` (a `ToolSpec`/registration shape
  and a "runs commands" marker so the chain can route the sandbox stage);
  `pkg/agent` re-exports for any new public name a custom-tool author needs.
- **Reuses:** the Phase 1 `Dispatcher` seam, `ToolCall`/`ToolResult`/`Tool`
  shapes, and the live `emit` stream for the future permission prompt.
- **External dependency:** content search shells out to `rg` (ripgrep); its
  absence degrades to a clear error, never a crash.
- **No network in tests.** Every tool and the chain's ordering/short-circuits are
  covered offline against `t.TempDir()` and fakes.
