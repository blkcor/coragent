# Phase 7 terminal verification matrix

Automated Unix PTY coverage is recorded in `tui/pty_test.go`. Phase 7 was
archived with the rows below retained as an explicit known-unverified portability
worksheet. Do not turn an automated result into a manual pass. Completing the
matrix remains recommended before a broad binary release, but it is not an
active OpenSpec task.

Use one real session per row at `80x24` and resize once to `60x20` and
`120x36`. Record terminal version, OS, multiplexer/remote layer, result, and any
required fallback.

| Terminal | Version / OS | Real caret + Unicode edit | Resize | Wheel-only history | Direct drag + OSC 52 | Permission revision | Cancel / quit restore | Result / fallback |
|---|---|---|---|---|---|---|---|---|
| Ghostty | pending | pending | pending | pending | pending | pending | pending | pending |
| iTerm2 | pending | pending | pending | pending | pending | pending | pending | pending |
| Terminal.app | pending | pending | pending | pending | pending | pending | pending | pending |
| tmux | pending | pending | pending | pending | pending | pending | pending | pending |
| VS Code terminal | pending | pending | pending | pending | pending | pending | pending | pending |
| JetBrains terminal | pending | pending | pending | pending | pending | pending | pending | pending |
| Linux console | pending | pending | pending | pending | pending | pending | pending | pending |
| Windows Terminal or documented equivalent | pending | pending | pending | pending | pending | pending | pending | pending |

## Required interaction script

1. Start with an empty focused composer and verify the terminal caret matches
   insertion inside `A界é👩‍💻Z`.
2. Insert a newline with `Ctrl+J`; use modified Enter only if the terminal
   reports enhanced keyboard support.
3. Paste a multiline bracketed payload and confirm it remains one editable
   draft.
4. Stream enough content to overflow; verify `PageUp`, arrows, and `j/k` do not
   move history, then browse only with the wheel while the composer caret and
   draft remain unchanged.
5. Drag without modifiers inside the current pane and confirm OSC 52 copy;
   repeat across a wide glyph and release outside the pane. If blocked, record
   terminal-native `Shift/Option+drag` as the fallback.
6. Review a structured diff, submit an argument revision with `Ctrl+S`, then
   allow the refreshed revision. At a size below `60x20`, verify only
   deny/cancel/quit remain available.
7. Cycle safe modes with `Shift+Tab`; verify bypass appears only through the
   `Ctrl+B` confirmation.
8. Cancel active work with `Ctrl+C`, then quit with `Ctrl+Q`. Confirm alternate
   screen, mouse tracking, bracketed paste, keyboard protocol, and cursor shape
   are restored.
9. Repeat with `NO_COLOR=1`, `CORAGENT_ASCII=1`, and
   `CORAGENT_REDUCED_MOTION=1`.

## Conservative fallbacks

- Enhanced keyboard unavailable: advertise and use only `Ctrl+J` for newline.
- OSC 52 blocked: use terminal-native `Shift/Option+drag`.
- Unicode glyphs or color unreliable: use ASCII plus no-color mode.
- Animation undesirable or terminal is `dumb`: use reduced motion.
- Terminal below `60x20`: resize before allowing, remembering, or editing a
  permission request.
