## MODIFIED Requirements

### Requirement: Real caret and position-aware composer editing
While the composer is focused and available for idle input, the frontend SHALL
expose a real terminal caret at the logical insertion point through the terminal
view cursor contract. A focus border, focus marker, or painted trailing glyph
MUST NOT be the only cursor indication. The composer SHALL support insertion and
deletion at the caret, horizontal and vertical cursor movement, line-boundary
movement, multiline drafts, and overlay focus transitions without forcing edits
to the end of the draft. Cursor position MUST survive overlay focus changes,
transcript wheel scrolling, and terminal resize as a logical grapheme position,
and rendering and editing MUST remain safe for
CJK, combining marks, emoji, and other variable-cell-width Unicode text.

When the composer has no vertical room for cursor movement (the draft is a single
line, or the caret is at the top or bottom line boundary), pressing Up SHALL
recall the most recent submitted input into the composer, replacing the current
draft, and pressing Down SHALL walk forward through history toward the newest
entry. When past the newest history entry, the composer SHALL clear to an empty
draft.

#### Scenario: Idle focused composer exposes the insertion point
- **WHEN** the session is idle and the composer owns focus
- **THEN** a real caret is visible at the current logical insertion point, including in an empty draft
- **THEN** the user is not required to infer focus or insertion position from color, a border, or a static marker

#### Scenario: User edits inside an existing draft
- **WHEN** the user moves left, right, up, down, to a line boundary, or across lines and then inserts or deletes text
- **THEN** the change occurs at the resulting caret position rather than only at the end of the draft
- **THEN** unaffected draft content and line breaks remain unchanged

#### Scenario: Composer focus leaves for an overlay and returns
- **WHEN** focus moves from the composer to a higher-priority modal or overlay and later returns
- **THEN** the composer caret is hidden while that overlay owns focus and becomes visible again on return
- **THEN** the original draft and logical insertion point are preserved unless that editor deliberately changed them

#### Scenario: Transcript wheel scrolling does not take composer focus
- **WHEN** the idle composer owns focus with a non-empty draft and the user wheels through overflow history
- **THEN** the transcript position changes without a transcript-focus transition
- **THEN** the composer remains focused and its draft, logical insertion point, and visible real caret remain unchanged

#### Scenario: Resize reflows a focused draft
- **WHEN** the terminal width changes while a multiline draft is focused
- **THEN** the composer reflows and keeps the caret on the same logical grapheme boundary
- **THEN** the caret remains within the visible composer viewport without moving the edit point to the draft end

#### Scenario: Unicode text surrounds the caret
- **WHEN** a draft contains CJK, emoji, combining sequences, or mixed narrow and wide characters
- **THEN** cursor movement, insertion, deletion, wrapping, and caret placement use grapheme-safe terminal-cell semantics
- **THEN** no operation splits a displayed grapheme, overwrites adjacent chrome, or panics

#### Scenario: Up recalls previous input from history
- **WHEN** the composer is focused, the slash-suggest dropdown is not active, the draft has no vertical room for cursor movement, and at least one previously submitted input exists
- **THEN** pressing Up replaces the current composer value with the most recent submission
- **THEN** pressing Up repeatedly walks backward through earlier submissions

#### Scenario: Down walks forward through history
- **WHEN** the composer is focused, no slash-suggest dropdown is active, the draft has no vertical room for cursor movement, and the user has navigated into history with Up
- **THEN** pressing Down walks forward toward newer entries
- **THEN** pressing Down past the newest entry clears the composer to an empty draft

#### Scenario: Empty submissions are not saved to history
- **WHEN** the user submits a draft consisting only of whitespace
- **THEN** the submission is not appended to input history

#### Scenario: Multi-line draft preserves vertical cursor movement
- **WHEN** the composer contains a multi-line draft with room for vertical cursor movement above or below the caret
- **THEN** pressing Up or Down moves the cursor within the draft rather than recalling history

## ADDED Requirements

### Requirement: In-memory input history

The frontend SHALL maintain an in-memory history of submitted inputs scoped to
the current session. History entries SHALL be appended in submission order.
History MUST NOT persist to disk and SHALL be lost on exit.

#### Scenario: History accumulates across turns
- **WHEN** the user submits "hello" in turn 1, then "world" in turn 2
- **THEN** the history contains ["hello", "world"] in that order
- **THEN** pressing Up twice from an empty composer in turn 3 recalls "hello"

#### Scenario: History does not persist across sessions
- **WHEN** the user exits the application and starts a new session
- **THEN** no history from the previous session is available
