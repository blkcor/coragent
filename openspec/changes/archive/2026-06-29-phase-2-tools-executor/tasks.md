# Tasks — Phase 2 Tools & Executor

> TDD: write each test group before its implementation. Every group ends green on
> `go build ./...`, `go test ./...`, `golangci-lint run ./...`. No network in
> tests; use `t.TempDir()` and fakes.

## 1. Public types (additive, `internal/core` + `pkg/agent` re-exports)

- [x] 1.1 Add the tool registration shape: a `Tool` behavior interface (`Descriptor() core.Tool`, `Execute(ctx, args) (string, error)`, `RunsCommands() bool`) in `internal/core`
- [x] 1.2 Add the stage seam interfaces — `PreToolCheck`, `Permission`, `Sandbox`, `PostToolCheck` — in `internal/core` (one method each)
- [x] 1.3 Re-export any new public name an SDK custom-tool author needs from `pkg/agent`
- [x] 1.4 Confirm `git diff` of `pkg/agent` is append-only (no prior phase shape changed)

## 2. Tool catalog (`internal/tools`)

- [x] 2.1 Write catalog tests: advertise exactly the registered set; stable order across runs; duplicate name rejected with first tool intact
- [x] 2.2 Implement the catalog: insertion-ordered registry, register-by-name, `Advertise() []core.Tool`, duplicate-name rejection at wire-up
- [x] 2.3 Tests green

## 3. Output truncation (`internal/executor`)

- [x] 3.1 Write truncation tests: under-budget passes unchanged; over-budget clips on a rune boundary, stays valid UTF-8, carries a legible elision marker
- [x] 3.2 Implement central truncation on a clean rune boundary with the marker
- [x] 3.3 Tests green

## 4. Inert stage placeholders (`internal/executor`)

- [x] 4.1 Write placeholder tests: allow-all permission allows + never edits; never-block checks pass; direct sandbox runs the tool's command path directly
- [x] 4.2 Implement `allowAllPermission`, `neverBlockChecks`, `directSandbox` as one-method drop-ins
- [x] 4.3 Tests green

## 5. The execution chain (`internal/executor`)

- [x] 5.1 Write chain-ordering tests with recording stand-in stages: shell call visits hard-pre → permission → sandbox → execute → hard-post in that exact order; file op skips sandbox; order asserted observably
- [x] 5.2 Write chain short-circuit tests: unknown tool before any stage; bad arguments before execute; hard-pre block stops permission/sandbox/tool; permission deny stops sandbox/tool; edited args re-validated then tool runs on them; hard-post veto turns success into error; all returned as `ToolResult{IsError}` tied to the call ID
- [x] 5.3 Write cancellation test: cancel mid-path returns a cancellation error result and stops child work
- [x] 5.4 Implement `executor.New(catalog, stages...) core.Dispatcher`: resolve+validate → hard-pre → permission → sandbox (command tools only) → execute → hard-post → truncate; sandbox routing keyed off `RunsCommands()`
- [x] 5.5 Implement short-circuit + edited-arg re-validation + failures-as-results (nil Go error except genuine harness breakage)
- [x] 5.6 Tests green

## 6. Built-in: read file (`internal/tools`)

- [x] 6.1 Write read tests: whole-file line-referenced; offset+limit window; missing/directory/binary/unreadable/out-of-range → clean error, no raw bytes
- [x] 6.2 Implement the read tool (`RunsCommands()=false`)
- [x] 6.3 Tests green

## 7. Built-in: write file (`internal/tools`)

- [x] 7.1 Write write tests: parent-creation scaffolds folders; replace existing contents; concise confirmation does not echo content
- [x] 7.2 Implement the write tool (`RunsCommands()=false`)
- [x] 7.3 Tests green

## 8. Built-in: precise edit (`internal/tools`)

- [x] 8.1 Write edit tests: unique match replaced; ambiguous (>1) rejected with count, file byte-for-byte unchanged; replace-all replaces every occurrence; missing target rejected unchanged; no-op (target==replacement) rejected unchanged
- [x] 8.2 Implement the edit tool with the uniqueness contract (`RunsCommands()=false`)
- [x] 8.3 Tests green

## 9. Built-in: shell command (`internal/tools`)

- [x] 9.1 Write shell tests: combined output + exit code; non-zero exit → error result with output; timeout kills with partial output and no orphan; cancellation stops child
- [x] 9.2 Implement the shell tool with `exec.CommandContext`, process-group kill, time budget (`RunsCommands()=true`)
- [x] 9.3 Tests green

## 10. Built-in: content search (`internal/tools`)

- [x] 10.1 Write content-search tests: known string → `file:line:match` results scopable by path/glob/case; no matches → successful "no matches"; `rg` absent → clear error (skip rg-dependent assertions when binary missing)
- [x] 10.2 Implement the content-search tool shelling out to `rg` (`RunsCommands()=false`)
- [x] 10.3 Tests green

## 11. Built-in: file-pattern search (`internal/tools`)

- [x] 11.1 Write file-find tests: matching paths in stable order with noise dirs skipped; no matches → successful empty; bad root → error
- [x] 11.2 Implement the file-find tool walking the root with a glob (`RunsCommands()=false`)
- [x] 11.3 Tests green

## 12. Wire-up, custom-tool parity & verification

- [x] 12.1 Register the six built-ins through the catalog; switch `SessionConfig.Dispatcher` default from `StandIn` to the real executor seeded with built-ins + inert stages
- [x] 12.2 Write custom-tool parity test: a registered custom tool travels the identical path, is sandboxed only if it declares command execution, and is truncated by the same budget
- [x] 12.3 Add/extend the demo so the six capabilities run through the one path end-to-end
- [x] 12.4 `go build ./...`, `go test ./...`, `golangci-lint run ./...` all green
- [x] 12.5 Verify invariants: one dispatch path, no `internal/` imports a frontend, `pkg/agent` diff append-only, prior phases' contracts unchanged
