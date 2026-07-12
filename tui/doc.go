// Package tui implements Coragent's full-screen Bubble Tea frontend.
//
// The frontend depends only on pkg/agent through a narrow SessionPort adapter.
// Its reducer owns run, focus, scroll, permission, overlay, mode, and terminal
// state; it never reads harness internals or the filesystem to infer actions.
// Structured observed facts drive correlated transcript blocks, permission
// revisions, usage chrome, omissions, hooks, and subagent lifecycle rows.
//
// All untrusted text is sanitized before Markdown parsing or terminal-cell
// measurement. The UI supports truecolor, ANSI-256, no-color, reduced-motion,
// and ASCII presentation, and fails safe when a permission prompt is displayed
// below the minimum supported terminal size.
package tui
