## Why

Phase 5 turns the executor's sandbox stage from a placeholder into a real
confinement boundary for shell commands. This is needed now because Phase 4
completed hard hooks, and Milestone M3 requires both unconditional guardrails and
OS-level command isolation before the harness can be considered safe for daily
coding use.

## What Changes

- Add a sandbox capability that derives a deterministic policy from working
  directory, configuration, and permission context.
- Enforce that policy for every shell command and command-declaring custom tool
  on the existing executor path.
- Provide a macOS `sandbox-exec` backend that enforces read, write, and network
  rules at the operating-system level.
- Provide a weaker policy-based fallback behind the same boundary when OS-level
  sandboxing is unavailable, with honest runtime reporting of the active level.
- Add configuration fields for extra read roots, extra write roots, and network
  grants that only widen the safe baseline.
- Surface sandbox blocks, cancellation, and timeout as recoverable tool errors
  rather than crashes or silent successes.

## Capabilities

### New Capabilities

- `sandbox`: Policy derivation, backend selection, confinement-level reporting,
  macOS OS-level enforcement, weaker fallback behavior, and blocked-command
  error handling.

### Modified Capabilities

- `tool-executor`: Fill the existing sandbox stage with real enforcement for
  shell and command-declaring custom tools while preserving the single ordered
  execution path.
- `configuration`: Extend the single JSON settings format with sandbox policy
  grants that merge through the existing home/project configuration rules.

## Impact

- Affected packages: `internal/sandbox`, `internal/executor`,
  `internal/tools`, `internal/permission`, `internal/config`, and `pkg/agent`
  where SDK-facing sandbox configuration or status must be exposed.
- Affected behavior: shell commands now run under confinement by default, with
  writes limited to the project and scratch temp area and network denied unless
  explicitly granted.
- Affected tests: offline policy derivation and fallback tests on all platforms,
  plus macOS-specific tests for real `sandbox-exec` enforcement where available.
