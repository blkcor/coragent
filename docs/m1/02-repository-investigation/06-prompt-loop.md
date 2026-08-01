# S2.6 Prompt Assembly and Multi-tool Investigation Loop

**Status:** pending acceptance
**Prerequisite:** [S2.5 accepted](05-instruction-discovery.md)

## Goal

Assemble each Provider request from current runtime facts and complete multi-hop
list-search-read investigations while preserving ToolCall pairing.

## Deliverables

- Separate stable policy and dynamic runtime prompt sections.
- Applicable project instructions, workspace facts, current goal, bounded recent
  Transcript, and read-only tool catalog on each request.
- Loop continuation based on completed ToolCalls.
- Sequential tool execution in Provider order.

## Acceptance

- [ ] Prompt tests keep stable and dynamic sections distinct.
- [ ] A scripted Provider completes a list-search-read-answer sequence through
      the real Session loop.
- [ ] Every ToolCall has one ToolResult before the next Provider request.
- [ ] Finish metadata cannot suppress completed ToolCalls.
- [ ] A non-success result stops later calls in the batch and pairs them with
      skipped results.
- [ ] Current instructions and explicit user constraints appear on every request.
- [ ] Prompt content comes from current state rather than an accumulating global
      system string.
- [ ] Provider wire values do not escape the adapter.

## Evidence

Retain prompt-section snapshots, scripted multi-hop traces, and pairing tests
under `artifacts/m1/s2/2.6/`.

## Failure and rollback boundary

Recoverable tool failures return to the model. Programmer errors or corrupt
durable state stop the run. No tool side effect is possible.
