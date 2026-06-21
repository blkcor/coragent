# Coragent — Agent Guidelines

Coragent is a **coding-agent harness in Go** — a reusable SDK that drives LLM-powered agents through tool use, with a terminal UI as the first frontend. Read `docs/architecture.md` before touching any code.

## Quick Pointers

| What | Where |
|------|-------|
| Architecture (conceptual north-star) | `docs/architecture.md` |
| Phased roadmap & milestones | `docs/roadmap.md` |
| Phase PRDs (user stories + acceptance) | `docs/prd/prd-phase-*.md` |
| Public SDK surface | `pkg/agent/` |
| Internal harness machinery | `internal/` |
| TUI frontend | `tui/` + `cmd/coragent/` |

## Module & Toolchain

- **Module:** `github.com/blkcor/coragent`
- **Go:** 1.22+
- **Linter:** `golangci-lint run ./...`
- **Test:** `go test ./...`
- **Build:** `go build ./cmd/coragent`
- **Format:** `gofmt -w .` (or `goimports`)

## Repository Layout

```
coragent/
├── cmd/coragent/       # TUI binary — the first frontend
├── pkg/agent/          # PUBLIC SDK surface (importable by anyone)
├── internal/           # harness internals (not importable outside module)
│   ├── loop/           #   agent loop (gather→act→verify)
│   ├── context/        #   context / history manager
│   ├── executor/       #   tool executor + middleware chain
│   ├── permission/     #   permission engine (soft, human-in-the-loop)
│   ├── hooks/          #   hooks engine (hard, unconditional gates)
│   ├── sandbox/        #   sandbox backends (sandbox-exec on macOS)
│   ├── provider/       #   OpenAI-compatible streaming client
│   ├── tools/          #   built-in tools
│   └── subagent/       #   subagent orchestrator
├── tui/                # Bubble Tea components
└── docs/               # architecture · roadmap · prd/
```

## Invariants (never break these)

1. **The harness never imports a frontend.** `pkg/agent` and `internal/` must not reference `tui/` or `cmd/`.
2. **The TUI imports only `pkg/agent`.** It never reaches into `internal/`.
3. **One execution path.** Every tool call flows through exactly one middleware chain (hooks → permission → sandbox → tool → post-hooks). No second door, no bypass.
4. **Provider, Sandbox, and Tool are seams.** OpenAI-compat and sandbox-exec are the first implementations — never the only allowed ones.
5. **A phase must not change a prior phase's public contract.** It integrates without breaking what came before.

## Go Conventions

### Naming

- Public API uses **user-facing concept names** (Conversation, ToolCall, ToolResult, Session). No invented abbreviations.
- Package names follow Go convention: short, lowercase, no underscores.
- Unexported helpers stay in `internal/`; anything in `pkg/agent/` is a public promise.

### Errors

- **Tool failures → `ToolResult{IsError: true}`.** The model sees the error and reacts. Never panic on a tool failure.
- **Only programmer errors panic** (e.g., duplicate tool registration at startup).
- Wrap errors with `%w` for unwrapping.

### Context & Cancellation

- Every blocking function takes `context.Context` as its first argument.
- Cancellation propagates from caller through provider streams, running tools, and shell commands.

### Concurrency

- One in-flight turn per `Session` (serialized). Subagents get their own `Session`.
- Tool calls within a turn execute **sequentially** in v1.
- `Run()` returns a read-only event channel; it closes on turn-finished or error.

### Config

- Single `settings.json`, merged home (`~/.coragent/settings.json`) then project (`.coragent/settings.json`). Project overrides home.
- SDK callers may supply the config struct directly instead of reading a file.

### Logging

- Structured logging via `log/slog`.
- Log to stderr or a file. **Never emit log output onto the event stream** — that channel is for the frontend.

## Testing

- **Every component is testable offline against fakes.** No network calls, no real model in tests.
- **Fake Provider:** scripts model replies (text deltas + tool calls) for deterministic test scenarios.
- **Fake Frontend:** drains the event channel, auto-answers permission requests with scripted decisions.
- Use table-driven tests (`t.Run` subtests) per package.
- Temp directories via `t.TempDir()` for file-tool tests — never touch real user files.
- External binary dependencies (e.g., `rg` for grep tool) degrade gracefully; tests handle absence.

## The Event Stream

The harness communicates outward as a **stream of typed events**. The frontend consumes it; it never reads internal state.

Events conceptually:
- Assistant text (streaming deltas)
- Tool-call started / finished (with result)
- Status changes (thinking, calling tool, idle)
- Turn finished / error (closes the channel)
- **Permission request** — the one event where the harness blocks on a human. It carries a reply path; the frontend answers (allow / deny / remember / edit args).

This stream is the entire decoupling. Swap the frontend, the harness is unchanged.

## The Tool-Execution Chokepoint

```
ToolCall
   │
   ▼
[ PreToolUse hooks ]     ← HARD gate — block ⇒ abort, model cannot override
   │
   ▼
[ Permission engine ]    ← SOFT gate — allow / deny / ask the human
   │
   ▼
[ Sandbox ]              ← shell tools only — OS-level confinement
   │
   ▼
[ Tool.Execute ]
   │
   ▼
[ PostToolUse hooks ]    ← HARD gate — inspect / annotate / block result
   │
   ▼
ToolResult → back into the loop
```

**Permission vs Hooks:** permission asks a human (advisory, convenience); hooks enforce unconditionally (the real guardrail). A blocking hook stops the call even in bypass mode.

## Phase Delivery

Phases are built in order; each is runnable on the one before it.

| Milestone | Phases | Meaning |
|-----------|--------|---------|
| M1 "It talks" | 0 + 1 | Multi-turn loop with a streaming provider |
| M2 "It acts" | 2 + 3 | Agent reads/edits/runs with human-in-the-loop |
| M3 "It's safe" | 4 + 5 | Hard hooks + OS sandbox enforced |
| M4 "It scales" | 6 | Subagent delegation |
| M5 "It ships" | 7 | Full TUI daily-driver |

### Definition of Done (every phase)

A phase is done when:
- [ ] Public surface documented and stable
- [ ] Behavior covered by offline tests against fakes
- [ ] PRD acceptance scenarios pass
- [ ] Integrates with prior phases without changing their public contracts

## Types Are Designed at Build Time

The architecture and PRDs describe **what** and **why** — not concrete Go types. Interface signatures and data shapes are designed in the planning step immediately before each phase's implementation, when surrounding code is in hand. The architecture fixes boundaries; the wire format is a task-time decision.

## CLI Environment

Prefer these tools (consistent with the developer's environment):

| Use | Not |
|-----|-----|
| `rg` | `grep` |
| `fd` | `find` |
| `eza` | `ls` |
| `sd` | `sed` |
| `golangci-lint` | manual vet invocations |
