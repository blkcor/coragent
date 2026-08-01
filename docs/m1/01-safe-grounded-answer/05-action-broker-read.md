# S1.5 Action Broker, Workspace Authority, and `read`

**Status:** pending acceptance
**Prerequisite:** [S1.4 accepted](04-provider-adapter.md)

## Goal

Deliver the first grounded repository answer through the only permitted tool
execution path.

## Deliverables

- One internal `Action Broker` path for resolve, schema validation, side-effect-
  free preparation, policy check, scoped execution, projection, bounding, and
  ToolResult recording.
- Pure-Go workspace-scoped `read` tool.
- Line-numbered UTF-8 output with optional line range.
- Per-result bound of 1,000 lines or 64 KiB, whichever comes first.
- Immutable M1 authority containing workspace reads only.

## Acceptance

- [ ] A scripted Provider requests `read`, receives one ToolResult, and produces
      a cited final answer in the next model request.
- [ ] In-workspace relative paths succeed with stable line numbers.
- [ ] Absolute paths, `..`, symlink escape, and symlink replacement fail closed.
- [ ] Missing, unreadable, protected, binary, and oversized files produce
      explicit bounded results.
- [ ] Unknown tools receive one unknown-tool result.
- [ ] The first non-success result stops its batch and pairs every remaining
      call with one prior-result skipped result.
- [ ] Cancellation during read produces one cancelled ToolResult and one
      terminal run Event.
- [ ] The catalog exposes no mutation, command, or network capability.

## Evidence

Retain pairing, path-policy, truncation, and cancellation results under
`artifacts/m1/s1/1.5/`.

## Failure and rollback boundary

Tool failures are model-visible results when recovery is possible. Corrupt
runtime state stops the run. `read` performs no repository mutation.
