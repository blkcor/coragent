# Coragent — Architecture (Conceptual)

> The conceptual north-star for the whole project. It describes **components,
> boundaries, responsibilities, and data flow** — not concrete types. Every phase
> PRD in `docs/prd/` references this document.
>
> **Concrete data types and interface signatures are intentionally absent.** They
> are designed per-task, immediately before each phase is implemented (at planning
> time). This keeps the architecture stable while the wire-level shapes stay free
> to adapt to what each task actually needs.

## 1. What we are building

A **coding agent harness in Go**, in the spirit of Claude Code / the Claude Agent
SDK. The model is fixed and swappable; the *harness* — the runtime scaffolding
around the model — is the product. The harness is exposed as an **SDK**; the
terminal UI is the first frontend built on that SDK.

**Harness engineering** is the discipline: context management, tool design, the
agent loop, permission gating, hooks, sandboxing, and subagent orchestration are
where agent quality is won or lost.

### Goals (v1)

- Personal daily-driver coding agent. Reliability and ergonomics over breadth.
- OpenAI-compatible backend first (reaches OpenAI/Codex, DeepSeek, Kimi, local).
- Full interactive TUI frontend.
- Decoupled agent-core SDK ("the harness"); TUI is a replaceable client.
- Real OS sandbox for shell execution (`sandbox-exec` on macOS).
- **Soft** human-in-the-loop permission layer **and** a **hard** hooks layer.
- Subagents/tasks.

### Non-goals (v1)

- Multi-provider abstraction beyond the single provider seam.
- Non-macOS kernel sandbox backends (the seam allows them later).
- MCP, web UI, plugin marketplace, billing, telemetry.

## 2. Layering

```
╔══════════════════════════════════════════════════════════════╗
║  FRONTENDS  (interchangeable clients of the SDK)             ║
║   TUI (Bubble Tea)  ·  one-shot CLI  ·  your own Go program  ║
╚══════════════════════════════════════════════════════════════╝
                ▲ events          ▼ prompts / permission decisions
╔══════════════════════════════════════════════════════════════╗
║  HARNESS  —  exposed as the SDK  (UI-agnostic)              ║
║  ── Public SDK surface ──────────────────────────────────   ║
║   Session · Run · Tool registry · Config · Event stream     ║
║   Hook registration · Permission policy · Subagent spawn    ║
║  ── Internal machinery ──────────────────────────────────   ║
║   Agent loop (gather→act→verify)   Context manager          ║
║   Tool executor + middleware       Permission engine        ║
║   Hooks engine (hard gates)        Subagent orchestrator    ║
╚══════════════════════════════════════════════════════════════╝
                ▲                          ▼
╔══════════════════════════════════════════════════════════════╗
║  PLUGGABLE BACKENDS  (seams, swappable implementations)     ║
║   Provider: OpenAI-compat client (stream + tool-calls)      ║
║   Sandbox:  sandbox-exec (macOS Seatbelt)                   ║
║   Tools:    read · write · edit · shell · grep · glob · task ║
╚══════════════════════════════════════════════════════════════╝
```

**Three invariants:**

1. Frontends depend on the harness; **the harness never imports a frontend**.
2. The **SDK surface is the only public contract**. Internal machinery is free to
   change behind it.
3. **Provider, Sandbox, and each Tool are seams** (interfaces). OpenAI-compat and
   sandbox-exec are merely the first implementations.

## 3. Repository layout

```
coragent/
├── cmd/coragent/      # TUI binary (frontend)
├── pkg/agent/         # PUBLIC SDK surface (importable by anyone)
├── internal/          # harness machinery (not importable outside module)
│   ├── loop/          #   agent loop
│   ├── context/       #   context/history manager
│   ├── executor/      #   tool executor + middleware chain
│   ├── permission/    #   permission engine
│   ├── hooks/         #   hooks engine
│   ├── sandbox/       #   sandbox backends (sandbox-exec)
│   ├── provider/      #   OpenAI-compat client
│   ├── tools/         #   built-in tools
│   └── subagent/      #   subagent orchestrator
├── tui/               # Bubble Tea frontend components
└── docs/              # architecture.md · roadmap.md · prd/
```

**Rule:** public concepts live in `pkg/agent`; everything in `internal/` is
implementation. The TUI imports the public SDK only — never `internal/`.

## 4. Domain concepts

Described in words. The precise shapes are fixed per-task at implementation time.

- **Conversation** — the ordered history of the exchange: the system framing, user
  turns, assistant turns, and the results of any tools the assistant invoked.
- **Tool call** — the assistant's request to use a named capability with some
  arguments. The model proposes; the harness disposes.
- **Tool result** — what a tool returns to the conversation, marked as success or
  error so the model can react.
- **Tool** — a named capability the model may invoke: a human-readable purpose, a
  declared shape for its arguments, and the behavior that runs. Tools are a seam:
  built-ins and subagent-provided tools are registered the same way.
- **Provider** — the model backend. Given the conversation and the available
  tools, it streams back the assistant's reply, including any tool calls. The
  first implementation speaks the OpenAI-compatible protocol.
- **Sandbox** — the confinement under which shell commands run, governed by a
  policy describing what the command may read, write, and reach over the network.

## 5. The event model (harness → frontend)

The harness is driven by a single **run** entry point and communicates outward as
a **stream of typed events** the frontend consumes; the frontend never reads
internal state directly. Conceptually the stream carries:

- **assistant text** as it streams in,
- **tool-call lifecycle** — a call started, a call finished (with its result),
- **status changes** — thinking, calling a tool, idle,
- **turn finished** and **error** signals that end the stream.

One event is special: a **permission request**. It is the single point where the
harness must wait on a human. It is modeled as an event that carries a way to
**reply** — the frontend renders a prompt and sends back a decision (allow/deny,
optionally "remember this", optionally edited arguments). Modeling it as an event
with an embedded reply path keeps the harness UI-agnostic: it simply waits for the
answer, and any frontend — TUI, CLI, automated test — can answer however it likes.

**Why this matters:** the entire decoupling rests on this stream. The harness
emits and waits; frontends render and answer. Swap the frontend, the harness is
unchanged.

## 6. The tool-execution chokepoint

Every tool call flows through **one** middleware chain in the executor. This single
path is where all safety lives:

```
ToolCall
   │
   ▼
[ PreToolUse hooks ]      ← HARD. block ⇒ abort, model cannot override
   │
   ▼
[ Permission engine ]     ← SOFT. allow / deny / ask-the-human
   │
   ▼
[ Sandbox ]               ← shell tools only: run inside the sandbox policy
   │
   ▼
[ Tool execution ]
   │
   ▼
[ PostToolUse hooks ]     ← HARD. inspect / annotate / block the result
   │
   ▼
ToolResult → back into the loop
```

The loop owns this chokepoint behind a single seam: it hands the executor one
tool call and gets back one result. The executor's job is to run that call through
every stage in exactly this order. Crucially, the stages that must talk to a human
(permission) are given access to the **same live event stream the frontend is
draining**, so a permission request reaches the user and its answer flows back
without the loop having to mediate.

This is why the chain is built *first* (with inert stages) and filled in later:
there must be exactly **one** execution path, so permission, hooks, and sandbox
can never be bypassed.

**Permission vs Hooks — the distinction, stated once:**

- **Permission** asks a *human* (allow / deny / ask, with remembered rules and
  modes). It is advisory and user-driven — a convenience, not a security boundary.
- **Hooks** enforce *unconditionally*. A blocking hook stops the call and the
  model has no recourse. Hooks (with the sandbox) are the real guardrails.

## 7. Concurrency model

- A run executes in its own goroutine; its event stream closes when the turn
  finishes or errors.
- One in-flight turn per session (serialized). Subagents get their own session.
- Cancellation propagates from the caller down through the provider stream and any
  running tool or command.
- Within a turn, tool calls execute **sequentially** in v1 (the model may request
  several; they run in order). Parallel tool execution is explicitly out of scope
  for v1.

## 8. Conventions (all phases)

- **Errors:** tool failures surface as error *results* the model can see and react
  to — not as crashes. Only genuine programmer errors abort hard.
- **Cancellation:** every blocking operation is cancellable.
- **Config:** a single settings file, merged home-then-project (project overrides
  home), loaded into a configuration the SDK caller can also supply directly.
- **Testing:** every component is testable offline against fakes — a fake provider
  scripts model replies; a fake frontend drives the stream and answers permission
  requests. No network, no real model in tests.
- **Naming:** public concepts in user-facing terms; no invented abbreviations.
- **Logging:** structured, to stderr/file, never onto the frontend event stream.

## 9. Definition of done (every phase)

A phase is done when: its public surface is documented and stable; its behavior is
covered by offline tests against fakes; the acceptance scenarios in its PRD pass;
and it integrates with prior phases **without changing their public contracts**.

## 10. A note on types

There are deliberately **no concrete Go type or interface definitions in this
document or in the phase PRDs.** The PRDs describe *what* each phase must do and
*why*, in user-story terms. The exact data shapes, interface signatures, and
package APIs are designed in the planning step that immediately precedes each
phase's implementation — when the surrounding code is in hand and the right shape
is clearest. The architecture fixes the boundaries; the boundaries' wire format is
a task-time decision.
