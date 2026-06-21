# Phase 2 — Tools & Executor (PRD)

> Implements node **2** of the dependency tree in [`roadmap.md`](../roadmap.md).
> Obeys [`architecture.md`](../architecture.md) as the canonical spec. Where this
> PRD and `architecture.md` disagree, `architecture.md` wins until amended.
>
> This is a **user-story PRD**: it describes *what* Phase 2 must do and *why*, in
> the voice of the people who depend on it. Concrete data shapes and interface
> signatures are deliberately absent — per `architecture.md` §10 they are designed
> in the planning step that immediately precedes implementation.

---

## 1. Introduction / Overview

Phase 1 gave the agent a voice: it can talk, take turns, and *ask* to use tools.
Phase 2 gives it hands. It delivers the part of the harness that **acts** — and,
just as importantly, the **single safe path** every action travels through.

Two things ship together, because neither is safe without the other:

1. **One execution path (the chokepoint).** Every tool the model invokes flows
   through exactly *one* ordered chain of stages — hard pre-checks, a
   human-in-the-loop gate, the sandbox, the tool itself, then hard post-checks.
   There is no second door. Building this path first — even with most stages
   still inert — is what guarantees that the safety layers arriving in later
   phases can never be bypassed.

2. **The built-in capabilities** the agent uses to do real work on a project:
   read a file, write a file, make a precise edit, run a shell command, search by
   content, and find files by name. Plus a way for SDK developers to register
   their *own* tools and trust they travel the exact same path.

Together with Phase 3 this forms Milestone **M2 — "It acts"** ([`roadmap.md`](../roadmap.md)):
the agent reads and edits files and runs commands, with human-in-the-loop
permission landing in [`prd-phase-3-permission.md`](prd-phase-3-permission.md).

**Why this is the right cut.** The execution path is the project's safety spine.
If each later phase bolted its own gate onto the agent separately, a forgotten
call site would become a hole. Instead Phase 2 lays down a single chain now, with
the human, hard, and sandbox stages present but pass-through, and proves the
ordering end-to-end. Phases 3, 4, and 5 then become pure "fill in this one stage"
work against a contract already nailed down.

**Personas.**

| Persona | Who they are | What they need from Phase 2 |
|---|---|---|
| **TUI end-user** | A developer using the coragent terminal app on their own project. | The agent to actually *do things* — open their files, change them precisely, run their tests, search their code — and to never quietly do something destructive behind their back. |
| **SDK developer** | An engineer embedding the harness in their own Go program, or extending it with bespoke tools. | To register a custom capability and trust it is gated, sandboxed, and truncated by the *same* chain as the built-ins — no special-casing, no second-class safety. |
| **Harness maintainer** (secondary) | A contributor building Phases 3–5 on top of this chain. | A single, well-ordered execution path with clearly marked slots to drop their stage into, without touching the path itself. |

---

## 2. Goals

- Every tool call — built-in or custom — passes through exactly **one** ordered
  execution path; no capability can reach the user's machine by another route.
- The path runs its stages in a single fixed order: **hard pre-checks → human
  permission → sandbox → execute → hard post-checks**, observable and asserted.
- The three not-yet-built stages (permission, hard checks, sandbox) ship as
  inert pass-through placeholders that a later phase replaces **without altering
  the path**.
- Six built-in tools work on a real project tree: read file, write file, precise
  edit, run shell command, content search, file-pattern search.
- The precise edit rejects an ambiguous match (more than one occurrence) and
  leaves the file byte-for-byte unchanged unless replace-all is explicitly opted
  into.
- Only the shell command routes through the sandbox stage; pure file operations
  take the direct path.
- SDK developers can register custom tools by name and have them treated
  identically to the built-ins.
- Any tool output exceeding the configured budget is truncated on a clean
  character boundary with a marker stating how much was elided.
- Every failure surfaces as an error *result* tied to its originating call —
  never a crash.
- All behavior is covered by offline tests against fakes — no network, no real
  model.

---

## 3. User Stories

One session per story. Behavioral acceptance criteria only — no concrete types.

### US-001: One door for every action

**Description:** As a TUI end-user, I want every single thing the agent does —
reading, writing, editing, running commands, searching — to pass through the same
safety checkpoint, so that no capability can ever sneak around the guardrails I
rely on.

**Acceptance Criteria:**
- [ ] Read, write, edit, content search, file-pattern search, and shell calls all
  flow through the one execution path; none has a private bypass route.
- [ ] Build, typecheck, and unit tests pass

### US-002: A guaranteed order of checks

**Description:** As a harness maintainer, I want the checkpoint to always run its
stages in one fixed order — hard pre-checks, then the human-permission gate, then
the sandbox, then the tool itself, then hard post-checks — so that later phases
can slot their real behavior into a known position.

**Acceptance Criteria:**
- [ ] For a command-running call, the stages run in exactly this order: hard
  pre-checks → human permission → sandbox → execute → hard post-checks.
- [ ] The order is observable and asserted in tests, not merely intended.
- [ ] For a file operation (read, write, edit, search, find) the sandbox stage is
  skipped and the order is hard pre-checks → permission → execute → post-checks.
- [ ] Build, typecheck, and unit tests pass

### US-003: Inert today, armed later, same path

**Description:** As a harness maintainer, I want the human, hard-gate, and sandbox
stages to exist from day one as pass-through placeholders, so that the path is
fully wired and testable now and later phases only have to replace a placeholder —
never re-route the path.

**Acceptance Criteria:**
- [ ] With the placeholders in place (allow-everything permission, never-block
  hard checks, run-directly sandbox), a read / edit / shell call run through the
  path produces the same correct result the tool would produce on its own.
- [ ] Each placeholder is a drop-in a later phase can replace without changing the
  path.
- [ ] Build, typecheck, and unit tests pass

### US-004: Failures come back as answers, not crashes

**Description:** As a TUI end-user, I want a tool that fails — a missing file, a
bad command, a denied action — to return a readable error result the agent can see
and react to, so that one bad step never takes down my whole session.

**Acceptance Criteria:**
- [ ] Any tool-, input-, or I/O-level failure anywhere on the path returns an
  error result the model can read, never a crash.
- [ ] Each result is tied back to its originating call so the model can match them.
- [ ] Build, typecheck, and unit tests pass

### US-005: A catalog of capabilities

**Description:** As an SDK developer, I want to register tools by name into one
catalog and have the agent be told about exactly those tools, so that the model
only ever offers to use capabilities I actually provided.

**Acceptance Criteria:**
- [ ] The list advertised to the agent contains one entry per registered tool and
  nothing else.
- [ ] Build, typecheck, and unit tests pass

### US-006: My own tools, same safety

**Description:** As an SDK developer, I want to register a custom tool and have it
travel the identical checkpoint as the built-ins — gated, sandboxed where
appropriate, truncated — so that I don't have to reimplement safety to extend the
agent.

**Acceptance Criteria:**
- [ ] A custom tool, when invoked, travels the same ordered path as a built-in,
  with no special-casing.
- [ ] A custom tool that declares it runs commands is routed through the sandbox
  stage like the shell tool.
- [ ] A custom tool's output is truncated by the same budget as the built-ins.
- [ ] Build, typecheck, and unit tests pass

### US-007: A stable, predictable tool list

**Description:** As an SDK developer, I want the advertised list of tools to be
presented in a stable order, so that runs are reproducible and the same prompt
doesn't shuffle from one invocation to the next.

**Acceptance Criteria:**
- [ ] The advertised tool order is identical across runs for the same set of
  registered tools.
- [ ] Build, typecheck, and unit tests pass

### US-008: Duplicate names are caught loudly

**Description:** As an SDK developer, I want registering two tools under the same
name to be rejected, so that I find the collision at wire-up time instead of
silently losing one of my tools at runtime.

**Acceptance Criteria:**
- [ ] Registering a second tool under an already-used name is rejected and the
  first tool is not lost.
- [ ] The rejection is surfaced at registration time, not silently at runtime.
- [ ] Build, typecheck, and unit tests pass

### US-009: Read a file, optionally a slice of it

**Description:** As a TUI end-user, I want the agent to read a file by path — and
optionally just a window of lines — so that it can understand my code before
changing it, and so that opening a large file doesn't flood the conversation.

**Acceptance Criteria:**
- [ ] Reading with no window returns the file's contents line-referenced so the
  model can cite lines.
- [ ] Reading with a line offset and limit returns only that window.
- [ ] A path that is missing, a directory, unreadable, binary, or an offset past
  the end of the file returns a clear error and no raw bytes are dumped.
- [ ] Build, typecheck, and unit tests pass

### US-010: Create or replace a file

**Description:** As a TUI end-user, I want the agent to write a whole file by path,
creating missing parent folders when asked, so that it can scaffold new code
without me hand-creating directories first.

**Acceptance Criteria:**
- [ ] Writing with parent-creation enabled creates missing folders and writes the
  file.
- [ ] Writing to an existing path replaces its contents.
- [ ] The result is a concise confirmation that does not echo the content back.
- [ ] Build, typecheck, and unit tests pass

### US-011: Surgical, unambiguous edits

**Description:** As a TUI end-user, I want the agent to edit a file by replacing an
exact snippet, and I want that snippet to be required to match exactly one spot
unless I explicitly allow replacing all, so that an edit can never silently land
in the wrong place.

**Acceptance Criteria:**
- [ ] A snippet that appears exactly once is replaced in place and the file is
  written.
- [ ] A snippet that appears more than once is rejected as ambiguous, reporting the
  match count, and the file is left byte-for-byte unchanged.
- [ ] When replace-all is explicitly opted into, every occurrence is replaced.
- [ ] Build, typecheck, and unit tests pass

### US-012: No wasted edits

**Description:** As a TUI end-user, I want an edit whose target and replacement are
identical, or whose target isn't present, to be rejected with a clear reason and
the file left untouched, so that the agent doesn't burn a turn pretending to
change nothing.

**Acceptance Criteria:**
- [ ] An edit whose target is not present in the file is rejected with a clear
  reason and the file is unchanged.
- [ ] An edit whose replacement is identical to its target is rejected with a clear
  reason and the file is unchanged.
- [ ] Build, typecheck, and unit tests pass

### US-013: Run commands, see everything

**Description:** As a TUI end-user, I want the agent to run a shell command and
return its combined output and exit code, so that it can build, test, and inspect
my project and show me what happened.

**Acceptance Criteria:**
- [ ] A command's combined output and exit code are returned.
- [ ] A non-zero exit is reported as an error result with the captured output, not
  a crash.
- [ ] Build, typecheck, and unit tests pass

### US-014: Commands can't hang forever

**Description:** As a TUI end-user, I want a command that runs too long to be
stopped and reported as timed out — with whatever output it produced — so that a
runaway process never freezes my session or leaves orphans behind.

**Acceptance Criteria:**
- [ ] A command that exceeds its time budget is killed and the result notes the
  timeout, including any partial output.
- [ ] When the surrounding work is cancelled, the call returns an error result
  noting the cancellation, any child process is stopped, and nothing is left
  leaking behind.
- [ ] Build, typecheck, and unit tests pass

### US-015: Shell is the one thing that gets confined

**Description:** As a harness maintainer, I want the shell command to be the single
built-in that routes through the sandbox stage, so that arbitrary command
execution is the thing the OS confinement (Phase 5) actually wraps, while pure
file operations take the direct path.

**Acceptance Criteria:**
- [ ] The shell command call passes through the sandbox stage.
- [ ] Read, write, edit, content search, and file-pattern search calls skip the
  sandbox stage.
- [ ] Build, typecheck, and unit tests pass

### US-016: Find text across the project

**Description:** As a TUI end-user, I want the agent to search file contents by
pattern, scoped by path and file glob, and get back located results, so that it
can locate code without me pointing at every file.

**Acceptance Criteria:**
- [ ] A search over a directory containing a known string returns results as
  `file:line:match` lines.
- [ ] Results can be scoped by path, by file glob, and by case sensitivity.
- [ ] When the underlying content-search binary is unavailable, the result is a
  clear, actionable error rather than a crash.
- [ ] Build, typecheck, and unit tests pass

### US-017: "Nothing found" is an answer, not an error

**Description:** As a TUI end-user, I want a search that matches nothing to come
back as a plain "no matches" result, so that the agent treats an empty search as a
normal finding and keeps going.

**Acceptance Criteria:**
- [ ] A content search that matches nothing returns a successful "no matches"
  result, not an error.
- [ ] Build, typecheck, and unit tests pass

### US-018: List files by name pattern

**Description:** As a TUI end-user, I want the agent to list file paths matching a
glob under a root, with noise like version-control internals skipped and results
in stable order, so that it can discover project structure cleanly.

**Acceptance Criteria:**
- [ ] Matching paths come back in stable order with common noise directories
  skipped.
- [ ] A glob matching nothing returns a successful "no files matched" result.
- [ ] A bad root returns an error.
- [ ] Build, typecheck, and unit tests pass

### US-019: Big outputs stay bounded

**Description:** As a TUI end-user, I want any tool's output that exceeds a budget
to be truncated with a clear note of how much was cut, so that one giant file or
chatty command doesn't blow up the conversation or my costs.

**Acceptance Criteria:**
- [ ] Output exceeding the configured budget is clipped to the budget and carries a
  marker stating how much was elided.
- [ ] Build, typecheck, and unit tests pass

### US-020: Truncation never corrupts text

**Description:** As an SDK developer, I want truncation to cut on a clean character
boundary and leave a machine-legible marker, so that the result handed to the
model is always valid text and the model knows it was shortened.

**Acceptance Criteria:**
- [ ] Truncation cuts on a clean character boundary and the truncated result
  remains valid text.
- [ ] The elision marker is machine-legible so the model can tell the result was
  shortened.
- [ ] Build, typecheck, and unit tests pass

### US-021: Hard gates the model can't argue with

**Description:** As a TUI end-user, I want the hard pre- and post-checks (arriving
in Phase 4) to be able to stop an action outright, with the model having no way to
override them, so that there is a real boundary and not just a polite request.

**Acceptance Criteria:**
- [ ] When a hard pre-check blocks the call, permission, sandbox, and the tool's
  own work never run, and the result is an error carrying the block's reason.
- [ ] There is no path for the model to override a hard block.
- [ ] When a hard post-check blocks an otherwise-successful result, the result
  handed back to the model becomes an error carrying the block's reason.
- [ ] Build, typecheck, and unit tests pass

### US-022: A human gate that can also correct

**Description:** As a TUI end-user, I want the permission stage (arriving in Phase
3) to be able to allow, deny, or hand back corrected arguments, so that I can fix
a slightly-wrong action instead of only accepting or rejecting it.

**Acceptance Criteria:**
- [ ] When the permission stage denies the call, the sandbox and the tool's work
  never run, and the result is a clear "permission denied" carrying the reason.
- [ ] When the permission stage returns edited arguments, the edited arguments are
  re-validated against the tool's declared shape and the tool runs on the
  corrected arguments, not the originals.
- [ ] Build, typecheck, and unit tests pass

### US-023: Unknown or malformed calls fail fast and clean

**Description:** As a TUI end-user, I want a call to a tool that doesn't exist, or
with arguments that don't fit the tool's declared shape, to be rejected before any
real work or gate runs, so that nothing acts on a request that never made sense.

**Acceptance Criteria:**
- [ ] A call naming an unregistered tool short-circuits to a clear "unknown tool"
  error before any stage runs.
- [ ] Arguments that don't fit the tool's declared shape are rejected with a
  validation error and the tool's own work is never invoked with bad input.
- [ ] Build, typecheck, and unit tests pass

---

## 4. Functional Requirements

- **FR-1** — The executor MUST route every tool call through one ordered chain:
  hard pre-checks → human permission → sandbox → execute → hard post-checks.
- **FR-2** — The order of stages MUST be fixed and externally observable so tests
  can assert it.
- **FR-3** — The sandbox stage MUST apply to the shell command (and to any custom
  tool that declares it runs commands) and MUST be skipped for read, write, edit,
  content search, and file-pattern search.
- **FR-4** — The permission, hard-check, and sandbox stages MUST ship as inert
  pass-through placeholders that a later phase can replace without altering the
  path.
- **FR-5** — A hard pre-check block MUST prevent permission, sandbox, and the
  tool's work from running and MUST return an error carrying the block's reason,
  with no model override.
- **FR-6** — A permission denial MUST prevent the sandbox and the tool's work from
  running and MUST return a "permission denied" result carrying the reason.
- **FR-7** — When the permission stage returns edited arguments, the executor MUST
  re-validate them against the tool's declared shape and run the tool on the
  corrected arguments.
- **FR-8** — A hard post-check block MUST turn an otherwise-successful result into
  an error carrying the block's reason.
- **FR-9** — A call naming an unregistered tool MUST short-circuit to an "unknown
  tool" error before any stage runs.
- **FR-10** — Arguments that don't match a tool's declared shape MUST be rejected
  with a validation error before the tool's work runs.
- **FR-11** — Any tool-, input-, or I/O-level failure MUST surface as an error
  result tied to its originating call, never a crash.
- **FR-12** — The catalog MUST register tools by name, advertise exactly the
  registered set in a stable order identical across runs, and reject a duplicate
  name without losing the first tool.
- **FR-13** — Custom tools MUST receive identical treatment to built-ins on the
  path.
- **FR-14** — The read-file tool MUST return line-referenced contents, support an
  optional line offset and limit, and return a clear error (no raw bytes) for
  missing, directory, unreadable, binary, or out-of-range inputs.
- **FR-15** — The write-file tool MUST create or replace a file, optionally create
  missing parent folders, and return a concise confirmation that does not echo the
  content.
- **FR-16** — The precise-edit tool MUST replace a unique snippet in place; MUST
  reject an ambiguous match (more than one occurrence), reporting the count and
  leaving the file unchanged; MUST replace all occurrences only when replace-all is
  explicitly opted into; and MUST reject a missing target or a no-op (target equals
  replacement) with the file unchanged.
- **FR-17** — The shell tool MUST return combined output and exit code, report a
  non-zero exit as an error result with the captured output, and enforce a time
  budget that kills the command, reports a timeout with partial output, and leaves
  no orphan process.
- **FR-18** — The content-search tool MUST return `file:line:match` results
  scopable by path, file glob, and case sensitivity; MUST treat no matches as a
  successful result; and MUST degrade to a clear error when its backend binary is
  unavailable.
- **FR-19** — The file-pattern-search tool MUST return matching paths in stable
  order with common noise directories skipped, treat no matches as a successful
  result, and return an error for a bad root.
- **FR-20** — Output exceeding the configured budget MUST be truncated on a clean
  character boundary, remain valid text, and carry a machine-legible marker stating
  how much was elided.
- **FR-21** — Every blocking operation on the path MUST be cancellable, returning a
  cancellation error result and stopping any child work.
- **FR-22** — The permission stage MUST be given access to the same live event
  stream the frontend is draining, so a future permission prompt can reach the
  human and its answer can flow back without the loop mediating.

---

## 5. Non-Goals (Out of Scope)

Deferred to named phases, or out of v1 entirely.

- **Parallel tool execution** — explicitly **out of v1** (`architecture.md` §7).
  Calls run one at a time, in the order the loop hands them over; a future version
  may run independent calls concurrently, but each must still funnel through this
  one chain.
- **Real permission behavior** — allow/deny/ask, modes, remembered rules —
  [`prd-phase-3-permission.md`](prd-phase-3-permission.md). Phase 2 ships only the
  allow-everything placeholder.
- **Real hard-gate behavior** — external and in-process checks, matchers,
  lifecycle events, blocking semantics — [`prd-phase-4-hooks.md`](prd-phase-4-hooks.md).
  Phase 2 ships only the never-block placeholder.
- **Real sandbox behavior** — OS confinement, policy derivation, profile
  generation — [`prd-phase-5-sandbox.md`](prd-phase-5-sandbox.md). Phase 2 ships only the
  run-directly placeholder.
- **The agent loop** that drives the path —
  [`prd-phase-1-agent-loop.md`](prd-phase-1-agent-loop.md) (upstream dependency).
- **The subagent / task capability** —
  [`prd-phase-6-subagents.md`](prd-phase-6-subagents.md). The catalog must accept tools
  registered at runtime; the task tool is just another tool registered there.
- **Rich UI rendering** of tool activity and diffs —
  [`prd-phase-7-tui.md`](prd-phase-7-tui.md). Phase 2 returns plain-text results;
  rendering is the frontend's job.

---

## 6. Design Considerations

- **One door, by construction.** The path is built first — with most stages inert
  — precisely so there is exactly one place every call passes. New tools and new
  safety stages plug into existing slots rather than adding call sites.
- **Sandbox applies to command execution only.** File operations take the direct
  path; the shell command (and command-declaring custom tools) is the single thing
  routed through confinement, matching the boundary in `architecture.md` §6.
- **The precise edit's uniqueness contract** is the single most important safety
  property of the edit tool: an ambiguous match is rejected, never guessed, and
  the file is left byte-for-byte unchanged.
- **Results, not crashes.** Every failure mode is a readable result the model can
  react to, tied back to its originating call.

---

## 7. Technical Considerations

Conceptual only — no type or interface definitions; those are designed at planning
time per `architecture.md` §10.

- **External content-search dependency.** The content-search tool invokes an
  external ripgrep binary. Its absence is detected and degraded to a clear,
  actionable error, never a crash.
- **Large-output truncation.** A single output budget applies across all tools;
  over-budget output is clipped on a clean character boundary with a legible
  elision marker. Per-tool budgets are a possible future refinement (see Open
  Questions).
- **Mutations happen in place.** Phase 2 writes and edits directly; a
  crash-atomic write-then-swap strategy is deferred and interacts with the
  sandbox's filesystem policy ([`prd-phase-5-sandbox.md`](prd-phase-5-sandbox.md)).
- **Integration without contract change.** Phase 2 answers the execution seam
  Phase 1 already calls and must integrate **without changing prior phases'
  public contracts** (`architecture.md` §9).
- **Offline testability.** Every tool and the path's ordering and short-circuit
  behavior are tested against temporary directories and fakes — no network, no
  real model (`architecture.md` §8).

---

## 8. Success Metrics

Behavioral, aligned with `architecture.md` §9 and Milestone **M2**.

- **The path is the only path.** Read, write, edit, search, find, and shell all
  travel the one ordered chain; the ordering is asserted observably and the sandbox
  stage is exercised for shell only.
- **Safety slots are real and inert.** The placeholder stages prove the chain runs
  end-to-end today, and each can be replaced by a later phase with no change to the
  path itself.
- **Short-circuits hold.** A hard pre-check block, a permission denial, and a hard
  post-check veto each stop the right downstream work and surface the right error.
- **The six built-ins work on a real project tree**, each with its failure modes
  returning clean error results, verified offline against temporary directories.
- **The precise edit's uniqueness contract holds**: unique match edits, while
  ambiguous and missing matches are rejected with the file untouched.
- **Custom tools are first-class**: a tool an SDK developer registers gets the
  identical treatment as a built-in.
- **Every failure is a result, never a crash**, and every result ties back to its
  originating call.
- **Behavior is covered by offline tests against fakes**, and a demonstration
  harness runs the full set of capabilities through the one path.
- **Prior phases' public contracts are unchanged** by integrating Phase 2.

---

## 9. Open Questions

- **Parallel tool execution.** Out of v1 (`architecture.md` §7). A future version
  may run independent calls concurrently, but each must still funnel through this
  one chain. Revisit only after Phases 3–5 make per-call safety stable.
- **Crash-atomic mutations.** A write-then-swap strategy would make mutations
  survive a crash mid-write; deferred until shown to matter, and it interacts with
  the sandbox's filesystem policy ([`prd-phase-5-sandbox.md`](prd-phase-5-sandbox.md)).
- **Confining the file-search and file-find tools.** Phase 2 treats content search
  and file walking as trusted and outside the sandbox stage. Phase 5 may later
  route all file-touching tools through a read-only confinement; the path already
  has the slot to opt them in without restructuring.
- **Per-tool output budgets.** Phase 2 uses one budget across all tools. The
  context manager ([`prd-phase-1-agent-loop.md`](prd-phase-1-agent-loop.md)) may later want
  different budgets per tool (roomier for reads, tighter for shell); this can grow
  without changing the contract.
- **Live command-output streaming.** Phase 2 returns a command's output as one
  final result. Streaming long-running output incrementally to the frontend is a
  TUI nicety ([`prd-phase-7-tui.md`](prd-phase-7-tui.md)), additive to the event model.
- **Structured diffs in edit results.** The precise edit returns a small text
  confirmation; whether the model benefits from a structured diff in the result,
  and how the TUI renders its own diff ([`prd-phase-7-tui.md`](prd-phase-7-tui.md)), is
  open.
- **Ignore list for file-find.** The exact set of skipped noise directories, and
  whether it becomes configurable, is a small follow-up; Phase 2 ships a minimal
  default.
