# S2.4 Pure-Go `search`

**Status:** pending acceptance
**Prerequisite:** [S2.3 accepted](03-list-tool.md)

## Goal

Provide deterministic content search adequate for multi-file repository
investigation without relying on `rg` or another child process.

## Deliverables

- Recursive UTF-8 text search through the `Action Broker`.
- Go regular-expression semantics.
- Results containing workspace-relative path, line number, and bounded excerpt.
- Lexical path and line ordering.
- Result bound of 200 matches or 64 KiB, whichever comes first.

## Acceptance

- [ ] Search finds every expected Mercury symbol and configuration reference in
      its golden search fixtures.
- [ ] Repeated searches return the same ordered result set.
- [ ] Invalid expressions, binary files, unreadable files, and protected paths
      produce explicit safe outcomes.
- [ ] Traversal and symlink escape fail closed during both walk and file open.
- [ ] Match and byte bounds report `truncated=true` with a narrowing hint.
- [ ] Cancellation stops traversal promptly and pairs the ToolCall once.
- [ ] Every call uses the existing `Action Broker`.
- [ ] No helper process is launched.

## Evidence

Retain golden-search, ordering, truncation, path-policy, and cancellation tests
under `artifacts/m1/s2/2.4/`.

## Failure and rollback boundary

A search error is a bounded ToolResult, not a runtime crash, unless durable
state is corrupt. Search never changes repository state.
