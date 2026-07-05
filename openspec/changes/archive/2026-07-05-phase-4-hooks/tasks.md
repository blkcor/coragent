## 1. Hook Domain And Engine

- [x] 1.1 Define hook moments, scopes, event payloads, verdicts, and outcome actions in `internal/core` with public aliases in `pkg/agent`.
- [x] 1.2 Implement scope matching for tool name, pattern detail, combined scopes, and unscoped hooks.
- [x] 1.3 Validate hook definitions at session construction, including unsupported moments, invalid patterns, invalid timeouts, and missing external commands when detectable.
- [x] 1.4 Implement deterministic hook ordering with in-process hooks before external hooks and first-block-wins semantics.
- [x] 1.5 Implement verdict composition for argument replacements, result replacements, and injected context.

## 2. External And In-Process Hooks

- [x] 2.1 Add SDK registration for in-process hooks without changing existing public contracts.
- [x] 2.2 Implement external hook execution with JSON stdin, exit-status default verdicts, and optional structured stdout verdicts.
- [x] 2.3 Enforce per-hook timeouts and process-group cleanup for external hooks.
- [x] 2.4 Recover in-process hook panics and convert them into fail-closed block verdicts.
- [x] 2.5 Bound and validate external hook stdout so malformed or oversized output blocks safely.

## 3. Executor Integration

- [x] 3.1 Replace inert pre-check behavior with before-tool hook evaluation in the existing executor chain.
- [x] 3.2 Ensure before-tool blocks short-circuit permission, sandbox, and tool execution while returning a readable error result.
- [x] 3.3 Revalidate hook-edited tool arguments before permission or tool execution.
- [x] 3.4 Replace inert post-check behavior with after-tool hook evaluation in the existing executor chain.
- [x] 3.5 Ensure after-tool result replacements and blocks are what the model receives.
- [x] 3.6 Prove permission bypass mode does not affect hook enforcement.

## 4. Loop And Session Lifecycle Integration

- [x] 4.1 Invoke session-start hooks before a session accepts runs and surface block reasons to callers.
- [x] 4.2 Apply session-start injected context before the first provider call.
- [x] 4.3 Invoke prompt-submit hooks before provider streaming starts for a run.
- [x] 4.4 Ensure prompt-submit blocks prevent provider calls and surface the block reason.
- [x] 4.5 Apply prompt-submit injected context to the conversation assembled for that turn.
- [x] 4.6 Invoke session-stop hooks during shutdown or cleanup and surface failures without preventing stop.
- [x] 4.7 Invoke run-finished hooks after a run outcome is known and before the terminal event, preserving the original outcome on hook failure.

## 5. Configuration And Events

- [x] 5.1 Extend settings structs and JSON loading for external hook declarations.
- [x] 5.2 Implement home/project merge behavior for hook settings according to documented configuration rules.
- [x] 5.3 Add hook outcome event payloads for blocks, replacements, and injections on the existing run event stream.
- [x] 5.4 Emit hook outcome events from executor and loop-owned hook moments without exposing internal hook state.
- [x] 5.5 Document the external hook JSON input/output shape in code comments or package docs.

## 6. Tests And Verification

- [x] 6.1 Add hook engine unit tests for moments, scope matching, ordering, verdict composition, and fail-closed behavior.
- [x] 6.2 Add executor tests proving before-tool hooks short-circuit downstream stages and after-tool hooks replace or block results.
- [x] 6.3 Add loop/session tests proving prompt-submit and session lifecycle hooks run at the correct time.
- [x] 6.4 Add configuration tests for external hook settings, merge behavior, and validation failures.
- [x] 6.5 Add external hook tests using temporary scripts and no network or real model.
- [x] 6.6 Add tests for run-finished hook timing, configuration validation, and failure outcome preservation.
- [x] 6.7 Run `go test ./...`.
- [x] 6.8 Run `go build ./cmd/coragent`.
