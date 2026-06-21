# Phase 6 — Subagents

> User-story PRD for the subagent capability. References [`../architecture.md`](../architecture.md)
> (the canonical conceptual spec). If this file conflicts with `architecture.md`,
> the architecture wins. Concrete data types and interface signatures are **not**
> defined here — they are designed at planning time, immediately before
> implementation, per `architecture.md` §10.

## 1. Introduction / Overview

This phase gives the agent the ability to **delegate a focused sub-task to a child
agent**. The child runs in its own isolated context, with a restricted set of
tools, drives its own loop to completion, and returns **only its final result** to
the parent. The parent's conversation is never cluttered with the child's
intermediate steps — no child tool calls, no child streaming text, just the
verdict.

Two things make this worth building, and both are core harness-engineering
concerns (`architecture.md` §1):

- **Context isolation.** A noisy sub-investigation burns the *child's* context
  window, not the parent's. The parent's history stays clean, holding only the
  answer. This keeps long sessions coherent and affordable.
- **Capability narrowing.** A delegated step can be handed a *subset* of the
  parent's tools — for example, read-only search tools with no ability to write
  files or run shell commands — bounding what the delegated work can do.

The capability is exposed to the model as a single way to **delegate a task**: the
model describes the sub-task and the instruction the child should follow, and may
name which tools the child is allowed to use. The harness spins up the child, runs
it, and folds its single final answer back into the parent conversation as the
result of that delegation.

Safeguards are part of the deal. A **recursion-depth limit** stops a chain of
delegations from running away, and **cancellation propagates** from the parent
down through every child so that stopping the parent stops everything beneath it.

This phase introduces a new capability and an internal orchestrator. It reuses the
existing run loop ([`prd-phase-1-agent-loop.md`](prd-phase-1-agent-loop.md)) and
the tool/executor machinery ([`prd-phase-2-tools-executor.md`](prd-phase-2-tools-executor.md))
unchanged, and rides on the existing event model (`architecture.md` §5) and
tool-execution chokepoint (`architecture.md` §6).

**Personas referenced throughout:**

- **TUI end-user** — drives the agent from the terminal. Wants the agent to farm
  out a self-contained chunk of work and get back a clean answer, without the main
  transcript filling up with the dozens of greps and reads the sub-investigation
  took. Wants to see *that* a sub-task is running, and to stop everything with one
  cancel.
- **SDK developer** — builds a program on the harness SDK. Wants to let the agent
  spawn child agents with a chosen toolset, so a delegated step is confined to
  exactly the capabilities it needs, with predictable limits on recursion and
  result size.

## 2. Goals

- The agent can **delegate a focused sub-task** to a child agent, and the
  capability is advertised to the model as a normal tool it may choose to use.
- A delegation starts a child whose context is **fresh** — seeded only by its own
  framing and the delegated instruction — never inheriting the parent's history.
- A child is offered **only** a chosen subset of tools; when no toolset is named it
  gets a **safe read-only default** (read and search only, never file-writing or
  shell).
- **Exactly one entry** — the child's final result — enters the parent
  conversation per delegation, regardless of how much work the child did.
- A **recursion-depth limit** refuses over-budget delegations without starting a
  child, and the calling loop continues.
- **Cancelling the parent** stops every in-flight child and grandchild promptly,
  including each child's model stream and any running tool or command.
- All acceptance criteria pass against a fake model and a fake frontend (no
  network, no real model), and the phase integrates with Phases 1–2 **without
  changing their public contracts**.

## 3. User Stories

Each story is independently demonstrable in **one session**. Criteria are
behavioral and observable against fakes (`architecture.md` §8); no concrete types
or code appear.

### US-001: Delegate a focused task to a child agent

**Description:** As a TUI end-user, I want the agent to hand off a self-contained
sub-task to a child agent, so that a big investigation gets done without me
micromanaging each step.

**Acceptance Criteria:**
- [ ] The agent can issue a delegation carrying a short human-readable label and an instruction for the child to follow.
- [ ] Issuing a delegation starts a child agent that runs its own loop over the given instruction.
- [ ] The delegation capability is advertised to the model as a tool it may choose to use.
- [ ] The label is visible to the frontend so the user can tell at a glance what the child is working on.
- [ ] Build, typecheck, and unit tests pass

### US-002: Child works in its own isolated context

**Description:** As a TUI end-user, I want the child to work in a fresh context
that does not inherit the parent's full history, so that the sub-task stays focused
and the parent's context budget is not spent on it.

**Acceptance Criteria:**
- [ ] When a child starts, its context is fresh — seeded only by its own framing and the delegated instruction.
- [ ] The child's context never contains the parent's prior conversation history.
- [ ] The child's intermediate work stays entirely within the child; the parent conversation grows by at most one entry per delegation no matter how much the child did.
- [ ] Build, typecheck, and unit tests pass

### US-003: Child restricted to a chosen subset of tools

**Description:** As an SDK developer, I want to choose which tools a child may use
when delegating, so that a delegated step is confined to exactly the capabilities
it needs.

**Acceptance Criteria:**
- [ ] When a delegation names a specific toolset, the child is offered only those tools; tools outside the set are not advertised to it.
- [ ] When a delegation names no tools (omitted or empty), the child receives the safe read-only default — read and search tools only, never file-writing or shell.
- [ ] A child that attempts to use a tool outside its allowed set is refused cleanly with a not-available result; the disallowed tool never runs.
- [ ] The delegation capability itself is withheld from the child's toolset; nesting is governed solely by the depth limit (US-005).
- [ ] Build, typecheck, and unit tests pass

### US-004: Only the child's final result returns to the parent

**Description:** As an SDK developer, I want only the child's final answer to enter
the parent conversation, so that the main transcript reflects conclusions, not raw
legwork.

**Acceptance Criteria:**
- [ ] When a child finishes, the parent conversation gains exactly one entry — the child's final answer — as the result of that delegation.
- [ ] None of the child's tool calls, tool results, or intermediate text appear in the parent conversation.
- [ ] A child answer within size limits returns to the parent in full.
- [ ] An oversized child answer is trimmed with a clear marker and returns as a non-error result.
- [ ] An empty child answer returns as an empty (non-error) result.
- [ ] A child that ends in failure surfaces an error result with a short description; the parent loop continues rather than crashing.
- [ ] Build, typecheck, and unit tests pass

### US-005: Depth limit prevents runaway recursion

**Description:** As an SDK developer, I want a hard ceiling on how deep delegations
can nest, so that an agent delegating to an agent delegating to an agent cannot
recurse without bound.

**Acceptance Criteria:**
- [ ] Delegations are allowed up to a fixed maximum nesting depth; each delegation in a chain within the limit is permitted.
- [ ] A delegation that would exceed the depth limit is refused with a clear depth-limit reason, and no new child is started.
- [ ] When a delegation is refused for depth, the calling loop continues rather than spinning up another layer.
- [ ] Build, typecheck, and unit tests pass

### US-006: Cancellation propagates parent to child

**Description:** As a TUI end-user, I want cancelling the parent to stop every
child and grandchild in flight, so that one cancel truly stops everything.

**Acceptance Criteria:**
- [ ] Cancelling the parent stops a mid-run child, including its model stream and any running tool or command, and the delegation returns a cancellation result promptly.
- [ ] Cancellation reaches all the way down a chain, stopping a grandchild beneath a child; no descendant keeps running.
- [ ] No orphaned work continues after a cancel.
- [ ] Build, typecheck, and unit tests pass

### US-007: See the sub-task running without raw interleaving

**Description:** As a TUI end-user, I want to see *that* a sub-task is running and
when it finishes — without the child's individual steps flooding my view — while
still being asked when the child needs my approval.

**Acceptance Criteria:**
- [ ] The frontend sees a status signal that a sub-task started and later finished, carrying its label.
- [ ] The frontend sees none of the child's raw streaming text or tool-call lifecycle interleaved with the parent's own stream.
- [ ] A child's request for human permission is forwarded to the frontend so a human can answer; only the child's streaming and steps are suppressed, not its human gates.
- [ ] Build, typecheck, and unit tests pass

## 4. Functional Requirements

- **FR-1.** The harness must expose a single **delegation capability** to the
  model, taking a short human-readable label and an instruction for the child to
  follow, and optionally the names of the tools the child may use.
- **FR-2.** Issuing a delegation must start a **child agent** that runs its own
  loop on the given instruction to completion.
- **FR-3.** The child must begin with a **fresh context** seeded only by its own
  framing and the delegated instruction; the parent's prior history must never be
  copied into it.
- **FR-4.** The child's intermediate steps — tool calls, tool results, and
  intermediate text — must stay entirely within the child and must never enter the
  parent conversation or the parent's event stream.
- **FR-5.** When a toolset is named, the child must be offered **only** the
  intersection of the parent's available tools and the requested set; tools outside
  that set must not be advertised to the child.
- **FR-6.** When no toolset is named, the child must receive a **safe read-only
  default** — read and search tools only, never file-writing or shell.
- **FR-7.** A child that attempts a tool outside its allowed set must be **refused
  cleanly** with a not-available result; the disallowed tool must not run.
- **FR-8.** The **delegation capability must be withheld** from the child's
  toolset; further nesting is governed solely by the depth limit.
- **FR-9.** On child completion, the parent conversation must gain **exactly one
  entry** — the child's final answer — as the delegation result.
- **FR-10.** An oversized child answer must be **trimmed** to a sane size with a
  clear marker and returned as a non-error result; an empty answer returns as a
  non-error empty result.
- **FR-11.** A child that ends in failure must surface an **error result** with a
  short description; the parent loop must continue rather than crash.
- **FR-12.** The harness must enforce a **recursion-depth limit**: a delegation
  beyond the fixed maximum depth is refused with a clear reason, no child is
  started, and the calling loop continues.
- **FR-13.** **Cancellation must propagate** from the parent down through every
  child and grandchild — reaching each child's model stream and any running tool or
  command — and the delegation must return a cancellation result promptly.
- **FR-14.** The frontend must receive a **status signal** when a sub-task starts
  and finishes (carrying its label), and must **not** receive the child's raw
  streaming or tool-call lifecycle interleaved with the parent's stream.
- **FR-15.** A child's **human-permission request** must still be forwarded to the
  frontend so a human can answer.

## 5. Non-Goals (Out of Scope)

- **Parallel subagents.** v1 runs **one child at a time, in order**, consistent
  with the sequential tool-execution rule (`architecture.md` §7). Tool calls — the
  delegation included — run sequentially; concurrent fan-out from one turn is a
  future optimization (see §9).
- **Shared state or memory between children.** Children are independent siblings;
  they do not see each other's work.
- **Streaming the child's partial output to the parent model.** The parent receives
  the result only when the child finishes.
- **Named subagent profiles** with preset framing and toolsets. v1 has only the
  generic delegation capability.
- **Per-subagent permission tightening.** v1 reuses the parent's permission
  ([`prd-phase-3-permission.md`](prd-phase-3-permission.md)) and hooks
  ([`prd-phase-4-hooks.md`](prd-phase-4-hooks.md)) behavior unchanged for children.
- **New public SDK contract types.** This phase reuses the existing run, tool, and
  event concepts; concrete shapes are settled at planning time per
  `architecture.md` §10.

## 6. Design Considerations

- The user must always be able to tell **that** a sub-task is running and when it
  finishes, identified by its **label** — never left guessing while the child's
  steps are hidden.
- Hidden steps must not mean hidden gates: a child's **permission request** surfaces
  to the user exactly as a top-level call's would (`architecture.md` §5).
- The demo shape: the user prompts "investigate the config loader." The agent
  delegates a sub-task labelled "find config defaults," instructing the child to
  locate the default settings path with read and search tools only. The frontend
  shows a nested "subagent: find config defaults" status appear and finish; the
  parent conversation then continues with that one sentence as the result — none of
  the child's grep or read steps visible in the parent transcript.

## 7. Technical Considerations

- The child **reuses the same agent loop recursively** — the gather → act → verify
  run from Phase 1, unchanged. The orchestrator must not fork a divergent loop.
- A child is constructed the **same way** the parent was — same model backend, same
  executor wiring, same hooks and permission behavior — differing only in: a fresh
  empty context, a restricted toolset, and an incremented recursion depth. This
  guarantees a child inherits all the safety machinery from Phases 3–5 without this
  phase re-implementing any of it.
- Every tool call, the delegation included, flows through the **single executor
  chokepoint** (`architecture.md` §6); the child's tool subset is built from the
  same tool registry as the parent's (`prd-phase-2-tools-executor.md`).
- The child's events are **drained internally** by the orchestrator and never
  merged onto the parent's outward event stream, except the status signal and any
  forwarded permission request.

## 8. Success Metrics

Behavioral, aligned with `architecture.md` §9 (definition of done) and roadmap
milestone **M4 — "It scales."**

- The agent can **delegate a focused sub-task**, and the capability is advertised
  to the model.
- A delegation **starts a child** that runs its own loop over an **isolated**
  context with a **restricted** toolset.
- **Only the child's final result** returns to the parent, as a single entry; the
  parent transcript shows **none** of the child's intermediate steps.
- The **safe read-only default** applies when no toolset is named; out-of-set tool
  use by a child is **refused cleanly**.
- The **depth limit** refuses over-budget delegations without starting a child and
  lets the caller continue.
- **Cancelling the parent** stops every in-flight child and grandchild promptly.
- The frontend sees **status-only** visibility of sub-tasks (started / finished
  with label) and **no** raw interleaving of the child's steps, while a child's
  **human-permission** requests still reach the frontend.
- A child **failure** surfaces as an error result; the parent loop continues.
- All acceptance criteria (§3) pass against fakes; the phase integrates with
  Phases 1–2 **without changing their public contracts**.

## 9. Open Questions

- **Parallel subagents.** v1 runs children sequentially (`architecture.md` §7).
  Running multiple delegations from one turn concurrently and joining their results
  is the prime future optimization; it needs an aggregation strategy and bounded
  concurrency. Out of v1.
- **Configurable default toolset.** The read-only fallback is fixed for v1; later
  it could be configured per project.
- **Smarter result shaping.** v1 trims oversized answers by size. A future
  "summarize before return" step could compress long child output more
  intelligently than a hard cut.
- **Named subagent profiles.** Preset framing and toolsets for named subagent types
  (as in Claude Code) are a future addition; v1 has only the generic delegation.
- **Per-subagent permission mode.** Whether a child should inherit, tighten, or
  loosen the parent's permission behavior is left open; v1 inherits it unchanged.
- **Tunable depth and result limits via config.** The maximum depth and result size
  are fixed in v1; exposing them through configuration is deferred.
