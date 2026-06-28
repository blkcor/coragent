## Context

Coragent is a coding-agent harness in Go. The architecture (`docs/architecture.md`) and roadmap (`docs/roadmap.md`) already exist, defining a phased delivery model. Phase 0 is the root of the dependency graph — everything builds on it. No code exists yet; this is a greenfield implementation against established specifications.

The harness must speak the OpenAI-compatible protocol (the one seam Phase 1 cannot be built without), stream replies incrementally, and reassemble fragmented tool calls. Configuration must work from a single settings file with home+project discovery, or be supplied directly in code by SDK embedders.

## Goals / Non-Goals

**Goals:**
- Establish a compiling repository skeleton that enforces the architecture's public/internal boundary from day one
- Deliver a configuration system that discovers, merges, and validates settings with loud failure on malformed input
- Implement an OpenAI-compatible streaming client that reassembles tool calls, reports reply endings, retries transients, and cancels promptly
- Make the entire foundation testable offline against faked backends and recorded transcripts
- Ship a runnable demonstration that streams text and tool calls end-to-end

**Non-Goals:**
- No agent loop, multi-turn orchestration, or context management (Phase 1)
- No tool registry, execution middleware, or built-in tools (Phase 2)
- No permission behavior — only the request/decision shape is declared (Phase 3)
- No hooks engine, sandbox, subagents, or TUI (Phases 4-7)
- No model backend beyond OpenAI-compatible protocol
- No token counting, context-window accounting, or usage/cost tracking
- No streaming of partial tool-call arguments or model "thinking" output

## Decisions

### 1. Repository layout: enforce public/internal boundary via Go conventions

**Decision:** Use Go's standard `internal/` visibility rule — packages under `internal/` cannot be imported outside the module. The public surface lives in `pkg/agent/` and depends on nothing in `internal/` or any frontend.

**Rationale:** Go's compiler enforces the boundary automatically. No custom tooling needed. Placeholder directories for unlanded phases contain minimal `doc.go` files with package comments so the tree builds cleanly.

**Alternatives considered:**
- Custom linter rules to enforce boundaries: more complex, redundant when Go's built-in rule suffices
- Separate modules for public vs internal: over-engineered for v1, complicates development workflow

### 2. Configuration format: JSON with field-level merge

**Decision:** Settings file is JSON (not YAML or TOML). Merge strategy: load home settings, then overlay project settings field-by-field (project wins per overlapping field, non-overlapping home fields preserved).

**Rationale:** JSON is universally understood, requires no additional parser dependency, and aligns with the OpenAI wire format the backend already speaks. Field-level merge (not file-level replacement) lets projects override only what they need while keeping personal defaults.

**Alternatives considered:**
- YAML: more human-readable but adds dependency, indentation-sensitive parsing can be error-prone
- TOML: less common in Go ecosystem, adds dependency
- File-level replacement (project file fully replaces home): too coarse-grained, forces duplication

### 3. Credential resolution: environment variables with late binding

**Decision:** Settings file contains environment variable names (e.g., `"api_key": "${OPENAI_API_KEY}"`), resolved at load time. Missing variables leave the credential empty; the first API request fails loudly.

**Rationale:** Keeps secrets out of committed files. Late binding (resolve at load, not at request) catches missing credentials early. Empty-on-missing (vs fail-at-load) allows configuration validation without requiring the environment to be fully populated.

**Alternatives considered:**
- Fail at load time if env var missing: breaks configuration validation in CI without credentials
- Inline secrets in settings file: security risk, cannot commit project settings

### 4. Streaming architecture: channel-based event stream

**Decision:** The provider returns a channel of typed events (text delta, tool call started, tool call finished, status change, reply ended, error). Consumer reads from the channel; it closes on reply-ended or error.

**Rationale:** Channels are Go's native concurrency primitive for streaming. Typed events let the consumer handle each case explicitly. Closing the channel signals completion naturally. Cancellation propagates via `context.Context`, which closes the channel and aborts the HTTP request.

**Alternatives considered:**
- Callback-based (pass a handler function): less idiomatic Go, harder to compose with select/timeout
- Pull-based iterator: more complex state management, less natural for concurrent producers

### 5. Tool-call reassembly: buffer fragments until complete

**Decision:** As tool-call fragments arrive in the stream, accumulate them in a buffer keyed by tool-call ID. When the stream signals a tool call is complete (via a finish marker or stream end), emit the fully assembled call with name and arguments parsed.

**Rationale:** OpenAI streams tool calls in fragments (name first, then argument chunks). The consumer needs complete, dispatchable calls — not fragments. Buffering by ID handles multiple concurrent tool requests and keeps them in first-appearance order.

**Alternatives considered:**
- Stream fragments to consumer: pushes reassembly burden to every consumer (loop, tests, demo)
- Emit partial calls incrementally: complicates consumer logic, no clear use case in v1

### 6. Retry strategy: exponential backoff with jitter, honor "retry-after"

**Decision:** Transient failures (429, 503, 504) trigger retry with exponential backoff plus jitter. Honor any `Retry-After` header. Permanent failures (401, 400) fail immediately without retry. Mid-stream failures do not retry (content already delivered).

**Rationale:** Backoff prevents thundering-herd on rate limits. Jitter avoids synchronized retries. Distinguishing transient vs permanent lets the consumer react appropriately ("wait" vs "fix your setup"). Mid-stream retries would duplicate already-delivered content.

**Alternatives considered:**
- Retry all failures: wastes time on permanent errors, no user signal
- No retry: brief hiccups surface as failed turns
- Retry mid-stream: duplicates delivered text, complex state management

### 7. Testing strategy: fake provider with scripted transcripts

**Decision:** A fake provider implements the same interface as the real OpenAI client but returns scripted replies (text deltas + tool calls) from recorded transcripts. Tests construct a fake with a transcript and assert on the reassembled output.

**Rationale:** Offline, deterministic, fast. Recorded transcripts prove reassembly logic against real provider behavior. No network or credentials needed. The same fake powers the runnable demonstration when no real endpoint is available.

**Alternatives considered:**
- Mock HTTP server: more complex setup, brittle to wire-format changes
- Integration tests against real endpoint: requires credentials, network, slower
- Property-based testing: good for edge cases but doesn't prove real-world reassembly

## Risks / Trade-offs

**[Risk] OpenAI wire format varies across providers** → Mitigation: reassembly tolerates common variations (DeepSeek, Kimi, local servers); add per-endpoint transcripts as each backend is onboarded.

**[Risk] Configuration merge semantics unclear for nested objects** → Mitigation: start with flat or shallow-merge (top-level keys only); revisit if nested config becomes necessary. Document merge behavior clearly.

**[Risk] Streaming partial tool-call arguments could be useful for UI** → Mitigation: defer to Phase 7 (TUI) if experience warrants; Phase 0 delivers only complete calls. Architecture allows additive extension without breaking changes.

**[Risk] Credential resolution leaves empty on missing env var** → Mitigation: first API request fails loudly with clear error. If louder fail-at-load is preferred, revisit in configuration-hardening pass.

**[Trade-off] JSON over YAML for settings** → JSON is less human-readable but avoids parser dependency and aligns with OpenAI wire format. Acceptable for v1 settings surface.

**[Trade-off] No mid-stream retry** → Simpler implementation, no content duplication. Transient failures after content starts delivering surface as errors; user retries the turn. Acceptable for v1.
