# Phase 3 — Permission (PRD)

> User-story PRD for the **soft, human-in-the-loop** permission layer.
> Obeys `architecture.md` as the canonical spec. Where this PRD and
> `architecture.md` disagree, `architecture.md` wins until amended. Concrete data
> types and interface signatures are intentionally **absent** here — they are
> designed at planning time, immediately before implementation, per
> `architecture.md` §10. This document describes *what* the phase must do and
> *why*, in user terms.

---

## 1. Introduction / Overview

Phase 3 gives the human **the final say** over what the agent does. Before the
agent edits a file or runs a command, the permission layer decides one of three
things: **allow it**, **deny it**, or **ask the human**. When it asks, the user
sees what the agent wants to do and answers — allow, deny, optionally tweak the
arguments first, and optionally remember the choice so the same action runs
freely next time.

This is the layer that turns the agent from something that acts silently into
something that acts **with you in the loop**. It is the difference between an
agent you supervise and one you simply trust. Phase 3 lets the user dial exactly
how much supervision they want, from "ask me about everything" to "trust
everything," with sensible stops in between.

Together with `prd/prd-phase-2-tools-executor.md` (the tools and the execution
path), Phase 3 completes Milestone **M2 — "It acts"** (`roadmap.md`): the agent
reads and edits files and runs commands, with human-in-the-loop permission on
every action.

**Primary persona — TUI end-user.** The person sitting at the terminal watching
the agent work. They approve or deny the agent's actions, flip modes as the task
changes, build up a set of remembered "always allow this" choices, and
occasionally fix the agent's arguments before letting a call through. They want
to stay in control without being nagged about safe, repetitive actions.

**Secondary persona — SDK developer.** A developer building their own frontend or
automation on the harness. They configure the starting mode and the rule set,
supply a settings file, and answer permission requests programmatically (e.g. a
test that scripts "allow" or an unattended job that runs in a trusting mode).
They want the permission behavior to be predictable and fully controllable from
code and configuration.

### Permission is soft. Hooks are hard. (Stated once, up front.)

Both permission (this phase) and hooks (`prd/prd-phase-4-hooks.md`) live in the
single tool-execution path (`architecture.md` §6), but they are different in
kind and must never be confused:

| | **Permission (this phase)** | **Hooks (`prd/prd-phase-4-hooks.md`)** |
|---|---|---|
| Who decides | a **human** | a rule the human pre-wrote, applied by the machine |
| Nature | **advisory** — a convenience | **enforcing** — a guardrail |
| Can be overridden | yes — the user can allow, edit, or remember | no — a blocking hook stops the call, the model has no recourse |
| Trust model | "let me supervise" | "this must never happen" |
| Bypassable | yes — `bypass` mode skips it entirely | no — bypass mode does **not** touch hooks |

The single most important design constraint of this phase: **permission never
enforces.** It only asks, allows, or denies on the user's behalf. The real
guardrails are hooks (`prd/prd-phase-4-hooks.md`) and the sandbox
(`prd/prd-phase-5-sandbox.md`). An allowlist is a convenience, not a security
boundary.

---

## 2. Goals

- **Gate every action.** No state-changing action runs without passing through a
  permission decision; an unmatched action always results in the human being
  asked — silence never means consent.
- **Three outcomes, one safe default.** Every action resolves to allow, deny, or
  ask, with **ask** as the default whenever nothing else decides.
- **Four modes that switch between turns.** Ship default, auto-accept edits,
  plan, and bypass, each with predictable effects, switchable mid-task and
  settable from configuration.
- **Remembered rules that persist.** An approval or denial can become a durable
  rule that silences future prompts, takes effect immediately, and survives a
  restart.
- **Allowlist / denylist with deny-wins.** Reusable per-action-type rules where a
  deny always beats an allow, and command matching covers a command's family
  without over-matching unrelated commands.
- **Editable, re-validated arguments.** The user can correct an action's
  arguments before approval, and edited arguments are re-checked before they run.
- **Plan-mode is a reliable safety switch.** Plan mode blocks every
  state-changing action with a clear reason and cannot be defeated by an allow
  rule.
- **Cannot wedge.** A missing or misbehaving frontend fails safe (denied) with
  no hang and no crash.
- **Offline-verifiable.** Every behavior is checkable against a fake frontend
  that scripts answers (`architecture.md` §8), with no network and no real model.

---

## 3. User Stories

Behavioral only. Each story is independently verifiable against a fake frontend
in **one session** unless it explicitly spans a restart.

### US-001: Pause and ask before uncovered actions

**Description:** As a TUI end-user, I want the agent to pause and ask me before
it does anything not already covered by a rule or mode, so that nothing
surprising happens behind my back.

**Acceptance Criteria:**
- [ ] In default mode with no rule covering the action, the agent requesting an action prompts the user exactly once
- [ ] Build, typecheck, and unit tests pass

### US-002: Understand each prompt

**Description:** As a TUI end-user, I want each prompt to tell me what the agent
wants to do and why it needs my approval, so that I can decide quickly and
confidently.

**Acceptance Criteria:**
- [ ] The prompt states what the action is and why approval is needed
- [ ] Build, typecheck, and unit tests pass

### US-003: Ask is the safe default

**Description:** As a TUI end-user, I want "ask me" to be the safe default when
nothing else applies, so that silence never means consent.

**Acceptance Criteria:**
- [ ] When no mode or rule decides an action, the outcome is to ask the human, never a silent allow
- [ ] Build, typecheck, and unit tests pass

### US-004: Allow an action

**Description:** As a TUI end-user, I want to allow an action so the agent
proceeds, so that I can keep the task moving.

**Acceptance Criteria:**
- [ ] Allowing a showing prompt lets the action proceed
- [ ] Build, typecheck, and unit tests pass

### US-005: Deny an action and have the agent adapt

**Description:** As a TUI end-user, I want to deny an action and have the agent
find out it was denied and adapt rather than crash or silently stall, so that
the task continues sensibly without that step.

**Acceptance Criteria:**
- [ ] Denying a showing prompt prevents the action from running
- [ ] The agent is told the action was denied and can adapt or stop, with no crash
- [ ] Build, typecheck, and unit tests pass

### US-006: Answer permission requests from code

**Description:** As an SDK developer, I want to answer permission requests from
code (allow/deny), so that I can drive the agent in tests and unattended jobs
without a human present.

**Acceptance Criteria:**
- [ ] A code-driven frontend answering allow or deny produces the matching outcome with no human present
- [ ] Build, typecheck, and unit tests pass

### US-007: Remember a decision within the session

**Description:** As a TUI end-user, I want to remember a decision so the same
kind of action runs without asking again, so that I am not nagged about
something I already approved.

**Acceptance Criteria:**
- [ ] After approving an action and choosing to remember it, the same kind of action recurring later runs without prompting
- [ ] A remembered choice takes effect immediately so the very next matching action benefits without a restart
- [ ] Build, typecheck, and unit tests pass

### US-008: Remembered choices persist across sessions

**Description:** As a TUI end-user, I want a remembered choice to persist across
sessions, so that I do not have to re-teach the agent every time I restart.

**Acceptance Criteria:**
- [ ] After a restart and settings reload, a previously remembered choice is still in effect
- [ ] Saving a remembered choice preserves unrelated settings
- [ ] If saving fails, the approved action still runs — only durability is lost, not the decision
- [ ] Build, typecheck, and unit tests pass

### US-009: Default mode

**Description:** As a TUI end-user, I want a default mode that consults my rules
and asks me about anything else, so that I have full supervision by default.

**Acceptance Criteria:**
- [ ] In default mode, rules are consulted and any uncovered action prompts the user
- [ ] Build, typecheck, and unit tests pass

### US-010: Auto-accept edits mode

**Description:** As a TUI end-user, I want an auto-accept edits mode that lets the
agent edit files without asking while still asking about other actions like
running commands, so that I can let it churn through edits during focused work
without losing oversight of riskier steps.

**Acceptance Criteria:**
- [ ] In auto-accept edits mode, a file edit runs without asking
- [ ] A non-edit action such as a command is still asked about
- [ ] A deny rule still blocks even in this mode
- [ ] Build, typecheck, and unit tests pass

### US-011: Plan mode

**Description:** As a TUI end-user, I want a plan mode in which the agent cannot
change anything — only read and think — so that I can ask it to investigate or
draft a plan with zero risk of side effects.

**Acceptance Criteria:**
- [ ] In plan mode, a write, edit, or command is blocked with a clear reason ("plan mode: changes are disabled")
- [ ] A read action still proceeds normally in plan mode
- [ ] Build, typecheck, and unit tests pass

### US-012: Bypass mode

**Description:** As a TUI end-user, I want a bypass mode that trusts everything
with no prompts, so that I can run a fully trusted, unattended task quickly.

**Acceptance Criteria:**
- [ ] In bypass mode, any requested action runs with no prompt and rules are not consulted
- [ ] Build, typecheck, and unit tests pass

### US-013: Switch modes between turns

**Description:** As a TUI end-user, I want to switch modes between turns, so that
I can tighten or loosen supervision as the task changes without restarting.

**Acceptance Criteria:**
- [ ] Selecting a different mode before the next turn makes the new mode govern subsequent actions
- [ ] Build, typecheck, and unit tests pass

### US-014: Set the starting mode in configuration

**Description:** As an SDK developer, I want to set the starting mode in
configuration, so that a frontend or job begins in the posture I intend.

**Acceptance Criteria:**
- [ ] The configured starting mode governs actions from the first turn
- [ ] Build, typecheck, and unit tests pass

### US-015: Allowlist of always-run actions

**Description:** As a TUI end-user, I want to keep an allowlist of actions that
always run (e.g. always allow `git status`), so that routine safe commands never
interrupt me.

**Acceptance Criteria:**
- [ ] An action covered by an allow rule runs without asking
- [ ] Build, typecheck, and unit tests pass

### US-016: Denylist of always-refused actions

**Description:** As a TUI end-user, I want a denylist of actions that are always
refused, so that dangerous things are blocked without my having to catch them
each time.

**Acceptance Criteria:**
- [ ] An action covered by a deny rule is refused without asking
- [ ] Build, typecheck, and unit tests pass

### US-017: Match a command and its variations

**Description:** As a TUI end-user, I want an allow rule to match a command and
its variations — approving `git status` should cover `git status --short` —
without accidentally matching an unrelated command like `git stash`, so that one
rule covers the family I meant and nothing more.

**Acceptance Criteria:**
- [ ] An allow rule for `git status` covers `git status --short` without asking
- [ ] The same rule does not cover `git stash` or `git statusfoo`
- [ ] Build, typecheck, and unit tests pass

### US-018: Deny wins over allow

**Description:** As a TUI end-user, I want a deny rule to win over an allow rule
when both could apply, so that the strict choice always prevails and I am never
surprised by a permissive rule.

**Acceptance Criteria:**
- [ ] When both an allow rule and a deny rule could match the same action, deny wins
- [ ] Build, typecheck, and unit tests pass

### US-019: Rules in settings, project layers over home

**Description:** As an SDK developer, I want rules expressed in the settings file,
merged so that project settings layer over my home settings, so that a project
can add its own rules on top of my personal defaults.

**Acceptance Criteria:**
- [ ] When home and project settings both define rules, both layers apply with project rules taking precedence
- [ ] A project's stricter deny is honored over a home allow
- [ ] Build, typecheck, and unit tests pass

### US-020: Edit arguments before approving

**Description:** As a TUI end-user, I want to tweak the arguments of an action
before approving it (e.g. narrow a command, fix a path), so that I can correct
the agent instead of denying and re-prompting it.

**Acceptance Criteria:**
- [ ] Editing the arguments and approving runs the action with the edited arguments, not the original
- [ ] Build, typecheck, and unit tests pass

### US-021: Re-validate edited arguments

**Description:** As a TUI end-user, I want my edited arguments to be checked for
validity before they run, so that I cannot accidentally hand the tool something
it never agreed to accept.

**Acceptance Criteria:**
- [ ] Approving with arguments edited into an invalid shape does not run the action
- [ ] The agent is told the edited arguments were rejected
- [ ] Build, typecheck, and unit tests pass

### US-022: Plan mode blocks every mutation with a reason

**Description:** As a TUI end-user, I want plan mode to block every
state-changing action and tell me why, so that I trust the agent truly cannot
alter anything while planning.

**Acceptance Criteria:**
- [ ] Every state-changing action is blocked in plan mode with a stated reason
- [ ] An unknown action is treated as state-changing (blocked) in plan mode, erring on the safe side
- [ ] Build, typecheck, and unit tests pass

### US-023: Plan mode is not defeatable by an allow rule

**Description:** As a TUI end-user, I want plan mode's block to not be defeatable
by an allow rule, so that turning on plan mode is a reliable safety switch
regardless of my allowlist.

**Acceptance Criteria:**
- [ ] In plan mode, an allow rule that would cover a write does not let the write through
- [ ] Build, typecheck, and unit tests pass

### US-024: Bypass disables only the asking

**Description:** As a TUI end-user, I want bypass mode to disable only the asking
— not the hard guardrails (hooks and sandbox) — so that "trust everything" still
cannot do the things I forbade unconditionally.

**Acceptance Criteria:**
- [ ] In bypass mode with a configured hard guardrail, a forbidden action is still stopped by that guardrail
- [ ] Build, typecheck, and unit tests pass

### US-025: Unanswered prompts fail safe

**Description:** As an SDK developer, I want a permission prompt that is never
answered to fail safe (treat the action as denied) rather than hang forever, so
that a missing or crashed frontend cannot wedge the agent.

**Acceptance Criteria:**
- [ ] When a prompt is emitted but never answered, passing the turn's deadline or cancelling treats the action as denied with a reason naming the timeout
- [ ] The agent does not hang
- [ ] Build, typecheck, and unit tests pass

### US-026: Tolerate a misbehaving frontend

**Description:** As an SDK developer, I want the permission layer to tolerate a
frontend that sends more than one answer to a single prompt, so that exactly one
decision is honored.

**Acceptance Criteria:**
- [ ] When a frontend sends two answers to one prompt, the second is ignored and exactly one decision is honored
- [ ] Build, typecheck, and unit tests pass

---

## 4. Functional Requirements

- **FR-1.** Every tool call reaching the permission stage resolves to exactly one
  outcome: allow, deny, or ask the human.
- **FR-2.** When no mode and no rule decides an action, the outcome is to ask the
  human; there is never a silent allow of an uncovered action.
- **FR-3.** The permission request presented to the user states what the action
  is and why approval is needed.
- **FR-4.** An allowed action proceeds through the rest of the execution path; a
  denied action does not run and the agent receives a denial result it can react
  to without crashing.
- **FR-5.** Any frontend — human or code-driven — can answer a permission request
  and produce the matching outcome.
- **FR-6.** The user may, when answering, choose to remember the decision; a
  remembered decision becomes a durable rule that takes effect immediately and is
  consulted for subsequent matching actions in the same session.
- **FR-7.** A remembered decision persists to settings and is honored after a
  restart; saving preserves unrelated settings, and a failed save does not block
  the action it accompanied.
- **FR-8.** The system supports four modes — default, auto-accept edits, plan,
  bypass — each with the effects described in §3, selectable between turns and
  settable from configuration.
- **FR-9.** In auto-accept edits mode, file edits run without asking while
  non-edit actions are still subject to asking and to deny rules.
- **FR-10.** In plan mode, every state-changing action is blocked with a stated
  reason; read actions proceed; an unknown action is treated as state-changing.
- **FR-11.** A plan-mode block is not defeatable by any allow rule.
- **FR-12.** In bypass mode, soft asking and rule consultation are skipped, but
  the hard guardrails (hooks and sandbox) still apply.
- **FR-13.** The system maintains a per-action-type allowlist and denylist;
  command matching covers a command and its argument variations without matching
  an unrelated command.
- **FR-14.** When both an allow rule and a deny rule could match the same action,
  the deny prevails.
- **FR-15.** Rules are read from the settings file and merged home-then-project,
  with project rules taking precedence and a project's stricter deny honored.
- **FR-16.** A combined resolution order produces the user-visible promises: deny
  beats allow; a plan-mode block beats any allow; bypass overrides everything
  soft.
- **FR-17.** The user may edit an action's arguments before approving; the edited
  arguments are re-checked for validity, and the action runs only if they are
  valid, otherwise the agent is told they were rejected.
- **FR-18.** A permission request that is never answered fails safe (treated as
  denied) when the turn's deadline passes or it is cancelled, with a reason
  naming the timeout.
- **FR-19.** For a single permission request, exactly one decision is honored;
  any additional answers are ignored.
- **FR-20.** The permission layer relies on the execution path to indicate
  whether an action changes state, so plan mode and auto-accept edits act on the
  correct set of actions.

---

## 5. Non-Goals (Out of Scope)

- **Permission is not a security boundary.** It only asks, allows, or denies on
  the user's behalf. The real guardrails are hooks (`prd/prd-phase-4-hooks.md`) and
  the sandbox (`prd/prd-phase-5-sandbox.md`).
- **Compound-command parsing is a known soft-layer limitation.** Allowlist
  matching understands a command as words, not as full shell syntax, so a chained
  command such as `git status; rm -rf /` would still match an allow rule for `git
  status`. This is by design for a soft layer; the hard limits live in hooks and
  the sandbox.
- **Hooks of any kind** — the hard, unconditional gates —
  `prd/prd-phase-4-hooks.md`. Permission must not enforce.
- **The execution path and the built-in tools** themselves —
  `prd/prd-phase-2-tools-executor.md`. Phase 3 plugs into the path Phase 2 exposes.
- **Sandboxing** — `prd/prd-phase-5-sandbox.md`. The sandbox is a separate,
  downstream stage, orthogonal to whether the human allowed the action.
- **Rendering the prompt UI** — the modal, the buttons, the diff view, the mode
  toggle widget — `prd/prd-phase-7-tui.md`. Phase 3 defines the behavior and emits
  the request; the TUI draws it. Phase 3 ships only a fake frontend for tests.
- **How a subagent inherits or restricts policy** — `prd/prd-phase-6-subagents.md`.
- **The model backend, the event stream itself, and the settings loader** —
  delivered in `prd/prd-phase-0-foundations.md`. Phase 3 is the first phase to
  actually emit a permission request and consume the answer.

---

## 6. Design Considerations

- **One prompt, one round-trip.** A permission request shows the user what the
  agent wants and why, and accepts back a single decision carrying: allow/deny,
  optionally edited arguments, optionally "remember this." Modeling it as an
  event with an embedded reply path keeps the harness UI-agnostic
  (`architecture.md` §5).
- **Deterministic resolution order.** Mode and rules combine in a fixed order so
  the user-visible promises always hold: deny beats allow; plan-mode block beats
  any allow; bypass overrides everything soft. The order is the contract; its
  internal representation is a planning-time decision.
- **Erring safe on unknown actions.** When the path cannot say whether an action
  changes state, plan mode treats it as state-changing and blocks it.
- **Fake frontend for tests.** All scenarios are driven by a frontend that
  scripts answers, so the whole phase is verifiable offline (`architecture.md`
  §8).

---

## 7. Technical Considerations

- **Settings are the source of rules and starting mode.** Remembered rules,
  allowlist, denylist, and the starting mode are read from the single settings
  file delivered in `prd/prd-phase-0-foundations.md`, merged **home-then-project**
  with project layering over home.
- **Permission sits in the one execution chokepoint.** It is consulted after the
  hard pre-checks and before the sandbox and the action itself
  (`architecture.md` §6); `prd/prd-phase-2-tools-executor.md` owns that path.
- **Decision delivery rides the event stream.** The prompt is delivered and its
  answer awaited over the outbound event stream from
  `prd/prd-phase-1-agent-loop.md`, with the reply path embedded in the request.
- **No concrete types here.** The exact shapes of rules, decisions, and the
  request are designed at planning time per `architecture.md` §10.

---

## 8. Success Metrics

Behavioral, aligned with `architecture.md` §9 and Milestone **M2** (`roadmap.md`).

- **Every action is gated.** No state-changing action runs without passing the
  permission decision; an unmatched action always asks the human (never a silent
  allow). — US-001, US-003, US-005
- **Allow/deny work and the agent adapts.** Approval lets the action proceed;
  denial stops it and the agent is informed and continues sensibly rather than
  crashing. — US-004, US-005, US-006
- **Remembering works end to end.** A remembered choice silences future prompts
  for the same kind of action, persists across restart, and survives a failed
  save. — US-007, US-008
- **All four modes behave as promised**, switchable between turns and settable
  from config; plan mode reliably blocks all changes and cannot be allowlisted
  around, and bypass disables only the asking. — US-009 through US-014, US-022,
  US-023, US-024
- **Rules match the family the user meant** and no more, with deny beating allow
  and project layering over home. — US-015 through US-019
- **Editing arguments is safe** — edited arguments run only if valid. — US-020,
  US-021
- **It cannot wedge.** A missing or misbehaving frontend fails safe (denied),
  with no hang and no crash. — US-025, US-026
- **It integrates cleanly.** Dropping permission into the execution path changes
  **no** public contract of Phases 0–2 (`architecture.md` §9), and every scenario
  passes offline against the fake frontend.
- **Demo.** A throwaway console harness shows the three headline scenarios: first
  prompt then auto-allow after "remember"; plan mode refusing a write with its
  reason; bypass allowing with no prompt. The same scenarios run in CI with
  scripted answers, so the demo is reproducible without a human.

---

## 9. Open Questions

- **How broad a remembered rule should be.** Remembering an approval has to pick
  how general the resulting rule is — just this exact command, the command and
  its first subcommand, or every command of that family. The default leans
  specific (program plus first subcommand for commands; the exact file for file
  actions). Offering the user a choice of breadth at the moment they remember is a
  `prd/prd-phase-7-tui.md` UX question.
- **Where a remembered choice is saved** — for this project or everywhere (home).
  The default is per-project. Letting the user pick per decision is a
  `prd/prd-phase-7-tui.md` UX addition.
- **How home and project rule lists combine.** The default keeps both layers and
  lets the project's rules take precedence; replacing the home list with a clean
  per-project slate is an alternative to revisit once real usage shows which is
  less surprising. Deny-beats-allow keeps the safe case robust either way.
- **Per-action default postures.** A future enhancement could let an action type
  declare its own default (e.g. reading defaults to allowed without a rule). v1
  keeps the uniform "unmatched ⇒ ask" and lets the user add a blanket allow rule
  for reads.
- **A wider auto-accept.** Auto-accept today covers only file edits, leaving
  commands gated. A broader "auto-accept everything mutating" convenience could
  be added later; it is deliberately omitted to keep the risky surface small.
- **An audit trail of decisions.** Recording every allow/deny/ask for later
  review is out of scope for v1 but is a natural future layer around the
  permission decision.
- **Subagent policy inheritance.** Whether a subagent shares the parent's mode
  and rules, gets a stricter read-only posture, or starts fresh is owned by
  `prd/prd-phase-6-subagents.md`.
