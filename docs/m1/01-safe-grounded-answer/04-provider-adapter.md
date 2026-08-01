# S1.4 OpenAI-compatible Streaming Provider Adapter

**Status:** pending acceptance
**Prerequisite:** [S1.3 accepted](03-data-projection.md)

## Goal

Connect the Session loop to one OpenAI-compatible streaming protocol without
leaking Provider-specific wire types into the runtime.

## Deliverables

- Internal streaming Provider adapter for text, ToolCalls, usage, and terminal
  reason.
- Explicit context-window and output-limit configuration.
- Provider cancellation and typed permanent/protocol failures.
- Offline HTTP fixture for the wire protocol.

Retry is deliberately excluded until S4.1.

## Acceptance

- [ ] The offline fixture proves streaming text, stable ToolCall IDs, complete
      tool arguments, optional usage, and terminal reason capture.
- [ ] Completed ToolCalls, not finish metadata alone, enter the tool phase.
- [ ] Authentication failure, invalid request, and malformed stream produce
      distinct typed causes without retry.
- [ ] Cancellation closes the stream and leaves no Provider goroutine or owned
      connection active.
- [ ] Configuration without an explicit context-window limit fails.
- [ ] Provider wire types remain inside the adapter.
- [ ] Captured test and benchmark artifacts contain no Provider credential.

## Evidence

Retain wire fixtures, adapter test output, and cancellation diagnostics under
`artifacts/m1/s1/1.4/`.

## Failure and rollback boundary

Permanent and protocol failures stop the run. No partial assistant stream is
persisted as a completed assistant block.
