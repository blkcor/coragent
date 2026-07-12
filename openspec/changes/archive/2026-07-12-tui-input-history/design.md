## Context

The TUI's `composerModel` wraps a Bubble Tea `textarea.Model`. Submitted drafts are currently lost — the only saved reference is `pendingSubmission`, which is transient. The `handleComposerKey` method already intercepts `up`/`down` keys but only when the slash-suggest dropdown is active (to navigate suggestions). When slash-suggest is inactive, `up`/`down` fall through to the textarea for vertical cursor movement within multi-line drafts.

No external dependencies are needed — this is purely in-process state.

## Goals / Non-Goals

**Goals:**
- Up arrow recalls the previous submitted input into the composer
- Down arrow walks forward through history, clearing when past the newest entry
- History is per-session, in-memory (lost on exit)

**Non-Goals:**
- Persistent history across sessions (disk write)
- Search or filter over history
- History deduplication against adjacent entries
- History in subagent sessions or non-TUI frontends

## Decisions

### History as a ring buffer in AppModel

Two new fields on `AppModel`: `inputHistory []string` and `historyIdx int`. `historyIdx` starts at `len(inputHistory)` (meaning "current draft, not in history"). Up decrements the index, Down increments it.

**Alternatives considered:**
- Storing history inside `composerModel` — rejected because `composerModel` is intentionally a thin wrapper around textarea and shouldn't know about app-level concepts like submission lifecycle.
- Using a dedicated `historyModel` struct — rejected as over-abstraction for a slice + int.

### Interception in handleComposerKey before textarea delegation

Up/Down are intercepted in `handleComposerKey` when: the composer is focused, no slash-suggest dropdown is active, and the draft is at a history-navigable boundary (single-line or cursor at first/last line). If the textarea has a multi-line draft with room to move vertically, the textarea keeps the keys for cursor movement.

**Alternatives considered:**
- Always intercept Up/Down regardless of draft state — rejected because it would break vertical cursor movement in multi-line drafts.
- Only intercept when draft is empty — simpler but too restrictive; users want to navigate history even when they've partially typed.

### Saving to history in submitDraft

The current draft is appended to `inputHistory` right before `composer.Reset()`, and `historyIdx` is reset to point past the end (the "new draft" position).

## Risks / Trade-offs

- **Multi-line draft conflict**: Up/Down for cursor movement vs. history navigation. Mitigation: only intercept when the draft has no vertical room to move (single-line draft, or cursor at top/bottom line boundary).
- **Draft loss**: Navigating into history replaces the current draft. The in-progress draft is lost unless the user navigates back past the newest entry. This matches standard terminal behavior (bash, zsh, etc.).
