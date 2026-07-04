## 1. Core types and seam (internal/core)

- [x] 1.1 Add `ActionKind` enum (`ActionUnknown`, `ActionRead`, `ActionEdit`, `ActionCommand`) and the optional `ActionClassifier` interface to `core/tool.go`
- [x] 1.2 Change `core.Permission.Decide` to take an `ActionKind` argument (`Decide(ctx, call, kind, emit)`)
- [x] 1.3 Add additive `Reason string` field to `core.PermissionRequest`
- [x] 1.4 Re-export new public names (`ActionKind` constants, `ActionClassifier`) from `pkg/agent`

## 2. Executor wiring (internal/executor)

- [x] 2.1 Compute each resolved call's `ActionKind` (classifier → kind; else `RunsCommands()` → command; else unknown)
- [x] 2.2 Pass the computed kind into `stages.Permission.Decide`
- [x] 2.3 Update inert `allowAllPermission` placeholder to the new `Decide` arity
- [x] 2.4 Update executor chain tests to the new arity; assert action kind reaches the permission stage

## 3. Built-in action classification (internal/tools)

- [x] 3.1 Implement `ActionClassifier` on read, content-search, file-find → `ActionRead`
- [x] 3.2 Implement `ActionClassifier` on write, edit → `ActionEdit`; shell → `ActionCommand`
- [x] 3.3 Table test asserting each built-in's classification

## 4. Permission settings (internal/config)

- [x] 4.1 Add `PermissionSettings{Mode, Allow, Deny}` to `Settings` with JSON tags and defaults (`mode: default`, empty lists)
- [x] 4.2 Extend `merge` to overlay mode (non-empty wins) and append allow/deny lists home-then-project
- [x] 4.3 Tests: permission section parsed, defaults applied when absent, home+project merge with project precedence

## 5. Permission engine (internal/permission)

- [x] 5.1 Define `Mode`, `Rule{Kind, Match}`, `RuleSet{Allow, Deny}`, and parse `"<kind>:<match>"` rule strings from settings
- [x] 5.2 Implement command-family matching (token-prefix at boundaries) and file-path matching
- [x] 5.3 Implement `Engine` with guarded current mode and `SetMode` for between-turn switching
- [x] 5.4 Implement `Decide` resolution order: bypass → plan-block (mutating/unknown) → deny rule → allow rule → auto-accept-edits → ask
- [x] 5.5 Emit `PermissionRequestedEvent` with reason and buffered-cap-1 reply channel; honor first decision, ignore extras
- [x] 5.6 Fail safe on `ctx.Done()` → deny with timeout reason; never hang
- [x] 5.7 Apply edited arguments from the decision into the `PermissionResult` (executor re-validates)
- [x] 5.8 Implement remembering: convert decision to a rule (specific default), add in-memory immediately
- [x] 5.9 Persist remembered rule by read-modify-write of project `.coragent/settings.json`; preserve unrelated fields; swallow+log save errors

## 6. Engine wiring (pkg/agent)

- [x] 6.1 Build the engine from `PermissionSettings` and inject into `executor.Stages.Permission` for the default executor
- [x] 6.2 Plumb permission config through `SessionConfig`/settings; expose mode-switch entry point for between-turn changes
- [x] 6.3 Confirm `Dispatcher` seam and chain order unchanged; Phase 0–2 public contracts intact

## 7. Tests and demo (offline, fake frontend)

- [x] 7.1 Fake frontend that drains events and answers permission requests with scripted decisions
- [x] 7.2 Scenario tests for every permission spec requirement (three outcomes, four modes, rules, deny-wins, family matching, merge, remember+persist, edit+revalidate, fail-safe, duplicate-answer)
- [x] 7.3 Throwaway console demo: first-prompt-then-remembered-auto-allow, plan-mode refusal with reason, bypass with no prompt — reproducible in CI with scripted answers
- [x] 7.4 `go build ./...`, `go test ./...`, `golangci-lint run ./...` all pass
