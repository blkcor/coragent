# S2.3 Pure-Go `list`

**Status:** pending acceptance
**Prerequisite:** [S2.2 accepted](02-task-seeds-triggers.md)

## Goal

Let the model discover repository structure deterministically without helper
processes or ambient filesystem authority.

## Deliverables

- Workspace-scoped `list` through the existing `Action Broker`.
- Workspace-relative paths in lexical order.
- Visibility of hidden path names unless hard policy protects them.
- Result bound of 2,000 entries or 64 KiB, whichever comes first.
- Explicit truncation and continuation metadata.

The tool lists repository files; it does not apply Mercury's application-level
discovery exclusions.

## Acceptance

- [ ] Repeated listing returns the same lexical order.
- [ ] Hidden entries remain visible as paths unless protected by hard policy.
- [ ] Absolute paths, `..`, symlink escape, and symlink replacement fail closed.
- [ ] Entry and byte bounds set `truncated=true` and provide a continuation hint.
- [ ] Cancellation of a large recursive listing stops promptly and records one
      cancelled ToolResult.
- [ ] Every call uses the `Action Broker` and scoped filesystem.
- [ ] No helper process is launched.

## Evidence

Retain ordering, hidden-path, truncation, path-policy, and cancellation tests
under `artifacts/m1/s2/2.3/`.

## Failure and rollback boundary

Unreadable directories produce bounded model-visible results. Listing never
changes repository state.
