# Coragent

A coding-agent harness in Go — a reusable SDK that drives LLM-powered agents through tool use.

## Phase 0 — Foundations (Complete)

This repository implements Phase 0 of the coragent roadmap, providing:

- **Repository scaffold** with architecture-mandated public/internal boundary
- **Configuration system** with single settings file, home+project discovery, environment credentials
- **OpenAI-compatible model backend** with streaming, tool-call reassembly, retry/backoff, and cancellation
- **Shared vocabulary** for core concepts (Conversation, Turn, Tool, ToolCall, ToolResult, Provider, RunEvent)
- **Fake provider** for offline testing
- **Runnable demonstration** of streaming text and tool calls

## Building

```bash
go build ./...
```

## Testing

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/config/ -v
go test ./internal/provider/ -v
```

## Running the Demo

### With Fake Provider (no credentials needed)

```bash
go run ./cmd/demo/ fake
```

This streams a scripted reply from the fake provider, demonstrating incremental text delivery.

### With Real OpenAI-Compatible Endpoint

1. Create a settings file at `~/.coragent/settings.json` or `.coragent/settings.json`:

```json
{
  "model": {
    "name": "gpt-4",
    "base_url": "https://api.openai.com/v1",
    "api_key": "${OPENAI_API_KEY}"
  }
}
```

2. Set your API key:

```bash
export OPENAI_API_KEY="your-api-key"
```

3. Run the demo:

```bash
go run ./cmd/demo/
```

## Architecture

```
coragent/
├── cmd/
│   ├── coragent/       # TUI binary (Phase 7)
│   └── demo/           # Streaming demonstration
├── pkg/agent/          # PUBLIC SDK surface (importable by anyone)
├── internal/           # harness internals (not importable outside module)
│   ├── config/         # configuration loading and validation
│   ├── provider/       # OpenAI-compatible streaming client
│   ├── loop/           # agent loop (Phase 1)
│   ├── context/        # context/history manager (Phase 1)
│   ├── executor/       # tool executor + middleware (Phase 2)
│   ├── permission/     # permission engine (Phase 3)
│   ├── hooks/          # hooks engine (Phase 4)
│   ├── sandbox/        # sandbox backends (Phase 5)
│   ├── tools/          # built-in tools (Phase 2)
│   └── subagent/       # subagent orchestrator (Phase 6)
├── tui/                # Bubble Tea components (Phase 7)
└── docs/               # architecture, roadmap, PRDs
```

## Key Features Implemented

### Configuration System

- Single JSON settings file format
- Home (`~/.coragent/settings.json`) and project (`.coragent/settings.json`) discovery
- Field-level merge with project taking precedence
- Environment variable resolution for credentials (`${VAR_NAME}` syntax)
- Loud failure on malformed input with file path in error message
- Direct in-code configuration for SDK embedders

### Model Backend

- OpenAI-compatible streaming client
- Incremental text delta delivery
- Tool-call fragment reassembly (buffered by ID, emitted complete)
- Multiple concurrent tool requests handled separately, in first-appearance order
- Reply-ending detection (finished, stopped-to-call-tools, cut-off)
- Per-request model and sampling options with fallback to defaults
- Retry transient failures (429, 503, 504) with exponential backoff
- Distinct permanent failures (401, 400) without retry
- Clean mid-stream error handling
- Context-based cancellation

### Testing

- Fake provider for offline testing
- Comprehensive unit tests for configuration
- Integration tests for streaming and tool-call reassembly

## Next Phases

- **Phase 1**: Agent loop, multi-turn orchestration, context management
- **Phase 2**: Tool registry, execution middleware, built-in tools
- **Phase 3**: Permission engine (human-in-the-loop)
- **Phase 4**: Hooks engine (hard gates)
- **Phase 5**: Sandbox backends
- **Phase 6**: Subagent orchestration
- **Phase 7**: Terminal UI (Bubble Tea)

See `docs/roadmap.md` for details.

## License

MIT
