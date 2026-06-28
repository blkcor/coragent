# Tasks — Phase 1 Agent Loop

> TDD: write each test group before its implementation. Every group ends green on
> `go build ./...`, `go test ./...`, `golangci-lint run ./...`.

## 1. Public types (additive, `pkg/agent/types.go`)

- [x] 1.1 Append new `RunEventType` constants: `ToolStartedEvent`, `ToolFinishedEvent`, `RunFinishedEvent`, `PermissionRequestedEvent`, `OverBudgetWarningEvent` (after existing iota values, Phase 0 order preserved)
- [x] 1.2 Add `RunEvent` fields: `ToolResult *ToolResult`, `RunFinished *RunFinished`, `Permission *PermissionRequest`, `Warning string`
- [x] 1.3 Add `StopReason` int + consts `StopCompleted`, `StopReachedStepLimit`, `StopCancelled`, `StopFailed`; `RunFinished{Reason StopReason; Err error}`; `StopReason.IsError()` (true only for `StopFailed`)
- [x] 1.4 Add status string consts `StatusThinking`, `StatusCallingTool`, `StatusIdle`
- [x] 1.5 Add `Dispatcher` interface: `Dispatch(ctx, ToolCall, emit func(RunEvent) error) (ToolResult, error)`
- [x] 1.6 Confirm `git diff pkg/agent/types.go` is append-only (no Phase 0 shape changed)

## 2. Context manager (`internal/context`)

- [x] 2.1 Write `manager_test.go`: snapshot deep-copy immutability, append order (system→user→assistant→tool), `EstimateTokens` estimate
- [x] 2.2 Implement `Manager`: mutex-guarded `Conversation` seeded with system turn; `AppendUser`/`AppendAssistant(content,calls)`/`AppendToolResults(results)`
- [x] 2.3 Implement `Snapshot()` deep-copying turns + nested `ToolCalls`/`ToolResults`/`Arguments` maps
- [x] 2.4 Implement `EstimateTokens()` = total content chars / 4; document inert compaction plug-in point
- [x] 2.5 Tests green

## 3. Stand-in dispatcher (`internal/executor`)

- [x] 3.1 Write `standin_test.go`: returns non-error placeholder result keyed to the call ID
- [x] 3.2 Implement `StandIn` satisfying `agent.Dispatcher`; replace the Phase-2 placeholder doc note
- [x] 3.3 Tests green

## 4. Loop driver (`internal/loop`)

- [x] 4.1 Write `loop_test.go` against `testutil.FakeProvider` + fake dispatcher covering: headline order; recoverable tool failure continues; unrecoverable dispatch error ends `StopFailed`; provider error ends `StopFailed` (no partial turn); step-limit `StopReachedStepLimit` (normal stop); cancel mid-think; cancel mid-tool; permission-pause ordering (start→permission→answer→finish); backpressure + abandoned-reader cancel
- [x] 4.2 Implement `Deps` struct and `Run(ctx, Deps, emit)` per design D5 (gather→act→verify, exactly one `RunFinishedEvent` on every path)
- [x] 4.3 Implement provider-stream consumption: text passthrough, tool-call accumulation, reply-end note, error capture; cancel/error precedence per D5 step 4
- [x] 4.4 Implement sequential dispatch with `ToolStartedEvent`/`ToolFinishedEvent` bracketing, recoverable-vs-unrecoverable branch, mid-tool cancel check
- [x] 4.5 Tests green

## 5. Session façade (`pkg/agent/session.go`)

- [x] 5.1 Write `session_test.go`: headline US-030 (exact event order + conversation order), concurrent-run refusal, multi-request history accumulation, over-budget warning
- [x] 5.2 Implement `SessionConfig`, `NewSession` (defaults for `MaxRounds`/`ContextBudgetTokens`, nil `Dispatcher` → `StandIn`)
- [x] 5.3 Implement `Run(ctx, input) (<-chan RunEvent, error)`: in-flight atomic guard, append user turn, spawn goroutine owning a cap-1 buffered channel, wire `internal/loop`
- [x] 5.4 Implement backpressure `emit` (select ch/ctx) + guaranteed terminal enqueue helper per design D6; `defer close` + clear guard
- [x] 5.5 Implement `Conversation()` returning `Manager.Snapshot()`
- [x] 5.6 Tests green

## 6. M1 readout & verification

- [x] 6.1 Add a Session-driven readout mode to `cmd/demo`: print status labels, inline text, bracketed tool start/finish, finish reason, errors; auto-approve any permission request
- [x] 6.2 `go run ./cmd/demo fake` reproduces the US-030 ordered sequence and closes
- [x] 6.3 `go build ./...`, `go test ./...`, `golangci-lint run ./...` all green
- [x] 6.4 Verify invariants: no `internal/` imports a frontend; TUI/`cmd` import only `pkg/agent` where applicable; types diff append-only
