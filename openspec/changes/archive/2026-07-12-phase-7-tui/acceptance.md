# Phase 7 acceptance evidence

Date: 2026-07-12

This ledger maps every requirement—and therefore every named scenario nested
under that requirement—in the Phase 7 delta specs to executable evidence. A row
that names multiple tests uses their table/subtest cases collectively for all
scenarios in the requirement. Source-compatibility fixtures are called out
explicitly where the acceptance condition is an unchanged prior contract.

## Action preview

| Requirement (all scenarios) | Evidence |
|---|---|
| Side-effect-free action preparation | `internal/tools/prepared_file_test.go`; executor preparation-failure paths in `internal/executor/rich_permission_test.go`; cancelled-context precheck in `prepareFileCandidate` exercised by package race/full tests |
| Effective arguments govern every prepared revision | `TestRichDispatchPreparesAfterHookAndBeforePermission`, `TestRichPermissionRevisionRequiresFreshRequestAndFinalAllow`, hook-block/preparation-failure revision tests |
| Structured previews describe the candidate effect | prepared create/edit/binary tests; `TestGenericPreparedDeletePreviewDoesNotRegisterDeleteBuiltin` |
| Preview payloads are bounded without hiding loss | `TestPreparedPreviewBoundsPreserveAggregateCounts`; TUI local-fold and omission tests |
| Approved preview and committed mutation stay consistent | `TestPreparedFileCommitRejectsIdentityAndPathRaces`, hard-link/symlink/unsupported-identity tests, executor preview-delivery test |

## Agent loop

| Requirement (all scenarios) | Evidence |
|---|---|
| Single run entry point | `TestRunAndRunObservedShareOneInFlightGuard`; legacy and observed session tests |
| One authoritative rich internal lifecycle | `internal/loop/rich_test.go`; `pkg/agent/legacy_compatibility_test.go` |
| Deterministic unchanged legacy projection | legacy compatibility fixtures and `TestRunObservedLegacyToolProjectionDoesNotInventRichLifecycle` |
| Complete ordered observed projection | `TestRunObservedTextEnvelopeAndOrder`, rich usage/omission ordering tests |
| Optional rich-provider selection and legacy adaptation | `TestRunRichSelectsOptionalProviderOnceAndKeepsSummaryOutOfConversation`; legacy fallback observed tests |
| Reasoning summary is observation only | rich loop summary/non-persistence test and OpenAI safe-summary tests |
| Usage and omission facts do not change control flow | `TestRunObservedRichUsagePrecedenceAndOmissionOrdering` |
| Stable origin propagation on the root stream | public subagent observed tests |
| One terminal computation across public projections | session terminal tests, observed cancellation tests, run-finished-hook tests |
| Projection compatibility is tested offline | `pkg/agent/legacy_compatibility_test.go` |
| Provider-channel waits remain cancellation-aware | rich and legacy never-closing-provider cancellation tests in loop/session packages |

## Built-in tools

| Requirement (all scenarios) | Evidence |
|---|---|
| Write and edit prepare exact file candidates | create, replacement, multi-hunk, invalid-edit, byte-limit, target-race, symlink, hard-link, atomic-cancel, umask, exact ACL/xattr/security-metadata preservation and inherited-ACL refusal, final-swap rollback, and exclusive-create cases in `prepared_file*_test.go` |
| File diffs remain structured and bounded | empty/binary/invalid-encoding, multi-hunk, computation fallback, retained bound, aggregate-count, and trailing-newline cases in `prepared_file_test.go` |

## Configuration

| Requirement (all scenarios) | Evidence |
|---|---|
| Public settings discovery for first-party clients | `TestLoadSettingsUsesCanonicalHomeProjectMergeAndEnvironmentResolution`, missing/malformed settings test |
| Validated public session bootstrap | bootstrap construction/validation tests and `cmd/coragent/main_test.go` |
| First-party Coragent product framing | `TestBootstrapSendsCoragentProductIdentity`, competing-persona case, explicit prompt preservation test |
| Secret-safe public configuration boundary | settings representation and bootstrap diagnostic secret-hygiene tests; descriptor tests |
| Frontend presentation remains outside harness settings | import audit plus explicit visual-mode environment tests in `cmd/coragent` |
| Legacy loading behavior remains compatible | internal config suite, remembered-rule persistence suite, legacy compile fixture |

## Context manager

| Requirement (all scenarios) | Evidence |
|---|---|
| Structured context-usage snapshots | manager known/unknown snapshot test; observed usage tests |
| Estimate the effective assembled request | `TestEstimateRequestTokensIncludesEffectiveConversationAndTools` |
| Stable context-usage lifecycle points | rich usage ordering and provider precedence tests |
| Provider usage has explicit precedence and validation | valid/mismatched/missing/malformed provider-usage cases |
| Truthful context-window semantics | known/unknown/over-budget manager and observed tests |
| Structured usage preserves legacy warning behavior | legacy warning compatibility tests plus observed structured cases |
| Usage observation never compacts history | no-compaction assertions in `pkg/agent/run_observed_test.go` |
| Offline deterministic usage testing | fixed estimator and scripted rich provider cases |

## Hooks

| Requirement (all scenarios) | Evidence |
|---|---|
| Before-tool input shaping | executor event-aware tests, legacy replacement reapproval test, rich revision hook block/composition tests |

## Model backend

| Requirement (all scenarios) | Evidence |
|---|---|
| Optional rich-provider extension | legacy provider compile fixture and optional-rich selection test |
| Ordered rich reply protocol | OpenAI rich stream ordering, malformed sequence, and failure cases |
| Provider-designated display-safe reasoning summaries | safe summary/raw reasoning/unknown reasoning field tests |
| Structured provider usage facts | final/partial/absent/malformed/ignored-stream-option usage cases |
| Optional provider context-window metadata | known and absent context-window cases |
| Distinct rich reply termination reasons | `TestOpenAIProviderRichFinishReasonsStayDistinct` plus provider-specific/failure cases |
| Stable legacy reply projection | recorded legacy provider tests and compatibility fixtures |
| Legacy-provider rich fallback | loop adapter and observed cutoff tests |
| Offline-testable rich provider | scripted HTTP/fake-provider tests only; no network model is used |

## Permission

| Requirement (all scenarios) | Evidence |
|---|---|
| Permission request states what and why | core observed request tests, public child-origin test, TUI modal/golden tests |
| Modes switch between turns and start from configuration | typed/string mode tests, mid-run rejection tests, TUI safe-cycle tests |
| Edit arguments before approving with re-validation | executor legacy/rich revision tests and `TestRichPermissionRevisionGrantRememberAndDraftRetention` |
| Exactly one decision honored | core concurrent reply races, stale/mismatched request tests, TUI submission-guard tests |
| Per-call sandbox grants are explicit and ephemeral | `TestRichSandboxGrantsAreValidatedOneCallAndNotRemembered`; TUI grant editor test |
| Interactive bypass confirmation belongs to the frontend | `TestBypassConfirmationHelpInspectorAndUnknownContext`, dismissal/rejection tests, direct typed SDK bypass test |

## Session observability

| Requirement (all scenarios) | Evidence |
|---|---|
| Opt-in versioned observed runs | public compile signatures and dual run tests |
| Stable envelope identity, sequence, and origin | observed envelope/order/consecutive-run/subagent-origin tests |
| Compatible observed-event evolution | public schema validation, unsupported-version, unknown-kind, payload-mismatch tests |
| Immutable truthful session descriptor | descriptor clone/ownership/capability tests |
| Descriptor and event secret hygiene | credentialed descriptor and safe-error tests |
| Typed observed payload vocabulary | `pkg/agent/observed_test.go`, rich tool/permission adapter tests |
| Provider-summary-only reasoning boundary | provider/loop/public no-chain-of-thought tests |
| Structured context-usage source | estimated/provider/unknown-window observed tests |
| Structured omission taxonomy | executor preview/output omissions, provider cutoff/filter, redaction/no-compaction tests, TUI every-omission test |
| Exactly one observed terminal envelope | success/cancellation/unread cancellation tests |
| Offline deterministic observability testing | scripted provider and legacy fallback suites |

## Subagents

| Requirement (all scenarios) | Evidence |
|---|---|
| Stable delegated-agent provenance | duplicate-label, nested-parent, and child-permission origin tests |
| Typed subagent lifecycle and terminal outcome | completed/failed/cancelled/step-limit/depth-refusal tests |
| Raw child activity remains isolated from the root stream | orchestrator strict-boundary tests and public observed raw-isolation tests |

## Tool catalog

| Requirement (all scenarios) | Evidence |
|---|---|
| Effective tool inventory is truthful and deterministic | `pkg/agent/descriptor_test.go` advertised/registered/custom-dispatcher/order cases |
| Capability inventory never grants execution authority | descriptor authority and subagent capability-regression tests |
| Optional capability categories come only from reporters | supported-empty/unsupported/available/unavailable reporter tests |

## Tool executor

| Requirement (all scenarios) | Evidence |
|---|---|
| Single ordered execution chain | executor event-aware/prepared/rich-permission tests |
| Central output truncation | structured omission + legacy marker tests, in-budget/replacement/error cases |
| Correlated effective action facts | rich dispatch ordering/revision/completion tests and legacy projection fixture |
| Preview precedes every mutation | prepared side-effect-free tests and preview-delivery failure test |
| Tool lifecycle duration is measured consistently | executor success/denied/failed event tests; TUI duration render fixture |

## TUI frontend

| Requirement (all scenarios) | Evidence |
|---|---|
| Public-SDK-only frontend boundary | `internal/architecture/import_boundary_test.go`; adapter live reply test |
| Deterministic startup and ready state | `tui/app_test.go`; operational startup-error fixture |
| Real caret and position-aware composer editing | composer Unicode/caret/resize tests and normal PTY interaction |
| Responsive layout across supported terminal classes | layout tests and 25-case golden matrix |
| Explicit too-small behavior | too-small modal reducer and golden cases |
| Ordered streaming transcript | complete reducer run and streaming block tests |
| Streaming-safe Markdown | adversarial Markdown/cache/chunk-boundary suite |
| Safe reasoning-summary disclosure | rich transcript reducer, inspector expansion, provider unsupported/empty fixtures |
| Correlated tool cards and inline lifecycle updates | complete reducer run, rich tool render, theme lifecycle tests |
| Effective action previews and diff rendering | rich/golden diff fixtures, generic preview and unavailable preview executor cases |
| Honest content folding and omission labels | cache/fold tests, every-omission test, continuation draft tests |
| Modal permission control | modal priority, viewport, too-small, child/legacy projection tests |
| Permission decisions, argument editing, and sandbox grants | rich editor/grant/remember test, decision table, submission guard, PTY revision flow |
| Deterministic keyboard routing and shortcut map | keyboard/composer-scroll tests, overlay reducer tests, PTY help/PageUp/wheel flow |
| Visible and safely changeable permission mode | safe cycle, bypass confirmation/dismissal/rejection, external ownership tests |
| Truthful activity, context, capability, and sandbox chrome | rich context threshold test, descriptor adapter test, sandbox safety notice/golden |
| Truthful capability and hook inspection | inspector test and descriptor capability-provider tests |
| Subagent and hook activity in the root narrative | rich reducer render and public raw-child-isolation tests |
| Stable scroll pinning and unread feedback | `tui/composer_scroll_test.go` including direct drag and pane clipping |
| Bounded animation and reduced motion | rail geometry/cadence tests, reduced-motion goldens, idle tick logic tests |
| Color-independent and Unicode-safe presentation | theme/layout/Unicode tests and visual golden matrix |
| Terminal-control sanitization | every-byte sanitizer splits, Markdown output filter, PTY hostile OSC fixture |
| Recoverable errors and authoritative terminal outcomes | app protocol/EOF/terminal tests and operational rich fixture |
| Cancellation and clean shutdown | reducer cancellation/quit tests and normal/forced/panic PTY tests |
| Offline reducer, golden, and PTY verification | rich transition snapshots, golden matrix, benchmarks, PTY suite, import audit, race gate |

## Non-runtime design and terminal gates

- The reviewer granted the permanent Phase 7 Figma waiver recorded in
  `figma-handoff.md`; local versioned SVG/PNG, `ui-design.md`, goldens, runtime
  fixtures, and the handoff are the final design evidence. The empty Figma file
  is not represented as populated.
- Automated Unix PTY coverage is complete. The named real-terminal manual
  matrix was archived as an explicit known-unverified portability worksheet;
  no pending row is represented here as a manual pass.
- Final verification on 2026-07-12 passed: `gofmt`,
  `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`,
  `golangci-lint run ./...` (official v2.12.2, workspace-local binary),
  `go build ./cmd/coragent`, `git diff --check`, and
  `openspec validate phase-7-tui --strict`.
