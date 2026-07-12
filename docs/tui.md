# Terminal UI guide

Coragent's TUI is a public-SDK client. It consumes `Session.RunObserved`, replies
through the permission operation carried by each request, and never imports
harness internals or reads files to reconstruct a proposed diff.

## Startup and settings

The binary calls `agent.LoadSettings`, merging
`~/.coragent/settings.json` first and `.coragent/settings.json` second. Project
fields override home fields. It then calls `agent.Bootstrap` with the current
working directory. Malformed settings fail before the app accepts input.

The minimum model configuration is:

```json
{
  "model": {
    "name": "gpt-4.1",
    "base_url": "https://api.openai.com/v1",
    "api_key": "${OPENAI_API_KEY}"
  }
}
```

`Settings` is secret-bearing and opaque. Its public formatting, JSON, and
structured logging expose only model/provider labels, hook count, permission
mode, and a coarse sandbox summary.

## Keyboard map

Keys are routed to the topmost active scope: permission editor or decision,
inspector/help, composer, then global actions.

| Context | Key | Action |
|---|---|---|
| Composer | `Enter` | Send a non-blank draft |
| Composer | `Ctrl+J` | Insert the baseline newline |
| Composer | `Shift+Enter`, `Alt+Enter` | Newline aliases only when enhanced keyboard reporting distinguishes them |
| Any non-permission screen | `Ctrl+/` | Context-aware help (`Ctrl+_` is accepted for legacy terminals that encode the same control byte) |
| Any non-permission screen | `Ctrl+I` | Session and transcript inspector |
| Inspector | `Up/Down`, `PageUp/PageDown` | Navigate inspector rows |
| Inspector | `Enter` | Expand retained reasoning, preview, arguments, or result content |
| Overlay/editor | `Esc` | Return to its parent screen |
| Idle | `Shift+Tab` | Cycle `DEFAULT → AUTO EDIT → PLAN → DEFAULT` |
| Idle | `Ctrl+B` | Open the explicit BYPASS confirmation |
| Active run | `Esc`, `Ctrl+C` | Request cancellation once |
| Idle | `Ctrl+C` twice within 1.5 s | Clean shutdown |
| Any state | `Ctrl+Q` | Bounded clean shutdown |

`PageUp`, `PageDown`, arrows, `j/k`, `Ctrl+U/D`, `End`, and `G` never move the
background transcript. They retain their ordinary composer meaning or act only
inside the active modal/inspector.

## Permission review

The permission sheet captures keyboard and wheel input and keeps the correlated
tool row in `approval needed` state. Depending on public request capabilities it
offers:

| Key | Decision |
|---|---|
| `a` | Allow once |
| `d` or decision-sheet `Esc` | Deny once |
| `A` | Allow and remember the displayed action scope |
| `D` | Deny and remember the displayed action scope |
| `e` | Edit JSON arguments; `Ctrl+S` submits a revision, not an approval |
| `s` | Edit additive read/write/network grants for this call only |

Invalid JSON or public schema feedback leaves the editor open and retains the
draft. A valid revision resolves the old request, re-enters hard hooks and
preparation, and displays a new request ID and preview revision before any final
allow decision. Remembering an action never persists its one-call sandbox
grants.

Below `60x20`, permission review remains deny/cancel/quit capable but disables
allow, remember, argument editing, and grant editing until the terminal is
resized.

## Permission modes

- `DEFAULT` consults remembered rules and asks when needed.
- `AUTO EDIT` automatically accepts eligible file edits.
- `PLAN` permits reads and rejects mutations.
- `BYPASS` skips soft permission prompts, but never bypasses hard hooks or
  sandbox confinement.
- `EXTERNAL` and `UNSUPPORTED` are non-selectable ownership states.

`Shift+Tab` never enters bypass. `Ctrl+B` opens a blocking explanation and only
an explicit `y`/`Enter` confirmation requests the public typed bypass mode.
Mode changes are accepted only between runs and chrome updates only after the
SDK setter succeeds.

## Transcript, history, and copy

Assistant Markdown streams progressively. Provider-supplied reasoning summaries
are temporarily expanded while streaming and collapse when the assistant item
finishes; Coragent never infers or persists hidden reasoning. Tool rows update in
place from preparation through permission, execution, and terminal outcome.
Structured file previews show operation, path, revision, aggregate counts,
hunks, and textual `+`/`-` markers.

History browsing is intentionally mouse-wheel-only. The composer retains focus,
draft, insertion point, and real caret while history moves. New output increments
an unread count until the wheel returns to live bottom. A one-cell scrollbar is
visible only when history overflows.

Unmodified left-button drag performs pane-clipped application selection and
copies through a trusted platform-native writer when available plus OSC 52 on
release. On macOS the native writer is the fixed platform path
`/usr/bin/pbcopy`; Coragent never discovers clipboard programs through `PATH`.
Wide graphemes remain whole and an
out-of-bounds release stays clipped to the current pane. Terminal-native
`Shift/Option+drag` is the fallback for terminals or policies that suppress OSC
52. Coragent requests `XTSHIFTESCAPE n=0` so supporting terminals keep Shift
available for that fallback.

Terminal bracketed paste is handled as sanitized application input. The
textarea library's `Ctrl+V` clipboard-command binding is disabled so focusing an
editor cannot execute a PATH-resolved `pbpaste`, `wl-paste`, or similar helper.

## Context, omissions, and inspection

The footer shows a percentage only when a trustworthy context window is known.
Estimated usage is labeled `est`; an unknown window shows an absolute token
count without a fabricated percentage. Known usage changes semantic state at
80% and 95%.

Locally folded content remains expandable in `Ctrl+I`. Irreversible omissions
are distinct and non-expandable: tool output budget, preview budget, provider
length, content filter, public redaction, and future context compaction. A
provider-length event may offer `Enter` to prepare an editable new-turn
continuation only when the composer is empty; it never auto-sends.

The inspector shows only secret-free descriptor facts: model/provider, effective
mode and ownership, sandbox posture, usage source, provider features, executable
and advertised tools, safe hook metadata, and optional skills/MCP categories
only when a capability reporter supplies them. Unsupported is not rendered as a
fabricated `0 loaded` count.

## Accessibility and fallbacks

Presentation can be selected before startup:

| Environment | Effect |
|---|---|
| non-empty `NO_COLOR` | Disable semantic color |
| `CORAGENT_ASCII=1` | Use width-stable ASCII borders and glyphs |
| `CORAGENT_REDUCED_MOTION=1` | Replace active animation with static states and a steady caret |
| `TERM=dumb` | Select no-color, ASCII, and reduced-motion together |

Every state also has a textual label or shape; color is never the only signal.
Cell-width and grapheme-aware paths cover CJK, emoji, combining sequences, and
long unbroken text. Untrusted model/tool/hook/path/error content is sanitized
before Markdown and measurement; OSC, OSC 52, CSI, DCS/APC/PM, unsafe renderer
hyperlinks, images, HTML features, carriage-return overwrite, and disallowed
controls cannot reach the terminal as active controls.

## Terminal support status

Automated PTY tests cover resize, bracketed paste, enhanced-keyboard capability
detection, real-caret edits, wheel input, absent keyboard history routing,
direct drag/OSC 52 copy, permission revision, cancellation, forced four-second
shutdown, panic restoration, and control-sequence injection on Unix PTYs.

The intended terminal matrix is Ghostty, iTerm2, Terminal.app, tmux, VS Code,
JetBrains, a Linux console, and Windows Terminal or an equivalent documented
fallback. The sign-off worksheet is [`terminal-matrix.md`](terminal-matrix.md).
Phase 7 archived the still-pending manual matrix as a known-unverified
portability item. Until each row is recorded, use these conservative fallbacks:

- no enhanced keyboard protocol: use `Ctrl+J` for multiline input;
- no application OSC 52 copy: use terminal-native `Shift/Option+drag`;
- weak glyph/color support: set `CORAGENT_ASCII=1` and `NO_COLOR=1`;
- no animation support or `TERM=dumb`: enable reduced motion;
- terminals smaller than `60x20`: resize before allowing a permission request.

## Shutdown contract

`Ctrl+Q` denies one open parent permission request, cancels the run, drains for
at most two seconds, then closes the public session with a separate two-second
limit. Terminal modes are restored even when caller-owned work ignores
cancellation or the reducer panics. A forced shutdown or close failure is
reported safely on stderr after restoration and exits nonzero.
