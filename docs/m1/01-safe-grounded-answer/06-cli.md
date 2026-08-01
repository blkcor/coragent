# S1.6 Line-oriented CLI and Slice 1 Gate

**Status:** pending acceptance
**Prerequisite:** [S1.5 accepted](05-action-broker-read.md)

## Goal

Expose a safe, demonstrable terminal product without implementing a TUI.

## Deliverables

- `coragent -C <workspace>` creates a session and enters a line-oriented loop.
- One input line submits one user turn.
- stdout renders streaming text, concise read status, final citations, and the
  session ID.
- Ctrl-C during a run sends `cancel` through `SessionCommand`.
- Ctrl-D while idle exits without closing the session.

## Acceptance

- [ ] From a clean checkout, a developer configures the Provider, starts the
      CLI, asks about a known repository file, and receives a cited answer in
      less than five minutes.
- [ ] An automated checker confirms that the citation path and line range exist
      and support the answer.
- [ ] Ctrl-C produces a visible cancelled outcome and returns the CLI to idle.
- [ ] No Provider or tool work remains after cancellation.
- [ ] The CLI has no Bubble Tea or other full-screen TUI dependency.
- [ ] No repository mutation, command execution, or tool-network activity
      occurs.
- [ ] The full Slice 1 suite passes with tool network disabled.

## Evidence

Retain the clean-checkout transcript, citation check, cancellation trace, test
output, and tested commit under `artifacts/m1/s1/1.6/`.

## Failure and rollback boundary

CLI rendering failure cannot become runtime state. Exiting leaves the durable
session intact. The repository remains unchanged.

## Slice 1 exit

Slice 1 is mergeable only when every S1.1-S1.6 acceptance item passes and the
real CLI completes the grounded-answer check without a safety violation. At
this boundary Coragent is a useful single-session read-only product.
