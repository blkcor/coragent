## Why

Phase 7 is the point where Coragent becomes a daily-driver product and proves that the public SDK is sufficient for a replaceable frontend. The existing PRD describes a minimum transcript, but the current public contract cannot honestly render several product-critical states, including safe reasoning summaries, continuous context usage, pre-apply diffs, structured omissions, effective capabilities, or stable subagent provenance.

## What Changes

- Deliver a keyboard-first Bubble Tea v2 application with a responsive transcript, a real position-aware caret in the multiline composer, streaming-safe Markdown that progressively styles each recognizable active construct from its first visible delta batch, compact tool timeline, reasoning-summary disclosure, diffs, permission and argument-editing flows, mode and safety chrome, context usage, capability inspection, animation, shortcuts, visible scrollbar feedback, focus-independent mouse-wheel-only scrollback, and deterministic error/cancel/quit behavior.
- Keep transcript scrolling orthogonal to composer focus so wheel input preserves the draft, logical insertion point, and visible caret. Bubble Tea's standard mouse mode has no wheel-only reporting option, so mouse tracking stays active and Coragent ignores non-wheel background pointer reports; terminal-native selection and copy use the terminal-owned `Shift/Option+drag` bypass advertised by the UI rather than an ordinary unmodified drag claim.
- Add an opt-in, versioned observed-run API and session descriptor while preserving the existing `Session.Run`, `Provider`, `RunEvent`, event ordering, and one-execution-path contracts unchanged.
- Adapt permission prompts emitted by caller-owned legacy dispatchers into an explicitly limited observed permission protocol, so `RunObserved` never drops a reply path or pretends custom dispatch owns rich preview, revision, or grant features.
- Carry only provider-supplied reasoning summaries. Hidden chain-of-thought is never requested, synthesized, persisted, or displayed; unsupported providers degrade to activity-only feedback.
- Produce structured action previews after hook and permission argument edits but before mutation, so file changes and approval prompts describe the action that will actually run.
- Distinguish UI folding, irreversible tool-output truncation, reply length cutoff, content filtering, and future compaction instead of parsing display strings or pretending omitted content can be recovered.
- Expose typed context usage with an explicit source (`estimated` or `provider`) and expose the session's effective model, permission-control ownership, typed mode when the standard engine owns it, sandbox posture, tools, hooks, and optional capability providers without leaking secrets.
- Keep child-agent raw work isolated. The root UI receives stable subagent identity, parent/depth provenance, lifecycle outcome, and routed permission requests, but not child reasoning, text, tool logs, hooks, or context.
- Promote settings/bootstrap behavior needed by the binary to the public SDK so `cmd/coragent` and `tui` never import `internal/*`.
- Give the first-party bootstrap a non-empty Coragent product framing that distinguishes the Coragent assistant identity from its replaceable model backend, while leaving an SDK embedder's explicit `SessionConfig.SystemPrompt` authoritative.
- Replace the old Phase 7 assumption that no additive SDK work is allowed; all readiness work is frontend-agnostic and legacy-compatible.
- Do not add a skills runtime or MCP client in this change. The UI renders those categories only when a future or custom capability provider truthfully reports them; it never shows fabricated loaded counts.
- Continue to defer multiple sessions/tabs, application-handled mouse clicking, selection, dragging, scrollbar manipulation, and other pointer interaction beyond transcript wheel scrolling, user-selectable themes, parallel tool execution, and a slash-command palette. Terminal-native modifier-drag selection remains terminal behavior, not a Coragent pointer feature.

## Capabilities

### New Capabilities

- `tui-frontend`: Full terminal application behavior, layout, rendering states, keyboard model, safety interactions, responsiveness, accessibility fallbacks, and visual verification.
- `session-observability`: Versioned observed-run events and immutable session descriptors for reasoning summaries, context usage, omissions, capabilities, stable correlation, and legacy projection.
- `action-preview`: Frontend-agnostic preparation of the effective action and a structured, bounded preview before any mutation occurs.

### Modified Capabilities

- `configuration`: Add public settings discovery and validated session bootstrap without exposing secrets or importing internal configuration packages.
- `model-backend`: Add an optional rich-provider extension for reasoning summaries, usage, and distinct reply cutoff reasons while preserving the required provider interface.
- `agent-loop`: Drive one rich internal event path, expose the observed stream, and project it to the unchanged legacy run contract with one authoritative terminal outcome.
- `context-manager`: Report structured estimated usage at stable lifecycle points while retaining the current non-compacting v1 history behavior.
- `tool-executor`: Expose effective prepared arguments, structured omissions, previews, correlation, and duration without changing the execution chokepoint.
- `builtin-tools`: Generate truthful create/write/edit previews before mutation and keep result behavior compatible.
- `tool-catalog`: Describe the effective advertised-and-executable capability inventory, including source and availability, without widening execution authority.
- `permission`: Expose permission-control ownership and typed current mode when the standard engine owns it, and bind each request to the effective preview, origin, remembered rule, and optional sandbox grants.
- `hooks`: Re-run hard before-tool checks for human-revised arguments so argument editing cannot bypass an unconditional gate.
- `subagents`: Add stable lifecycle provenance and outcomes while retaining result-only return and raw-child-event isolation.

## Impact

- Public API: additive observed-run, descriptor, permission ownership and typed engine mode, bootstrap, usage, omission, preview, and provenance types under `pkg/agent`; the first-party bootstrap supplies Coragent product framing, while explicit SDK session construction and its system prompt remain caller-owned. Existing APIs remain source-compatible and documented in-contract behavior remains compatible. Two fail-safe clarifications are explicit: the already documented between-turn string mode setter now rejects mid-run misuse, and a legacy edited approval requires a fresh prompt if rerun hard hooks change the displayed arguments.
- Harness: `internal/sessionrun`, provider parsing, loop/context, executor, tools, permission, catalog, and subagent orchestration emit richer frontend-neutral facts through the same execution path.
- Frontend: replaces `tui/doc.go` and the placeholder `cmd/coragent/main.go` with a tested Bubble Tea v2 client that imports only `pkg/agent`.
- Dependencies: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`, and `charm.land/glamour/v2`, pinned in `go.mod` during implementation.
- Verification: offline fake-provider/fake-session tests, reducer and golden-render tests, PTY smoke tests, race/cancellation checks, real-terminal visual checks across compact and wide sizes, full Go verification, and strict OpenSpec validation.
