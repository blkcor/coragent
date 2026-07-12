## Context

Coragent has completed the harness phases, but its product surface is still two placeholders: `tui/doc.go` contains only a package comment and `cmd/coragent/main.go` prints one line. The existing public SDK already exposes root assistant text deltas, tool start/finish, permission requests, advisory status, hook outcomes, subagent labels, budget warnings, cancellation, and one authoritative run terminal event. It does not expose reasoning content, exact or continuous context usage, effective capabilities, structured truncation, distinct reply cutoff reasons, final prepared tool arguments, or a real pre-apply diff.

The current Phase 7 PRD also contains a planning contradiction. It says missing frontend concepts must be promoted to the public SDK, then lists every harness or public-surface change as a non-goal. This design resolves that contradiction with additive, frontend-neutral APIs while leaving every Phase 0 through 6 required API and behavior intact.

The primary stakeholder is a developer working in one terminal session for hours. The secondary stakeholder is an SDK developer copying the reference frontend pattern. The interface must remain honest under unsupported provider features, custom dispatchers, degraded sandboxing, narrow terminals, cancellation, huge output, and untrusted terminal text.

Reference products were initially used for mechanisms. After the first runnable review, Claude Code's terminal narrative became the explicit layout reference: Coragent adopts its top-aligned conversational flow and compact action hierarchy while retaining its own typed mode, sandbox, permission, and provenance facts.

- OpenAI Codex models reasoning, command/file items, approvals, and context compaction as typed client events, and keeps animations, raw output, status-line content, and reasoning visibility configurable in its official [app-server protocol](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md) and [configuration source](https://github.com/openai/codex/blob/main/codex-rs/core/src/config/mod.rs).
- Gemini CLI exposes approval modes, model/context/sandbox footer information, and explicit tool discovery in its official [configuration](https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/configuration.md) and [tools reference](https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/tools.md).
- Claude Code treats permission modes and MCP as runtime capabilities rather than decorative UI claims in its official [CLI reference](https://docs.anthropic.com/en/docs/claude-code/cli-usage).

The transferable pattern is a quiet transcript with inline work and focused approvals. Coragent deliberately avoids a permanently open dashboard, raw chain-of-thought, fabricated capability counts, and a flat command palette.

## Goals / Non-Goals

**Goals:**

- Ship a responsive, keyboard-first, daily-driver TUI built only on `pkg/agent`.
- Make streaming text, reasoning summaries, tools, diffs, permissions, hooks, subagents, omissions, context usage, modes, sandbox posture, and terminal outcomes visually distinct and causally ordered.
- Add one opt-in rich observation contract and preserve the existing `Run`, `Provider`, `RunEvent`, tool chokepoint, sequential execution, and permission reply behavior.
- Guarantee that any displayed mutation preview describes the final effective arguments and cannot silently go stale before execution.
- Make unsupported features disappear or state `unsupported`; never infer rich facts from display strings.
- Keep rendering deterministic, bounded, cancellable, testable offline, safe for untrusted text, and useful from 60x20 through 160x48 cells.
- Make the idle composer unambiguous through a real position-aware caret, and make overflow history discoverable through a non-color-dependent scrollbar plus focus-independent mouse-wheel-only navigation.
- Make the first-party assistant identify as Coragent while describing any configured provider or model only as a replaceable backend fact.
- Deliver reviewable visual tokens, components, state frames, event fixtures, and an implementation-ready task breakdown before coding.

**Non-Goals:**

- A skills runtime, MCP client, or change to the architecture's v1 MCP exclusion.
- Raw or hidden model chain-of-thought.
- Multiple sessions, tabs, application-handled mouse clicks, selection, scrollbar dragging, other pointer gestures beyond transcript wheel scrolling, user-selectable themes, a slash-command palette, or parallel tool execution. Terminal-native modifier-drag selection is not an application gesture.
- Full child-agent transcript mirroring; result-only return and child isolation remain intact.
- Context compaction. The contract can describe a future compaction omission, but this change does not compact history.
- Images, arbitrary HTML, executable terminal hyperlinks, or remote content inside assistant Markdown.

## Decisions

### Decision: Deliver three independently mergeable implementation slices

1. **SDK readiness** adds observed runs, descriptors, typed usage, optional rich-provider facts, prepared actions, public bootstrap, and compatibility tests. It is useful to any future frontend even if the TUI never ships.
2. **Core TUI** adds the app shell, transcript, composer, tool and permission flows, mode/cancel/quit behavior, compact layouts, and fake-session acceptance tests. It is a usable daily driver without the final polish slice.
3. **Daily-driver polish** adds streaming-safe Markdown, reasoning disclosure, capability inspector, context meter, motion, wide layouts, no-color/ASCII fallbacks, golden renders, PTY smoke tests, and final visual QA.

Each slice ends in a working state and can be reverted independently. There is no investigation-only phase.

Alternative considered: implement the PRD's minimal TUI first and retrofit observability later. Rejected because it would hard-code inferred states and make the first frontend validate an insufficient SDK rather than the intended public contract.

### Decision: Add an opt-in observed-run contract and keep the legacy stream unchanged

`Session.RunObserved` is additive. It starts the same single in-flight run and returns a stream of versioned observed envelopes. `Session.Run` remains available and is a deterministic projection of the same internal rich event path. Calling either API observes the same one-run limit; no second execution path or duplicate event producer is introduced. Provider invocation is explicitly selected at that shared path: legacy `Run` always calls the required provider method, while `RunObserved` may opt into the provider's additive rich method and documented optional usage request fields. This preserves compatible legacy endpoints without issuing a probe or duplicate request. A provider that implements only the required method is adapted identically for both public run APIs.

Each observed envelope carries:

| Field | Meaning |
|---|---|
| `SchemaVersion` | Starts at `1`; consumers reject unsupported values cleanly |
| `RunID` | Stable opaque ID for one root run |
| `Sequence` | Strictly increasing from one, with no duplicates within the run |
| `Timestamp` | Harness observation time for duration and ordering display |
| `Origin` | Stable agent ID, optional parent ID, depth, and delegation call ID |
| `Kind` | Closed v1 event-kind value with one typed payload |

The closed v1 kind set is: `run_started`, `status_changed`, `assistant_started`, `assistant_text_delta`, `assistant_reasoning_summary_delta`, `assistant_finished`, `tool_proposed`, `tool_prepared`, `permission_requested`, `tool_executing`, `tool_finished`, `context_usage_updated`, `omission_reported`, `hook_outcome`, `subagent_started`, `subagent_finished`, `warning`, `error`, and `run_finished`. A producer that needs a new kind or incompatible meaning uses a new schema version. An unknown kind declared as schema v1 is a protocol error and is never silently skipped.

`RunFinished` is the only authoritative run terminal event. Advisory `idle` and subagent-finished statuses are not required on a cancelled stream. The TUI restores its ready state from the terminal event, not from status text.

Legacy projection preserves existing order and payload semantics. Rich-only reasoning, usage, preparation, omission, provenance, and capability facts are dropped, not encoded into `Status`, `Warning`, JSON strings, or new fields on `RunEvent`. Existing custom providers implement no new required method.

Alternative considered: add fields and event constants directly to `RunEvent`. Rejected because exported Go structs can be constructed positionally by external callers, and display-string reuse would not be a typed contract.

### Decision: Expose immutable session description and typed permission mode

`Session.Describe` returns a deep-copied, secret-free descriptor before the first run and between turns. It includes effective model identity, provider feature support, working directory, permission ownership plus a typed mode when the standard engine owns it, sandbox level and reason, context-window knowledge, and the effective advertised-and-executable capability inventory.

Capability entries contain kind, name, source, availability, and a safe status detail. Kinds include tool, hook, sandbox, subagent, and optional external categories. `skill` and `mcp` entries appear only when a custom or future runtime reports them. API keys, system prompts, hook command arguments, persisted rules, environment values, and credentials never appear.

The standard engine's permission mode becomes a public enum with a getter and a typed setter. The existing string setter stays as a compatibility wrapper for its documented between-turn use. Both setters return the same stable in-flight error when called during a run; the old method's prior unguarded mid-run effect was outside its documented contract and is now made fail-safe. A custom dispatcher may report permission control as externally owned or unsupported without inventing an engine mode. The UI updates its chip only after the typed setter succeeds.

`RunObserved` remains usable with an existing caller-owned dispatcher that emits a legacy permission event. The rich boundary wraps that event as protocol `legacy_one_shot`, assigns request and origin correlation, marks preview, rich revision, schema-aware edit, and per-call grants unsupported, and exposes allow/deny plus remember only when the legacy request supplies a safe remembered rule. Its exactly-once wrapper forwards one ordinary legacy decision with no invented edited arguments or grants. Unsupported rich decisions return `validation_rejected` and leave the prompt open. This adapter prevents a waiting custom dispatcher from deadlocking while keeping external ownership explicit.

Alternative considered: let the TUI copy its startup configuration and hard-code built-ins. Rejected because default construction can add `task`, custom handlers or dispatchers change the effective catalog, and copied mode can drift from the engine.

### Decision: Give first-party bootstrap a Coragent product identity without overriding SDK callers

The public `Bootstrap` path used by `cmd/coragent` seeds every first-party session with a non-empty product framing. That framing identifies the assistant as Coragent, describes Coragent as the coding-agent product, and treats the configured provider and model as a replaceable backend rather than as the assistant's product identity. When asked who it is, the first-party session therefore answers as Coragent and may separately report a backend only when that fact is truthfully available; it does not adopt a provider-trained branded assistant persona.

This default belongs only to first-party bootstrap. An SDK embedder that explicitly supplies `SessionConfig.SystemPrompt` through `NewSession` or `NewSessionWithError` remains authoritative: Coragent does not prefix, replace, or reinterpret that caller-owned framing. Product framing follows the existing secret-hygiene boundary and is not exposed through descriptors, logs, errors, or safe settings views.

Alternative considered: rewrite an assistant answer when it claims to be Claude or another provider persona. Rejected because output filtering is brittle, can alter legitimate content, and fixes the symptom after inference rather than giving the model the correct first-party product context.

### Decision: Extend providers optionally and show only safe reasoning summaries

The required `Provider.StreamReply` contract remains unchanged. An optional rich-provider interface can emit:

- provider-authored reasoning-summary deltas,
- exact prompt, completion, and total token usage,
- model context-window metadata when known,
- distinct message finish reasons for normal stop, tool stop, length cutoff, content filtering, and provider failure.

The OpenAI-compatible backend requests usage when supported and accepts provider-specific reasoning-summary fields only through an explicit adapter. It never requests hidden chain-of-thought. The summary is not appended to the model conversation, not persisted in `Conversation`, and not returned as assistant text.

When unsupported, the observed stream still emits thinking activity and estimated context usage. The UI omits an empty reasoning block and labels estimated context with `est`.

Alternative considered: label assistant prose or elapsed time as reasoning. Rejected because it would misrepresent provider output and encourage accidental chain-of-thought disclosure.

### Decision: Prepare actions twice when permission editing changes arguments

Preparation stays inside the single executor chokepoint:

```text
model call
  -> validate proposed arguments
  -> pre-tool hooks and argument replacement
  -> prepare candidate action and preview
  -> permission request with candidate preview
  -> optional human argument revision
  -> validate, re-run before-tool hooks, and re-prepare the revised action
  -> on success, new permission request for the revised preview
  -> on hard-hook or preparation failure, terminate without a new request
  -> emit final prepared action
  -> sandbox when command-running
  -> execute the prepared action
  -> post-tool hooks
  -> bounded result and structured omission
```

An optional prepared-handler interface is additive to `ToolHandler`. `Prepare` is cancellable and side-effect-free. It returns effective arguments, action kind, a safe summary, a bounded public preview, and an opaque identity-bound commit token. `ExecutePrepared` verifies that token and performs the mutation. Existing handlers keep the legacy execution path and report preview support as unavailable.

File previews use unified hunks and an operation of create, modify, or delete. Prepared target/candidate bytes have a fixed 16 MiB safety ceiling, and line-diff construction has separate 1 MiB combined-input and 20,000-line computation ceilings. Inputs beyond the diff budget produce a metadata-only `preview_budget` omission with exact before/candidate byte sizes and unknown uncomputed aggregate counts. Computed textual bodies are bounded to 64 KiB of valid UTF-8 or 800 logical preview lines, whichever is reached first; terminal wrapping does not change this frontend-neutral bound, and any removed portion carries structured omission metadata. Binary or undecodable files show metadata, not guessed text. Built-in write/edit preparation records target type, no-follow symlink state, parent-directory identity, stable file identity when present, hard-link count, and preimage bytes. Existing targets with a link count other than one fail closed as `hard_link_alias_unsupported`, because Phase 7 cannot enumerate every affected alias truthfully. Commit reacquires the parent and target with no-follow, directory-relative semantics. Existing targets are staged with `fclonefileat(CLONE_ACL)` from the verified file descriptor so ownership, mode, flags, ACLs, and extended attributes survive; the clone's security stat and full bounded `ATTR_CMN_EXTENDED_SECURITY` object are compared exactly with the source, so a destination-inherited ACL difference fails closed. Final publication uses an atomic swap, validates the displaced identity and preimage, and rolls back a validation-to-swap race. New files derive their final mode from an actual umask-governed exclusive create, narrow staging to `0600` while bytes are written, and publish with an exclusive directory-relative rename under the verified parent. Safe parent creation walks from a verified existing ancestor without following links. If the platform or filesystem cannot provide stable identity, link count, metadata cloning, no-follow validation, and atomic exchange, the action fails closed. Cancellation or a write failure before replacement leaves the original intact and removes the temporary candidate. A changed path, parent, type, identity, link state/count, or preimage returns a recoverable stale-preview result without truncating or partially writing the target.

The first preview is shown in the permission modal. Editing is a two-step rich-permission protocol, not an implicit approval: the editor submits a `revise_arguments` decision for the current request. A malformed, schema-invalid, mismatched, or inapplicable reply returns typed validation feedback and leaves that request open, so the user can correct it or deny. Once a revision passes reply and schema validation, the executor closes the request, runs the matching hard before-tool hooks again, and re-prepares the action. A hook block, hook failure, invalid hook replacement, or preparation failure terminates the call without creating another approval prompt. A successful preparation emits a new permission request with a new request ID and preview revision. The user must allow or deny that displayed revision. Each request accepts exactly one valid reply, and no action can execute from an undisplayed, stale revision. Re-running hooks is mandatory because a human edit occurs after the first hard-gate pass and must not create a bypass. The legacy edit-and-allow decision remains available with its existing reply shape and one reply per request. Its edited arguments are revalidated and pass through before-tool hooks; if hooks allow them unchanged, they can execute under that approval, but if hooks replace them, the old approval no longer applies and a fresh legacy permission request must display the replacement before execution. This is a fail-safe restoration of the existing hard-hook invariant, not a second execution path. The observed TUI uses the richer two-request revision flow.

Alternative considered: derive diffs inside the TUI from tool arguments. Rejected because write calls lack the old file, hooks can replace arguments, the frontend must not read around the SDK, and preview/execution races would remain invisible.

### Decision: Give omissions a typed, non-deceptive taxonomy

Harness omission reasons are `output_budget`, `preview_budget`, `provider_length`, `content_filter`, `redacted`, and future `context_compaction`. `output_budget` means tool-result bytes were not retained; `preview_budget` means candidate-action preview content was bounded or intentionally not constructed because its inputs exceeded the safe computation budget. Computed aggregate change counts remain truthful; deliberately uncomputed counts remain explicitly unknown. Metadata includes original and retained bytes/lines when known, scope, and a typed continuation mode of `unknown`, `unavailable`, or `new_user_turn`. `new_user_turn` means the session can accept an editable follow-up prompt; it never promises token-level resume, exact continuation, or automatic submission.

UI folding is local state, not a harness omission event. A folded result retains all received content and can expand. A harness-truncated result displays the retained content plus an explicit `cannot expand` marker. Provider length cutoff offers an editable new-turn continuation draft only when the typed omission says `new_user_turn`. Content filtering is `unavailable` in Phase 7 and shows a neutral non-retry notice. An unrecognized provider-specific incomplete ending becomes a sanitized provider/protocol error, not a guessed omission kind. The UI never parses `[output truncated: ...]` to guess this state on the observed API.

The legacy tool result retains its current text marker so old consumers behave exactly as before.

Alternative considered: use one `truncated` boolean. Rejected because recoverability and user action differ materially among view folding, executor loss, provider cutoff, filtering, and compaction.

### Decision: Report context usage at stable round boundaries with provenance

Before each provider request, the context manager emits an estimate that includes durable conversation, transient injected context, tool schemas, and the pending request framing available to the harness. When a rich provider reports exact usage, a provider-sourced update supersedes the estimate for that round. Each payload includes used tokens, optional window tokens, optional remaining tokens, source, round, and measurement time.

The footer shows `used/window percent` only when the window is known. Estimated values carry `est`; unknown windows show an absolute estimate only. At 80 percent the meter becomes warning-colored, at 95 percent critical-colored. The existing configured budget warning remains an independent legacy advisory and compaction is not implied.

Alternative considered: expose the current `chars/4` value as an exact percentage. Rejected because the provider window can be unknown or model-dependent, and false precision is worse than a clearly labeled estimate.

### Decision: Keep subagent work isolated but give lifecycle events stable provenance

Every root and created child agent receives a stable opaque agent ID. Child lifecycle events carry parent ID, depth, delegation call ID, label, start time, finish time, and an outcome of completed, failed, cancelled, or reached-step-limit. A depth-limit refusal that occurs before child construction remains a recoverable task error and does not fabricate a child lifecycle. Child permissions carry the same origin so the modal can say which delegated task is waiting.

Raw child text, reasoning summaries, tool lifecycle, hooks, warnings, errors, context usage, and terminal events stay private. The parent still receives one final task result through the existing tool path. Duplicate labels are safe because correlation uses IDs.

Alternative considered: merge child streams into the transcript or add a permanent tree pane. Rejected because it breaks Phase 6 isolation, creates interleaving noise, and spends terminal space on an implementation detail.

### Decision: Use a single-owner reducer architecture in Bubble Tea v2

The root `AppModel` owns all state. SDK channel reads, permission replies, cancellation, close, ticks, and resize handling enter the reducer as messages produced by `tea.Cmd`; `Update` never blocks. One event-wait command is re-armed immediately after every event so the SDK stream keeps draining.

An unknown schema-v1 kind, invalid payload, or EOF before `run_finished` is fatal to that observed session. The reducer cancels the run, keeps draining without applying untrusted non-terminal payloads when a channel remains open, disables new runs, and uses the bounded shutdown path. It never invents an authoritative terminal or stops reading in a way that can backpressure the producer.

The TUI depends on a narrow internal `SessionPort` implemented by an adapter around `*agent.Session`. Tests provide a fake port. No production frontend file imports `internal/*`.

State is split into orthogonal dimensions rather than one explosive enum:

- run: booting, idle, running, cancelling, startup-error, quitting;
- focus: composer, transcript, permission, argument-editor, inspector, help;
- scroll: pinned-bottom or browsing-history with unread count;
- mode display: default, auto-accept-edits, plan, bypass, externally-owned, or unsupported;
- terminal: wide, standard, compact, minimal, too-small;
- each transcript block owns its own streaming/final/error/cancelled/folded state.

The reducer indexes tools by call ID while retaining a stable ordered block slice. Completed block renders are cached by width and visual mode. Only the active streaming tail and changed blocks rewrap. Tests inject a clock, so animation and double-Ctrl-C behavior use no real sleeps.

Alternative considered: independent Bubble Tea models reading SDK channels and mutating shared transcript state. Rejected because permission focus, scrolling, cancellation, and quit ordering would race.

### Decision: Treat the transcript as a compact execution narrative

The default screen has no sidebar. A one-line identity row precedes a top-aligned transcript viewport; a hairline composer, status row, and shortcut hint stay anchored at the bottom. Short conversations add unused rows after the transcript rather than before it, so prose begins at the top while input geometry stays stable. Once content reaches the available height, the transcript follows the live tail. A temporary inspector opens on demand.

Block defaults:

- user prompts use a quiet tinted field, begin with `›`, omit role labels and timestamps, and remain fully visible;
- assistant prose is cardless, begins with one active marker, and remains visually dominant;
- provider reasoning summaries are collapsed to a one-line disclosure after completion and expanded on demand;
- running, awaiting-permission, error, and diff tools are expanded as a tool line followed by indented result branches, without a reserved status column;
- successful read/search/shell tools collapse to one summary row;
- a plain result over 12 lines folds locally to the first 8 and last 3 lines with an expandable marker;
- hook blocks and sandbox fallback are safety notices, not permission denials;
- subagents appear as nested lifecycle rows and their final answer remains the normal task tool result.

Assistant Markdown is streaming-safe and progressively styled. A stable completed prefix is rendered and cached with Glamour. The active trailing block is sanitized and re-rendered through a bounded Markdown preview after each visible delta batch, so recognized headings, emphasis, lists, quotes, tables, inline code, and fenced code adopt terminal styling as soon as their current syntax permits. Only the shortest genuinely ambiguous suffix may remain literal; an open fence is rendered as bounded safe code rather than exposing the entire response as raw Markdown. The active block may reflow or restyle when later delimiters arrive, while cached prefix blocks remain byte-for-byte stable. Reply completion performs one deterministic full-source render whose visible result matches rendering the completed source in one pass. Tool output is always sanitized preformatted text, never Markdown. HTML, images, OSC, CSI, C0 controls except newline/tab, and unsafe hyperlinks are stripped or visibly escaped before width measurement.

The composer is a Bubbles v2 textarea, not a manually rendered string. While it is focused and the run is idle, the textarea exposes a real terminal caret at the logical insertion point. Left/right/up/down movement, line-boundary movement, insertion, deletion, multiline drafts, overlay focus transitions, resize reflow, and Unicode grapheme or cell widths all operate on that same logical cursor. Opening a higher-priority overlay hides the composer caret; closing it restores the caret at the preserved insertion point. Transcript wheel scrolling is orthogonal state and never moves focus away from the composer, hides its idle caret, changes the draft, or changes the logical insertion point. A colored border, focus marker, or painted trailing glyph alone does not satisfy this behavior.

When the transcript overflows, one terminal cell is reserved for a vertical scrollbar whose track and thumb remain distinguishable by shape in truecolor, no-color, and ASCII modes. The thumb represents the clamped visible range over rendered history, with a one-cell minimum. Mouse-wheel up/down is the only transcript-history scrolling input; `PageUp`, `PageDown`, arrows, `j/k`, `Ctrl+U/D`, `End`, and `G` never route to background history scrolling. Wheel input changes scroll state without entering a transcript focus mode, so normal composer editing, its draft, and its caret continue unchanged.

Bubble Tea's standard mouse mode does not expose a wheel-only reporting option. Coragent therefore keeps standard mouse tracking active throughout the supported interactive screen, including composer focus, and consumes only wheel reports for background transcript scrolling; other background click, press, release, and motion reports are ignored. Clicking the scrollbar track, dragging its thumb, and application-managed text selection remain out of scope. Because reporting stays active, ordinary unmodified drag is not promised to select or copy. At startup Coragent emits `XTSHIFTESCAPE` with `n=0`, declaring that it does not need Shift-modified mouse reports so supporting terminals can reserve Shift for native selection. The UI advertises `Shift/Option+drag copy` as the terminal-native mouse-reporting bypass and documents that the effective modifier is owned by the user's terminal and may vary by terminal configuration.

History browsing stores a semantic anchor consisting of a stable transcript block identity plus an intra-block visual offset, not only a distance from the current bottom. New deltas, in-place block height changes, folding, and resize preserve that anchor as closely as reflow permits. Every recalculation clamps offsets to the current scrollable range so short or shrinking history never produces an empty page. Wheeling to the live bottom repins auto-follow and clears unread state; new content while browsing preserves the anchor and increments the non-color unread indicator.

Alternative considered: gate mouse reporting behind a `PageUp`-entered transcript focus and provide a parallel keyboard navigation mode. Rejected because it couples browsing to editor focus, hides the caret, adds a mode transition around the user's draft, and cannot provide ordinary drag copy while wheel reporting is active anyway. The selected contract keeps one composer focus and one wheel-driven scroll state.

Alternative considered: rerender the whole Markdown transcript on every token. Rejected because incomplete fences flicker and long history makes input latency depend on conversation length. Coragent instead re-renders only the bounded active Markdown tail per visible batch and promotes stable prefix blocks into the width-and-visual-mode cache.

### Decision: Make keyboard precedence and shutdown behavior explicit

Focus precedence is permission modal, argument editor, inspector/help, composer, then global actions. Transcript scroll state is orthogonal and never owns keyboard focus.

Core bindings:

| Context | Binding | Action |
|---|---|---|
| Composer | `Enter` | Submit non-empty request |
| Composer | `Ctrl+J` | Insert newline |
| Enhanced terminal | `Shift+Enter`, `Alt+Enter` | Newline aliases when distinguishable |
| Global idle | `Shift+Tab` | Cycle default, auto-accept-edits, plan |
| Global | `Ctrl+B` | Enter guarded bypass confirmation; `Shift+Tab` exits bypass to default |
| Running | `Esc` or first `Ctrl+C` | Cancel the current run |
| Idle | two `Ctrl+C` within 1.5 seconds | Quit; first press shows the armed hint |
| Global | `Ctrl+Q` | Immediate clean shutdown sequence |
| Permission | `a`, `d`, `A`, `D`, `e`, `s` | Allow, deny, remember variants, edit args, edit sandbox grants |
| Argument editor | `Ctrl+S` | Submit argument revision; on successful hooks/preparation, return to a new permission request |
| Argument editor | `Esc` | Return to permission modal without answering |
| Normal screen, no blocking overlay | mouse wheel | Browse transcript without changing composer focus; wheel down to the bottom repins live output |
| Terminal-native selection | `Shift/Option+drag` | Bypass application mouse reporting and let the terminal select/copy; the effective modifier is terminal-owned |
| Selected disclosure | `Enter` | Expand or collapse |
| Selected resumable cutoff | `Enter` | Prefill an editable continuation draft when composer is empty; never auto-send |
| Global | `Ctrl+I` | Session/capability inspector |
| Global | `Ctrl+/` | Context-sensitive shortcut help |

Modified Enter aliases are advertised only after Bubble Tea reports enhanced keyboard support. Multiline bracketed paste is always preserved through Bubble Tea's sanitized `PasteMsg`; the textarea's native `Ctrl+V` clipboard helper is disabled because its transitive backend resolves executable names through `PATH`. No keyboard binding changes transcript scroll position; keys such as arrows and `End` retain their composer or overlay meanings instead of leaking into background history. Shortcut help describes history as wheel-only and shows direct drag auto-copy plus the terminal-owned `Shift/Option+drag` fallback. Mode changes during a run are rejected with a short notice. Bypass is intentionally excluded from the casual mode cycle and requires a focused confirmation.

`Esc` on the permission modal submits deny and lets the run continue after acceptance. `Ctrl+C` on the permission modal or either child editor submits deny once for the parent request, then cancels the run. `Ctrl+Q` from any focus starts a four-second bounded shutdown: submit deny once when needed, cancel, drain for at most two seconds, then close the session with a separate two-second context. A terminal event received during drain remains authoritative. If a contract-violating provider or handler never settles, the frontend stops waiting at the deadline, restores the terminal, reports forced shutdown to stderr, and exits nonzero rather than hanging. Provider-channel receives inside the loop select on cancellation and synthesize the one cancelled terminal instead of ranging forever on a non-closing channel. A local submission guard blocks repeated keys while a reply is in flight; it becomes permanently resolved only on `accepted`, clears on `validation_rejected`, and reconciles from observed state on `already_resolved`.

Alternative considered: one global Esc/Ctrl-C handler. Rejected because it can both dismiss a permission and cancel or quit in the same keypress, leaving ambiguous safety behavior.

### Decision: Use a terminal-native visual system called Terminal Narrative

The visual thesis is a calm, low-chroma terminal conversation with warm terracotta signal color, dense technical typography, and one animated action marker. The interface orients, shows status, and enables action; it has no decorative hero, gradients, glass, shadows, generic rounded cards, or telemetry-table status columns.

The runtime uses the user's terminal font. Figma uses Recursive Mono only as a cell-grid reference, never as a shipped dependency. One reference cell is 8x18 px; frames are specified in terminal cells first and pixels second.

Semantic dark-theme tokens:

| Token | Hex | Role |
|---|---|---|
| canvas | `#0C0D0C` | terminal background |
| surface | `#131512` | user prompt and selected rows |
| elevated | `#1A1D19` | modal and inspector |
| border | `#30352E` | one-cell dividers |
| text | `#E7E9E3` | primary copy |
| muted | `#90988B` | metadata |
| accent | `#D97757` | focus, current activity, mode |
| success | `#7EBC77` | completed and added content |
| warning | `#D6A95F` | context and degraded posture |
| danger | `#E17A72` | denied, failed, removed content |
| info | `#8FA9A1` | neutral notices |

Color is never the sole signal. Glyph plus label distinguishes every state. The no-color mode preserves weight, prefixes, and borders. ASCII fallback replaces `● ◐ ○ ✓ × ▍` with `* > o + x |`.

Motion is state-driven and bounded: one 120 ms activity glyph for thinking/executing and a 600 ms caret cadence when a focused editor cursor is present. Subagent indentation is static. Idle stops all ticks except the focused editor caret. Reduced-motion and `TERM=dumb` use static `...` labels and a steady real caret. No animation changes layout width.

Responsive rules:

| Size | Behavior |
|---|---|
| 160x48 | Full metadata, two-column permission or inspector detail, one transcript column |
| 120x36 | Reference layout and all primary information |
| 80x24 | Compact tool summaries, stacked permission, shortened path segments |
| 60x20 | Essential mode/status, minimal borders, one composer content row between hairlines, one-column details |
| below 60x20 | Too-small surface with size requirement and deny/cancel/quit actions; blind approval and editing disabled |

Paths compact by whole segments with the filename retained; metric labels never end in an ellipsis. The visual contract and frame inventory live in `ui-design.md` and `figma-handoff.md`.

### Decision: Pin the current stable Charm v2 stack

Implementation starts with `charm.land/bubbletea/v2` v2.0.6, `charm.land/bubbles/v2` v2.1.0, `charm.land/lipgloss/v2` v2.0.3, and `charm.land/glamour/v2` v2.0.0. Bubble Tea v2 supplies declarative alt-screen and keyboard enhancements, Bubbles supplies textarea/viewport/spinner primitives, Lip Gloss handles terminal-aware styling and width, and Glamour handles completed Markdown blocks. Versions are rechecked only for security or compatibility fixes before the implementation commit, not redesigned mid-phase.

Alternative considered: custom terminal input/rendering or Charm v1. Rejected because v2 has stable synchronized output, progressive keyboard reporting, a real cursor, and matching v2 components, while custom rendering would duplicate difficult terminal behavior.

## Risks / Trade-offs

- [Risk] The change spans more than eight files and multiple SDK packages. -> Mitigation: merge the three slices independently, require compatibility tests before TUI work, and review public types before implementation.
- [Risk] Rich events and legacy projection could diverge. -> Mitigation: generate both from one internal stream and use table-driven projection snapshots for every event kind and terminal path.
- [Risk] Preparing a diff can race path replacement, symlink/hard-link aliasing, or file mutation outside Coragent. -> Mitigation: bind preparation to parent and target identity, require a single hard link, reacquire with no-follow semantics, validate through the same handle used for commit, use exclusive directory-relative creation, and fail closed when the platform cannot provide the required guarantee.
- [Risk] Provider-specific reasoning fields vary or contain unsafe content. -> Mitigation: optional adapters, explicit feature flags, no raw chain-of-thought request, sanitization, bounded summaries, and graceful unsupported behavior.
- [Risk] Exact provider usage arrives after an estimate and makes the meter move backward. -> Mitigation: label source, replace values per round instead of treating estimates as cumulative facts, and animate no geometry.
- [Risk] Markdown and ANSI processing can cause flicker, injection, or quadratic rendering. -> Mitigation: sanitize before measurement, cache completed blocks, render only the active tail, cap visible content, and benchmark burst and long-history fixtures.
- [Risk] Terminal key capabilities and native mouse-reporting bypass modifiers differ across Ghostty, iTerm, Terminal.app, tmux, VS Code, JetBrains, Linux consoles, and Windows terminals. -> Mitigation: Ctrl+J is the baseline newline, enhanced aliases are capability-gated, help labels `Shift/Option+drag` as terminal-native rather than application-owned, support notes state that the effective modifier is terminal-specific, and PTY/manual matrices cover fallbacks.
- [Risk] A modal permission reply can be sent twice during quit/cancel races. -> Mitigation: one reducer owner, explicit focus precedence, an answered guard, and shutdown tests that observe the exact reply count.
- [Risk] Revised permission arguments can bypass the hard pre-tool gate if they resume after the original hook pass. -> Mitigation: route every revision back through matching before-tool hooks inside the same executor path before re-preparation and any new approval.
- [Risk] The dark palette can be unreadable in low-color or user-themed terminals. -> Mitigation: automatic downsampling, no-color and ASCII modes, labels plus glyphs, and golden renders at truecolor, 256-color, and no-color.
- [Risk] Capability inventory can accidentally disclose sensitive configuration. -> Mitigation: use a positive allowlist of descriptor fields and adversarial tests for API keys, environment values, commands, prompts, and remembered rules.
- [Risk] The reference Figma font cannot represent every terminal's metrics. -> Mitigation: specify every layout in cells, test runtime widths with Lip Gloss, and treat pixel frames as review aids only.

## Migration Plan

1. Add observed-event, descriptor, typed mode, optional provider, preparation, preview, usage, omission, and provenance types behind additive public APIs.
2. Refactor the internal runtime to emit the rich stream once and project the unchanged legacy stream; land compatibility, race, and protocol tests.
3. Add public settings/bootstrap and migrate the demo plus future binary construction without changing existing settings merge behavior.
4. Implement prepared actions for built-in write/edit and a safe legacy fallback; land stale-preview, permission-edit, hook-edit, and truncation tests.
5. Build the core Bubble Tea app against a fake `SessionPort`, then connect the public SDK adapter and binary.
6. Add visual tokens, responsive renderers, streaming-safe Markdown, inspector, reasoning/context/omission states, motion fallbacks, and terminal safety sanitization.
7. Run golden renders, PTY and manual terminal checks, full offline Go tests, race tests, build/lint, import-boundary checks, and strict OpenSpec validation.

Rollback is slice-based. The observed APIs can remain unused while `Run` continues unchanged. Prepared built-ins can fall back to legacy execution by removing their optional interface registration. The TUI binary can revert to its prior placeholder without changing the SDK or persisted data. No data migration is required.

Compatibility is source-first and preserves all documented valid use. Two intentional fail-safe corrections are not rolled back independently because doing so would restore a safety bypass: mid-run use of the already between-turn-only string mode setter returns an error, and a legacy edited approval receives another prompt when rerun hard hooks replace its arguments. Both keep existing method and reply shapes; the latter preserves exactly one reply per request.

## Open Questions

There are no implementation-blocking unknowns in this design. Two scope choices are intentionally placed in review:

- This design recommends keeping skills and MCP runtime out of Phase 7 while making their future truthful status renderable. If product scope overrides that decision, create and approve a separate architecture change before applying this change.
- This design recommends guarded bypass entry instead of including bypass in the casual `Shift+Tab` cycle. If review restores one-key bypass cycling, the safety acceptance criteria and visual warning state must be amended together.
