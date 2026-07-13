# tui-frontend Specification

## Purpose
TBD - created by archiving change phase-7-tui. Update Purpose after archive.
## Requirements
### Requirement: Public-SDK-only frontend boundary
The `tui` package and `cmd/coragent` binary SHALL consume Coragent exclusively
through `pkg/agent`. They MUST NOT import a Coragent `internal/*` package, read
harness state directly, recreate tool execution, or establish a second event or
permission channel. Every rendered runtime fact and every permission reply SHALL
flow through the public session descriptor and observed-run contract.

#### Scenario: Import-boundary audit passes
- **WHEN** the frontend and binary imports are inspected transitively
- **THEN** their only Coragent harness dependency is `pkg/agent`
- **THEN** no `internal/*` package is imported by either frontend package

#### Scenario: Run state comes from the public contract
- **WHEN** a session streams text, tool activity, permissions, hooks, omissions, usage, or subagent lifecycle events
- **THEN** the frontend derives its state from the public descriptor and observed event stream
- **THEN** it does not poll or infer private harness state

#### Scenario: Permission reply uses the originating request
- **WHEN** the user answers a public permission request
- **THEN** the frontend sends exactly one decision through that request's public reply path
- **THEN** no alternate dispatch path is invoked

### Requirement: Deterministic startup and ready state
The application SHALL bootstrap its session through the public SDK before
accepting input. Startup SHALL visibly distinguish loading, ready, and fatal
configuration or construction failure, and SHALL leave the terminal restored on
every exit path.

#### Scenario: Successful startup reaches a focused composer
- **WHEN** settings discovery and session bootstrap succeed
- **THEN** the app opens with an empty transcript, idle status, effective mode and sandbox labels, and a focused composer with a visible real caret

#### Scenario: Startup remains visibly active
- **WHEN** session bootstrap has not completed
- **THEN** the app shows a bounded loading state
- **THEN** submit actions are unavailable until bootstrap either succeeds or fails

#### Scenario: Startup failure is actionable
- **WHEN** settings, provider, hook, sandbox, or session construction fails
- **THEN** the app shows a concise error naming the failing category and a safe remediation hint
- **THEN** it exits non-zero after restoring the terminal
- **THEN** the error does not reveal resolved credentials or other secret values

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

### Requirement: Responsive layout across supported terminal classes
The frontend SHALL preserve a usable transcript, status, safety state, and
composer at `60x20`, `80x24`, `120x36`, and `160x48`. Resize handling MUST be
state-preserving and MUST recompute wrapping from semantic content rather than
from previously rendered lines.

#### Scenario: Ultra-compact layout at 60x20
- **WHEN** the terminal is `60x20`
- **THEN** the app uses a single-column transcript, one compact chrome line, a compact status line, and a composer of at least one visible row
- **THEN** secondary metadata moves to the inspector rather than overwriting content

#### Scenario: Standard layout at 80x24
- **WHEN** the terminal is `80x24`
- **THEN** the app uses a single-column transcript with full tool-state labels, status, mode, context summary, and a multiline composer

#### Scenario: Spacious layout at 120x36
- **WHEN** the terminal is `120x36`
- **THEN** the app gives the transcript additional width and may expose a metadata rail without reducing the composer or transcript below their compact guarantees

#### Scenario: Wide layout at 160x48
- **WHEN** the terminal is `160x48`
- **THEN** the app shows full metadata and keeps one primary transcript column
- **THEN** an opened inspector or permission detail may use a second column without becoming a permanent competing pane
- **THEN** transcript ordering remains identical to narrower layouts

#### Scenario: Live resize preserves interaction state
- **WHEN** the terminal moves between any supported size classes while text is streaming, a draft exists, or history is scrolled
- **THEN** the app preserves the draft, modal, expanded or folded state, selected inspector item, and semantic scroll anchor
- **THEN** no transcript item is duplicated, dropped, or reordered

### Requirement: Explicit too-small behavior
Below `60` columns or `20` rows, the frontend SHALL enter a dedicated too-small
view that reports the current and minimum dimensions. Safety and lifecycle keys
MUST remain operational, and a pending permission decision MUST take precedence
over the size notice so the harness cannot be stranded. Because this size cannot
guarantee review of the complete action, reason, and preview, allow, remember,
argument-edit, and grant-edit actions MUST remain disabled until the terminal
returns to a supported size.

#### Scenario: Non-modal screen becomes too small
- **WHEN** the terminal is narrower than `60` columns or shorter than `20` rows with no permission pending
- **THEN** normal layout is replaced by a plain minimum-size notice showing the current dimensions
- **THEN** resize, cancel, and quit handling remains active

#### Scenario: Permission fails safe when too small
- **WHEN** the terminal becomes narrower than `60` columns or shorter than `20` rows while a permission request is pending
- **THEN** the app renders the most compact permission identity, resize instruction, and deny/cancel/quit hints that fit
- **THEN** deny-once, cancel, and safe quit remain functional, but no allow, remember, argument-edit, or grant-edit action can be invoked blindly
- **THEN** leaving the request unanswered preserves it for full review after resize

#### Scenario: Resize restores the prior screen
- **WHEN** a too-small terminal returns to at least `60x20`
- **THEN** the app restores the prior transcript, draft, scroll anchor, overlays, and run state without restarting the session

### Requirement: Ordered streaming transcript
The frontend SHALL render user turns, assistant replies, tool cards, notices,
hook outcomes, and subagent lifecycle items as one ordered transcript. Assistant
text deltas MUST become visible incrementally, MUST update the correlated reply
in place, and MUST converge to the exact completed public reply without loss,
duplication, or reordering.

#### Scenario: First reply content appears before completion
- **WHEN** an observed run emits assistant text deltas before its terminal outcome
- **THEN** the reply becomes visible as deltas arrive rather than waiting for run completion

#### Scenario: Streaming reply converges exactly
- **WHEN** an assistant reply emits multiple text chunks and then completes
- **THEN** the final transcript text equals the ordered concatenation represented by the public stream
- **THEN** re-render batching does not drop or repeat content

#### Scenario: Tool activity remains interleaved in event order
- **WHEN** a reply pauses for tools and later resumes streaming
- **THEN** the tool cards appear at their observed positions between the assistant segments
- **THEN** completed history remains stable when later events arrive

#### Scenario: User request remains visible
- **WHEN** the user submits a non-blank request
- **THEN** the request is appended once to the transcript before the run's first response event

### Requirement: Streaming-safe Markdown
Assistant prose and provider-supplied reasoning summaries SHALL support Markdown
without treating an incomplete streaming suffix as a final construct. The
renderer MUST preserve a cached stable prefix, progressively render recognized
Markdown in the active tail from the first visible delta batch, keep only the
shortest genuinely ambiguous suffix literal, and re-render the completed message
deterministically when the reply ends. An active tail MAY reflow or restyle when
later delimiters resolve ambiguity, but already-promoted prefix blocks MUST NOT
flicker or change. Tool, hook, error, and path content MUST remain literal unless
the public event marks it as trusted Markdown.

#### Scenario: First recognizable Markdown renders immediately
- **WHEN** the first visible assistant deltas form a heading, emphasis, list, quote, inline code, table row, or fenced-code opener
- **THEN** the recognizable construct is displayed through the terminal Markdown style during streaming rather than exposing its source markers until reply completion
- **THEN** only a minimal unresolved delimiter suffix may remain literal until later input disambiguates it

#### Scenario: Incomplete fenced code block streams safely
- **WHEN** a chunk opens a fenced code block and the closing fence has not arrived
- **THEN** completed preceding blocks retain their formatting
- **THEN** the open tail is displayed as bounded safe code without swallowing surrounding chrome or reverting the completed prefix to raw Markdown

#### Scenario: Inline delimiter crosses chunk boundaries
- **WHEN** backticks, emphasis markers, links, lists, or table syntax are split across chunks
- **THEN** the renderer neither drops characters nor emits terminal control sequences from the partial construct
- **THEN** the final rendering matches rendering the completed source in one pass

#### Scenario: Tool output contains Markdown punctuation
- **WHEN** an untrusted tool result contains backticks, brackets, or HTML-like text
- **THEN** those characters are displayed literally unless the event explicitly identifies trusted Markdown

#### Scenario: Narrow code and table content remains bounded
- **WHEN** Markdown contains a line wider than the viewport
- **THEN** the frontend wraps or visibly clips within its own content region without overwriting chrome or causing a layout panic

### Requirement: Safe reasoning-summary disclosure
The frontend SHALL render only provider-supplied reasoning summaries carried by
the public observed stream. Summary content SHALL be labeled as a summary,
temporarily expanded while provider summary text is actively streaming, collapsed
by default after completion, and inspectable on demand. The frontend MUST NOT
request, synthesize, infer, persist, or display hidden chain-of-thought.

#### Scenario: Provider supplies a reasoning summary
- **WHEN** the public stream supplies reasoning-summary content
- **THEN** the transcript shows a collapsed, clearly labeled reasoning-summary disclosure at the correlated point in the run
- **THEN** opening it reveals only the supplied summary

#### Scenario: Reasoning summary streams incrementally
- **WHEN** summary deltas arrive before assistant text
- **THEN** the disclosure is expanded while it updates in place without creating one item per delta
- **THEN** its final content preserves public event order

#### Scenario: Completed summary folds by default
- **WHEN** a streaming reasoning summary completes
- **THEN** it becomes a one-line collapsed disclosure until the user explicitly expands it

#### Scenario: Provider does not support summaries
- **WHEN** the session descriptor reports no reasoning-summary support and no summary event arrives
- **THEN** the app shows only ordinary activity feedback such as `Thinking`
- **THEN** it does not fabricate a summary or label ordinary assistant prose as reasoning

#### Scenario: Summary remains private to the current UI session
- **WHEN** a summary has been rendered and the app exits
- **THEN** the frontend does not independently persist it to settings, logs, or a cache

### Requirement: Correlated tool cards and inline lifecycle updates
Each observed tool call SHALL have one stable card at its transcript position.
The card SHALL show a sanitized tool name and effective argument summary, and
SHALL update that same card through applicable `preparing`, `waiting for
approval`, `running`, `succeeded`, `errored`, `blocked`, and `cancelled` states.
States that do not occur MUST NOT be invented, and sequential execution order
MUST remain visible.

#### Scenario: Tool starts and completes successfully
- **WHEN** a tool starts and later succeeds
- **THEN** one card appears at start and the same card changes from active to succeeded
- **THEN** its result summary and duration are shown when those facts are available

#### Scenario: Tool waits for permission
- **WHEN** a correlated permission request arrives before execution
- **THEN** the card changes to waiting-for-approval while the modal is open
- **THEN** it does not claim that the tool is running

#### Scenario: Tool emits an inline update
- **WHEN** the public stream emits progress or prepared-action updates for a tool identity
- **THEN** the existing card incorporates the newest update without appending a duplicate tool card

#### Scenario: Tool fails or is blocked
- **WHEN** a tool errors, a hard hook blocks it, permission denies it, or the sandbox refuses it
- **THEN** the card uses a distinct textual terminal state that identifies the category
- **THEN** the application remains usable for subsequent transcript events and turns

#### Scenario: Turn cancellation reaches an active tool
- **WHEN** cancellation produces a tool-cancelled outcome
- **THEN** the active card becomes cancelled rather than remaining in a perpetual spinner state

### Requirement: Effective action previews and diff rendering
When the public stream provides a structured action preview, the frontend SHALL
show the preview associated with the effective post-hook and post-edit arguments
before mutation. File previews SHALL render paths, hunks, and added and removed
lines with both textual markers and styling; a typed non-diff preview SHALL fall
back to bounded literal text. The frontend MUST NOT parse arbitrary result text
and present the guess as an authoritative pre-apply diff.

#### Scenario: File edit has a structured diff
- **WHEN** a mutating file tool emits a structured preview before execution
- **THEN** its card shows the affected path and hunks before the permission decision or mutation
- **THEN** additions use a `+` marker and removals use a `-` marker in addition to any color

#### Scenario: Hook changes arguments before preview
- **WHEN** a before-tool hook replaces arguments and preparation emits the resulting preview
- **THEN** the card and permission modal show the post-hook effective action
- **THEN** the stale pre-hook preview is not presented as the action that will run

#### Scenario: User edits arguments
- **WHEN** edited arguments validate and produce a replacement preview
- **THEN** the modal replaces the old summary and preview before asking for the final allow decision
- **THEN** the preview corresponds to the arguments returned in that decision

#### Scenario: Structured preview is not diff-shaped
- **WHEN** the public action preview is plain text or metadata rather than a file diff
- **THEN** the card renders that content literally within its bounds
- **THEN** the absence of a diff does not break the card

#### Scenario: Preview is unavailable
- **WHEN** a mutating action has no public structured preview
- **THEN** the modal explicitly says that a preview is unavailable
- **THEN** the UI does not imply that the shown argument summary is an exact diff

### Requirement: Honest content folding and omission labels
The frontend SHALL distinguish reversible UI folding from irreversible tool or
action-preview truncation, model reply length cutoff, provider content filtering,
public redaction, and future context compaction. Every omission marker SHALL state
the category and whether the hidden content can be expanded. Counts SHALL be shown
only when the public event supplies them or the UI folded the content itself.

#### Scenario: UI folds locally retained content
- **WHEN** a long tool result, diff, reasoning summary, or notice is folded solely for display
- **THEN** the marker labels it as locally hidden and expandable
- **THEN** opening it reveals all locally retained sanitized content without rerunning the tool

#### Scenario: Executor truncated tool output
- **WHEN** the public result identifies central tool-output truncation
- **THEN** the card labels the missing tail as unavailable and non-expandable
- **THEN** the UI does not offer a fake reveal control

#### Scenario: Action preview was truncated
- **WHEN** an action preview carries a structured `preview_budget` omission
- **THEN** the preview labels itself incomplete and non-expandable while retaining the supplied operation and aggregate change metadata
- **THEN** the UI does not imply that the retained hunks are the complete proposed change

#### Scenario: Model reply hits a length limit
- **WHEN** the observed reply ends because of a model output limit
- **THEN** the assistant item ends with a distinct reply-cutoff notice rather than a tool-truncation label

#### Scenario: New-turn cutoff offers an editable continuation draft
- **WHEN** a selected provider-length omission carries continuation mode `new_user_turn`, the run is idle, and the composer is empty
- **THEN** `Enter` pre-fills and focuses an editable request to continue from the cutoff point
- **THEN** the frontend does not submit or start a new run until the user explicitly sends that draft

#### Scenario: Cutoff is not resumable or a draft already exists
- **WHEN** continuation mode is `unknown` or `unavailable`, or the composer already contains a draft
- **THEN** the cutoff notice does not advertise or trigger an automatic continuation
- **THEN** the existing draft is not overwritten

#### Scenario: Provider filtered content
- **WHEN** the public stream reports content filtering
- **THEN** the transcript shows a content-unavailable notice without inventing or summarizing the removed content

#### Scenario: Public payload was redacted
- **WHEN** the public stream reports a redacted omission
- **THEN** the correlated item labels the removed content as redacted and unavailable
- **THEN** the UI neither exposes nor attempts to reconstruct the removed value

#### Scenario: Future context compaction is reported
- **WHEN** a future public event reports conversation compaction
- **THEN** the UI labels it as history compaction and does not conflate it with display folding or reply cutoff

#### Scenario: Omitted count is unknown
- **WHEN** an omission event supplies no byte, line, token, or item count
- **THEN** the UI states that content was omitted without fabricating a numeric count

### Requirement: Modal permission control
Every public permission request SHALL open one focus-capturing modal associated
with its tool card. The modal SHALL show the action, reason, effective arguments
or preview, origin, remembered-rule scope when available, and any proposed
one-call sandbox grants. Background composer, transcript scrolling, help, and
inspector commands MUST be inert until the modal or its child editor is resolved.
Live `Shift+Tab` and confirmed `Ctrl+B` mode changes MAY remain reachable, but
MUST NOT resolve, approve, or dismiss the already-open request.

#### Scenario: Root action requests permission
- **WHEN** a root tool emits a permission request
- **THEN** the modal identifies the root action, reason, effective preview, and available decisions
- **THEN** the correlated tool card shows waiting for approval

#### Scenario: Subagent action requests permission
- **WHEN** a routed child permission request arrives
- **THEN** the modal identifies the stable subagent label, parent/depth provenance, and child action
- **THEN** raw child reasoning, transcript, and tool history remain absent

#### Scenario: Caller-owned legacy dispatcher requests permission
- **WHEN** the public request declares protocol `legacy_one_shot`
- **THEN** the modal shows external ownership and the available allow/deny plus applicable remember actions
- **THEN** argument revision, schema-aware editing, preview, and grant controls are hidden or disabled according to the request instead of being fabricated

#### Scenario: Exact remembered scope is safe
- **WHEN** the standard permission engine supplies an exact-call remembered scope
- **THEN** allow-and-remember and deny-and-remember are selectable
- **THEN** the modal shows the safe display label and does not show the hash or raw secret-bearing arguments as the remembered scope

#### Scenario: Non-modal input arrives during a prompt
- **WHEN** a permission modal is open and the user presses a composer, scroll, mode, help, or inspector key
- **THEN** the key has no effect unless it is one of the modal's displayed commands

### Requirement: Permission decisions, argument editing, and sandbox grants
The permission modal SHALL provide `a` for allow once, `d` for deny once, `A`
for allow and remember, `D` for deny and remember, `e` for argument editing, and
`s` for one-call sandbox grant editing when those operations are supported.
It SHALL build one stable list containing only enabled actions. `Up/Down` and
`j/k` SHALL move the visible selection, while `Enter` or `Space` SHALL submit or
open the selected action exactly once. `PageUp/PageDown`, `Ctrl+U/D`, `Home/End`,
and the mouse wheel SHALL scroll only the review body, with page movement
retaining at least one line of overlap so no action, reason, or preview line is
skipped. Arrow selection MUST NOT scroll the review body.
Argument and grant editors SHALL validate before returning to the decision modal;
neither editing operation alone SHALL approve execution. Remembered action rules
MUST NOT silently persist per-call sandbox grants. Every rich decision SHALL
enter a `submitting` state that keeps the modal and drafts present with decision
keys disabled until the public reply operation returns its typed outcome. The
modal SHALL close only on `accepted`; `validation_rejected` SHALL restore the
same request, clear the submission guard, preserve drafts, and display feedback;
`already_resolved` SHALL close and reconcile from the authoritative stream.

#### Scenario: Allow once sends a one-call decision
- **WHEN** the user presses `a` on the decision modal
- **THEN** exactly one allow decision with `remember=false` is submitted and the modal remains visibly pending
- **THEN** the modal closes and execution may continue only after the public reply outcome is `accepted`

#### Scenario: Arrow selection activates with Enter
- **WHEN** the user moves from allow once to allow-and-remember with `Down` and presses `Enter`
- **THEN** the focus marker follows allow-and-remember and exactly one remembered allow reply is submitted
- **THEN** the review scroll position remains unchanged by the arrow key

#### Scenario: Review paging does not skip content
- **WHEN** a long permission review is paged repeatedly with `PageDown`
- **THEN** consecutive pages overlap and every retained ACTION, WHY, SCOPE, and PREVIEW line remains reachable
- **THEN** the selected decision remains unchanged

#### Scenario: Deny once sends a one-call decision
- **WHEN** the user presses `d` or dismisses the decision modal with `Esc`
- **THEN** exactly one deny decision with `remember=false` is submitted
- **THEN** an `accepted` outcome closes the modal so the harness is not left waiting

#### Scenario: Remembered decision shows its scope
- **WHEN** a remembered rule is available and the user presses `A` or `D`
- **THEN** the modal has already displayed the exact generalized scope
- **THEN** exactly one matching allow-or-deny decision with `remember=true` is submitted and closes the modal only on `accepted`

#### Scenario: Rich decision is rejected without consuming the request
- **WHEN** an allow, remember, revision, or grant-bearing reply returns `validation_rejected`
- **THEN** the current modal or editor is restored with its arguments and grant draft intact
- **THEN** the submission guard clears, typed feedback is shown, and the same request can be corrected or denied

#### Scenario: Submitted request was already resolved
- **WHEN** a rich reply returns `already_resolved` because terminal or competing state already settled the request
- **THEN** the modal closes and the reducer follows the authoritative observed state
- **THEN** it does not reopen the request or submit a second decision

#### Scenario: Argument editor validates JSON-shaped arguments
- **WHEN** the user presses `e`, modifies the effective arguments, and submits them with `Ctrl+S`
- **THEN** malformed input remains in the editor with a field-level validation error
- **THEN** locally valid input sends an argument-revision reply without approving execution; a refreshed effective preview appears in a new permission request only after public validation, hard hooks, and preparation succeed

#### Scenario: Harness rejects a revision before consuming the request
- **WHEN** the rich reply path returns typed schema or applicability feedback without resolving the permission request
- **THEN** the editor retains the user's draft and shows the field-level feedback
- **THEN** the user can correct and resubmit the draft or return to deny the still-open request

#### Scenario: Hard hook rejects an accepted revision
- **WHEN** the original request was resolved by a schema-valid revision and the public stream then reports a hard hook block or failure
- **THEN** the editor closes into the related tool error or blocked state
- **THEN** the UI does not fabricate a replacement permission request

#### Scenario: Argument editor is cancelled
- **WHEN** the user presses `Esc` inside the argument editor
- **THEN** the editor closes back to the parent permission modal without sending a permission decision
- **THEN** the previously valid effective arguments remain selected

#### Scenario: One-call sandbox grants are edited
- **WHEN** the user presses `s` and adds readable roots, writable roots, or network access
- **THEN** the editor labels them as additive grants for this call and shows the resulting sandbox summary
- **THEN** the grants are included only if the user subsequently allows the action

#### Scenario: Remembering an approval with grants
- **WHEN** the user selects allow-and-remember after adding one-call sandbox grants
- **THEN** the action rule may be remembered according to the displayed scope
- **THEN** the sandbox grants remain one-call grants and are not silently written into the remembered rule

#### Scenario: Quit occurs inside an editor
- **WHEN** the user presses `Ctrl+Q` in the argument or grant editor
- **THEN** the frontend sends one deny-once decision for the parent request before beginning shutdown

#### Scenario: Control-C occurs on a decision modal
- **WHEN** the user presses `Ctrl+C` on the permission decision modal
- **THEN** the frontend sends exactly one deny-once decision and then requests cancellation of the run
- **THEN** the same keypress does not send a second permission reply or immediately terminate the process

#### Scenario: Control-C occurs inside a permission editor
- **WHEN** the user presses `Ctrl+C` in the argument or grant editor
- **THEN** the frontend sends exactly one deny-once decision for the parent request and then requests cancellation of the run
- **THEN** editor focus does not swallow cancellation or cause a duplicate reply

### Requirement: Deterministic keyboard routing and shortcut map
The application SHALL route each key to exactly one active scope in this order:
permission modal, argument or grant editor, inspector/help overlay, composer,
then global actions. Transcript scroll state MUST NOT own keyboard focus. The
built-in key map SHALL be visible from `Ctrl+/`; transcript history is the
intentional pointer exception and SHALL be described as mouse-wheel-only.

#### Scenario: Composer submit and multiline keys
- **WHEN** the composer is focused with non-blank text and no higher-priority overlay is active
- **THEN** `Enter` submits the request
- **THEN** `Ctrl+J` inserts a newline without submitting
- **THEN** distinct `Shift+Enter` and `Alt+Enter` events from enhanced terminal keyboard protocols act as newline aliases

#### Scenario: Legacy terminal cannot distinguish an alias
- **WHEN** a terminal does not emit a distinct `Shift+Enter` or `Alt+Enter` event
- **THEN** `Ctrl+J` remains the guaranteed multiline binding
- **THEN** the UI does not claim that an indistinguishable alias is active

#### Scenario: Bracketed multiline paste stays a draft
- **WHEN** the composer receives a bracketed paste containing multiple lines
- **THEN** all sanitized pasted lines are inserted into the draft in order
- **THEN** embedded newlines do not implicitly submit the request

#### Scenario: Empty composer is submitted
- **WHEN** the composer contains only whitespace and the user presses `Enter`
- **THEN** no run starts and no empty user turn is appended

#### Scenario: Help and inspector shortcuts
- **WHEN** no permission modal is active and the user presses `Ctrl+/` or `Ctrl+I`
- **THEN** `Ctrl+/` opens the complete context-aware shortcut help
- **THEN** `Ctrl+I` opens the session inspector for mode, model, context, sandbox, capabilities, hooks, and inspectable transcript details
- **THEN** `Esc` closes the active overlay without cancelling an otherwise running turn

#### Scenario: Inspector navigation and local expansion
- **WHEN** the inspector is open
- **THEN** arrow keys move within its sections, `Enter` toggles a locally expandable item, and `Esc` returns to the prior screen
- **THEN** non-expandable omissions remain labeled as unavailable

#### Scenario: Keyboard never scrolls transcript history
- **WHEN** `PageUp`, `PageDown`, arrows, `j/k`, `Ctrl+U/D`, `End`, or `G` is pressed outside a scrollable modal or inspector
- **THEN** no background transcript scroll action or transcript-focus transition occurs
- **THEN** the active composer or overlay may apply its own normal editing meaning without changing the transcript semantic anchor

#### Scenario: Modal priority wins
- **WHEN** a permission modal is active and the user presses `Enter`, `Ctrl+/`, `Ctrl+I`, `PageUp`, or `PageDown`
- **THEN** the background composer and transcript action are not invoked
- **THEN** only the selected decision, review viewport, or another command explicitly displayed by the modal may take effect

#### Scenario: Modal priority also captures the mouse wheel
- **WHEN** a permission modal or child editor is active and the user turns the mouse wheel
- **THEN** the background transcript does not move
- **THEN** only an explicitly scrollable modal preview or editor viewport may consume that wheel event

### Requirement: Visible and safely changeable permission mode
The frontend SHALL always show the effective permission-control state in a fixed
chrome area. When the session owns the default permission engine, that state is
one of `default`, `auto-accept edits`, `plan`, or `bypass`. When permission is
caller-owned or unavailable, the area SHALL instead show `external` or
`unsupported` with safe ownership detail in the inspector. While idle or running,
`Shift+Tab` SHALL cycle only
`default`, `auto-accept edits`, and `plan`; bypass MUST require `Ctrl+B` followed
by explicit confirmation and MUST NOT be part of the casual cycle. Mode changes
during a run SHALL use the public live setter, and chrome SHALL update only after
that setter succeeds. An open permission request SHALL remain explicitly pending.

#### Scenario: Mode cycles between safe modes
- **WHEN** the effective mode is `default`, `auto-accept edits`, or `plan` and the user presses `Shift+Tab`
- **THEN** the next mode in `default`, `auto-accept edits`, `plan`, `default` order is requested
- **THEN** chrome changes only if the public SDK accepts it

#### Scenario: Bypass requires confirmation
- **WHEN** the user presses `Ctrl+B` from a safe mode while idle, running, or waiting on a permission request
- **THEN** a blocking confirmation explains that permission prompts may be skipped while hard hooks and sandbox confinement still apply
- **THEN** bypass becomes effective only after the user explicitly confirms
- **THEN** an already-open permission request remains open and is not silently approved

#### Scenario: Bypass confirmation is dismissed
- **WHEN** the user presses `Esc` or selects no in the bypass confirmation
- **THEN** the prior effective safe mode remains unchanged

#### Scenario: Cycle continues from bypass
- **WHEN** bypass is already effective and the user presses `Shift+Tab` outside a modal
- **THEN** the next selected mode is `default`

#### Scenario: Mode changes during a run
- **WHEN** a run is active and the user presses `Shift+Tab` or `Ctrl+B`
- **THEN** the frontend requests the selected mode and leaves current execution unchanged
- **THEN** chrome changes only after the setter succeeds and later permission decisions use the new mode

#### Scenario: Public SDK rejects a mode change
- **WHEN** the session cannot apply the requested mode, including a custom dispatcher that owns permission
- **THEN** the prior effective mode remains visible
- **THEN** an actionable non-fatal notice explains that the mode is controlled externally

#### Scenario: Permission mode is externally owned or unsupported
- **WHEN** the public descriptor reports caller-owned or unsupported permission control
- **THEN** the fixed mode area shows `EXTERNAL` or `UNSUPPORTED` instead of fabricating one of the four engine modes
- **THEN** `Shift+Tab` and `Ctrl+B` mode actions are disabled and help directs the user to the ownership detail

### Requirement: Truthful activity, context, capability, and sandbox chrome
Persistent chrome SHALL derive its model, status, effective mode, context usage,
capability, and sandbox facts from the public descriptor and events. It MUST NOT
invent loaded counts, estimate a provider-sourced value without labeling it, or
present a fallback sandbox as OS enforcement. Narrow layouts MAY abbreviate the
chrome only when the full values remain available in the inspector.

#### Scenario: Activity follows observed state
- **WHEN** the run transitions among thinking, waiting for permission, calling a tool, cancelling, and idle
- **THEN** the status text reflects the latest authoritative state
- **THEN** a visual spinner never substitutes for the textual state

#### Scenario: Context usage is provider-reported
- **WHEN** usage arrives with source `provider`
- **THEN** the chrome shows used and limit values or percentage as available and labels the source as provider-reported in the inspector

#### Scenario: Context usage is estimated
- **WHEN** usage arrives with source `estimated`
- **THEN** the chrome and inspector visibly identify it as an estimate
- **THEN** the value is not relabeled as exact provider usage

#### Scenario: Context usage is unknown
- **WHEN** no typed usage or context limit is available
- **THEN** the chrome shows context as unknown rather than `0%`

#### Scenario: Sandbox uses OS enforcement
- **WHEN** the session descriptor reports OS-enforced confinement
- **THEN** the chrome labels the sandbox as OS enforced

#### Scenario: Sandbox uses a policy fallback or is externally owned
- **WHEN** the descriptor reports policy fallback, unsupported details, or a custom dispatcher
- **THEN** the chrome uses the corresponding weaker, unknown, or externally owned label
- **THEN** it never displays the OS-enforced label for that session

### Requirement: Truthful capability and hook inspection
The session inspector SHALL list effective executable-and-advertised tools and
reported hooks with their availability and source. Compact skills and MCP counts
SHALL appear only when a public capability provider truthfully reports them; the
explicit inspector MAY distinguish `not reported` or unsupported from a reported
empty inventory, but MUST NOT render absence as a fabricated `0 loaded` count.
Capability details MUST omit secrets and MUST NOT imply execution authority for
an unavailable item.

#### Scenario: Built-in and custom tools are reported
- **WHEN** the descriptor reports effective tool capabilities from multiple sources
- **THEN** the inspector groups or labels each tool by source and availability
- **THEN** an unavailable or descriptor-only tool is not labeled executable

#### Scenario: Skills are reported by a capability provider
- **WHEN** a public capability provider reports loaded skills
- **THEN** the chrome may show the truthful count and the inspector lists their safe names, source, and availability

#### Scenario: Skills are supported but none are loaded
- **WHEN** a public capability provider reports skills as supported with an empty effective inventory
- **THEN** the inspector may show a truthful supported-empty state attributed to that provider
- **THEN** it does not treat the category as unsupported or invent skill entries

#### Scenario: MCP servers or tools are reported
- **WHEN** a public capability provider reports MCP capabilities
- **THEN** the inspector shows the reported servers or tools and their availability without exposing credentials or connection secrets

#### Scenario: No skills or MCP provider exists
- **WHEN** the descriptor reports skills or MCP as unsupported because no provider exists
- **THEN** normal chrome omits loaded categories and counts
- **THEN** the explicit inspector may say `not reported` or unsupported but does not equate that state with zero loaded

#### Scenario: Hooks are loaded
- **WHEN** the descriptor reports configured in-process or external hooks
- **THEN** the inspector shows safe hook identity, lifecycle moment, and availability
- **THEN** raw environment values and secret-bearing command arguments are not shown

### Requirement: Subagent and hook activity in the root narrative
The frontend SHALL render stable subagent lifecycle and material hook outcomes
inline without creating separate panes. Subagents SHALL be identified by stable
identity, label, parent, and depth and updated in place through their reported
outcomes. The root UI MUST NOT render raw child assistant text, reasoning,
context usage, hook logs, or child tool logs; only routed permission requests and
public lifecycle/result facts may cross that boundary.

#### Scenario: Subagent starts and completes
- **WHEN** a subagent lifecycle start and terminal outcome arrive
- **THEN** one indented transcript item updates from running to its reported completed, failed, cancelled, or reached-step-limit outcome
- **THEN** its stable label and depth remain unchanged

#### Scenario: Nested provenance is visible
- **WHEN** lifecycle events describe a nested descendant
- **THEN** the root transcript shows its parent relationship and depth without opening a child transcript pane

#### Scenario: Delegation is refused at the depth limit
- **WHEN** the task tool reports a depth-limit refusal before creating another child
- **THEN** the originating task card shows a recoverable depth-limit error
- **THEN** the UI does not invent a subagent lifecycle item or child terminal outcome

#### Scenario: Raw child event is unavailable
- **WHEN** a child performs reasoning, model text, hooks, or ordinary tool calls internally
- **THEN** the root UI does not synthesize or display those private events

#### Scenario: Hook blocks an action
- **WHEN** a public hook outcome reports a hard block
- **THEN** the related tool card and transcript notice identify the safe hook name, lifecycle moment, and sanitized reason
- **THEN** the UI does not imply that permission or bypass can override the block

#### Scenario: Hook annotates or changes an action
- **WHEN** the observed stream reports a material hook annotation or argument replacement
- **THEN** the related card is updated in place and the inspector can show the safe outcome metadata
- **THEN** routine raw hook stdout, stderr, and injected context are not dumped into the root transcript

### Requirement: Stable scroll pinning and unread feedback
The transcript SHALL auto-follow only while pinned to the live bottom. Any
upward wheel navigation SHALL preserve a semantic scroll anchor as new content
arrives, and the UI SHALL show a non-color-only unread counter until the user
returns to the bottom. Whenever rendered history exceeds the transcript viewport,
the frontend SHALL reserve one cell for a visible vertical scrollbar whose track
and thumb remain distinguishable by shape in color, no-color, and ASCII modes.
Mouse-wheel up/down SHALL be the only transcript-history scrolling input, and no
keyboard command SHALL change the transcript scroll position. Wheel handling
MUST remain orthogonal to composer focus and MUST use the semantic-anchor, unread,
live-bottom, and clamping reducer state without changing the draft, logical
insertion point, or caret visibility. Because Bubble Tea standard mouse reporting
has no wheel-only mode, the frontend SHALL keep standard mouse tracking active
throughout the supported interactive screen, including composer focus. An
unmodified left-button drag SHALL create a visible application-managed text
selection from the frontend's own rendered-cell snapshot, and release SHALL copy
that selection to the system clipboard through a trusted platform-native writer
when available and a frontend-owned OSC 52 command. Native helpers MUST use a
platform-owned absolute path and MUST NOT be discovered through `PATH`.
Editor keymaps MUST disable library clipboard-read helpers that resolve native
programs through `PATH`; paste input SHALL arrive through Bubble Tea's terminal
`PasteMsg` path and pass the same sanitization as every other pasted value.
The snapshot and all pointer coordinates MUST be clipped to the current frontend
pane width and height so a drag cannot incorporate an adjacent tmux pane. Selection
MUST preserve complete Unicode graphemes and MUST NOT change transcript scroll,
composer focus, draft, or logical insertion point. Scrollbar clicking, thumb
dragging, and other pointer actions remain out of scope. Every scroll offset and
thumb range MUST be
clamped to current rendered bounds after content changes,
folding, or resize so navigation cannot produce a blank page beyond history.
The UI SHALL advertise ordinary `drag copy` as the primary path.
`Shift/Option+drag` MAY remain documented as a terminal-native reporting bypass
whose effective modifier is terminal-owned and terminal-specific. The frontend
SHALL emit `XTSHIFTESCAPE` with `n=0` at startup to preserve that fallback, while
treating terminal support for the request as optional rather than
application-controlled.

#### Scenario: Editor Ctrl+V cannot execute a PATH helper
- **WHEN** the composer or permission editor is focused and the process `PATH` contains an attacker-controlled `pbpaste`, `wl-paste`, or equivalent name
- **THEN** the disabled library clipboard binding executes no helper and changes no draft
- **THEN** terminal bracketed paste remains available through the sanitized application-level paste message

#### Scenario: Live output arrives while pinned
- **WHEN** the viewport is at the bottom and a text delta or item update arrives
- **THEN** the viewport follows the newest visible content

#### Scenario: Live output arrives while scrolled back
- **WHEN** the user has moved above the bottom and new output arrives
- **THEN** the previously anchored content stays at the same visual position as closely as reflow permits
- **THEN** an unread event or line count increases without moving the viewport to the bottom

#### Scenario: User returns to live bottom
- **WHEN** the user wheels downward until the viewport reaches the current bottom
- **THEN** auto-follow resumes and the unread indicator clears

#### Scenario: In-place update occurs above the viewport
- **WHEN** a tool or subagent card above the current viewport changes height
- **THEN** the semantic anchor is preserved rather than jumping to the updated card or live bottom

#### Scenario: Overflow history shows a non-color scrollbar
- **WHEN** rendered transcript history is taller than its viewport
- **THEN** a vertical track and at-least-one-cell thumb show the visible range and relative position
- **THEN** track and thumb remain distinguishable through glyph or shape in no-color and ASCII modes

#### Scenario: Short history has no false overflow affordance
- **WHEN** all rendered transcript content fits within the viewport
- **THEN** the scrollbar is absent or visibly inactive and consumes no misleading scroll range
- **THEN** wheel input keeps the viewport clamped to the complete visible history

#### Scenario: Mouse wheel browses and returns to live output
- **WHEN** the composer is focused, no modal or overlay captures input, and the user wheels upward over overflow history
- **THEN** history enters browsing mode through the semantic-anchor path without moving focus
- **THEN** the composer draft, logical insertion point, and visible idle caret remain unchanged
- **WHEN** the user wheels downward to the current bottom
- **THEN** the viewport repins to live output and clears the unread indicator

#### Scenario: Standard mouse tracking remains active
- **WHEN** the composer owns focus or the transcript is scrolled away from live output
- **THEN** standard mouse tracking remains enabled because Bubble Tea provides no wheel-only reporting mode
- **THEN** left-button press, motion, and release route only to pane-local text selection while wheel reports retain their existing scroll behavior

#### Scenario: Unmodified drag selects and copies within the current pane
- **WHEN** mouse reporting is active and the user drags with the unmodified left button
- **THEN** Coragent visibly highlights the selected rendered cells and copies their sanitized plain text through OSC 52 on release
- **THEN** composer focus, draft, logical insertion point, and transcript scroll state remain unchanged

#### Scenario: Drag cannot cross a tmux pane boundary
- **WHEN** a drag begins in Coragent and its reported motion or release coordinates reach beyond the current pane bounds
- **THEN** coordinates and the captured render snapshot clamp to Coragent's current pane width and height
- **THEN** clipboard text contains no cells from an adjacent pane and wide graphemes remain complete

#### Scenario: Native terminal copy remains a reporting fallback
- **WHEN** the user explicitly chooses terminal-native selection instead of application-managed copy
- **THEN** Coragent has requested that supporting terminals reserve Shift through `XTSHIFTESCAPE n=0`
- **THEN** contextual help identifies `Shift/Option+drag` as a terminal-owned fallback rather than a prerequisite for ordinary drag copy

#### Scenario: Over-scroll and resize remain clamped
- **WHEN** repeated wheel input requests a position above the oldest content or below the newest content, or resize reduces the scrollable range
- **THEN** the viewport and scrollbar thumb clamp to the nearest valid bound
- **THEN** the transcript never becomes an empty page while renderable history exists

#### Scenario: New output preserves scrollbar anchor and unread state
- **WHEN** new content arrives or a block changes height while the user is browsing history
- **THEN** the anchored block and intra-block visual offset remain at the same visual position as closely as reflow permits
- **THEN** the thumb updates to the new total range, unread increases, and the viewport does not jump to live bottom

### Requirement: Bounded animation and reduced motion
Animation SHALL communicate active state without changing semantic layout or
driving unbounded redraws. Only currently active states MAY animate. Reduced
motion, selected through a frontend-owned option or implied by `TERM=dumb`, SHALL
replace spinners and pulsing indicators with stable text while preserving every
state transition. Phase 7 SHALL use a 120 ms cadence for the active execution-rail
glyph and a 600 ms cadence for a focused editor caret; it MUST NOT add another
periodic decorative animation.

#### Scenario: Active work animates
- **WHEN** the app is thinking, running a tool, or cancelling and reduced motion is disabled
- **THEN** a bounded spinner or progress glyph advances at a controlled cadence
- **THEN** transcript content and chrome widths do not jitter between frames

#### Scenario: Work reaches a terminal state
- **WHEN** an animated activity becomes idle, succeeds, errors, blocks, or cancels
- **THEN** its animation stops and the final textual state remains visible

#### Scenario: Reduced motion is enabled
- **WHEN** the frontend reduced-motion option is active or `TERM=dumb`
- **THEN** no spinner, pulse, shimmer, or repeated decorative animation runs
- **THEN** stable text still distinguishes thinking, running, waiting, cancelling, and idle, and a focused editor retains a steady real caret

### Requirement: Color-independent and Unicode-safe presentation
All meaning SHALL be available through text, markers, shape, or placement rather
than color alone. A non-empty `NO_COLOR` environment value or a terminal without
color support SHALL disable semantic color. A frontend ASCII option,
or a terminal unable to render required glyphs SHALL replace Unicode borders,
spinners, ellipses, and status icons with width-stable ASCII fallbacks.

#### Scenario: Color is disabled
- **WHEN** `NO_COLOR` is non-empty or terminal color support is unavailable
- **THEN** tool errors, diff additions/removals, modes, warnings, and focus remain distinguishable without ANSI color

#### Scenario: ASCII fallback is enabled
- **WHEN** the frontend ASCII option is active or glyph support is insufficient
- **THEN** the app uses ASCII borders and symbols without losing labels or controls
- **THEN** layout remains within the same viewport bounds

#### Scenario: Wide or combining characters are rendered
- **WHEN** transcript or user content contains CJK, emoji, or combining characters
- **THEN** measurement and wrapping use terminal cell width and grapheme-safe boundaries
- **THEN** content does not overwrite adjacent chrome or panic the renderer

### Requirement: Terminal-control sanitization
The frontend SHALL sanitize every untrusted string from users, models, providers,
tools, hooks, file paths, capability providers, errors, and subagents before Markdown
parsing, width measurement, wrapping, or rendering. The sanitizer MUST neutralize
ANSI/CSI styling, OSC sequences including hyperlinks and clipboard operations,
DCS/APC/PM sequences, carriage-return overwrite behavior, and C0/C1 controls
other than intentional newline and tab. Only the frontend's own renderer MAY
emit terminal control sequences.

#### Scenario: Tool output attempts an OSC clipboard write
- **WHEN** a tool result contains OSC 52 or another OSC command
- **THEN** the sequence is removed or rendered inert before display
- **THEN** the terminal clipboard, title, and hyperlink state are not changed

#### Scenario: Model text contains ANSI cursor movement
- **WHEN** streamed model text contains CSI styling, erase, or cursor-movement bytes
- **THEN** those bytes cannot move the cursor outside the content region or alter later chrome
- **THEN** surrounding printable content remains readable

#### Scenario: Content contains carriage returns or C0 controls
- **WHEN** untrusted content contains carriage returns, backspaces, bells, or other disallowed controls
- **THEN** they are normalized, removed, or represented visibly without overwriting prior cells, ringing the terminal, or corrupting layout

#### Scenario: Escape sequence is split across stream chunks
- **WHEN** an ANSI, OSC, or other control sequence is divided across multiple deltas
- **THEN** the streaming sanitizer retains enough state to neutralize the complete sequence
- **THEN** no partial prefix reaches the terminal

### Requirement: Recoverable errors and authoritative terminal outcomes
The frontend SHALL turn non-startup provider, tool, hook, permission, sandbox,
protocol, and rendering errors into sanitized transcript notices or correlated card states when
the public contract permits continued use. Each run SHALL settle exactly once to
completed, failed, cancelled, or step-limit status, after which the composer
becomes usable again unless shutdown is in progress. Every authoritative
terminal outcome SHALL synchronously settle all still-active tool, subagent,
reasoning, permission, and activity views for that run without waiting for
optional item-level finish events. Cancelled terminals map active items to
cancelled; failed terminals map them to error; step-limit terminals map them to
step-limit/incomplete as applicable; a completed terminal with dangling active
items SHALL retain the authoritative completion but add one protocol-
inconsistency notice.

#### Scenario: Recoverable step error occurs
- **WHEN** an observed tool, hook, sandbox, or provider step error allows the session to continue
- **THEN** the error is shown at the correlated transcript position
- **THEN** subsequent events and future turns remain usable

#### Scenario: Run fails terminally
- **WHEN** the observed run emits a failed terminal outcome
- **THEN** one terminal notice is shown and all active visual states settle
- **THEN** the app returns to idle input without appending a second contradictory outcome

#### Scenario: Cancelled terminal arrives without item finish events
- **WHEN** `run_finished(cancelled)` arrives while a tool, subagent, reasoning disclosure, permission modal, or activity glyph is still active
- **THEN** every active view for that run settles locally as cancelled and all animation stops
- **THEN** the frontend does not wait for item-level events that are not required after the authoritative terminal

#### Scenario: Completed terminal has dangling active state
- **WHEN** `run_finished(completed)` arrives while any item for that run remains active
- **THEN** the authoritative run label remains completed, dangling views are settled as inconsistent, and one protocol notice identifies the contradiction
- **THEN** no spinner or modal remains indefinitely active

#### Scenario: Unknown v1 kind or invalid payload is received
- **WHEN** an envelope claims schema v1 but contains an unknown kind or invalid typed payload
- **THEN** the frontend enters a fatal local protocol state, requests cancellation, and keeps the event reader draining without applying further non-terminal payloads
- **THEN** it accepts a valid terminal only as the run's authoritative outcome, closes the session through the bounded shutdown path, restores the terminal, and exits nonzero with a safe protocol error

#### Scenario: Observed channel closes before terminal
- **WHEN** the observed channel reaches EOF without `run_finished`
- **THEN** the frontend synthesizes no authoritative terminal, marks a fatal local protocol failure, disables new runs, and performs bounded session close and terminal restoration
- **THEN** it does not remain visually running or wait for an event that cannot arrive

#### Scenario: Step limit is reached
- **WHEN** the run reaches its configured model-round limit
- **THEN** the UI labels the stop as a step limit rather than success, cancellation, or a crash

### Requirement: Cancellation and clean shutdown
With no permission modal or blocking overlay active, the frontend SHALL make
`Esc` or `Ctrl+C` cancel the run through its public context and return to idle only
after the authoritative cancelled outcome. `Ctrl+Q` SHALL request application
shutdown from any state. Shutdown MUST resolve any permission request with one
deny-once decision, cancel and drain active work for at most two seconds, then
close the public session with a separate two-second close context. The terminal
MUST be restored by the four-second overall deadline even if a caller-owned
provider, dispatcher, or tool violates its cancellation contract. Contract-
compliant providers, tools, commands, and subagents MUST not remain orphaned;
forced shutdown SHALL report a safe stderr warning and exit nonzero rather than
leave the terminal trapped.

#### Scenario: Escape cancels an in-flight run
- **WHEN** a run is active with no modal and the user presses `Esc`
- **THEN** cancellation is requested once and status changes to cancelling
- **THEN** the app returns to idle only after the run settles

#### Scenario: Control-C cancels before it quits
- **WHEN** a run is active and the user presses `Ctrl+C`
- **THEN** the first press cancels the run rather than immediately terminating the process
- **THEN** the app waits for the authoritative cancelled outcome before becoming idle

#### Scenario: Double Control-C quits while idle
- **WHEN** the app is idle and the user presses `Ctrl+C` twice within `1.5` seconds
- **THEN** the first press shows an armed-quit hint without exiting
- **THEN** the second press starts clean shutdown

#### Scenario: Armed idle quit expires
- **WHEN** the app is idle, one `Ctrl+C` has armed quit, and `1.5` seconds elapse without a second press
- **THEN** the armed hint clears and the next `Ctrl+C` is treated as a new first press

#### Scenario: Control-Q quits from idle
- **WHEN** the app is idle and the user presses `Ctrl+Q`
- **THEN** the session closes and the terminal is restored cleanly

#### Scenario: Control-Q quits with a permission pending
- **WHEN** a permission request is open and the user presses `Ctrl+Q`
- **THEN** exactly one deny-once decision is delivered before cancellation and shutdown
- **THEN** the permission reply path is not left waiting

#### Scenario: Control-Q quits during active work
- **WHEN** a contract-compliant run, tool, command, or subagent is active and the user presses `Ctrl+Q`
- **THEN** the app requests cancellation, drains for up to two seconds, and closes the session with a separate two-second context
- **THEN** no descendant or command remains orphaned

#### Scenario: Active work ignores cancellation
- **WHEN** a caller-owned provider, dispatcher, or tool does not settle within the two-second drain window
- **THEN** the app stops waiting, attempts session close for at most two additional seconds, restores the terminal, prints a forced-shutdown warning to stderr, and exits nonzero
- **THEN** it never presents the forced local exit as an authoritative run terminal event

#### Scenario: Session close reports an error
- **WHEN** public session close fails or exceeds its two-second context during shutdown
- **THEN** the frontend restores terminal mode before printing a safe close error to stderr

#### Scenario: Repeated cancel key arrives while cancelling
- **WHEN** cancellation is already in progress and the user presses `Esc` or `Ctrl+C` again
- **THEN** the app does not emit duplicate terminal outcomes or duplicate permission decisions
- **THEN** `Ctrl+Q` remains available for the clean shutdown path

### Requirement: Offline reducer, golden, and PTY verification
Frontend behavior SHALL be testable without a network or real model. A pure
state reducer SHALL consume scripted public descriptors, events, resize events,
ticks, and keys; render tests SHALL use deterministic time and styles; PTY tests
SHALL exercise the built binary. Tests MUST cover safety interactions, streaming
chunk boundaries, accessibility fallbacks, and the SDK import boundary.

#### Scenario: Reducer tests script a complete observed run
- **WHEN** offline tests feed text, reasoning, tool, preview, permission, usage, omission, hook, subagent, and terminal events
- **THEN** reducer state and emitted public decisions match the scripted order exactly
- **THEN** no network or real provider is used

#### Scenario: Protocol fixtures cannot wedge the reader
- **WHEN** fixtures inject an unknown v1 kind, invalid payload, or EOF before terminal
- **THEN** the reader continues safe drain when possible, reducer enters fatal protocol state, no new run starts, and bounded shutdown restores the terminal

#### Scenario: Golden layouts cover responsive classes
- **WHEN** deterministic render goldens run
- **THEN** they cover `60x20`, `80x24`, `120x36`, `160x48`, and at least one too-small size
- **THEN** they include streaming Markdown, permission, diff, omission, error, no-color, reduced-motion, and ASCII states

#### Scenario: Streaming and sanitizer boundaries are adversarial
- **WHEN** tests split Markdown delimiters and terminal escape sequences at every relevant chunk boundary
- **THEN** completed output is deterministic and no untrusted terminal control reaches the render output

#### Scenario: PTY smoke test drives critical keys
- **WHEN** the built app runs under an offline fake session in a pseudo-terminal
- **THEN** the test submits with `Enter`, inserts a newline with `Ctrl+J`, edits around a real caret, resizes, scrolls only by mouse wheel without moving composer focus, selects and copies pane-local text with an unmodified drag, proves `PageUp` does not scroll history, exposes the terminal-native copy fallback, changes mode, answers and edits a permission request, cancels, and quits
- **THEN** the terminal mode is restored on exit

#### Scenario: Import and race gates run
- **WHEN** frontend verification executes
- **THEN** an import audit rejects any `internal/*` dependency from `tui` or `cmd/coragent`
- **THEN** cancellation, resize, streaming, permission, and shutdown tests pass under the Go race detector

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

### Requirement: Slash suggestion dropdown includes skill entries

The composer's slash suggestion dropdown SHALL render skill entries with a distinct source badge (`[user]` or `[project]`) alongside their name and description. The rendering SHALL accommodate a larger suggestion list mixing built-in commands and skills while maintaining the existing 8-entry visible cap and prefix-matching behavior.

#### Scenario: Skill entry rendered with source badge
- **WHEN** the slash suggestion dropdown contains a skill named `code-review` from project source
- **THEN** the suggestion row shows the skill name and a `[project]` badge
- **THEN** the skill description is shown alongside the name and badge

#### Scenario: Mixed built-in and skill suggestions
- **WHEN** the user types `/` and both built-in commands and skills are loaded
- **THEN** built-in command rows render without source badges
- **THEN** skill rows render with their source badges
- **THEN** all rows are navigable with Up/Down and selectable with Tab/Enter

#### Scenario: Suggestion count exceeds visible cap
- **WHEN** more than 8 commands and skills match the current prefix
- **THEN** only the first 8 matches are rendered in the dropdown
- **THEN** the scrollable behavior is unchanged from the current implementation

