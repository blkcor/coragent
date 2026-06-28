## 1. Repository Scaffold

- [x] 1.1 Initialize Go module with `github.com/blkcor/coragent` path
- [x] 1.2 Create `pkg/agent/` directory with minimal `doc.go` and package comment
- [x] 1.3 Create `internal/` subdirectories (loop, context, executor, permission, hooks, sandbox, provider, tools, subagent) each with placeholder `doc.go`
- [x] 1.4 Create `tui/`, `cmd/coragent/`, `docs/` directories with placeholders
- [x] 1.5 Add `.gitignore`, `go.mod`, basic `Makefile` or `Taskfile.yml`
- [x] 1.6 Verify `go build ./...` succeeds from fresh checkout
- [x] 1.7 Verify `golangci-lint run ./...` passes with no errors
- [x] 1.8 Verify `go test ./...` passes (no tests yet, but no build errors)
- [x] 1.9 Add linter rule or build check to verify `pkg/agent/` does not import `internal/`, `tui/`, or `cmd/`

## 2. Shared Vocabulary (Public Types)

- [x] 2.1 Define `Conversation` type on `pkg/agent/` with documentation
- [x] 2.2 Define `Turn` type on `pkg/agent/` with documentation
- [x] 2.3 Define `Tool` type on `pkg/agent/` with name, description, parameters
- [x] 2.4 Define `ToolCall` type on `pkg/agent/` with ID, tool name, arguments
- [x] 2.5 Define `ToolResult` type on `pkg/agent/` with ID, result, error flag
- [x] 2.6 Define `Provider` interface on `pkg/agent/` for model backend
- [x] 2.7 Define `RunEvent` types on `pkg/agent/` (text delta, tool call, status, reply ended, error)
- [x] 2.8 Define `PermissionRequest` type with reply path, tool call, context
- [x] 2.9 Define `PermissionDecision` type with allow/deny, remember flag, edited arguments
- [x] 2.10 Document all types with user-facing names and no invented abbreviations
- [x] 2.11 Add package-level documentation for `pkg/agent/` explaining core concepts

## 3. Configuration System

- [x] 3.1 Define `Settings` struct in `internal/config/` with JSON tags and documented fields
- [x] 3.2 Implement settings file loader that reads JSON from a given path
- [x] 3.3 Implement home directory discovery (`~/.coragent/settings.json`)
- [x] 3.4 Implement project directory discovery (`.coragent/settings.json` in current working directory)
- [x] 3.5 Implement field-level merge logic (project overrides home per field, preserves non-overlapping home fields)
- [x] 3.6 Implement environment variable resolution for credentials (parse `${VAR_NAME}` syntax)
- [x] 3.7 Add validation that fails loudly on malformed JSON, naming the offending file
- [x] 3.8 Add validation that fails loudly on invalid field values, naming the file and field
- [x] 3.9 Document default values for all settings fields
- [x] 3.10 Implement direct in-code configuration path that skips file discovery
- [x] 3.11 Write unit tests for settings loader (valid JSON, invalid JSON, missing files)
- [x] 3.12 Write unit tests for merge logic (home only, project only, both with overlap)
- [x] 3.13 Write unit tests for environment variable resolution (set, unset, malformed)
- [x] 3.14 Write integration test that loads settings from temp directories (home + project)

## 4. Model Backend - Core Streaming

- [x] 4.1 Define OpenAI-compatible request/response types in `internal/provider/`
- [x] 4.2 Implement HTTP client with streaming support (read SSE chunks)
- [x] 4.3 Parse streaming text delta events and emit as `RunEvent` text deltas
- [x] 4.4 Parse streaming tool-call fragment events (name, arguments chunks)
- [x] 4.5 Implement event channel that streams typed `RunEvent` to consumer
- [x] 4.6 Implement context-based cancellation that aborts HTTP request and closes channel
- [x] 4.7 Implement reply-ending detection (finished, stopped-to-call-tools, cut-off)
- [x] 4.8 Emit reply-ended event when stream completes
- [x] 4.9 Implement per-request model and sampling options override
- [x] 4.10 Implement fallback to configured defaults when options omitted
- [x] 4.11 Write unit test for text delta streaming (incremental delivery, order preserved)
- [x] 4.12 Write unit test for context cancellation (aborts request, closes channel)

## 5. Model Backend - Tool-Call Reassembly

- [x] 5.1 Implement tool-call fragment buffer keyed by tool-call ID
- [x] 5.2 Accumulate tool name from fragments
- [x] 5.3 Accumulate tool arguments from JSON chunks
- [x] 5.4 Detect tool-call completion (via finish marker or stream end)
- [x] 5.5 Parse accumulated JSON arguments into structured form
- [x] 5.6 Emit complete `ToolCall` event with ID, name, parsed arguments
- [x] 5.7 Handle multiple concurrent tool requests, keeping them separate
- [x] 5.8 Preserve first-appearance order when emitting multiple tool calls
- [x] 5.9 Handle edge cases: empty arguments, malformed JSON, duplicate IDs
- [x] 5.10 Write unit test for single tool-call reassembly from fragments
- [x] 5.11 Write unit test for multiple tool-call reassembly (separate, ordered)
- [x] 5.12 Write unit test for tool-call with malformed JSON arguments

## 6. Model Backend - Resilience

- [x] 6.1 Distinguish transient failures (429, 503, 504) from permanent (401, 400)
- [x] 6.2 Implement exponential backoff with jitter for transient failures
- [x] 6.3 Parse and honor `Retry-After` header from backend
- [x] 6.4 Implement retry loop up to configured limit
- [x] 6.5 Ensure no retry on permanent failures
- [x] 6.6 Surface distinct error types for transient vs permanent failures
- [x] 6.7 Handle mid-stream failures: emit error event, close channel, preserve delivered text
- [x] 6.8 Ensure no half-formed tool request is delivered on mid-stream failure
- [x] 6.9 Ensure no already-delivered content is duplicated on retry
- [x] 6.10 Write unit test for transient failure retry (retries with backoff, succeeds)
- [x] 6.11 Write unit test for retry limit exhausted (surfaces error)
- [x] 6.12 Write unit test for permanent failure (no retry, immediate error)
- [x] 6.13 Write unit test for mid-stream failure (error event, no duplication)
- [x] 6.14 Write unit test for retry-after header (honors hint)

## 7. Fake Provider & Testing

- [x] 7.1 Implement `FakeProvider` in `internal/provider/testutil/` that scripts replies
- [x] 7.2 Support scripted text deltas with configurable timing
- [x] 7.3 Support scripted tool-call fragments (name, then argument chunks)
- [x] 7.4 Support scripted errors (transient, permanent, mid-stream)
- [x] 7.5 Add recorded transcript fixtures from real OpenAI-compatible endpoints
- [x] 7.6 Write integration test: fake provider streams text, asserts incremental delivery
- [x] 7.7 Write integration test: fake provider streams tool calls, asserts reassembly
- [x] 7.8 Write integration test: fake provider streams multiple tool calls, asserts order
- [x] 7.9 Write integration test: recorded transcript reassembles into expected tool calls
- [x] 7.10 Write integration test: fake provider error handling (transient, permanent, mid-stream)

## 8. Runnable Demonstration

- [x] 8.1 Create `cmd/demo/` with minimal main program
- [x] 8.2 Accept endpoint URL (or "fake") as command-line argument
- [x] 8.3 Load settings from default locations or environment
- [x] 8.4 Construct a sample conversation with a user message
- [x] 8.5 Define a sample set of available tools
- [x] 8.6 Call provider to stream reply
- [x] 8.7 Print text deltas as they arrive (incremental output)
- [x] 8.8 Print tool calls with assembled arguments when complete
- [x] 8.9 Print reply-ending reason when stream completes
- [x] 8.10 Handle errors gracefully (print error, exit with code)
- [x] 8.11 Document how to run demo against real endpoint
- [x] 8.12 Document how to run demo against fake provider (no credentials needed)
- [x] 8.13 Verify demo runs successfully against fake provider in CI
