# Phase 7 — TUI Frontend (PRD)

> User-story PRD. References [`../architecture.md`](../architecture.md) (the
> canonical spec). If this document conflicts with the architecture, the
> architecture wins until amended. Per architecture §10 there are **no concrete
> data types here** — only behavior. The exact shapes are designed at planning
> time, immediately before build.

## 1. Introduction / Overview

Phase 7 delivers the **interactive terminal application** for coragent: the
full-screen frontend a developer drives day to day. The user types a request,
watches the agent think, sees each tool call and its outcome, reads a diff
before an edit lands, approves or denies the risky ones through a prompt, and
keeps an eye on a status line and a mode indicator they can toggle.

This is the **first real client of the harness**, and that is the point. The TUI
**consumes only the public SDK** (architecture §2–§3 — the public surface in
`pkg/agent`); it is a **replaceable client** and never reaches into harness
internals. If the TUI cannot be built against the public surface alone, that is a
defect in the SDK — fixed by promoting the missing concept to the public surface,
not by the TUI breaking the boundary. So the TUI is both the daily-driver product
**and** the proof that the SDK is a sufficient, replaceable contract: swap this
frontend for a one-shot CLI or someone's own Go program and the harness is
unchanged (architecture §2 invariants, §5 event model).

What the frontend does, in one line: it **renders the harness's event stream and
answers the one event that waits on a human** — the permission request.

**Personas.** The **TUI end-user** (primary) is a developer using the finished
terminal coding agent for real work; they want a responsive, legible session and
to stay in control of edits and commands. The **SDK developer** (secondary) wants
a reference frontend that proves the public SDK is sufficient and shows the
canonical pattern for consuming the event stream and answering permission
requests.

## 2. Goals

- Render a streaming assistant reply incrementally — first visible token well
  before the turn ends, never a frozen screen.
- Show every tool call as a card (name + argument summary) that updates in place
  to a success or error outcome, in execution order.
- Render proposed edits as an added/removed-line diff before the change lands.
- Surface every gated action as a modal permission prompt answerable in a single
  keystroke, including remember and edit-arguments paths.
- Always deliver a decision back to the harness for any prompt — never leave the
  harness waiting, even on dismiss or quit.
- Make the active mode (default / auto-accept edits / plan / bypass) always
  visible and toggleable with one key.
- Show a status line reflecting current agent activity (thinking / calling a tool
  / idle).
- Cancel an in-flight turn (Esc / Ctrl-C) and quit cleanly without killing work
  mid-decision.
- Scroll back through long history while live output continues without the two
  fighting.
- Build the entire frontend against the public SDK with **zero** harness-internal
  imports.

## 3. User Stories

Each story is a single session. Behavioral only — no Go types, no Bubble Tea
internals. Stories that change what the user sees on screen carry the
**Verify in the running TUI** check (our frontend is a terminal UI).

### US-001: Submit a multi-line request
**Description:** As a TUI end-user, I want to type a multi-line request and submit it, so that I can ask the agent to do work in my own words.

**Acceptance Criteria:**
- [ ] With focus in the input, typing text and submitting places my request in the conversation and starts the agent replying
- [ ] Multi-line input is supported before submit
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-002: Watch the reply stream in
**Description:** As a TUI end-user, I want the assistant's reply to appear incrementally as it is produced, so that I see progress immediately instead of staring at a frozen screen.

**Acceptance Criteria:**
- [ ] While a turn runs, assistant text appears incrementally, not all at once at the end
- [ ] When the turn finishes, the input becomes ready for the next request and status returns to idle
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-003: Read the exchange as a transcript
**Description:** As a TUI end-user, I want my request and the reply to both stay visible, so that I can read the exchange as a continuous transcript.

**Acceptance Criteria:**
- [ ] Submitted requests and their replies remain in the conversation as a continuous, ordered transcript
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-004: See tool-call cards
**Description:** As a TUI end-user, I want each tool the agent invokes to show up as a distinct card with its name and a readable summary of what it was asked to do, so that I always know what the agent is touching.

**Acceptance Criteria:**
- [ ] When a tool call starts, a card appears showing the tool name and an argument summary
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-005: See tool outcomes
**Description:** As a TUI end-user, I want each card to update with the outcome when the tool finishes — success content or a clear error state — so that I can tell whether the step worked.

**Acceptance Criteria:**
- [ ] When the call finishes, the same card updates with its result
- [ ] A failed call is shown in a distinct error state
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-006: Read tool steps in order
**Description:** As a TUI end-user, I want tool steps to read in the order they ran, so that the session is a coherent narrative rather than a jumble.

**Acceptance Criteria:**
- [ ] When several tools run in a turn, their cards appear in execution order (sequential in v1 — [`../architecture.md`](../architecture.md) §7)
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-007: See edit diffs
**Description:** As a TUI end-user, I want a proposed file edit shown as an added/removed-line diff, so that I can review the exact change before deciding whether it should apply.

**Acceptance Criteria:**
- [ ] When an edit is shown, the change is rendered as an added/removed-line diff with the two clearly distinguished
- [ ] A non-diff-shaped result falls back to plain readable text rather than breaking the card
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-008: Get a permission prompt
**Description:** As a TUI end-user, I want a prompt to appear when the agent proposes a gated action, showing what it wants to do and why, so that nothing risky happens behind my back.

**Acceptance Criteria:**
- [ ] When the harness requests a decision, a prompt appears showing the action and the reason for asking, and it captures keyboard focus
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-009: Allow or deny in one keystroke
**Description:** As a TUI end-user, I want to allow or deny the proposed action with a single keystroke, so that approving routine work stays frictionless.

**Acceptance Criteria:**
- [ ] Choosing allow delivers the decision, closes the prompt, and resumes the turn
- [ ] Choosing deny prevents the action from running and the turn continues or ends without it
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-010: Remember a decision
**Description:** As a TUI end-user, I want to choose "remember this decision" when I allow or deny, so that I am not asked again for the same kind of action.

**Acceptance Criteria:**
- [ ] Choosing allow-and-remember or deny-and-remember applies the decision now and suppresses the prompt for the same kind of action for the session ([`prd-phase-3-permission.md`](prd-phase-3-permission.md))
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-011: Edit arguments before allowing
**Description:** As a TUI end-user, I want to edit the action's arguments before allowing it, so that I can correct a not-quite-right command instead of denying and re-prompting.

**Acceptance Criteria:**
- [ ] Choosing edit-arguments lets me amend and submit, and the action runs with my edited arguments
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-012: Keep the screen inert while deciding
**Description:** As a TUI end-user, I want the rest of the screen inert while a prompt is up, so that I cannot accidentally type into the conversation while a decision is pending.

**Acceptance Criteria:**
- [ ] While the prompt is up, conversation and scroll keys have no effect until I answer
- [ ] Dismissing the prompt (Esc) is treated as a deny, and a decision is always delivered — the harness is never left waiting
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-013: See the active mode
**Description:** As a TUI end-user, I want a visible indicator of the active mode (default / auto-accept edits / plan / bypass), so that I always know how much the agent will do without asking me.

**Acceptance Criteria:**
- [ ] The active mode is always visible in the chrome
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-014: Toggle the mode
**Description:** As a TUI end-user, I want to toggle the mode with a keystroke, so that I can loosen or tighten oversight to match the task at hand.

**Acceptance Criteria:**
- [ ] Pressing the mode-toggle key advances to the next mode and updates the indicator
- [ ] The active mode governs how aggressively subsequent actions are auto-answered without my being prompted
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-015: See status and progress
**Description:** As a TUI end-user, I want a status line telling me what the agent is doing right now (thinking, calling a tool, idle), so that I can tell a working session from a stuck one.

**Acceptance Criteria:**
- [ ] When the agent starts thinking, calls a tool, or goes idle, the status line reflects the current activity
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-016: Interrupt and cancel the turn
**Description:** As a TUI end-user, I want to cancel the in-flight turn at any time (Esc / Ctrl-C), so that I can stop a wrong direction without killing the whole app.

**Acceptance Criteria:**
- [ ] Pressing Esc or Ctrl-C mid-turn cancels it, the agent stops, and the app returns to idle ready for a new request
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-017: Quit cleanly
**Description:** As a TUI end-user, I want a clear way to quit the program, so that I can leave cleanly when I'm done.

**Acceptance Criteria:**
- [ ] When idle (or after a first cancel), the quit key (a second Ctrl-C, or Ctrl-Q) exits cleanly
- [ ] If a prompt was open, a deny is delivered before exit so nothing is left waiting
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-018: Scroll long history
**Description:** As a TUI end-user, I want to scroll back through a long conversation, so that I can re-read earlier steps while the session continues.

**Acceptance Criteria:**
- [ ] Scrolling up shows earlier content and holds my position there as new output arrives
- [ ] When scrolled back to the bottom, the view follows new output automatically
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-019: Survive a step error
**Description:** As a TUI end-user, I want an error to appear as a notice in the conversation rather than crashing the app, so that one bad step doesn't end my session.

**Acceptance Criteria:**
- [ ] When an error reaches the frontend (including a hard-gate abort — [`prd-phase-4-hooks.md`](prd-phase-4-hooks.md)), it appears as a notice in the conversation and the app stays usable
- [ ] **[UI]** Verify in the running TUI (manual interactive check)
- [ ] Build, typecheck, and unit tests pass

### US-020: Trust the SDK boundary
**Description:** As an SDK developer, I want the entire frontend built against the public SDK with no reach into harness internals, so that I can trust the SDK is a sufficient contract and copy the pattern for my own frontend.

**Acceptance Criteria:**
- [ ] The frontend imports only the public SDK and no harness-internal package ([`../architecture.md`](../architecture.md) §3)
- [ ] Build, typecheck, and unit tests pass

## 4. Functional Requirements

- **FR-1** — The app renders a full-screen, scrollable conversation containing
  requests, streaming replies, tool cards, diffs, and error notices.
- **FR-2** — The input accepts multi-line text and a submit action; on submit the
  request is added to the conversation and a run is started.
- **FR-3** — Assistant text renders incrementally as it streams; the view follows
  new output when pinned to the bottom.
- **FR-4** — Each tool call renders a card with name and argument summary on
  start, updated in place with a success result or a distinct error state on
  finish, in execution order.
- **FR-5** — Edit results render as an added/removed-line diff with added and
  removed clearly distinguished; non-diff results fall back to plain text.
- **FR-6** — A permission request renders a modal prompt showing the action and
  the reason, capturing keyboard focus and rendering the rest of the screen
  inert.
- **FR-7** — The prompt offers allow, deny, allow-and-remember, deny-and-remember,
  and edit-arguments-then-allow; the chosen decision (including edited arguments)
  is delivered back to the harness.
- **FR-8** — A decision is **always** delivered for an open prompt — dismiss and
  quit both deliver a deny so the harness never hangs.
- **FR-9** — The chrome always shows the active mode; a key advances it through
  default, auto-accept edits, plan, and bypass.
- **FR-10** — A status line reflects current activity: thinking, calling a tool,
  or idle.
- **FR-11** — Esc / Ctrl-C cancels an in-flight turn and returns to idle; a
  subsequent quit key exits cleanly.
- **FR-12** — Scrolling back holds position as new output arrives; returning to
  the bottom resumes auto-follow.
- **FR-13** — Errors surface as in-conversation notices; the app stays usable.
- **FR-14** — The frontend consumes only the public SDK event stream and answers
  permission requests over it; window resize relays out.
- **FR-15** — The binary entry point constructs a session via the SDK and
  launches the app.

## 5. Non-Goals (Out of Scope)

- Any change to the harness or its public surface (Phases 1–6 own those).
- **Themes / color schemes** beyond a single default — deferred (see §9).
- **Mouse support** (scroll and click) — deferred (see §9).
- **Web frontend** — explicitly out of v1 ([`../architecture.md`](../architecture.md)
  §1 non-goals); the render-events-and-answer pattern here is the reference a
  future web frontend would reuse over a network transport.
- One-shot non-interactive CLI mode — a separate frontend, not this phase.
- Parallel-tool-execution UI — the harness is sequential in v1
  ([`../architecture.md`](../architecture.md) §7).
- Slash commands / command palette and markdown rendering — candidate future work.

## 6. Design Considerations

The screen, illustratively (layout, not a spec):

```
┌─ coragent ─────────────────────────────────── default ─┐  ← mode indicator
│                                                          │
│  › refactor the parser to use a lookahead table          │  ← my request
│                                                          │
│  I'll start by reading the current parser…               │  ← streaming reply
│                                                          │
│  ┌ tool: read  parser.go ──────────────────── done ┐    │  ← tool card
│  │ 320 lines read                                    │    │
│  └───────────────────────────────────────────────────┘  │
│                                                          │
│  ┌ tool: edit  parser.go ──────────────────── done ┐    │  ← edit card w/ diff
│  │  - table := buildTable()                          │    │   (removed, red)
│  │  + table := buildLookaheadTable()                 │    │   (added, green)
│  └───────────────────────────────────────────────────┘  │
│                              (scrollable conversation)    │
├──────────────────────────────────────────────────────────┤
│ status: calling a tool ⠴                                  │  ← status line
├──────────────────────────────────────────────────────────┤
│ › █                                                       │  ← input
└──────────────────────────────────────────────────────────┘

   ┌─ Permission required ─────────────────────────┐
   │ tool: shell                                    │   ← permission prompt
   │ rm -rf ./build                                 │      (over conversation)
   │ reason: matches a destructive pattern          │
   │                                                │
   │ [a]llow  [d]eny  [A]llow+remember  [e]dit args │
   └────────────────────────────────────────────────┘
```

The conversation scrolls; the mode indicator, status line, and input stay fixed
as chrome. The permission prompt is modal — it overlays the conversation and
holds focus until answered.

## 7. Technical Considerations

- The frontend depends on a **single contract**: the public SDK surface
  ([`../architecture.md`](../architecture.md) §2 — "the SDK surface is the only
  public contract"). No harness-internal dependency may appear in it.
- The app **renders the public SDK event stream** (architecture §5): streaming
  assistant text, tool-call lifecycle, status changes, turn-finished, and error
  signals. It never reads internal harness state directly.
- The one event that waits on a human is the **permission request**; the frontend
  renders it and **answers back to the harness** with a decision (allow / deny,
  optionally remember, optionally edited arguments). On dismiss or quit it still
  answers, so the harness never hangs (architecture §5).
- Cancellation flows from a keystroke through the SDK run down to the provider and
  any running tool (architecture §7).
- Subagent activity ([`prd-phase-6-subagents.md`](prd-phase-6-subagents.md))
  surfaces inline in the conversation; no separate panes in v1.

## 8. Success Metrics

Behavioral; aligned with [`../architecture.md`](../architecture.md) §9 and roadmap
milestone **M5 ("It ships — full TUI daily-driver")**. A Phase 7 build is done
when this interactive session works end to end against a real harness session:

1. Launch the app; the full-screen UI appears with an empty conversation, focused
   input, idle status, and the **default** mode indicator.
2. Submit a request that triggers tool use (e.g. "read and summarize a file"); the
   **reply streams in** incrementally.
3. A **tool card** appears when the call starts and fills in with its result when
   it finishes.
4. Submit a request that triggers a gated edit; a **diff** is shown and a
   **permission prompt** appears; allowing it **applies the edit** and the turn
   resumes (the headline criterion).
5. Toggling the mode cycles the **indicator** through auto-accept edits, plan, and
   bypass, and back.
6. Pressing Esc mid-turn **cancels**; the UI returns to idle and accepts a new
   request.
7. An induced error appears as a **notice** in the conversation and the app stays
   usable.
8. Inspecting the build confirms it **imports only the public SDK** — no
   harness-internal dependency anywhere in the frontend.

And, per architecture §8–§9:

- Behavior is covered by **offline tests against fakes** — a fake on the SDK side
  scripts the event stream (including a permission request whose answer the test
  reads back) and a test harness drives the frontend's reactions. No network, no
  real model.
- The permission round-trip is tested: a request is rendered, a key is pressed,
  and the **exact decision** (allow / deny / remember / edited arguments) is
  observed to reach the harness, with no dangling wait.
- Phase 7 integrates with Phases 1–6 **without changing any of their public
  contracts**.

## 9. Open Questions

- **Themes** — single default style in v1; pluggable color themes deferred.
- **Mouse support** — scroll and click deferred to a later pass.
- **Web frontend** — out of v1 ([`../architecture.md`](../architecture.md) §1); the
  render-events-and-answer pattern here is the reference a future web frontend
  would re-implement over a network transport — strong evidence for the
  SDK-is-replaceable claim.
- **Markdown rendering** — replies are plain text in v1; rich rendering is a
  candidate enhancement.
- **Slash commands / command palette** — in-input commands (e.g. clear, switch
  mode) are a possible ergonomic add, not required for v1.
- **Diff robustness** — v1 assumes the edit tool's result is diff-shaped; if that
  result format evolves, the diff view follows it.
- **Multiple sessions / tabs** — one session per program in v1; subagent work
  renders inline rather than in separate panes.
- **Remembered-decision durability** — should remembered permission decisions
  persist across app restarts, or stay session-scoped as in v1? Depends on Phase
  3's remembered-rules durability ([`prd-phase-3-permission.md`](prd-phase-3-permission.md))
  — to be confirmed at planning time.
