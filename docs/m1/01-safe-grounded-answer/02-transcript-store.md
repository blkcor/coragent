# S1.2 Append-only Transcript and Initial Session Store

**Status:** pending acceptance
**Prerequisite:** [S1.1 accepted](01-command-event.md)

## Goal

Make semantic session history durable without confusing streaming UI data with
the Transcript.

## Deliverables

- Append-only records for user messages, completed assistant blocks, ToolCalls,
  ToolResults, cancellation boundaries, and terminal run outcomes.
- Initial session storage directly under `~/.coragent/sessions/` in production
  and `t.TempDir()` in tests.
- A session manifest with explicit `format_version`.
- Durable Event cursor high-water mark.

## Acceptance

- [ ] Reloading the store reproduces the same semantic Transcript.
- [ ] Existing Transcript records are never updated or deleted by a later turn.
- [ ] Completed assistant content is durable; individual streaming deltas are
      not persisted as separate Transcript records.
- [ ] Every persisted ToolCall has exactly one terminal ToolResult before its
      Transcript can be reused for a Provider request.
- [ ] The Event cursor high-water mark survives a store reopen.
- [ ] Corrupt records and unsupported formats stop with a typed cause and are
      never rewritten.
- [ ] Tests cannot read or write the real user session directory.

## Evidence

Retain reload, corruption, and append-only test output under
`artifacts/m1/s1/1.2/`.

## Failure and rollback boundary

Failed writes expose no partial valid record. This step writes only test storage
until its production path is exercised by the CLI acceptance step.
