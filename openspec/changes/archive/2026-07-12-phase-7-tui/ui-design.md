# Coragent TUI product and visual design

This document is the review source for Phase 7 interaction and visual behavior. Technical event and execution decisions live in `design.md`; normative acceptance lives in the delta specs.

## Direction lock

The five visual-direction questions are answered as follows for review:

1. **User and context**: a developer working keyboard-first in one terminal for long coding sessions, commonly at 80 to 160 columns, sometimes inside tmux or an IDE terminal.
2. **Aesthetic direction**: Claude Code-inspired terminal narrative, dark low-chroma surfaces, warm terracotta signal color, dense but breathable transcript, no decorative dashboard.
3. **Design signature**: user, assistant, and tool work share one left execution marker; tool results continue beneath the action as a small tree instead of using a fixed telemetry column.
4. **Hard constraints**: Bubble Tea v2, host terminal font, cell-based geometry, 60x20 minimum working layout, keyboard-only completion of every flow, 256-color/no-color/ASCII fallbacks, no raw chain-of-thought, no untrusted control sequences.
5. **Signature micro-interaction**: the one-cell action marker advances from `○` to `◐` to `●` without moving the tool name; reduced-motion replaces the animation with a static `...` label.

### Alternatives considered

| Direction | Density | Material and color | Layout | Motion | Verdict |
|---|---|---|---|---|---|
| Terminal Narrative | medium-dense | charcoal neutrals, terracotta signal | one top-aligned transcript column with a bottom composer, inspector on demand | one active marker | **Selected**: closest to Claude Code's readable command flow while preserving Coragent safety facts |
| Paper Terminal | medium | warm light canvas, ink and red pencil | editorial blocks with larger gaps | almost static | Rejected for v1: glare and low contrast during long dark-terminal sessions |
| Ops Console | very dense | near-black with high telemetry color | persistent split inspector | multiple live indicators | Rejected for v1: capability data competes with the conversation and degrades at 80 columns |

## Visual thesis

**Visual theme and atmosphere:** A native terminal conversation for agent work. Dark surfaces recede; assistant prose and the current decision stay dominant; metadata is present but never louder than the task.

**Content plan:** orient with project/mode/safety, show the ordered conversation and work, expose the current decision, keep status and context visible, let the user inspect detail on demand.

**Interaction thesis:** work advances through one-cell markers and indented result branches, disclosures expand inline, and permission takes full focus in a bottom-aligned decision panel with no background input leakage.

## 1. Color palette and roles

The runtime maps these semantic roles through Lip Gloss and lets Bubble Tea downsample for terminal capability. Figma records both OKLCH intent and sRGB reference.

| Token | OKLCH intent | Hex | Use |
|---|---|---|---|
| `color/canvas` | `oklch(0.16 0.006 145)` | `#0C0D0C` | terminal canvas |
| `color/surface` | `oklch(0.20 0.008 145)` | `#131512` | composer, selected row |
| `color/elevated` | `oklch(0.24 0.010 145)` | `#1A1D19` | modal, inspector |
| `color/border` | `oklch(0.32 0.018 145)` | `#30352E` | hairline separation |
| `color/text-primary` | `oklch(0.93 0.008 105)` | `#E7E9E3` | assistant and primary labels |
| `color/text-secondary` | `oklch(0.67 0.020 140)` | `#90988B` | timestamps, summaries, hints |
| `color/accent` | `oklch(0.67 0.14 42)` | `#D97757` | focus, activity, mode |
| `color/success` | `oklch(0.72 0.12 140)` | `#7EBC77` | completed, diff additions |
| `color/warning` | `oklch(0.76 0.11 75)` | `#D6A95F` | context pressure, fallback |
| `color/danger` | `oklch(0.70 0.13 25)` | `#E17A72` | failure, denial, diff removals |
| `color/info` | `oklch(0.70 0.035 175)` | `#8FA9A1` | neutral system notices |
| `color/diff-add-bg` | `oklch(0.26 0.055 145)` | `#16301F` | added-line background |
| `color/diff-remove-bg` | `oklch(0.25 0.050 25)` | `#351C1C` | removed-line background |

Rules:

- Never use a purple-blue gradient or cyan-on-black default.
- Never rely on color alone: every semantic state has a glyph and word label.
- Primary text is soft white, not full `#FFFFFF`.
- One active signal color appears at a time; success, warning, and danger are reserved for meaning.
- No-color mode removes fills first, not labels or layout.

## 2. Typography rules

Coragent cannot and should not replace the user's terminal font. Figma uses Recursive Mono as a reference because its cell rhythm remains legible at dense sizes; it is not a runtime dependency.

Brand words: precise, calm, accountable. Reflex choices rejected for the design file: JetBrains Mono, SF Mono, IBM Plex Mono.

| Role | Reference size | Weight | Line height | Tracking | Runtime rule |
|---|---:|---:|---:|---:|---|
| Product mark | 14 px | 700 | 18 px | 0 | one line only |
| Body | 14 px | 400 | 18 px | 0 | terminal default |
| Strong body | 14 px | 650 | 18 px | 0 | names and decisions |
| Metadata | 12 px | 450 | 18 px | 0 | never smaller than one readable cell row |
| Code and diff | 13 px | 400 | 18 px | 0 | tabular numbers where available |

One reference cell is 8 by 18 px. Pixel values exist only for Figma. Runtime widths, wrapping, hit regions, and compacting are computed in terminal cells with ANSI-aware measurement.

## 3. Layout principles

### Shell anatomy

```text
header: product | project
transcript: one ordered narrative of user, assistant, tools, notices

──────────────── hairline composer ────────────────────────────────
› editable request
───────────────────────────────────────────────────────────────────
activity                       mode | sandbox | model | context
dynamic shortcut hints
```

- Default layout is one transcript column. A permanent sidebar is forbidden.
- A short transcript starts at the top while the composer, status, and hints remain anchored at the bottom. Unused viewport rows sit after the transcript, never before it.
- Once content reaches the available height, the transcript follows the live tail. Header, status, composer, and hints keep stable geometry so normal, streaming, and permission states do not jump vertically.
- Horizontal padding is two cells at 80 columns and above, one cell below.
- Vertical block gaps are one row; connected tool/subagent rows may use zero extra rows.
- Assistant prose remains cardless. Containers exist only for user input, selected tool detail, decisions, and safety notices.
- Long readable prose caps at 92 cells inside wide terminals; metadata can use the remaining width.
- Composer content grows from one to six visible rows between two hairlines. Spinner frames never alter width.

### Depth and elevation

Terminal depth uses background steps, not shadows:

- Level 0: canvas.
- Level 1: surface for composer, user prompt, selection.
- Level 2: elevated for permission, inspector, help.
- One-cell border separates modal or safety-critical regions; ordinary transcript blocks do not get boxes.

## 4. Responsive behavior

| Frame | Pixel reference | Required behavior |
|---|---:|---|
| 160x48 | 1280x864 | full project path, model and context, two-column modal body, inspector beside transcript when open |
| 120x36 | 960x648 | canonical design frame, full primary labels, stacked inspector overlay |
| 80x24 | 640x432 | compact header/footer, whole-segment path compression, one-column modal, successful tools as one row |
| 60x20 | 480x360 | one-cell padding, essential mode/status only, one content row between composer hairlines |
| below 60x20 | variable | too-small safety surface; retain deny, cancel, quit, and resize guidance, but disable blind approval |

Responsive rules:

- Path compression retains the filename and nearest parent, for example `…/executor/chain.go`; no metric footer ends in an ellipsis.
- At 80 columns, header priority is mode, sandbox degradation, project name, then path.
- At 60 columns, context percent moves to the footer and model name moves into the inspector.
- Permission content never scrolls behind its actions. If needed, the preview gets its own bounded viewport.
- A tiny pending permission view is more important than the normal conversation; it replaces the shell body, retains deny/cancel/quit, and requires resize to at least 60x20 before allow, remember, argument edit, or grant edit becomes available.
- CJK and wide glyph tests use cell widths, not byte/rune counts.

## 5. Component styling and state matrix

### Identity and status

```text
coragent  /workspace/coragent
                                  [PLAN] · sandbox os · gpt-5.4 · ctx 22% est
```

- Product mark and project use primary/muted text, no logo illustration.
- Mode, sandbox, model, and known context facts live in the status row below the composer instead of competing with the project identity.
- Mode is a typed label: `DEFAULT`, `AUTO EDIT`, `PLAN`, `BYPASS`, `EXTERNAL`, or `UNSUPPORTED`. The last two are ownership display states, not selectable permission modes, and disable mode shortcuts.
- Bypass uses danger glyph plus word, persists until exited, and never relies on a red fill alone.
- Sandbox shows `os`, `fallback`, `unknown`, or `externally owned`; caller-owned dispatch is never hidden or mislabeled as OS enforcement.

### User message

```text
› Refactor the parser and verify the generated table.
```

Quiet surface fill, no rounded bubble, avatar, role label, or timestamp. The first line begins with `›`; continuation lines align with the prompt text.

### Assistant message

```text
● I will inspect the parser and its tests first.
  Then I will run the focused tests.
```

Cardless. The one-cell marker carries streaming activity, so prose does not gain a trailing block cursor. Completed Markdown headings use weight, not oversized type.

### Reasoning disclosure

```text
◇ Reasoning summary  3 steps · 8.4s                         [Enter]
```

States: unsupported is absent; streaming is expanded only while it contains provider text; completed defaults collapsed; selected/expanded shows bounded summary text. Copy says `reasoning summary`, never `thoughts` or `chain-of-thought`.

### Execution rail and tool row

```text
● read_file(internal/core/types.go) · succeeded
  └ Read 214 lines
◐ search_content("PermissionRequest") · running
× run_command(go test ./...) · failed
  └ exit 1 · 1.8s
```

States and default disclosure:

| State | Glyph | Default detail |
|---|---|---|
| proposed/preparing | `○` | arguments summary visible |
| awaiting permission | `◆` | preview expanded and modal open |
| executing | `◐` | live summary expanded |
| success | `●` | collapsed except mutation diff |
| error | `×` | expanded with safe result |
| cancelled | `∅` | collapsed with cancelled label |
| blocked by hook | `■` | expanded safety notice |

Only the glyph and terminal state use semantic color. Tool name stays strong, arguments stay muted, and no row reserves a fixed right-hand status column. Shell shows command and working directory, file tools show compact path, search shows pattern plus scope, task shows label.

### Result folding and omission

```text
│ first retained line
│ ... 41 lines folded locally · Enter to expand
│ last retained line
```

Local fold uses first 8 and last 3 lines when result exceeds 12 lines. It is reversible.

```text
! 30.0 KiB retained · 14.2 KiB omitted by harness · cannot expand
```

Source omission uses warning/danger semantics based on reason and never offers an expand action.

### Diff hunk

```diff
@@ parser.go:84-88 @@
- table := buildTable(tokens)
+ table := buildLookaheadTable(tokens)
```

- Path and operation are always visible.
- Added/removed lines use color plus `+`/`-`.
- Context lines remain muted.
- Preview omission and stale-preview errors are explicit.
- Binary change preview shows operation, byte counts, and fingerprint status, not fake text.

### Permission sheet

```text
────────────────────────────────────────────────────────────────────
◆ Permission required · shell · root agent
  Run: rm -rf ./build
  Why: command is not covered by a permission rule
  Preview: unavailable

Do you want to proceed?
› [a] Allow once
  [d] Deny
Esc deny · ↑/↓ review · Ctrl+C deny + cancel
```

- The panel occupies transcript width, aligns near the composer, and replaces composer input while active. It does not float in a centered dashboard card.
- Background input and navigation remain inert.
- After any decision key, the sheet remains visible in `submitting` with decision keys disabled until the typed reply outcome arrives; rejection restores the same draft and accepted closes it.
- Child request names the delegated task and depth.
- Remember actions disappear when no safe remembered rule exists.
- Sandbox-grant editor lists exact paths/network and explains that grants are additive for this call.
- On compact frames actions wrap by semantic group, not arbitrary character width.

### Argument editor

Pretty-printed JSON uses a bounded textarea. `Ctrl+S` submits a revision only. The sheet enters `re-preparing`; only successful validation, hard hooks, and preparation produce a new permission request with a new preview revision. Invalid JSON or a typed schema/applicability rejection appears adjacent to the editor and retains the draft because the current request remains open. A hard hook or preparation failure after an accepted revision closes the editor into the tool's blocked or error state and does not fabricate a replacement prompt. `Esc` returns to the current permission without answering it.

### Subagent row

```text
  ├─ ◐ inspect provider contract                      task 2 · depth 1
  └─ ● verify cancellation                            6.2s
```

Only lifecycle and final task result appear. Duplicate labels remain distinguishable internally by IDs. No child raw transcript is exposed.

### Context meter

Wide:

```text
ctx 38.4k / 128k  30% est
```

Unknown window:

```text
ctx ~38.4k est
```

The meter does not draw a decorative progress bar. At 80 percent text becomes warning; at 95 percent it becomes critical. Provider usage replaces the estimate for that round and removes `est`.

### Capability inspector

```text
SESSION
model     gpt-5.4                         reasoning summary: supported
sandbox   os-enforced                     network: denied
mode      plan

CAPABILITIES
tools     7 ready                         builtin 6 · subagent 1
hooks     2 ready                         prompt-submit · before-tool
skills    not reported                    hidden in normal chrome
mcp       not reported                    hidden in normal chrome
```

Only reported categories appear in the compact summary. `not reported` is used inside the inspector when the user explicitly asks, never converted to `loaded: 0`.

### Notices

Separate forms exist for provider failure, step limit, reply length cutoff, content filter, context warning, sandbox fallback, hook block, cancellation, startup/config error, and session-close error. Each notice has one job and one next action. No copy begins with `Oops` and success labels have no exclamation mark.

### Composer

- One editable row between two hairlines by default, grows to six visible content rows, then scrolls internally.
- Placeholder: `Describe what you want changed...` only in an empty idle composer.
- While running, input remains visible but disabled with `Esc interrupt` unless queueing is later added.
- Multiline paste stays multiline and is not submitted automatically.
- IME composition is never interpreted as a submit until committed.
- Transcript wheel scrolling never takes composer focus or changes its draft, logical insertion point, or idle caret.

### Shortcut help

Help is context-sensitive and reflects detected enhanced keyboard support. It separates composer, wheel-only transcript browsing, terminal-native `Shift/Option+drag copy`, permission, and global groups. It never advertises `PageUp` or another keyboard history binding, and it does not claim that ordinary drag selects while standard mouse tracking is active. It is a full-focus overlay at 80 columns or less and a right inspector at 120 columns or more.

## 6. Key screen wireframes

### Empty and ready, 120x36

```text
coragent  /workspace/coragent
⋮ unused transcript rows remain below this top-aligned content
────────────────────────────────────────────────────────────────────────────
› Describe what you want changed...
────────────────────────────────────────────────────────────────────────────
                                     [DEFAULT] · sandbox os · gpt-5.4
Enter send · Ctrl+J newline · Wheel history · Shift+Tab mode
```

### Streaming and tool use

```text
coragent  /workspace/coragent
› Refactor the parser and verify the generated table.

● I will inspect the parser and its tests first.
  Then I will run the focused tests.

● read_file(internal/parser/parser_test.go) · succeeded
  └ Read 214 lines
◐ search_content("buildTable") · running

⋮ unused transcript rows, if any
────────────────────────────────────────────────────────────────────────────
› Esc interrupts the current run
────────────────────────────────────────────────────────────────────────────
calling tool                         [PLAN] · sandbox os · gpt-5.4 · ctx 22% est
Esc interrupt · Ctrl+C cancel · Ctrl+Q quit
```

### Permission with diff

```text
coragent  /workspace/coragent
● I found one stale constructor and prepared a focused replacement.
◆ edit_file(internal/parser/parser.go) · approval needed

────────────────────────────────────────────────────────────────────────────
◆ Permission required · edit_file · preview r1
  Action: Modify internal/parser/parser.go
  Preview: @@ parser.go:84-88 @@  - buildTable  + buildLookaheadTable
Do you want to proceed?
› [a] Allow once
  [d] Deny
Esc deny · ↑/↓ review · Ctrl+C deny + cancel
                         [AUTO EDIT] · sandbox os · gpt-5.4 · ctx 31% provider
```

### Scrollback with unseen output

```text
│ ● shell  go test ./...                                   passed · 8.4s    │
│ ... earlier transcript ...                                                │
│                                                                            │
├ browsing history · 37 new lines                  wheel down return live ──┤
│ › draft and caret remain focused                                            │
├ Wheel history · Shift/Option+drag copy · Esc cancel ──────────────────────┤
```

### Provider cutoff

```text
! Response stopped at the provider length limit.
  The visible text is incomplete. If continuation is marked possible and the
  composer is empty, select this notice and press Enter to prepare an editable
  continuation draft. Review it and press Enter in the composer to send.
```

No continuation action is advertised when the provider marks the omission non-resumable. An existing composer draft is never overwritten.

## 7. Interaction flows

### Primary flow

```text
idle
  -> submit
  -> thinking
  -> assistant streaming
  -> tool proposed/prepared
  -> permission when required
  -> tool executing/result
  -> next model round
  -> assistant final
  -> completed/idle
```

### Permission revision flow

```text
permission r1
  -> edit arguments
  -> validate locally enough to submit
  -> invalid reply feedback keeps r1 open and retains draft
  -> revise_arguments reply closes r1
  -> harness revalidates
  -> rerun hard before-tool hooks
  -> hook/preparation failure terminates without another prompt
  -> re-prepare successful candidate
  -> permission r2 with updated diff
  -> allow/deny r2
```

### Cancel and quit flow

```text
running + Esc       -> cancel context -> RunFinished(cancelled) -> idle
permission + Esc    -> deny once -> run continues
permission + Ctrl+C -> deny once -> cancel -> terminal event -> idle
editor + Ctrl+C     -> deny parent once -> cancel -> terminal event -> idle
any + Ctrl+Q        -> deny once if needed -> cancel -> close(2s) -> restore terminal
idle + Ctrl+C       -> arm 1.5s -> second Ctrl+C quits
```

### Mode flow

```text
DEFAULT -> AUTO EDIT -> PLAN -> DEFAULT        via Shift+Tab while idle
any safe mode -> guarded BYPASS                via Ctrl+B + confirmation
BYPASS -> DEFAULT                              via Shift+Tab
running -> mode change rejected with notice
```

## 8. Do and do not

Do:

- Keep assistant prose visually stronger than telemetry.
- Use one execution rail and update tool rows in place.
- State whether context is estimated or provider-reported.
- Distinguish local folding from irreversible omission.
- Show exact remembered rules and additive sandbox grants before approval.
- Preserve scroll position when new output arrives and show an unread count.
- Make every safety state understandable in no-color mode.
- Restore the terminal before reporting shutdown errors.

Do not:

- Render raw chain-of-thought or infer reasoning from assistant prose.
- Put every transcript block in a rounded card.
- Keep multiple independent spinners running.
- Claim skills or MCP are loaded when no descriptor reports them.
- Let tool output emit raw ANSI/OSC/clipboard/title control sequences.
- Enter bypass by accidentally cycling through ordinary modes.
- Offer expand for content already discarded by the harness.
- Tail-truncate context, mode, status, or safety labels into ambiguous fragments.

## 9. Accessibility and verification

- Every state uses glyph, text, and color.
- Focus is visible with accent glyph/border; the background becomes inert during modal focus.
- Color references meet a minimum 4.5:1 target for normal text in the Figma reference; runtime no-color remains fully usable.
- Reduced motion stops ticks and uses static labels.
- `TERM=dumb`, `NO_COLOR`, 16-color, 256-color, and truecolor outputs have golden fixtures.
- Unicode width fixtures include CJK, emoji, combining marks, long paths, and malformed UTF-8 replacement.
- Security fixtures include CSI cursor moves, OSC title changes, OSC 52 clipboard writes, terminal hyperlinks, bells, backspaces, and embedded NUL.
- Visual fixtures cover 60x20, 80x24, 120x36, 160x48, and below-minimum permission states.
- Real-terminal manual matrix: Terminal.app, iTerm2, Ghostty, tmux, VS Code terminal, one JetBrains terminal, one Linux terminal, and Windows Terminal when available.

## Agent prompt guide for implementation

Use these prompts verbatim when implementing individual visual pieces:

1. `Implement the Coragent identity row in Bubble Tea/Lip Gloss using canvas #0C0D0C, text #E7E9E3, muted #90988B, accent #D97757, one cell vertical height, two-cell horizontal padding at 80+ columns and one cell below. Show product and compact project path only; render mode and sandbox in the status row below the composer. Preserve labels in NO_COLOR and compact paths by whole segments.`
2. `Implement the tool narrative with a one-cell glyph and states proposed ○, awaiting permission ◆, executing ◐, success ●, error ×, cancelled ∅, hook-blocked ■. Use 14px-equivalent terminal body weight, no box or fixed status column, render name strong and arguments muted, and place results beneath the action with a └ branch. Update the same row by call ID.`
3. `Implement the permission panel at full transcript width using divider #30352E, primary #E7E9E3, muted #90988B, accent #D97757, danger #E17A72. Align it near the composer and replace composer input while active; do not center it as a floating card. Render action, reason, preview revision, remembered rule, sandbox grants, vertical decisions, and context-specific keys. Background input and scroll must be inert.`
4. `Implement the context footer as text, not a decorative bar. Use ctx used/window percent source, tabular numbers, warning #D6A95F at 80 percent, danger #E17A72 at 95 percent, and append est only for estimated values. Hide percent when window is unknown.`
5. `Implement streaming-safe assistant Markdown by caching a stable completed prefix through Glamour and progressively re-rendering only the bounded sanitized active tail from the first visible delta batch. Style each recognizable construct immediately, keep only the shortest ambiguous suffix literal, render open fences as bounded safe code, and converge to a deterministic full-source render on completion. Strip HTML, CSI, OSC, OSC 52, unsafe links, and C0 controls except newline and tab before width measurement.`

## Figma page and frame inventory

The editable Figma deliverable is specified as:

1. `00 Cover and decision log`
2. `01 Foundations` with cell grid, colors, typography, glyphs, spacing, borders, and motion frames
3. `02 Components` with header, mode, context, capability, message, reasoning, tool, diff, permission, composer, notices, inspector, and help
4. `03 Primary flow` at 120x36
5. `04 Responsive` at 60x20, 80x24, 120x36, 160x48, and too-small
6. `05 Safety and edge states`
7. `06 Prototype map`
8. `07 Developer handoff` with cell measurements and event fixtures

Required key frames: empty, streaming, reasoning expanded, tool preparing, tool executing, result folded, source omission, permission with diff, argument revision, sandbox grants, subagent permission, browsing history, cancellation, step limit, provider cutoff, content filter, context warning, sandbox fallback, hook block, startup error, no-color, ASCII, and too-small permission.

See `figma-handoff.md` for the Phase 0 gap analysis and `assets/phase-7-reference.svg` for the importable reference board.
