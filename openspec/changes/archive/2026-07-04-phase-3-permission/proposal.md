## Why

Phase 2 shipped the one execution path with an inert allow-everything permission
stage: the agent reads, edits, and runs commands with no human oversight. Phase 3
arms that stage so the human has the final say — allow, deny, or ask — completing
Milestone **M2 "It acts"**: the agent acts *with a person in the loop*, dialable
from "ask me about everything" to "trust everything."

## What Changes

- **Arm the permission stage.** Replace the inert allow-everything placeholder
  with a real engine that resolves every tool call to exactly one outcome: allow,
  deny, or ask — with **ask** as the safe default whenever nothing else decides.
- **Four modes, switchable between turns.** `default`, `auto-accept-edits`,
  `plan`, and `bypass`, settable from configuration and changeable mid-task.
- **Allow / deny rules with deny-wins.** Per-action-type rules; command matching
  covers a command's family (`git status` → `git status --short`) without
  over-matching (`git stash`, `git statusfoo`); a deny always beats an allow.
- **Remembered decisions.** Answering a prompt can persist the choice as a durable
  rule that takes effect immediately and survives a restart; a failed save never
  blocks the action it accompanied.
- **Action classification.** Each built-in declares whether it reads, edits, or
  runs commands so plan mode (blocks all mutation, reads pass) and
  auto-accept-edits (edits pass, commands still ask) act on the correct set; an
  unclassified action is treated as state-changing (errs safe).
- **Fail-safe and tolerant.** An unanswered prompt resolves to deny on the turn's
  deadline or cancellation (never hangs); a frontend that answers twice has its
  first decision honored and the rest ignored.
- **Permission settings.** The settings file gains a permission section (starting
  mode, allow list, deny list), merged home-then-project.
- Edited-argument re-validation already lives in the Phase 2 chain; this phase
  feeds it real edited arguments from human decisions.

Permission is **soft** — advisory, bypassable, never a security boundary. The hard
guardrails are hooks (Phase 4) and the sandbox (Phase 5); bypass mode skips
asking but never touches them.

## Capabilities

### New Capabilities
- `permission`: the soft human-in-the-loop gate — three outcomes (allow/deny/ask)
  with ask as default, four switchable modes, allow/deny rules with deny-wins and
  command-family matching, remembered durable rules, a fixed resolution order,
  edited-argument approval, fail-safe on no answer, and tolerance of duplicate
  answers, all driven over the run event stream and verifiable against a fake
  frontend.

### Modified Capabilities
- `configuration`: settings file gains a permission section (starting mode, allow
  rules, deny rules), merged field-by-field home-then-project with project rules
  layering over home and a project's stricter deny honored.
- `builtin-tools`: each built-in declares its action classification (read-only,
  edits-files, or runs-commands) so plan mode and auto-accept-edits gate the
  correct set of actions.
- `tool-executor`: the executor classifies each resolved call's action kind and
  hands it to the (now real) permission stage, so the soft gate can apply
  mode-aware decisions; the inert placeholder is replaced without changing the
  chain order or the `Dispatcher` seam.

## Impact

- **New code:** `internal/permission/` (engine, rules, modes, command matching,
  remembering/persistence).
- **Modified code:** `internal/executor/` (action classification, wire real
  permission via `Stages`), `internal/core/` (additive: action-kind type,
  permission seam may carry action kind, request may carry a reason field),
  `internal/config/` (permission settings + merge), `internal/tools/` (built-ins
  declare action kind), `pkg/agent/` (re-export new public names; wire the engine
  from settings when building the default executor).
- **No breaking changes** to Phase 0–2 public contracts: new public names are
  additive; the `Dispatcher` seam and chain order are unchanged.
- **Dependencies:** none new; persistence reuses the existing settings file.
