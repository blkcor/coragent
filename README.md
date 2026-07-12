# Coragent

Coragent is a coding-agent harness in Go: a reusable SDK that drives
LLM-powered agents through tool use, with a terminal UI as the first frontend.

The repository includes the Phase 0–6 harness and the archived Phase 7 TUI.
The completed Phase 7 change record is
[`openspec/changes/archive/2026-07-12-phase-7-tui/tasks.md`](openspec/changes/archive/2026-07-12-phase-7-tui/tasks.md).

## Build and test

```bash
go build ./cmd/coragent
go test ./...
```

The full release gate also runs the race detector, vet, golangci-lint, strict
OpenSpec validation, and the PTY terminal-restoration suite.

## Run the TUI

Create `~/.coragent/settings.json` and optionally override fields in the
project-local `.coragent/settings.json`:

```json
{
  "model": {
    "name": "gpt-4.1",
    "base_url": "https://api.openai.com/v1",
    "api_key": "${OPENAI_API_KEY}"
  },
  "permission": {
    "mode": "default"
  }
}
```

Then run Coragent from the project it should work in:

```bash
export OPENAI_API_KEY="your-api-key"
go run ./cmd/coragent
```

Home settings load first and project settings override them field by field.
Credential environment references are resolved during loading but are excluded
from public descriptors, formatting, logs, and serialization.

See [`docs/tui.md`](docs/tui.md) for shortcuts, permission modes, terminal
support, accessibility options, and copy/scroll behavior.

## Public SDK bootstrap

First-party clients can use the same validated construction path as the binary:

```go
settings, err := agent.LoadSettings()
if err != nil {
    return err
}
session, err := agent.Bootstrap(settings, agent.BootstrapOptions{
    WorkingDirectory: projectRoot,
})
if err != nil {
    return err
}
defer session.Close(context.Background())
```

SDK embedders can continue constructing `SessionConfig` directly. Existing
`Provider`, `SessionConfig`, `Session.Run`, and legacy `RunEvent` clients remain
source-compatible. New frontends may opt into `Session.RunObserved` and
`Session.Describe`; both run APIs share one execution path and one in-flight
guard.

## Architecture

```text
coragent/
├── cmd/
│   ├── coragent/       # full-screen TUI binary
│   └── demo/           # small offline streaming demonstration
├── pkg/agent/          # public SDK surface
├── internal/
│   ├── loop/           # canonical gather → act → verify loop
│   ├── context/        # conversation and usage manager
│   ├── executor/       # one tool-execution middleware chain
│   ├── permission/     # soft human-in-the-loop decisions
│   ├── hooks/          # unconditional hard gates
│   ├── sandbox/        # command confinement backends
│   ├── provider/       # OpenAI-compatible streaming provider
│   ├── tools/          # built-in tools and prepared file actions
│   └── subagent/       # delegated-session orchestration
├── tui/                # Bubble Tea frontend; imports only pkg/agent
└── docs/               # architecture, roadmap, PRDs, and user notes
```

Every tool call follows exactly one path:

```text
before-tool hooks → permission → sandbox → tool → after-tool hooks
```

The harness never imports a frontend, and the TUI never reaches into
`internal/`. Typed events are the entire decoupling boundary.

## Implemented capabilities

- OpenAI-compatible streaming with legacy and optional rich provider paths.
- Versioned observed events for assistant, tool, permission, usage, omission,
  hook, subagent, warning, error, and terminal lifecycle facts.
- Bounded structured action previews and identity-bound, fail-closed file
  commits for `write_file` and `edit_file`.
- Exactly-once rich permission replies, schema-aware argument revision,
  remembered scopes, and ephemeral one-call sandbox grants.
- Public secret-free session descriptors and truthful capability inventory.
- Subagent provenance with raw child transcript isolation.
- Responsive terminal layouts, progressive Markdown, terminal-control
  sanitization, real caret editing, mouse-wheel-only history, pane-local drag
  copy, no-color, ASCII, and reduced-motion fallbacks.
- Offline fake-provider, reducer, golden, benchmark, race, and PTY coverage.

## Offline demo

The original streaming demonstration remains available without credentials:

```bash
go run ./cmd/demo/ fake
```

See [`docs/architecture.md`](docs/architecture.md) for the conceptual contract
and [`docs/roadmap.md`](docs/roadmap.md) for milestone history.

## License

MIT
