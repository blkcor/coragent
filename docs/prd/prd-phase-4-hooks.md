# Phase 4 — Hooks (PRD)

> Implements node **4** of the dependency tree in `../roadmap.md` — the **hard,
> mandatory restraint** layer. Obeys `../architecture.md` as the canonical spec.
> Where this PRD and `../architecture.md` disagree, `../architecture.md` wins
> until amended.
>
> This is a **user-story PRD**: it describes *what* hooks must do and *why*, in
> the voice of the people who use them. It contains **no concrete data types,
> interface signatures, or code** — those are designed at planning time, just
> before implementation, per `../architecture.md` §10.

---

## 1. Introduction / Overview

Phase 4 delivers **hooks**: the hard, unconditional restraint layer of the
tool-execution chokepoint (`../architecture.md` §6). A hook is a rule that fires
at a moment in the agent's lifecycle — before a tool runs, after a tool runs,
when the user submits a prompt, when a session starts, when a session stops —
and that can **stop an action outright**.

The defining contrast, stated once and kept crisp everywhere:

| | **Permission** (`prd-phase-3-permission.md`) | **Hooks** (this phase) |
|---|---|---|
| Nature | **Soft** — asks a human | **Hard** — enforces a rule |
| Who decides | A person, in the loop | The rule, unconditionally |
| Recourse | Allow / deny / ask; remember; edit args | None — the model cannot override |
| Bypass | A bypass mode disables it entirely | **Bypass mode has no effect on hooks** |
| Role | A convenience | A real guardrail |

Put plainly: **permission asks; hooks enforce.** When a person wants to wave a
risky action through, permission lets them. When an operator has decided an
action must never happen, a hook makes it so — even when the agent runs with
every human prompt turned off.

Hooks come in two flavors, serving the same purpose from two directions:

- **External-command hooks** — a program the operator wires up in settings. The
  harness hands it the details of what is about to happen; the program's verdict
  decides whether the action proceeds. This is the git-hook-style escape hatch:
  any language, any logic, owned entirely by the operator.
- **In-process (SDK) hooks** — a rule registered directly through the SDK by a Go
  program embedding the harness. No separate process; the rule runs in-process
  and returns its verdict directly. This is how an SDK developer bakes a
  program's own invariants into the agent.

Together with the OS sandbox (`prd-phase-5-sandbox.md`), hooks form Milestone
**M3 — "It's safe"** (`../roadmap.md`): hard restraint enforced on every call.

**Personas referenced throughout:**

- **SDK developer** — builds a Go program on the harness and bakes in invariants
  the model must never violate. Registers in-process hooks and wires external
  ones; treats hooks as the program's safety contract.
- **Operator / power user** — runs the agent as a daily driver and wires up
  guardrails in settings without writing Go.
- **TUI end-user** — the person at the terminal. Does not author hooks but is
  protected by them, usually without having to notice.

---

## 2. Goals

- A blocking hook **stops a tool call before the tool runs**, even with every
  human prompt bypassed; the model receives an error it cannot override.
- A blocking hook **short-circuits everything downstream** — a refused call never
  reaches permission, the sandbox, or the tool itself.
- **Five lifecycle moments** are covered: before a tool, after a tool, on prompt
  submit, on session start, on session stop.
- Hooks are **scopable** by tool name, by pattern over the relevant detail, or
  both combined (both must match); an unscoped hook fires for every action of its
  moment.
- **Both flavors** — external-command and in-process SDK — support the same
  moments, scopes, and verdicts, and behave identically.
- Multiple hooks on one moment run in a **deterministic order** with
  **first-block-wins**; non-blocking outputs compose.
- **Every failure mode fails closed**: error, timeout, missing program,
  malformed or oversized output, panic, or cancellation all block.
- **Malformed hook definitions are caught when the session is built**, not at the
  moment they would fire.
- Hooks slot into the existing executor chokepoint with **no change to any prior
  phase's public contract**.

---

## 3. User Stories

One session per story. Acceptance criteria are verifiable, observable, and
behavioral — no Go types or code.

### US-001: Block a call outright

**Description:** As an operator, I want a hook that blocks an action to stop it outright, so that the model cannot proceed no matter what it tries.

**Acceptance Criteria:**
- [ ] A blocking hook scoped to a command refuses that command; the tool never runs.
- [ ] The model receives an error result and can only adapt or stop, never override.
- [ ] Build, typecheck, and unit tests pass

### US-002: Blocking holds in bypass mode

**Description:** As an SDK developer, I want a blocking hook to hold even when the agent runs with permission prompts bypassed, so that "bypass" only relaxes the human convenience layer and never my hard guardrails.

**Acceptance Criteria:**
- [ ] With every human prompt disabled (bypass mode), the blocking hook still refuses the call.
- [ ] Bypass mode demonstrably affects permission but has no effect on hooks.
- [ ] Build, typecheck, and unit tests pass

### US-003: The model is told it was blocked and why

**Description:** As a TUI end-user, I want the agent to be told it was blocked and why, so that it adapts or stops instead of silently failing or retrying blindly.

**Acceptance Criteria:**
- [ ] A blocked call yields an error result carrying a human-readable reason.
- [ ] The reason surfaces through the normal event stream for the frontend to show.
- [ ] Build, typecheck, and unit tests pass

### US-004: A block short-circuits everything downstream

**Description:** As an operator, I want a blocking hook to short-circuit everything downstream, so that a refused tool call never reaches permission, the sandbox, or the tool itself.

**Acceptance Criteria:**
- [ ] When a before-the-tool hook blocks, permission is never consulted for that call.
- [ ] The sandbox and the tool body never execute for the blocked call.
- [ ] Build, typecheck, and unit tests pass

### US-005: Hooks before a tool runs

**Description:** As an operator, I want hooks that fire before a tool runs, so that I can refuse or reshape a dangerous call before any side effect happens.

**Acceptance Criteria:**
- [ ] A before-the-tool hook fires before any side effect of the call occurs.
- [ ] The hook can refuse the call or reshape its input.
- [ ] Build, typecheck, and unit tests pass

### US-006: Hooks after a tool runs

**Description:** As an operator, I want hooks that fire after a tool runs, so that I can inspect, annotate, or reject what a tool produced before the model sees it.

**Acceptance Criteria:**
- [ ] An after-the-tool hook can replace the result content the model sees.
- [ ] The model sees the replaced content, not the original.
- [ ] Build, typecheck, and unit tests pass

### US-007: Hook on prompt submit

**Description:** As an operator, I want a hook that fires when the user submits a prompt, so that I can stop a forbidden turn before any model or tool work begins, or inject standing context into it.

**Acceptance Criteria:**
- [ ] A blocking prompt-submit hook stops the turn; the prompt is never sent to the model and the reason surfaces as an error event.
- [ ] An injecting prompt-submit hook makes its context present in that turn before the model is called.
- [ ] Build, typecheck, and unit tests pass

### US-008: Hooks on session start and stop

**Description:** As an SDK developer, I want hooks that fire when a session starts and stops, so that I can refuse to start in a forbidden directory, seed standing context once, or run cleanup when the session ends.

**Acceptance Criteria:**
- [ ] A blocking session-start hook aborts startup; no turns run.
- [ ] A session-stop hook runs teardown; a stop-block is recorded but cannot un-stop the session.
- [ ] Build, typecheck, and unit tests pass

### US-009: Scope a hook to a named tool

**Description:** As an operator, I want to scope a hook to a named tool, so that a rule about shell commands does not also fire on file reads.

**Acceptance Criteria:**
- [ ] A hook scoped to one tool fires only for that tool.
- [ ] Actions on other tools do not trigger the hook.
- [ ] Build, typecheck, and unit tests pass

### US-010: Scope a hook by pattern

**Description:** As an operator, I want to scope a hook by a pattern over the relevant detail (the command line, the file path, the prompt text), so that I can target exactly the risky shapes and let everything else through.

**Acceptance Criteria:**
- [ ] The hook fires only when the pattern matches the moment's relevant detail.
- [ ] Non-matching details pass through untouched.
- [ ] Build, typecheck, and unit tests pass

### US-011: Unscoped hooks fire broadly

**Description:** As an operator, I want a hook with no scope to fire for every action of its moment, so that broad rules (redact secrets everywhere) are simple to express.

**Acceptance Criteria:**
- [ ] An unscoped hook fires for every action of its moment.
- [ ] No tool name or pattern is required to express the broad rule.
- [ ] Build, typecheck, and unit tests pass

### US-012: Combine tool-name and pattern scopes

**Description:** As an operator, I want tool-name and pattern scopes to combine (both must match), so that I can be as precise as "only shell commands that look like `rm -rf`."

**Acceptance Criteria:**
- [ ] When both a tool-name scope and a pattern scope are set, the hook fires only if both match.
- [ ] If either the tool name or the pattern does not match, the hook does not fire.
- [ ] Build, typecheck, and unit tests pass

### US-013: Wire an external program as a hook

**Description:** As an operator, I want to wire up an external program as a hook in settings, so that I can write guardrails in any language without touching the harness.

**Acceptance Criteria:**
- [ ] An external program declared in settings is invoked at its configured moment and scope.
- [ ] No harness change is needed to add the guardrail.
- [ ] Build, typecheck, and unit tests pass

### US-014: Hand the program the full detail

**Description:** As an operator, I want the harness to hand my program the full detail of what is about to happen, so that my program has everything it needs to decide.

**Acceptance Criteria:**
- [ ] The external hook receives the event data describing the action it is gating.
- [ ] The detail is sufficient for the program to reach a verdict without guessing.
- [ ] Build, typecheck, and unit tests pass

### US-015: Exit status is the default verdict

**Description:** As an operator, I want my program's success-or-failure result to be the default verdict (success allows, failure blocks), so that the simplest possible script — one that just exits with a status — already works as a hook.

**Acceptance Criteria:**
- [ ] An external hook that merely exits successfully allows the action.
- [ ] An external hook that merely exits with failure blocks the action, with no extra output required.
- [ ] Build, typecheck, and unit tests pass

### US-016: Optional richer verdict

**Description:** As an operator, I want my program to optionally return richer output to override the default verdict, give a human-readable block reason, reshape a tool's input, replace a tool's result, or inject context, so that a hook can do more than a yes/no.

**Acceptance Criteria:**
- [ ] Structured output with an explicit block decision wins over the exit status.
- [ ] Reshaped input, replaced result, and injected context from the output are honored.
- [ ] Build, typecheck, and unit tests pass

### US-017: Reshaped input is re-checked

**Description:** As an operator, I want a reshaped tool input to be re-checked against the tool's expected shape before it runs, so that a hook can fix up a call but can never push malformed input through.

**Acceptance Criteria:**
- [ ] Reshaped input that does not fit the tool's expected shape blocks the call.
- [ ] A well-shaped reshaped input proceeds to the tool.
- [ ] Build, typecheck, and unit tests pass

### US-018: Register an in-process hook through the SDK

**Description:** As an SDK developer, I want to register a hook as a normal, type-safe function through the SDK, so that I can express my program's invariants in Go without spawning a process or serializing anything.

**Acceptance Criteria:**
- [ ] An in-process hook registered through the SDK fires at its moment and scope.
- [ ] No separate process is spawned and no serialization is required.
- [ ] Build, typecheck, and unit tests pass

### US-019: In-process parity with external hooks

**Description:** As an SDK developer, I want my in-process hook to support the same moments, scopes, and verdicts as external hooks, so that I learn one model and choose the flavor by convenience, not by capability.

**Acceptance Criteria:**
- [ ] An in-process hook that blocks, annotates, or injects behaves identically to the equivalent external hook.
- [ ] All five moments and all scope forms are available to in-process hooks.
- [ ] Build, typecheck, and unit tests pass

### US-020: Both flavors coexist in a defined order

**Description:** As an SDK developer, I want both external and in-process hooks to coexist in one session and run in a defined order, so that I can layer program-level invariants beneath operator-level config.

**Acceptance Criteria:**
- [ ] On a shared moment, in-process and external hooks run in a deterministic order.
- [ ] The first hook that blocks wins and short-circuits the rest.
- [ ] Build, typecheck, and unit tests pass

### US-021: Redact a result without blocking

**Description:** As an operator, I want an after-the-fact hook to replace what the model sees, so that I can redact a secret a tool printed without blocking the whole call.

**Acceptance Criteria:**
- [ ] An after-the-tool hook replaces the result content the model sees while letting the call complete.
- [ ] The redacted content, not the original, reaches the model.
- [ ] Build, typecheck, and unit tests pass

### US-022: Inject standing context

**Description:** As an operator, I want a before-the-prompt or session-start hook to inject standing context, so that repo policy or project facts are present every turn without the user pasting them.

**Acceptance Criteria:**
- [ ] Injected context from a prompt-submit or session-start hook is present before the model is called.
- [ ] The user does not have to paste the standing context.
- [ ] Build, typecheck, and unit tests pass

### US-023: Hook outcomes surface on the event stream

**Description:** As a TUI end-user, I want hook outcomes — a block, a redaction, an injection — to surface through the normal event stream, so that the frontend can show me what happened.

**Acceptance Criteria:**
- [ ] A block, redaction, or injection produces an observable signal on the event stream.
- [ ] The signal is consumable by any frontend without reading internal state.
- [ ] Build, typecheck, and unit tests pass

### US-024: A broken hook fails closed

**Description:** As an operator, I want a hook that errors, times out, is missing, or returns garbage to be treated as a block, so that a broken guardrail fails safe instead of silently letting a dangerous action through.

**Acceptance Criteria:**
- [ ] A hook that errors, times out, is a missing program, or returns malformed or oversized output blocks the action.
- [ ] The harness never fails open on a tool-gating hook.
- [ ] Build, typecheck, and unit tests pass

### US-025: A misbehaving hook never wedges the session

**Description:** As an SDK developer, I want a misbehaving hook (one that panics, hangs, or floods output) to never crash or wedge the session, so that one bad rule cannot take down the agent.

**Acceptance Criteria:**
- [ ] A hook that hangs is killed at its timeout (including child processes), treated as a block, and the session continues.
- [ ] A panicking or output-flooding hook neither crashes nor leaks processes or goroutines.
- [ ] Build, typecheck, and unit tests pass

### US-026: Malformed definitions caught at build time

**Description:** As an operator, I want a malformed hook definition (a bad scope pattern) to be caught when the session is built, not at the moment it would have fired, so that I find my mistake immediately instead of mid-run.

**Acceptance Criteria:**
- [ ] A hook with a bad scope pattern fails session construction with an error naming the offending hook.
- [ ] The bad definition is never silently ignored or deferred to run time.
- [ ] Build, typecheck, and unit tests pass

### US-027: Cancellation cleanly stops in-flight hooks

**Description:** As an SDK developer, I want cancelling the run to cleanly stop any in-flight hook and its child processes, so that hooks never leak processes or goroutines.

**Acceptance Criteria:**
- [ ] Cancelling the run stops any in-flight hook and its child processes.
- [ ] No processes or goroutines leak after cancellation.
- [ ] Build, typecheck, and unit tests pass

---

## 4. Functional Requirements

- **FR-1** — A hook that blocks an action stops that action; the model receives
  an error result it cannot override.
- **FR-2** — A blocking hook holds regardless of permission mode; bypassing human
  prompts has no effect on hooks.
- **FR-3** — A blocked before-the-tool call short-circuits the rest of the chain:
  permission, sandbox, and the tool body never execute for it.
- **FR-4** — The harness supports exactly five lifecycle moments in v1: before a
  tool runs, after a tool runs, on prompt submit, on session start, on session
  stop.
- **FR-5** — A before-the-tool hook may refuse the call or reshape its input.
- **FR-6** — An after-the-tool hook may inspect, replace, or reject the result
  before the model sees it.
- **FR-7** — A prompt-submit hook may block the turn (the prompt is never sent) or
  inject standing context present before the model is called.
- **FR-8** — A session-start hook may abort startup; a session-stop hook may run
  teardown and record a block but cannot un-stop the session.
- **FR-9** — A hook may be scoped by tool name, by a pattern over the moment's
  relevant detail, or both; both must match when both are set.
- **FR-10** — An unscoped hook fires for every action of its moment.
- **FR-11** — External-command hooks are declared in settings; the harness hands
  the program the full detail of the action and consumes its verdict.
- **FR-12** — An external hook's exit status is the default verdict: success
  allows, failure blocks, with no extra output required.
- **FR-13** — An external hook may return richer output that overrides the exit
  status: explicit block decision, human-readable reason, reshaped input,
  replaced result, or injected context.
- **FR-14** — Reshaped tool input is re-checked against the tool's expected shape
  before the tool runs; ill-shaped input blocks the call.
- **FR-15** — In-process hooks register through the SDK with full parity to
  external hooks across moments, scopes, and verdicts.
- **FR-16** — On a shared moment, all hooks run in a deterministic order;
  first-block-wins short-circuits the rest; non-blocking outputs compose
  (chained input edits, content replacements, accumulated injected context).
- **FR-17** — Every failure mode fails closed: error, timeout, missing program,
  malformed or oversized output, panic, and cancellation all block.
- **FR-18** — Each hook has a per-hook timeout; on timeout the hook and its child
  processes are killed and the action is blocked.
- **FR-19** — Malformed hook definitions are caught at session construction with
  an error naming the offending hook.
- **FR-20** — Hook outcomes (block, redaction, injection) surface on the event
  stream so any frontend can render them.

---

## 5. Non-Goals (Out of Scope)

- **The executor chain itself** — stage ordering and where hooks are invoked is
  owned by `prd-phase-2-tools-executor.md`. Phase 4 fills the stages; it does not
  reshape the chain.
- **Permission behavior** — allow/deny/ask, modes, remembered rules
  (`prd-phase-3-permission.md`). Hooks never prompt and never consult permission.
- **Sandboxing** — `prd-phase-5-sandbox.md`. The sandbox confines *how* a command
  runs; hooks decide *whether* it runs. An external hook program is itself **not**
  sandboxed in v1 — it is trusted operator config (see §9).
- **TUI rendering** of hook activity — `prd-phase-7-tui.md`. Phase 4 surfaces
  outcomes through the event stream and tool results; rendering is the frontend's.
- **Subagent hook inheritance** — owned by `prd-phase-6-subagents.md`.
- **Lifecycle moments beyond the v1 five** — notifications, subagent start/stop,
  and context-compaction hooks are out of v1; they are additive later (§9) and the
  v1 five do not constrain them.
- **MCP / plugin hook sources** — `../architecture.md` §1 non-goals. v1 sources
  are exactly settings (external) and SDK registration (in-process).

---

## 6. Design Considerations

- **One chokepoint, two hook stages.** The before-tool and after-tool hook stages
  live inside the single execution chain of `../architecture.md` §6. Phase 4 makes
  the already-present pass-through stages enforcing; it never adds a second path.
- **Permission sits between the two hook stages.** A before-tool block keeps the
  call from ever reaching permission; permission and hooks never call each other.
- **Out-of-chain moments are loop-owned.** Prompt submit, session start, and
  session stop are invoked by the agent loop (`prd-phase-1-agent-loop.md`), not by
  the executor chain.
- **Outcomes ride the existing event stream.** Hooks surface blocks, redactions,
  and injections as events; no frontend reads internal hook state.

---

## 7. Technical Considerations

- **Hooks are declared in settings**, extending the Phase 0 settings file
  (`prd-phase-0-foundations.md`), merged home-then-project (project overrides
  home), declaring external hooks per moment with their scopes and timeouts.
- **An external hook is given the event data** describing the action and **signals
  a block via its exit** (failure blocks, success allows). It may optionally emit
  richer structured output to override that default, supply a reason, reshape
  input, replace a result, or inject context.
- **Reshaped input reuses the Phase 2 input-shape check** to re-validate before the
  tool runs, so a hook can fix a call but never push malformed input through.
- **Per-hook timeouts and process-group teardown** ensure a hanging or flooding
  hook is killed (with its children), treated as a block, and leaves no leaks.
- **Bad definitions are validated at session construction**, so a faulty scope
  pattern surfaces immediately rather than mid-run.

These are conceptual; concrete data shapes and interface signatures are designed
at planning time per `../architecture.md` §10.

---

## 8. Success Metrics

Aligned with `../architecture.md` §9 and Milestone **M3 — "It's safe"**
(`../roadmap.md`). Behavioral, not implementation-shaped.

- **Hard restraint is real.** A blocking hook stops a tool call before the tool
  runs, and does so **even in bypass mode**; the model receives an error it cannot
  override.
- **Both flavors work and match.** An external program wired in settings and an
  in-process SDK function behave identically across blocking, reshaping,
  replacing, and injecting.
- **Scoping is precise and deterministic.** Tool-name and pattern scopes combine;
  unscoped hooks fire broadly; matching is reproducible.
- **Order is defined.** Multiple hooks on one moment run in a deterministic order
  with first-block-wins; non-blocking outputs compose.
- **Fail-closed is universal.** Every failure mode — error, timeout, missing
  program, malformed or oversized output, panic, cancellation — blocks; bad
  definitions are caught at build time; nothing crashes or leaks.
- **It integrates cleanly.** Hooks drop into the Phase 2 chokepoint with no change
  to any prior phase's public contract.
- **It is covered offline.** Every behavior above is exercised by tests against
  fakes — in-process hooks as plain functions, external hooks as tiny throwaway
  scripts — with no network and no real model. The centerpiece test drives the
  **real execution path** and proves a blocking hook short-circuits before the
  tool ever runs.

---

## 9. Open Questions

- **Sandboxing external hooks.** v1 runs external hook programs unconfined, by
  design: they are operator-authored config, like git hooks, and the model can
  neither add nor edit them. The threat model is "restrain the model," not
  "restrain the operator." A future version could route them through the
  `prd-phase-5-sandbox.md` sandbox for defense-in-depth.
- **More lifecycle moments.** Notifications, subagent start/stop (owned by
  `prd-phase-6-subagents.md`), and a context-compaction hook (owned by the
  `prd-phase-1-agent-loop.md` context manager) are natural, purely additive
  extensions; the v1 five do not constrain them.
- **Parallel hooks.** v1 runs a moment's hooks sequentially with first-block-wins,
  matching the sequential tool execution of `../architecture.md` §7. Running
  independent non-blocking hooks concurrently is a later optimization that must
  preserve deterministic composition and the short-circuit guarantee.
- **Re-checking reshaped input per edit.** v1 chains input edits across
  non-blocking before-tool hooks and re-checks the shape once at the end.
  Re-checking after each edit is a refinement; the fail-closed guarantee holds
  either way.
- **Append vs replace after a tool.** v1's after-tool hooks fully replace the
  result content. A future "append without discarding the original" mode is
  additive.
- **Hot reload.** v1 loads hooks when the session is built. Watching the settings
  file and recompiling mid-session is deferred.
- **Subagent hook inheritance.** Whether a subagent inherits the parent's hooks,
  runs a stricter set, or none, is owned by `prd-phase-6-subagents.md`. Phase 4
  only guarantees hooks are constructible per session and fire per session.
