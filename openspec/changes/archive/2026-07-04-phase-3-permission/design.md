## Context

Phase 2 left the permission stage as `allowAllPermission` in
`internal/executor/stages.go`, wired through the `core.Permission` seam:

```go
type Permission interface {
    Decide(ctx context.Context, call ToolCall, emit func(RunEvent) error) PermissionResult
}
```

The executor chain (`chain.go`) already calls `Decide`, treats `!Allow` as a
denial error result, and re-validates `EditedArguments`. The public types
`PermissionRequest{ToolCall, ReplyPath}` and `PermissionDecision{Allow, Remember,
EditedArguments}` already exist in `core` and are re-exported via `pkg/agent`. The
loop hands the dispatcher the live `emit` stream, so a real engine can raise a
`PermissionRequestedEvent` and block on `ReplyPath`.

What is missing: a real engine behind the seam, an action-kind classification the
engine needs for plan/auto-accept modes, permission settings, and persistence of
remembered rules. The settings file currently holds only `Model`.

Constraints: no Phase 0–2 public contract may break (invariant 5); one execution
path (invariant 3); everything offline-testable against a fake frontend.

## Goals / Non-Goals

**Goals:**
- A real `internal/permission` engine implementing `core.Permission` with the
  three outcomes, four modes, rules, remembering, fail-safe, and one-decision
  tolerance.
- Action classification flowing from the executor into the engine.
- Permission settings (mode + allow/deny lists) read and merged home-then-project,
  with remembered rules persisted to the project settings file.
- Wire the engine into the default executor from settings, additively.

**Non-Goals:**
- Hooks and sandbox stay inert this phase (Phases 4/5).
- No TUI rendering of the prompt — only the fake frontend for tests (Phase 7).
- No compound-command shell parsing — token-prefix matching only, by design.
- No audit trail, no per-decision save-location choice, no subagent inheritance.

## Decisions

### 1. Action classification via an optional interface + executor-computed kind

Add a small enum and an **optional** classifier interface in `core`:

```go
type ActionKind int
const ( ActionUnknown ActionKind = iota; ActionRead; ActionEdit; ActionCommand )

type ActionClassifier interface { ActionKind() ActionKind }
```

The executor computes the kind for each resolved handler:
- handler implements `ActionClassifier` → use its kind;
- else `RunsCommands()` true → `ActionCommand`;
- else → `ActionUnknown` (plan mode errs safe and blocks it).

The six built-ins implement `ActionClassifier` (read/search/find → `ActionRead`,
write/edit → `ActionEdit`, shell → `ActionCommand`).

*Why optional interface over adding a method to `ToolHandler`:* `ToolHandler` is
re-exported as `agent.ToolHandler`, a public contract — adding a required method
breaks every external implementer (invariant 5). A type-asserted optional
interface with a safe default is additive.

*Alternative rejected:* a classification map keyed by tool name in the engine —
breaks for custom tools and duplicates knowledge the tool already owns.

### 2. Pass the action kind through the permission seam

Change the internal `core.Permission.Decide` signature to carry the kind:

```go
Decide(ctx, call ToolCall, kind ActionKind, emit func(RunEvent) error) PermissionResult
```

This is an **internal** seam (`internal/core`, used only by the executor); it is
not part of the `pkg/agent` public surface, so changing it does not break the
public contract. The inert `allowAllPermission` and tests update to the new arity.

*Alternative rejected:* passing the whole `ToolHandler` — couples the engine to
execution details it does not need; the kind is the one fact it requires (FR-20).

### 3. Engine structure and the resolution order

`internal/permission.Engine` holds the current mode (atomic/guarded, switchable
between turns via `SetMode`) and the merged rule set. `Decide` applies a fixed
order that yields every user-visible promise (FR-16):

1. **bypass mode** → allow (skip rules entirely). US-012, US-024.
2. **plan mode** and kind is mutating (`ActionEdit`/`ActionCommand`/`ActionUnknown`)
   → deny, reason `"plan mode: changes are disabled"`. Not reachable by allow
   rules because this precedes rule checks. US-011, US-022, US-023.
3. **deny rule** matches → deny. US-016, US-018, deny-wins.
4. **allow rule** matches → allow. US-015, US-017.
5. **auto-accept-edits mode** and kind is `ActionEdit` → allow. US-010.
6. otherwise → **ask the human** (emit request, block on reply). US-001, US-003.

Reads in plan mode fall through step 2 to allow only if a rule covers them or the
human approves; to keep US-011 ("read proceeds normally") true without forcing a
prompt, a read in plan mode is allowed at step 2's else-branch. (Plan mode blocks
mutation; it does not add prompts for reads.)

### 4. Rule model and command-family matching

```go
type Rule struct { Kind ActionKind; Match string }   // Match: command prefix or file path
type RuleSet struct { Allow, Deny []Rule }
```

Command matching tokenizes both the rule's `Match` and the call's `command`
argument on whitespace and matches when the rule tokens are a **prefix** of the
call tokens at token boundaries. `git status` (tokens `[git status]`) covers
`git status --short` (`[git status --short]`) but not `git stash` (token 2
differs) nor `git statusfoo` (token 2 differs) — US-017. File actions match on the
resolved path equality. Deny is checked before allow so deny-wins holds (FR-14).

*Known limitation (Non-Goal, by design):* a chained `git status; rm -rf /` still
matches an allow for `git status` — the soft layer reads words, not shell syntax.

### 5. Permission settings and merge

Add to `config.Settings`:

```go
type PermissionSettings struct {
    Mode  string   `json:"mode,omitempty"`   // default|auto-accept-edits|plan|bypass
    Allow []string `json:"allow,omitempty"`
    Deny  []string `json:"deny,omitempty"`
}
```

Each `Allow`/`Deny` entry is a `"<kind>:<match>"` string (e.g. `"command:git
status"`, `"edit:/path"`) so rules are typed in the flat settings list. `merge`
gains: mode overrides when non-empty; allow/deny lists **append** home then
project (both layers apply, project precedence by ordering, deny-wins makes order
safe — US-019). Default mode is `default`.

### 6. Remembering and persistence

A `Remember`-set decision is converted to a `Rule` (kind from the call's action
kind; match from the command's program+first-subcommand for commands, or the file
path for edits — the "lean specific" default from PRD Open Questions) and:
- added to the in-memory `RuleSet` immediately (US-007), then
- persisted by read-modify-write of the **project** `.coragent/settings.json`:
  load current file, append the rule string, marshal, write. Unrelated fields are
  preserved because we round-trip the whole struct (US-008). A save error is
  logged via `slog` and swallowed — the action still runs (US-008 AC3).

### 7. Fail-safe and one-decision tolerance

`Decide` emits the `PermissionRequestedEvent` (the engine allocates the reply
channel **buffered cap 1**, sets `ReplyPath`), then:

```go
select {
case d := <-reply: // honor first
case <-ctx.Done(): return deny("permission timed out: " + ctx.Err())
}
```

The turn's deadline/cancellation rides on `ctx` (US-025) — no separate timer
needed; `emit` already returns an error on cancel and the engine denies. The
buffered-cap-1 channel tolerates a second send without wedging the frontend: the
first is consumed by the engine, the second lands in the freed buffer slot and is
never read (US-026). Only the first decision is acted on.

### 8. Wiring from settings

`pkg/agent` builds the engine from loaded `PermissionSettings` and injects it into
`executor.Stages.Permission` when constructing the default executor. A new
`SessionConfig` field (or settings plumbing) supplies the permission config;
`PermissionRequest` gains an additive `Reason string` field for US-002. The
`Dispatcher` seam and chain order are unchanged.

## Risks / Trade-offs

- **Changing the internal `Decide` arity touches Phase 2 tests/inert stage** →
  Mitigation: it is internal-only; update the placeholder and the executor tests
  in the same change; `pkg/agent` surface unchanged.
- **Adding `Reason` to the public `PermissionRequest`** → additive field, safe for
  named-field construction; document it.
- **Token-prefix command matching is coarse** → accepted Non-Goal; the hard limits
  live in hooks/sandbox; deny-wins keeps the safe case robust.
- **Read-modify-write persistence can race a concurrent edit of settings** →
  acceptable for a single local session in v1; one in-flight turn per session
  bounds concurrency.
- **Buffered-cap-1 reply tolerates exactly one extra answer, not many** → matches
  US-026 (two answers); a third concurrent send would block its sender, which a
  conformant frontend never does.

## Open Questions

- Breadth of a remembered rule (exact vs program+subcommand vs whole family) —
  default leans specific; finer UX choice deferred to Phase 7.
- Save location of a remembered choice (project vs home) — default project;
  per-decision choice deferred to Phase 7.
- Whether to surface the resolution-order trace for debugging — out of scope now.
