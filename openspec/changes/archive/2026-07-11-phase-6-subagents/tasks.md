## 1. Shared Runtime, Executor, and Catalog Foundations

- [x] 1.1 Extract a frontend-agnostic internal session runtime that owns conversation lifecycle and drives session-start, prompt-submit, `loop.Run`, run-finished, and session-stop hooks
- [x] 1.2 Refactor the root `Session` to use the shared runtime and add regression tests proving its public run, lifecycle, conversation, cancellation, and one-in-flight behavior is unchanged
- [x] 1.3 Define an immutable child runtime blueprint containing provider options, limits, lifecycle hooks, executor safety collaborators, and exact parent advertised-descriptor/executable-handler intersections
- [x] 1.4 Add stable `subagent_started` and `subagent_finished` status values that carry the task label through existing `RunEvent` fields without adding a public struct field or payload type
- [x] 1.5 Add an internal optional event-aware tool-handler invocation at the executor's existing execution slot without changing the required public `ToolHandler` or `Dispatcher` contracts
- [x] 1.6 Add executor tests proving event-aware handlers still observe validation, pre-hooks, permission, sandbox routing, post-hooks, output truncation, backpressure, and cancellation in the existing order
- [x] 1.7 Add deterministic catalog restricted-view support that filters handler lookup and advertisement together while preserving exact parent descriptors and parent advertisement order
- [x] 1.8 Add catalog tests for explicit intersections, unknown and unadvertised names, exact custom descriptor preservation, first-wins duplicate descriptors, unchanged parent catalogs, and the fixed read/search default set

## 2. Task Tool and Subagent Orchestration

- [x] 2.1 Define the `task` descriptor and reject missing, empty, or whitespace-only labels/instructions and non-string-array tool lists before child construction
- [x] 2.2 Implement the non-command, read-classified event-aware task handler inside `internal/subagent` with no reverse import from `internal/tools` and no import of `pkg/agent`
- [x] 2.3 Construct a child from the shared runtime and blueprint with focused system framing, the delegated instruction, a fresh conversation manager, and no copied parent turns
- [x] 2.4 Preserve child-scoped session-start and prompt-submit hook injection while preventing any parent history from entering the child provider input
- [x] 2.5 Implement explicit tool intersection and omitted/empty safe defaults using restricted advertised-and-executable catalog views
- [x] 2.6 Install delegation independently from requested ordinary tools, enforce the fixed maximum depth of three, and return a specific over-depth error without spawning or lifecycle status
- [x] 2.7 Drain child events internally, forward permission requests and nested subagent lifecycle status only, and emit labeled started/finished status on writable-stream terminal paths without delaying cancellation
- [x] 2.8 Return only the final assistant turn, preserve successful empty answers, and convert missing, failed, or step-limited outcomes into recoverable errors
- [x] 2.9 Derive each child context from its parent call context, cancel it when permission forwarding fails, drain it on return, and propagate cancellation through grandchildren without detached work

## 3. Default Session and Compatibility Wiring

- [x] 3.1 Wire default session construction to retain the immutable child blueprint and build every child executor from the restricted catalog and inherited hook, permission, and sandbox collaborators
- [x] 3.2 Register the standard `task` handler only when no caller `ToolHandler` owns that name, preserving an existing custom `task` handler without panic or replacement
- [x] 3.3 Preserve `SessionConfig.Tools` semantics: nil derives catalog descriptors, a non-nil list remains authoritative, and child views use only descriptors that are both advertised and executable
- [x] 3.4 Leave caller-supplied custom dispatchers and their advertised tools untouched, with no automatic task or child-runtime installation
- [x] 3.5 Preserve sequential dispatch so the parent waits for one child result before starting the next requested tool call

## 4. Offline Acceptance and Regression Coverage

- [x] 4.1 Add fake-provider tests proving a valid task starts the existing loop and, without hook injection, the first child input contains only child framing plus the delegated instruction
- [x] 4.2 Add lifecycle-hook tests proving child-scoped injections remain active, parent turns never enter the child context, startup blocks prevent provider work, run-finished blocks preserve outcomes, and session-stop blocks surface as cleanup errors
- [x] 4.3 Add tests proving explicit subsets and safe defaults match the advertised and executable child catalogs, preserve exact parent descriptors, and never run disallowed or parent-hidden handlers
- [x] 4.4 Add tests proving only the final child answer reaches the parent, including empty, oversized, missing, failed, and step-limited outcomes
- [x] 4.5 Add depth-chain tests for allowed child and grandchild delegation, ungrantable recursion through requested names, and refusal beyond depth three without a spawn or lifecycle status
- [x] 4.6 Add cancellation tests for a child provider stream, child tool, and grandchild command, asserting the parent ends cancelled promptly and no orphaned work or recoverable task result remains
- [x] 4.7 Add fake-frontend tests proving labeled child/grandchild lifecycle status is visible on normal and failed paths, raw child events are suppressed, child permission requests remain answerable, and cancellation does not wait for an undeliverable finished status
- [x] 4.8 Add hook, permission-mode, sandbox-routing, custom-handler, and event-aware executor regression tests for inherited safety behavior
- [x] 4.9 Add compatibility tests for a caller-owned `task` handler, explicit empty/omitted advertised tool lists, and caller-supplied custom dispatchers
- [x] 4.10 Add a sequential-dispatch test proving two task calls from one round run one at a time in model order

## 5. Documentation and Verification

- [x] 5.1 Document task arguments, safe read-only defaults, isolated-context and child-hook behavior, result-only return, depth, cancellation, status visibility, caller-owned `task`, and custom-dispatcher boundaries
- [x] 5.2 Run `gofmt`, `go test ./...`, `go build ./cmd/coragent`, `golangci-lint run ./...`, and `openspec validate phase-6-subagents --strict`
- [x] 5.3 Address independent-review findings for monotonic descendant capabilities and explicit provider completion, add the missing safety/event/depth integration regressions, and complete an independent re-review
