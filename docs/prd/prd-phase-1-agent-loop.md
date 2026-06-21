# Phase 1 — Agent Loop (PRD) ⭐

> The heart of the harness. This is a **user-story PRD**: it describes *what* the
> Agent Loop must do and *why*, in the language of the people who use it — never
> *how* it is built. Concrete data shapes, interfaces, and package APIs are
> designed at planning time, immediately before implementation, per
> `../architecture.md` §10.
>
> Canonical spec: `../architecture.md`. Where this PRD and the architecture
> disagree, the architecture wins until amended. User-facing concept names only
> (no invented abbreviations); phases are cross-referenced by document path.

---

## 1. Introduction / Overview

Phase 1 turns the streaming model backend delivered in `prd-phase-0-foundations.md`
into a **working agent** — something that keeps going on its own until a task is
done, not a single question-and-answer exchange.

The defining behavior is the **agent loop**: a repeating cycle of

> **gather** the conversation so far → **act** by asking the model and running
> whatever tools it requests → **verify** by feeding the results back so the
> model can react → and around again, until the task is finished or a safe stop
> fires.

Everything the agent does while it works is surfaced as a **live event stream**:
text appearing as the model writes it, a tool starting, a tool finishing, status
changing from thinking to working to idle, and finally a clean end signal. A
frontend simply watches this stream; it never reaches inside the agent
(`../architecture.md` §5).

Phase 1 also fixes how the loop **stops safely**. A run ends for exactly one of a
small, named set of reasons — the task is complete, a safety limit was reached,
someone cancelled it, or the model backend failed — and that reason is always
reported, never ambiguous.

This phase delivers Milestone **M1 — "It talks"** (`../roadmap.md`): a scripted
or real model drives a multi-step conversation and the whole thing streams to a
trivial readout. **No real tools run yet** — the agent can already *request* a
tool and *incorporate its result*, but the actual file-reading and
command-running tools arrive in `prd-phase-2-tools-executor.md`. Phase 1 proves the
loop end-to-end with a stand-in tool that does nothing but return a placeholder.

**Why this is the deepest PRD.** The loop is where context handling, tool
requests, the wait-for-a-human moment, and cancellation all meet. Get the
*behavior at these seams* right now and every later phase becomes additive: the
real tools, the permission prompts, the hard safety gates, and the sandbox all
slot **inside** the act step without the loop — or the people watching it —
noticing any difference.

**Personas.** Two audiences consume Phase 1, and every user story below is tagged
with whom it serves:

- **SDK / harness developer** — builds on the agent as a library: starts a run,
  drains its event stream, and answers any prompt the agent raises. They care
  about a predictable lifecycle, a stable set of events, deterministic stop
  reasons, and the ability to test everything offline with no real model. They
  may be building the TUI, a one-shot command-line tool, an automated test, or
  their own program (`../architecture.md` §2).
- **TUI end-user** — the person at the terminal who types a task and watches the
  agent work. They care that the agent *keeps going* across many steps without
  re-prodding, that they can *see* what it is doing as it happens, that it *stops
  on its own* when finished, and that they can *interrupt* it at any moment.

The SDK developer is served first because the end-user's experience is built on
the stream the developer consumes. The full TUI is `prd-phase-7-tui.md`; Phase 1's
only frontend is a throwaway readout (see Success Metrics). End-user stories here
describe the *behavior the loop must make possible*, realized visually later.

---

## 2. Goals

- **Multi-round autonomy.** After one task input, the agent completes a
  multi-step job — requesting tools, reading their results, and continuing —
  with **zero** further user prompting per step.
- **Single start, single stream.** Exactly **one** entry point starts a run and
  hands back **one** ordered, read-only event stream that carries every
  observable thing the agent does.
- **Small, stable event vocabulary.** A frontend, a test harness, and a custom
  program all interpret a run identically from the same named events.
- **Exactly-one stop reason.** Every run ends with **exactly one** outcome from
  the named set — completed, reached the step limit, cancelled, or failed — never
  two and never none.
- **Bounded looping.** A model stuck requesting tools forever is halted by a
  configured maximum number of model rounds, reported as a **normal stop**, not
  an error.
- **Prompt cancellation, all the way down.** An interrupt mid-think or mid-tool
  stops the run promptly and leaves **no** background work churning.
- **Recoverable tool failures.** A failed tool surfaces to the model as a result
  it can react to and the run **continues**; only an unrecoverable condition ends
  it.
- **One dispatch path.** Every tool request flows through a single seam, so
  later-phase permission, hooks, and sandbox apply universally with no bypass
  (`../architecture.md` §6).
- **Conversation that grows.** History accumulates across requests in one
  conversation, is exposed only as an uncorruptable snapshot, and an over-budget
  conversation **warns and proceeds** rather than failing.
- **Fully offline-testable.** The entire loop runs against a scripted model and a
  stand-in frontend — no network, no real model — with deterministic, reproducible
  scenarios (`../architecture.md` §8).

---

## 3. User Stories

Each story is implementable in one focused session. Behavioral acceptance
criteria only — never Go types or code. "The stream" means the single ordered
event stream a run produces; all criteria are verifiable offline against a
scripted model (see US-019).

### US-001: Keep working across steps

**Description:** As a TUI end-user, I want the agent to keep working across
several steps after I give it one task, so that it can finish multi-part work
without me re-prompting at every step.

**Acceptance Criteria:**
- [ ] Given the model requests a tool, when the tool returns, the agent
  incorporates the result and continues to the next round with no user
  intervention.
- [ ] Build, typecheck, and unit tests pass

### US-002: Tool, see result, continue — automatically

**Description:** As a TUI end-user, I want the agent to use a tool, see the
result, and then continue reasoning automatically, so that the work flows like a
competent assistant rather than a vending machine.

**Acceptance Criteria:**
- [ ] Given a tool result returns to the loop, when the next round begins, the
  model's continuation reflects that result with no extra prompting.
- [ ] Build, typecheck, and unit tests pass

### US-003: One obvious way to start a run

**Description:** As an SDK developer, I want a single way to start a run with the
user's input and get back a stream I can watch, so that driving the agent is one
obvious call and not a tangle of setup.

**Acceptance Criteria:**
- [ ] Given the user's input, when a run is started through the single entry
  point, a live read-only event stream is returned that the caller can drain to
  completion.
- [ ] Build, typecheck, and unit tests pass

### US-004: Tools run in requested order, one at a time

**Description:** As an SDK developer, I want the agent to run tool requests in the
order the model asked for them, one at a time, so that the conversation history
and the event stream are deterministic. (Parallel execution is out of v1 —
`../architecture.md` §7.)

**Acceptance Criteria:**
- [ ] Given the model requests several tools in one round, when they run, each
  produces a start and a finish on the stream before the next begins, in the
  order requested.
- [ ] Build, typecheck, and unit tests pass

### US-005: One and only one dispatch path

**Description:** As an SDK developer, I want each tool request dispatched through
one and only one path, so that later phases (permission, safety gates,
sandboxing) apply to every tool with no way to slip past. (The path is filled in
by `prd-phase-2-tools-executor.md` onward; `../architecture.md` §6.)

**Acceptance Criteria:**
- [ ] Given any tool request, when it is dispatched, it travels the single
  dispatch path — observable in Phase 1 by the stand-in runner being the sole
  thing that ever produces a tool result.
- [ ] Build, typecheck, and unit tests pass

### US-006: Words appear as the model writes them

**Description:** As a TUI end-user, I want the agent's words to appear as it
writes them, so that I get immediate feedback instead of staring at a blank
screen while it thinks.

**Acceptance Criteria:**
- [ ] Given the model is producing text, when each fragment arrives, it appears
  on the stream as it is produced, not buffered to the end of the reply.
- [ ] Build, typecheck, and unit tests pass

### US-007: See a tool start and finish

**Description:** As a TUI end-user, I want to see when a tool starts and when it
finishes — and what it produced — so that I understand what the agent is doing on
my behalf.

**Acceptance Criteria:**
- [ ] Given a tool runs, when it begins and when it ends, a distinct start (with
  the request, its name and arguments) and a finish (with the result) appear,
  bracketing the work.
- [ ] Build, typecheck, and unit tests pass

### US-008: A clear sense of agent state

**Description:** As a TUI end-user, I want a clear sense of the agent's current
state — thinking, working a tool, or idle — so that I know whether to wait or
whether it is done.

**Acceptance Criteria:**
- [ ] Given any run, when the agent changes phase, a status signal marks the
  transition: thinking as it consults the model, working a tool as tools run,
  idle as the run ends.
- [ ] Build, typecheck, and unit tests pass

### US-009: One ordered stream for everything

**Description:** As an SDK developer, I want a single, ordered stream that carries
every observable thing the agent does, so that any frontend watches one channel
and never has to peek at internal state. (`../architecture.md` §5.)

**Acceptance Criteria:**
- [ ] Given any run, when anything observable happens, it arrives on one ordered
  stream and nowhere else; the watcher never inspects internal state.
- [ ] Build, typecheck, and unit tests pass

### US-010: Small, named, stable event vocabulary

**Description:** As an SDK developer, I want the stream's event vocabulary to be
small, named, and stable, so that the TUI, a test harness, and my own program all
interpret a run the same way.

**Acceptance Criteria:**
- [ ] Given the documented event vocabulary, when a run is consumed by different
  frontends, each interprets the same run identically from the same named events.
- [ ] Build, typecheck, and unit tests pass

### US-011: Stop on its own when finished

**Description:** As a TUI end-user, I want the agent to stop on its own when the
task is genuinely finished, so that it doesn't ramble or keep calling tools after
the work is done.

**Acceptance Criteria:**
- [ ] Given the model produces a reply with no tool request, when that reply
  completes, the run finishes with reason "completed" and the stream closes.
- [ ] Build, typecheck, and unit tests pass

### US-012: A safety limit on looping

**Description:** As a TUI end-user, I want a safety limit on how long the agent
will keep looping, so that a model stuck in a tool-calling rut can't run forever
or run up cost.

**Acceptance Criteria:**
- [ ] Given a model that requests a tool on every round forever, when the run
  reaches the configured maximum number of model rounds, the run ends with reason
  "reached the step limit", reported as a normal stop (not an error), and the
  stream closes.
- [ ] Build, typecheck, and unit tests pass

### US-013: Exactly one named stop reason

**Description:** As an SDK developer, I want every run to end with exactly one
named reason — finished, hit-the-limit, cancelled, or failed — and never with two
endings or none, so that my "what happened" logic is a simple, total switch.

**Acceptance Criteria:**
- [ ] Given any completed run, when it ends, it ends with exactly one outcome
  from the named set, followed by the stream closing — never two endings and
  never none.
- [ ] Build, typecheck, and unit tests pass

### US-014: The step limit is a normal stop, not an error

**Description:** As an SDK developer, I want the hit-the-limit case reported as a
normal stop and not as an error, so that I can tell "the guard tripped" apart
from "something broke."

**Acceptance Criteria:**
- [ ] Given a run that reaches the step limit, when the developer inspects the
  outcome, it is distinguishable from a failed outcome and classified as a normal
  stop.
- [ ] Build, typecheck, and unit tests pass

### US-015: Interrupt at any moment

**Description:** As a TUI end-user, I want to interrupt the agent at any moment —
while it's thinking or mid-tool — and have it stop promptly, so that I'm never
trapped waiting on work I no longer want.

**Acceptance Criteria:**
- [ ] Given the agent is mid-think (model streaming), when the user cancels, the
  live model stream is abandoned, the run ends promptly with a cancelled outcome,
  the stream closes, and no further events follow.
- [ ] Given the agent is mid-tool, when the user cancels, the cancellation
  reaches the running tool, the run ends promptly with a cancelled outcome, and
  the agent does not hang on a stuck tool.
- [ ] Build, typecheck, and unit tests pass

### US-016: Cancellation reaches all the way down

**Description:** As an SDK developer, I want a cancellation to reach all the way
down — into the live model stream and into a running tool — so that nothing keeps
churning in the background after I've pulled the plug.

**Acceptance Criteria:**
- [ ] Given a cancelled run, when it ends, the live model stream and any running
  tool have been signalled to stop and no background work remains alive.
- [ ] Build, typecheck, and unit tests pass

### US-017: A distinguishable cancellation outcome

**Description:** As an SDK developer, I want a cancelled run to end cleanly and
promptly with a distinguishable cancellation outcome, so that I can tell "the
user stopped it" apart from "it finished" or "it failed."

**Acceptance Criteria:**
- [ ] Given a cancelled run, when the developer inspects the outcome, it is
  distinguishable from both a clean finish and a backend failure.
- [ ] Build, typecheck, and unit tests pass

### US-018: A failed tool becomes a recoverable step

**Description:** As a TUI end-user, I want a tool that fails to be shown as a
failed step the agent then tries to recover from, so that one bad command doesn't
kill my whole session.

**Acceptance Criteria:**
- [ ] Given a tool fails, when its failure comes back, it is surfaced on the
  stream as a finished-but-failed step and the run continues so the model can
  recover — the run is not ended.
- [ ] Build, typecheck, and unit tests pass

### US-019: Tool failure reaches the model, not a crash

**Description:** As an SDK developer, I want a tool failure surfaced back to the
model as a result it can react to — not as a crash — so that the agent can
self-correct the way a person would after a failed command. (`../architecture.md`
§8.)

**Acceptance Criteria:**
- [ ] Given a tool fails, when the failure surfaces, it is recorded as a failed
  result the model can see on the next round, and no crash occurs.
- [ ] Build, typecheck, and unit tests pass

### US-020: Backend failure ends the run with a named cause

**Description:** As an SDK developer, I want a genuine backend failure (the model
provider erroring out) to end the run with a clear error outcome that names the
cause, so that I can surface a real problem instead of pretending the turn
finished.

**Acceptance Criteria:**
- [ ] Given the model backend errors before or during a reply, when the failure
  surfaces, the run ends with a failed outcome that names the cause, the stream
  closes, and no partial reply is treated as a real turn.
- [ ] Build, typecheck, and unit tests pass

### US-021: Unrecoverable conditions are distinguishable

**Description:** As an SDK developer, I want truly unrecoverable conditions to be
distinguishable from ordinary tool failures, so that the rare "we cannot
continue" case ends the run while the common "that tool didn't work" case lets it
carry on.

**Acceptance Criteria:**
- [ ] Given a truly unrecoverable condition during dispatch, when it occurs, the
  run ends with a failed outcome distinct from an ordinary tool failure (which
  would merely have continued the run).
- [ ] Build, typecheck, and unit tests pass

### US-022: Pause mid-tool to ask the person, then resume

**Description:** As an SDK developer, I want the agent able to pause mid-tool and
ask the person a question — through the same stream I'm already watching — and
then carry on once I answer, so that approval prompts need no special side-channel
and no change to the loop. (The *content* of permission prompts is
`prd-phase-3-permission.md`; Phase 1 only carries the question out and the answer
back. `../architecture.md` §5–§6.)

**Acceptance Criteria:**
- [ ] Given a dispatch path that pauses to ask a question (simulated in Phase 1),
  when it raises the question, the question appears on the same stream the watcher
  is draining; when the watcher answers, the agent resumes and the tool completes,
  with no change to the loop.
- [ ] Build, typecheck, and unit tests pass

### US-023: The approval prompt appears in context

**Description:** As a TUI end-user, I want the moment the agent asks for approval
to appear right where it happens in the flow — after the tool starts and before it
finishes — so that the prompt has obvious context and isn't a free-floating popup.

**Acceptance Criteria:**
- [ ] Given a tool that raises an approval question, when it appears on the
  stream, it is positioned after the tool starts and before it finishes.
- [ ] Build, typecheck, and unit tests pass

### US-024: Remember the whole conversation

**Description:** As a TUI end-user, I want the agent to remember everything said
and done earlier in our conversation, so that follow-up requests build on prior
work instead of starting from scratch.

**Acceptance Criteria:**
- [ ] Given two requests in the same conversation, when the second runs, the
  model sees the full prior history — system framing, both user turns, prior
  assistant replies, and any tool results — in order.
- [ ] Build, typecheck, and unit tests pass

### US-025: Read history as an uncorruptable snapshot

**Description:** As an SDK developer, I want the running conversation readable as a
snapshot — what was said, what tools ran, what they returned — so that I can
inspect, log, or render history without being able to corrupt it.

**Acceptance Criteria:**
- [ ] Given a run, when the developer reads the conversation, they get a snapshot
  copy they can inspect or render but cannot mutate to corrupt the live
  conversation.
- [ ] Build, typecheck, and unit tests pass

### US-026: Warn before the context window overflows

**Description:** As an SDK developer, I want the agent aware when the conversation
is growing too large for the model's window and warn rather than silently fail, so
that I have a signal to act on before quality degrades. (Actually shrinking
history — compaction — is out of v1; the warning and the inert plug-in point are
in scope.)

**Acceptance Criteria:**
- [ ] Given the assembled conversation exceeds the model's context budget, when
  the agent is about to consult the model, it emits an advisory warning on the
  stream and proceeds anyway — it does not fail the run, and nothing is dropped.
- [ ] Build, typecheck, and unit tests pass

### US-027: One in-flight run per conversation

**Description:** As an SDK developer, I want only one task in flight per
conversation at a time, with a second concurrent start cleanly refused, so that
history can never be scrambled by overlapping runs.

**Acceptance Criteria:**
- [ ] Given a run already in flight, when a second run is started on the same
  conversation, the second is cleanly refused with no effect on the first and no
  change to history.
- [ ] Build, typecheck, and unit tests pass

### US-028: Set up against fakes, fully offline

**Description:** As an SDK developer, I want the agent set up against fakes — a
scripted model and a stand-in frontend — with no network and no real model, so
that the entire loop is testable offline and deterministically.
(`../architecture.md` §8.)

**Acceptance Criteria:**
- [ ] Given the whole agent, when it is exercised in tests, it runs against a
  scripted model and a stand-in frontend (no network, no real model) and the
  scenarios are reproducible deterministically.
- [ ] Build, typecheck, and unit tests pass

### US-029: Slow or abandoned readers never lose events or wedge the agent

**Description:** As an SDK developer, I want a slow or abandoned reader never to
silently lose events or wedge the agent forever, so that backpressure throttles
the work and cancellation always frees it.

**Acceptance Criteria:**
- [ ] Given a slow reader, when events pile up, the agent throttles itself to the
  reader's pace and loses no events.
- [ ] Given an abandoned reader that also cancels, when the agent is blocked, it
  unblocks and exits with no leftover background work.
- [ ] Build, typecheck, and unit tests pass

### US-030: The headline scenario — a tool, then a finish

**Description:** As an SDK developer, I want a scripted model that requests a tool
then finishes on the next round to produce the full ordered event sequence on the
stream, so that the end-to-end loop is proven before any real tool exists.

**Acceptance Criteria:**
- [ ] Given a scripted model whose first round says a little text then requests
  one tool, and whose second round says a little more text then stops, plus the
  Phase 1 stand-in tool runner, when an SDK developer starts a run and drains the
  stream to completion, the watcher observes, in this exact order:

  | # | Observed on the stream |
  |---|---|
  | 1 | status changes to **thinking** |
  | 2 | the model's first text appears |
  | 3 | status changes to **working a tool** |
  | 4 | the tool **starts** (the request, with its name and arguments, is visible) |
  | 5 | the tool **finishes** (its result is visible) |
  | 6 | status changes back to **thinking** |
  | 7 | the model's second text appears |
  | 8 | status changes to **idle** |
  | 9 | the run **finishes**, reason "completed" |
  | 10 | the stream **closes** |

- [ ] And the conversation afterward reads, in order: the system framing, the
  user's request, the assistant's first reply (carrying the tool request), the
  tool's result, and the assistant's final reply.
- [ ] Build, typecheck, and unit tests pass

---

## 4. Functional Requirements

Behavioral, unambiguous, and free of concrete types.

- **FR-1** The system must expose a **single entry point** that starts a run from
  the user's input and returns a live, read-only event stream.
- **FR-2** The system must drive a **gather → act → verify cycle**: assemble the
  conversation, consult the model, run any requested tools, feed results back, and
  repeat until a stop fires.
- **FR-3** The system must **continue automatically** after a tool returns,
  beginning the next round with the tool's result available to the model, with no
  user prompting.
- **FR-4** The system must execute multiple tool requests from one round
  **sequentially in the order requested**, each emitting a start then a finish
  before the next begins.
- **FR-5** The system must route **every** tool request through **one** dispatch
  path, so later-phase stages apply universally with no bypass.
- **FR-6** The system must **stream assistant text incrementally**, emitting each
  fragment as it is produced rather than buffering to the end of the reply.
- **FR-7** The system must emit a **distinct tool-start** (carrying the request,
  its name and arguments) and a **distinct tool-finish** (carrying the result)
  for every tool invocation.
- **FR-8** The system must emit **status signals** marking transitions among
  thinking, working a tool, and idle.
- **FR-9** The system must deliver every observable event on **one ordered
  stream** and expose no other channel for observing a run.
- **FR-10** The system must use a **small, named, stable** event vocabulary
  interpreted identically across frontends.
- **FR-11** The system must **finish on its own** with reason "completed" when the
  model returns a reply with no tool request, then close the stream.
- **FR-12** The system must enforce a **configured maximum number of model
  rounds**; on reaching it, the run ends with reason "reached the step limit",
  classified as a normal stop, then the stream closes.
- **FR-13** The system must end every run with **exactly one** outcome from the
  named set — completed, reached the step limit, cancelled, failed — followed by
  the stream closing; never two outcomes and never none.
- **FR-14** The system must **propagate cancellation** to the live model stream
  and any running tool, ending promptly with a cancelled outcome and leaving no
  background work alive.
- **FR-15** The system must make the **cancelled outcome distinguishable** from
  both a clean finish and a backend failure.
- **FR-16** The system must surface a **tool failure** as a failed result the
  model can see on the next round and **continue** the run; it must not crash and
  must not end the run.
- **FR-17** The system must end the run with a **failed outcome that names the
  cause** when the model backend errors, treating no partial reply as a real turn.
- **FR-18** The system must make a **truly unrecoverable condition** end the run
  with a failed outcome distinct from an ordinary, recoverable tool failure.
- **FR-19** The system must **carry a wait-for-a-human question** outward on the
  shared stream and the answer back, without the loop interpreting it, positioned
  after a tool's start and before its finish.
- **FR-20** The system must **accumulate conversation history** across requests in
  the same conversation, presenting the full prior history to the model in order.
- **FR-21** The system must expose the conversation only as a **read-only
  snapshot** that cannot mutate the live conversation.
- **FR-22** The system must, when the assembled conversation exceeds the model's
  context budget, emit an **advisory over-budget warning** on the stream and
  **proceed** without dropping anything or failing the run.
- **FR-23** The system must **refuse a second concurrent run** on the same
  conversation with no effect on the in-flight run and no change to history.
- **FR-24** The system must **apply backpressure** to a slow reader (losing no
  events) and **unblock and exit** cleanly when an abandoned reader cancels.
- **FR-25** The system must be **fully exercisable against fakes** — a scripted
  model and a stand-in frontend — with no network and no real model.

---

## 5. Non-Goals (Out of Scope)

Deferred work, cross-referenced by document path. The loop is designed so each
slots in **without changing** Phase 1's public contract.

- **Context compaction / summarization is OUT of v1.** Actually shrinking history
  — folding or dropping old turns, summarizing long tool output, sliding windows —
  is not built. Only the inert plug-in point and the over-budget warning ship now.
- **Parallel tool execution is OUT of v1** (`../architecture.md` §7). Tools run
  one at a time within a round; concurrent execution preserving result order is a
  later optimization the dispatch seam already permits.
- **Real tools** — file read/write/edit, shell, search — and the dispatch chain's
  actual stages: `prd-phase-2-tools-executor.md`. Phase 1 holds only the seam and a
  do-nothing stand-in.
- **When and why a permission question is raised**, and the allow / deny / ask
  rules and modes: `prd-phase-3-permission.md`. Phase 1 only carries it.
- **Hard safety gates**: `prd-phase-4-hooks.md` — they live inside the dispatch path.
- **OS sandboxing** of shell commands: `prd-phase-5-sandbox.md` — also inside the path.
- **Subagents** and the delegate-a-task tool: `prd-phase-6-subagents.md` (each
  subagent gets its own conversation; the one-run-per-conversation rule already
  accommodates it).
- **The full terminal UI**: `prd-phase-7-tui.md`. Phase 1's only frontend is the
  throwaway readout.
- **Exact token counting.** v1 uses a cheap estimate sufficient for the warning; a
  precise tokenizer is future work.

---

## 6. Design Considerations

- **The stream is the whole contract.** All decoupling rests on it: the harness
  emits and waits; frontends render and answer. Swap the frontend, the harness is
  unchanged (`../architecture.md` §5).
- **The wait-for-a-human moment is an event, not a side-channel.** The question
  rides the same stream the frontend is already draining and carries a way to
  reply, so any frontend — TUI, CLI, automated test — answers however it likes.
- **One dispatch path, built first with an inert stand-in.** There must be exactly
  one execution path so permission, hooks, and sandbox can never be bypassed; the
  path exists in Phase 1 holding a do-nothing runner.
- **Status signals are advisory.** Thinking / working a tool / idle aid the
  watcher but carry no control semantics; the named stop reasons are the
  authoritative lifecycle.
- **Throwaway readout, not the TUI.** Phase 1's frontend prints the stream as
  plain lines; the real terminal experience is `prd-phase-7-tui.md`.

---

## 7. Technical Considerations

Conceptual constraints only — no concrete type or interface definitions
(`../architecture.md` §10).

- **One in-flight turn per conversation**, serialized; a second concurrent start
  is cleanly refused (`../architecture.md` §7).
- **Cancellation propagates** from the caller down through the provider stream and
  any running tool or command; every blocking operation is cancellable
  (`../architecture.md` §7–§8).
- **Sequential tool execution** within a round (`../architecture.md` §7).
- **Errors are results, not crashes.** Tool failures surface as error results the
  model can react to; only genuine unrecoverable conditions abort the run
  (`../architecture.md` §8).
- **History exposed only as a snapshot** that callers cannot mutate.
- **Budget awareness uses a cheap estimate** sufficient to fire the warning; the
  shrinking step behind it is inert in v1.
- **Backpressure over loss.** A slow reader throttles the agent; an abandoned,
  cancelled reader unblocks it — no events are dropped and no run leaks background
  work (`../architecture.md` §8).
- **Upstream dependency is `prd-phase-0-foundations.md` only**: the conversation
  concepts, the streaming model backend, the event vocabulary including the
  wait-for-a-human question, and the merged configuration. Phase 1's additions are
  purely additive (`../architecture.md` §4–§5, §9).

---

## 8. Success Metrics

Behavioral, aligned with `../architecture.md` §9 and Milestone **M1 — "It talks"**
(`../roadmap.md`).

- **M-1** The single way to start a run and watch it is documented and stable;
  later phases build on it without changing its shape. (US-003, US-009.)
- **M-2** The headline scenario (US-030) passes against a scripted model and the
  stand-in runner: the exact ordered sequence, then the stream closes, and the
  conversation reads in the expected order. (US-001, US-002, US-007.)
- **M-3** Every stop reason is exercised and each produces exactly one terminal
  outcome followed by stream close: completed, reached the step limit (a normal
  stop), cancelled, failed. (US-011–US-014, US-017, US-020.)
- **M-4** A tool failure keeps the run going and reaches the model as a
  recoverable result; only an unrecoverable condition ends the run. (US-018,
  US-019, US-021.)
- **M-5** Cancellation mid-think and mid-tool both stop the run promptly with a
  cancelled outcome and leave nothing running in the background. (US-015–US-017,
  US-029.)
- **M-6** The wait-for-a-human question is carried out on the shared stream and
  the answer back, positioned between a tool's start and finish, proven with no
  permission code present. (US-022, US-023.)
- **M-7** History accumulates across requests in one conversation and is exposed
  only as an uncorruptable snapshot; an over-budget conversation warns and
  proceeds. (US-024–US-026.)
- **M-8** A second concurrent run on the same conversation is refused with no side
  effects. (US-027.)
- **M-9** The whole loop is covered by offline tests against fakes — no network,
  no real model — and no run leaves a background task alive after it ends.
  (US-028, US-029.)
- **M-10** It integrates with `prd-phase-0-foundations.md` without changing that
  phase's public contract — Phase 1's additions are purely additive.

**The M1 demo (throwaway readout, not the TUI).** A tiny program starts a run and
prints the stream: status changes as labels, assistant text inline, tool start
and finish as bracketed lines, the finish reason at the end, an error if one
occurs, and an auto-approval for any wait-for-a-human question. Pointed at the
scripted model it reproduces the headline scenario in CI with no credentials;
pointed at a real model backend it holds a real text-only conversation —
completing **M1**.

---

## 9. Open Questions

- **Context compaction (deferred).** When to trigger shrinking (how close to
  budget), what to always preserve (system framing, the latest user turn, any open
  tool request), and whether to show a "compacting" status. (US-026.)
- **Parallel tool execution (deferred).** Running independent tool requests
  concurrently while preserving result order is a later optimization the dispatch
  seam already permits. (US-004.)
- **Cancellation as a clean stop vs. an error.** Phase 1 reports cancellation as a
  distinguishable failed-style outcome. Whether the TUI prefers a benign
  "cancelled" finish is an additive decision to settle in `prd-phase-7-tui.md`.
  (US-017.)
- **Precise token counting.** The over-budget warning rests on a cheap estimate; a
  model-accurate count is an internal swap that changes only *when* the warning
  fires, not the behavior. (US-026.)
- **Live tool-argument streaming.** Phase 1 surfaces a tool start once its request
  is complete. Showing arguments as they stream in is an additive UX deferred to
  `prd-phase-7-tui.md`. (US-007.)
- **Mid-run steering.** v1 is one request → one run → close, with no way to inject
  a message while the agent is thinking. A steering capability is future work.
  (US-001.)
- **Turn-level retry.** The loop does not retry a failed model round (the backend
  retries at its own layer). Whether a run-level retry belongs in the loop or the
  frontend is open. (US-020.)

---

### Cross-references

- Canonical: `../architecture.md`.
- Upstream: `prd-phase-0-foundations.md`.
- Downstream: `prd-phase-2-tools-executor.md`, `prd-phase-3-permission.md`,
  `prd-phase-4-hooks.md`, `prd-phase-5-sandbox.md`, `prd-phase-6-subagents.md`,
  `prd-phase-7-tui.md`.
- Roadmap & milestones: `../roadmap.md` (Milestone M1).
