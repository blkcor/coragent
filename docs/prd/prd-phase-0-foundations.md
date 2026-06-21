# Phase 0 — Foundations (PRD)

> Product requirements for the root of the dependency tree in
> [`../roadmap.md`](../roadmap.md). This phase obeys
> [`../architecture.md`](../architecture.md) as the canonical spec; where the two
> disagree, the architecture wins until amended.
>
> Per [`../architecture.md`](../architecture.md) §10, this document describes
> **what** the phase delivers and **why**, in behavioral terms. Concrete data
> shapes, interface signatures, and package APIs are designed at planning time,
> immediately before implementation — they are intentionally absent here.

---

## 1. Introduction / Overview

Phase 0 lays the slab everything else is poured on. It gives a developer the
minimum, trustworthy foundation needed to start building the coragent harness:

1. **A scaffolded repository** with the public/internal boundary the
   architecture mandates, so work on later phases starts from a clean,
   compiling, correctly-shaped tree.
2. **A single configuration system** — one settings file, discovered in the
   user's home and in the project, merged so a project's choices override the
   user's personal defaults. A developer embedding the harness can also supply
   configuration directly, without any files on disk.
3. **A working model backend** that speaks the OpenAI-compatible protocol,
   streams the assistant's reply back as it is produced, and reassembles the
   model's tool-call requests into complete, dispatchable form.

Phase 0 has **no agent loop, no tools, no UI**. What it produces is a library
that can take a conversation plus a set of available tools and stream back the
model's reply — text and tool calls — from a real or faked OpenAI-compatible
endpoint. This is the lower half of Milestone **M1 — "It talks"**
([`../roadmap.md`](../roadmap.md)); Phase 1
([`prd-phase-1-agent-loop.md`](prd-phase-1-agent-loop.md)) wires these primitives
into a turn loop.

**The problem it solves.** The model backend is the one seam Phase 1 cannot be
built without. Getting streaming and tool-call reassembly correct here — proven
against recorded transcripts, with no network — de-risks the heart of the project
before any loop logic exists. And settling the configuration story first means
every later phase reads its settings from one well-understood place.

**Personas.** The **harness/SDK developer (primary)** sets up the repository and
builds the agent runtime on top of it; they need a sound skeleton, one obvious
place to configure the model, and a backend they can trust to stream replies.
The **SDK-embedding developer (secondary)** imports the harness into their own Go
program and benefits from constructing configuration in code and skipping file
discovery. The **end user (indirect)** never touches Phase 0 but benefits
downstream from a reliable foundation, a single settings file, and live replies.

---

## 2. Goals

- Stand up the repository skeleton with the architecture's public/internal
  boundary, building and passing static checks from a fresh checkout.
- Establish one shared, documented vocabulary for the core concepts (conversation,
  turn, tool, tool call, tool result, model backend, run events) including the
  permission request/decision pair, declared now and acted on later.
- Deliver one configuration system: a single settings file, home-and-project
  discovery, project-over-home field-level merge, documented defaults, loud
  failure on malformed input, credentials drawn from the environment, and direct
  in-code construction.
- Deliver an OpenAI-compatible model backend that streams assistant text
  incrementally and reassembles fragmented tool requests into complete ones.
- Make the backend resilient: retry transient failures with backoff, report
  permanent failures distinctly, surface mid-stream errors cleanly, and cancel
  promptly.
- Make the whole foundation testable entirely offline against a faked backend and
  recorded transcripts, with a small runnable streaming demonstration.

---

## 3. User Stories

### US-001: Architecture-shaped repository layout

**Description:** As a harness developer, I want the repository to follow the
layout and the public/internal boundary defined in the architecture, so that
later phases drop into predictable homes and the public contract can never
accidentally depend on private machinery.

**Acceptance Criteria:**
- [ ] Every directory the architecture prescribes exists in a fresh checkout.
- [ ] Directories whose phase has not landed still build, as documented placeholders.
- [ ] Build, typecheck, and unit tests pass.

### US-002: Clean build and static checks from day one

**Description:** As a harness developer, I want the project to build and pass
static checks from day one, even where later phases have not landed, so that a
broken tree is always my own change and never a pre-existing gap.

**Acceptance Criteria:**
- [ ] A fresh checkout builds and passes the standard static checks with no edits.
- [ ] Placeholder directories for unlanded phases do not break the build.
- [ ] Build, typecheck, and unit tests pass.

### US-003: Public surface importable on its own

**Description:** As a harness developer, I want the public surface to be
importable on its own, free of any dependency on internal machinery or on a
frontend, so that anyone embedding the harness pulls in only the contract, not
the guts.

**Acceptance Criteria:**
- [ ] The public surface depends on no internal machinery and no frontend.
- [ ] This independence is verified automatically, not left to convention.
- [ ] Build, typecheck, and unit tests pass.

### US-004: One shared vocabulary for core concepts

**Description:** As a harness developer, I want one shared, documented vocabulary
for the core concepts — the conversation and its turns, a tool and its
description, a tool call and its result, the model backend, and the stream of
things that happen during a run — so that every later phase speaks the same
language and integrations line up without translation.

**Acceptance Criteria:**
- [ ] The conversation, turn, tool, tool call, tool result, model backend, and run-event concepts are all present on the public surface.
- [ ] Each is documented and named in user-facing terms with no invented abbreviations.
- [ ] Build, typecheck, and unit tests pass.

### US-005: Permission request/decision shape declared early

**Description:** As a harness developer, I want the human-in-the-loop moment — a
permission request the harness pauses on, and the decision sent back —
expressible in the shared vocabulary from the start, even though nothing in this
phase acts on it, so that the permission and frontend phases have a stable shape
to build against rather than reshaping the core later.

**Acceptance Criteria:**
- [ ] A permission request carries a way to send a decision back: allow or deny, optionally "remember this", optionally with edited arguments.
- [ ] The shape is present and documented even though no Phase 0 component emits or acts on it.
- [ ] Build, typecheck, and unit tests pass.

### US-006: Single settings file format

**Description:** As a harness developer, I want a single settings file format
that configures the harness, so that there is one obvious place to set the model
backend and its options.

**Acceptance Criteria:**
- [ ] A single settings file configures the model backend and its options.
- [ ] The format is documented with its available fields and defaults.
- [ ] Build, typecheck, and unit tests pass.

### US-007: Home-and-project discovery with project precedence

**Description:** As a harness developer, I want settings discovered in both my
home directory and the current project, with the project's settings taking
precedence field by field, so that I keep personal defaults once and let each
project override only what it needs.

**Acceptance Criteria:**
- [ ] When settings exist only in home, the home settings apply.
- [ ] When settings exist only in the project, the project settings apply.
- [ ] When both exist with overlapping fields, the project's value wins per overlapping field while non-overlapping home fields are kept.
- [ ] Build, typecheck, and unit tests pass.

### US-008: Missing settings file is harmless

**Description:** As a harness developer, I want a missing settings file to be
harmless — falling back to the other location or to documented defaults — so that
the harness runs out of the box without ceremony.

**Acceptance Criteria:**
- [ ] When neither location has a settings file, loading succeeds and documented defaults apply.
- [ ] A missing file in one location falls back to the other location.
- [ ] Build, typecheck, and unit tests pass.

### US-009: Malformed settings file fails loudly

**Description:** As a harness developer, I want a malformed settings file to fail
loudly and name the offending file, so that I fix the right file immediately
instead of chasing a silent misconfiguration.

**Acceptance Criteria:**
- [ ] A present settings file with invalid content causes loading to fail.
- [ ] The reported error names the offending file.
- [ ] Build, typecheck, and unit tests pass.

### US-010: Credentials drawn from the environment

**Description:** As a harness developer, I want my model credentials drawn from
the environment rather than written in plain text in the settings file, so that I
can commit project settings without leaking secrets.

**Acceptance Criteria:**
- [ ] When credentials reference an environment variable, the value is resolved from the environment at load.
- [ ] No credential value is stored literally in the settings file.
- [ ] Build, typecheck, and unit tests pass.

### US-011: Direct in-code configuration

**Description:** As an SDK-embedding developer, I want to supply configuration
directly in code and skip file discovery entirely, so that I can embed the
harness in a program that manages its own settings.

**Acceptance Criteria:**
- [ ] When configuration is supplied in code, no file discovery happens.
- [ ] The supplied configuration is honored as given.
- [ ] Build, typecheck, and unit tests pass.

### US-012: Stream a reply for a conversation plus tools

**Description:** As a harness developer, I want to hand the backend a conversation
plus the tools currently available and get the assistant's reply streamed back,
so that the eventual loop can render the answer as it is produced rather than
waiting for the whole message.

**Acceptance Criteria:**
- [ ] Given a conversation and a set of available tools, the backend returns the assistant's reply as a stream.
- [ ] The reply is consumable before the whole message has arrived.
- [ ] Build, typecheck, and unit tests pass.

### US-013: Incremental assistant text

**Description:** As an end user served through the developer, I want assistant
text to appear incrementally as the model produces it, so that the agent feels
responsive instead of silent-then-sudden.

**Acceptance Criteria:**
- [ ] Assistant text arrives incrementally and in order as the model produces it.
- [ ] No part of the text waits on the completion of the whole message.
- [ ] Build, typecheck, and unit tests pass.

### US-014: Reassembled, dispatchable tool calls

**Description:** As a harness developer, I want the model's tool requests — which
the backend receives in fragments — reassembled into whole, dispatchable tool
calls before they reach me, so that the loop can act on a complete request
without stitching fragments itself.

**Acceptance Criteria:**
- [ ] Streamed tool-call fragments are reassembled into one complete tool call before it is surfaced.
- [ ] Each surfaced tool call has its name and arguments fully assembled.
- [ ] Multiple distinct tool requests in one reply are kept separate and surfaced in the order they first appeared.
- [ ] Build, typecheck, and unit tests pass.

### US-015: Report how the reply ended

**Description:** As a harness developer, I want to know how the reply ended —
finished normally, stopped to call tools, or cut off at a length limit — so that
the loop can decide what to do next.

**Acceptance Criteria:**
- [ ] When the stream ends, the consumer learns whether it finished normally, stopped to call tools, or was cut off at a length limit.
- [ ] The ending reason is distinguishable across these cases.
- [ ] Build, typecheck, and unit tests pass.

### US-016: Per-request model and sampling options with fallback

**Description:** As a harness developer, I want the model and the sampling options
to come from the request when set and otherwise fall back to my configured
defaults, so that I tune behavior per call without restating defaults every time.

**Acceptance Criteria:**
- [ ] When a request sets the model or sampling options, those values are used.
- [ ] When a request omits them, the configured defaults are used instead.
- [ ] Build, typecheck, and unit tests pass.

### US-017: Retry transient failures with backoff

**Description:** As a harness developer, I want transient backend failures — rate
limiting and temporary server errors — retried automatically with sensible
backoff before any of the reply is delivered, so that a brief hiccup doesn't
surface as a failed turn.

**Acceptance Criteria:**
- [ ] A transient failure before any reply is delivered is retried with backoff up to the configured limit.
- [ ] Any "retry after" hint from the backend is honored.
- [ ] A failure is surfaced only once the retry limit is exhausted, with no already-delivered content duplicated.
- [ ] Build, typecheck, and unit tests pass.

### US-018: Distinct, non-retried permanent failures

**Description:** As a harness developer, I want permanent failures — bad
credentials, malformed requests — reported clearly and distinguishably without
pointless retries, so that I can tell "try again" from "fix your setup" at a
glance.

**Acceptance Criteria:**
- [ ] A permanent failure such as bad credentials or a malformed request is not retried.
- [ ] The failure is reported as a distinct error the consumer can recognize and act on.
- [ ] Build, typecheck, and unit tests pass.

### US-019: Clean mid-stream error handling

**Description:** As a harness developer, I want a failure that happens partway
through a stream surfaced as an error that ends the stream cleanly, rather than a
crash or a silently truncated reply, so that the loop can react instead of
hanging or panicking.

**Acceptance Criteria:**
- [ ] A failure partway through a streaming reply ends the stream with an error rather than a crash or a silent truncation.
- [ ] Text already delivered before the failure stands.
- [ ] No half-formed tool request is delivered.
- [ ] Build, typecheck, and unit tests pass.

### US-020: Prompt cancellation of an in-flight reply

**Description:** As a harness developer, I want to cancel an in-flight reply and
have the backend stop promptly — aborting the request and releasing its resources
— so that the user can interrupt a long generation without leaks.

**Acceptance Criteria:**
- [ ] Cancelling an in-flight reply aborts the underlying request promptly.
- [ ] The stream ends after cancellation and no resources are leaked.
- [ ] Build, typecheck, and unit tests pass.

### US-021: Offline-testable foundation

**Description:** As a harness developer, I want every part of this foundation
testable offline against a faked backend, with no network and no real model, so
that the test suite is fast, deterministic, and runnable anywhere.

**Acceptance Criteria:**
- [ ] The whole suite passes offline against a faked backend and recorded transcripts, with no network and no real model.
- [ ] Coverage includes fragmented tool requests reassembling into the exact expected calls.
- [ ] Build, typecheck, and unit tests pass.

### US-022: Runnable streaming demonstration

**Description:** As a harness developer, I want a tiny runnable demonstration that
streams a reply from either a real endpoint or the fake, so that I can see the
foundation working end to end before any loop exists.

**Acceptance Criteria:**
- [ ] The demonstration, pointed at any OpenAI-compatible endpoint or the fake, prints streamed text as it arrives.
- [ ] It prints decoded tool requests with their assembled arguments.
- [ ] Build, typecheck, and unit tests pass.

---

## 4. Functional Requirements

1. The system must provide a repository layout matching the architecture's
   prescribed directories, with documented placeholders so unlanded phases build.
2. The system must keep the public surface free of any dependency on internal
   machinery or any frontend, and verify this automatically.
3. The system must expose one shared, documented vocabulary for the conversation,
   turn, tool, tool call, tool result, model backend, and run events, named in
   user-facing terms.
4. The system must include the permission request/decision shape in that
   vocabulary — carrying allow/deny, optional "remember this", and optional edited
   arguments — without emitting or acting on it in this phase.
5. The system must configure the harness from a single settings file with a
   documented format.
6. The system must discover settings in both the home directory and the current
   project and merge them field by field with the project value taking precedence.
7. The system must treat a missing settings file as harmless, falling back to the
   other location or to documented defaults.
8. The system must fail loudly on a malformed settings file and name the offending
   file in the error.
9. The system must resolve model credentials from the environment rather than from
   literal values in the settings file.
10. The system must accept configuration supplied directly in code and, in that
    case, perform no file discovery.
11. The system must accept a conversation plus the available tools and return the
    assistant's reply as a consumable stream.
12. The system must deliver assistant text incrementally and in order as it is
    produced.
13. The system must reassemble fragmented tool requests into complete, dispatchable
    tool calls, keep multiple distinct requests separate, and surface them in first
    appearance order.
14. The system must report how each reply ended: finished normally, stopped to call
    tools, or cut off at a length limit.
15. The system must use per-request model and sampling options when set and fall
    back to configured defaults when omitted.
16. The system must retry transient failures with backoff up to a configured limit,
    honor any "retry after" hint, and never duplicate already-delivered content.
17. The system must report permanent failures as distinct, non-retried errors.
18. The system must surface a mid-stream failure as a clean stream-ending error,
    preserving already-delivered text and delivering no half-formed tool request.
19. The system must abort an in-flight reply promptly on cancellation and release
    its resources.
20. The system must be exercisable entirely offline against a faked backend and
    recorded transcripts, and must ship a runnable streaming demonstration.

---

## 5. Non-Goals (Out of Scope)

Deferred to named phases:

- The agent loop, multi-turn orchestration, and context management —
  [`prd-phase-1-agent-loop.md`](prd-phase-1-agent-loop.md).
- The tool registry, the execution middleware, and the built-in tools —
  [`prd-phase-2-tools-executor.md`](prd-phase-2-tools-executor.md).
- Permission behavior; this phase only names the request/decision shape —
  [`prd-phase-3-permission.md`](prd-phase-3-permission.md).
- The hooks engine — [`prd-phase-4-hooks.md`](prd-phase-4-hooks.md).
- The sandbox backend; this phase only names the sandbox concept —
  [`prd-phase-5-sandbox.md`](prd-phase-5-sandbox.md).
- Subagents — [`prd-phase-6-subagents.md`](prd-phase-6-subagents.md).
- The terminal frontend — [`prd-phase-7-tui.md`](prd-phase-7-tui.md).

Explicit non-goals for this phase:

- Any model backend beyond the single OpenAI-compatible seam —
  [`../architecture.md`](../architecture.md) §1.
- Token counting and context-window accounting — owned by Phase 1.
- Streaming partial tool-call arguments to a UI, surfacing model "thinking"
  output, usage/cost accounting, and multiple alternative completions — see §9.

---

## 6. Design Considerations

- **Single obvious place to configure.** The settings file is the one place a
  developer points the harness at a model; defaults are documented so the
  out-of-the-box experience needs no file at all.
- **Stable vocabulary up front.** Naming the conversation, run events, and the
  permission request/decision pair early lets later phases build against a fixed
  shape instead of reshaping the core; concepts use user-facing terms with no
  invented abbreviations ([`../architecture.md`](../architecture.md) §8).
- **Demonstration as the felt experience.** The runnable demo is the first place
  streaming text and assembled tool calls are seen working end to end, doubling as
  living documentation of the foundation.

---

## 7. Technical Considerations

- **Architecture invariants.** The public surface is the only public contract and
  must never import internal machinery or a frontend; the model backend is a seam
  whose first implementation speaks the OpenAI-compatible protocol
  ([`../architecture.md`](../architecture.md) §2).
- **Minimal external surface.** The OpenAI wire format is simple enough to speak
  directly, keeping the backend seam clean and the dependency surface small,
  consistent with the v1 non-goals.
- **Cancellation and errors as conventions.** Every blocking operation is
  cancellable, and backend failures surface as errors the consumer can react to
  rather than crashes ([`../architecture.md`](../architecture.md) §8).
- **Offline-first testing.** A faked backend scripts replies and recorded
  transcripts drive reassembly, so the suite runs with no network and no real
  model.
- **Conceptual only.** Per [`../architecture.md`](../architecture.md) §10, no
  concrete data shapes or interface signatures are fixed here; they are designed at
  planning time immediately before implementation.
- **Dependencies.** Upstream: none — Phase 0 is the root of the dependency graph.
  Downstream: everything, with [`prd-phase-1-agent-loop.md`](prd-phase-1-agent-loop.md)
  the first consumer driving the backend through the turn loop.

---

## 8. Success Metrics

Aligned with [`../architecture.md`](../architecture.md) §9. Phase 0 is done when:

- The public surface is documented and stable: the shared vocabulary and run
  events are present, documented, named in user-facing terms, and ready to build
  on.
- Behavior is covered by offline tests against fakes: the whole suite runs with no
  network and no real model, deterministically and fast.
- The acceptance criteria in §3 pass — the repository builds and checks clean,
  configuration merges and fails correctly, and the backend streams text,
  reassembles tool calls, reports how replies end, retries transients, fails
  permanents distinctly, handles mid-stream errors cleanly, and cancels promptly.
- It integrates without changing prior contracts — trivially satisfied, as there
  is no prior phase.
- The demonstration runs, streaming a reply and any tool calls against either a
  real endpoint or the fake, reproducibly and without credentials in CI.

---

## 9. Open Questions

- **Model "thinking" output.** Some OpenAI-compatible endpoints stream a separate
  reasoning channel. Phase 0 ignores it; it can be surfaced later additively, once
  a backend we use emits it.
- **Live tool-call argument streaming.** Phase 0 delivers only completed tool
  calls. Streaming partial arguments for a richer UI is left to
  [`prd-phase-7-tui.md`](prd-phase-7-tui.md) if the experience warrants it; it is
  purely additive.
- **Usage and cost accounting.** Streamed usage figures are out of scope here;
  Phase 1's context management will decide whether to request and surface them.
- **Multiple alternative completions.** Phase 0 assumes a single reply per request.
  Revisit only if a feature needs alternatives.
- **Backend-specific quirks.** DeepSeek, Kimi, and local servers vary slightly in
  how they stream tool calls. The reassembly tolerates the common variations;
  per-endpoint transcripts should be added as each backend is onboarded.
- **Credential resolution policy.** Phase 0 warns and leaves the credential empty
  when its environment variable is unset, so a missing key fails loudly at the
  first request. If a louder failure at load time is preferred, revisit in a
  configuration-hardening pass.
- **Sandbox concept fields.** The sandbox vocabulary is named here as a
  placeholder; its final shape is owned by
  [`prd-phase-5-sandbox.md`](prd-phase-5-sandbox.md).
