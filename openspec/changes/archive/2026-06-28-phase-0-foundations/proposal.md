## Why

Phase 0 establishes the foundation every subsequent phase depends on. Without a stable repository structure, shared vocabulary, configuration system, and model backend, Phase 1's agent loop cannot be built. This phase de-risks the core streaming and tool-call reassembly logic against recorded transcripts before any loop exists, and settles the configuration story so later phases read settings from one well-understood place.

## What Changes

- **New repository layout** with the architecture's public/internal boundary: `pkg/agent/` (public SDK surface), `internal/` (harness internals), placeholder directories for unlanded phases that build cleanly
- **Shared vocabulary** for core concepts: conversation, turn, tool, tool call, tool result, model backend, run events — including the permission request/decision shape (declared but not acted on)
- **Single configuration system**: one settings file format, home-and-project discovery with field-level merge (project overrides home), environment-variable credential resolution, direct in-code construction for SDK embedders, loud failure on malformed input
- **OpenAI-compatible model backend**: streaming assistant text incrementally, reassembling fragmented tool requests into complete dispatchable calls, reporting how replies end (finished/stopped-to-call-tools/cut-off), per-request model and sampling options with fallback to configured defaults
- **Resilient backend communication**: retry transient failures with backoff (honoring "retry after" hints), distinct non-retried permanent failures, clean mid-stream error handling, prompt cancellation of in-flight replies
- **Offline-testable foundation**: entire suite passes against faked backend and recorded transcripts with no network, plus a runnable streaming demonstration

## Capabilities

### New Capabilities

- `repository-scaffold`: repo layout matching architecture's public/internal boundary, placeholder directories for unlanded phases, clean build and static checks from fresh checkout
- `configuration`: single settings file format, home+project discovery with field-level merge, environment credential resolution, in-code construction, documented defaults and loud failure on malformed input
- `model-backend`: OpenAI-compatible streaming client, incremental text delivery, tool-call fragment reassembly, reply-ending reason reporting, per-request options with fallback, retry/backoff for transients, distinct permanent failures, mid-stream error handling, cancellation

### Modified Capabilities

None — Phase 0 is the root of the dependency graph with no prior capabilities to modify.

## Impact

- **Code**: Introduces `pkg/agent/` (public surface), `internal/provider/` (OpenAI-compatible client), `internal/config/` (settings loader), and placeholder packages for all phases
- **APIs**: Establishes the public vocabulary types (Conversation, Turn, Tool, ToolCall, ToolResult, Provider, RunEvent, PermissionRequest/Decision) that all downstream phases build against
- **Dependencies**: Minimal external surface — speaks OpenAI wire format directly without heavy SDK dependencies
- **Testing**: Establishes the fake provider pattern (scripted model replies) that all phase tests will use
- **Documentation**: Architecture and roadmap already exist; this phase adds the first runnable code and proves the foundation works
