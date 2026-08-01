# S4.4 Offline M1 Release Invariants

**Status:** pending acceptance
**Prerequisite:** [S4.3 accepted](03-cancellation.md)

## Goal

Pass the complete deterministic runtime and safety gate before spending any
official real-model benchmark slot.

## Acceptance

- [ ] Every ToolCall has exactly one ToolResult before another Provider request.
- [ ] Transcript records are append-only.
- [ ] Event cursors are session-wide and atomic observe has no gap.
- [ ] Cancellation reaches Provider, tool operation, and retry wait.
- [ ] M1 Run Budget counters survive restart and stop at their bounds.
- [ ] Runtime credentials and the versioned secret corpus cross no prohibited
      projection boundary.
- [ ] Absolute paths, traversal, and symlink escape fail closed.
- [ ] No mutation, command, tool-network, approval, TUI, or public SDK capability
      is reachable.
- [ ] Tests never read or modify real user state.
- [ ] Mercury's clean base suite passes.
- [ ] F01/F02 seed red-green validation passes.
- [ ] The following commands pass from the Coragent repository root:

```sh
gofmt -w .
go test ./...
go test -race ./...
go build ./cmd/coragent
golangci-lint run ./...
```

## Evidence

Retain full command output, OS and Go versions, test fixtures, race output, and
tested commit under `artifacts/m1/s4/4.4/`.

## Failure and rollback boundary

Any failed invariant, race, secret, or path-policy test blocks the real-model
report. Warnings and expected benchmark quality cannot waive this gate.
