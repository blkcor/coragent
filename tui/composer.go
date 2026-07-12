package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
)

const composerMaxContentRows = 10_000

// composerModel wraps the Charm textarea so the widget, rather than an
// append-only string, owns draft content, cursor position, wrapping, and its
// internal viewport.
type composerModel struct {
	textarea textarea.Model
}

func newComposerModel(theme Theme, layout Layout) composerModel {
	area := textarea.New()
	area.CharLimit = 0
	area.DynamicHeight = true
	area.MinHeight = 1
	area.MaxContentHeight = composerMaxContentRows
	area.MaxWidth = 0
	area.ShowLineNumbers = false
	area.EndOfBufferCharacter = ' '
	area.SetVirtualCursor(false)
	// The upstream transpose binding swaps individual runes and can split an
	// extended grapheme (for example an emoji joined with ZWJ). Keep the
	// operation disabled until the textarea exposes grapheme-level mutation.
	area.KeyMap.TransposeCharacterBackward.SetEnabled(false)
	// Bubble Tea already delivers terminal bracketed paste as tea.PasteMsg,
	// which AppModel sanitizes and inserts directly. Disable the textarea's
	// Ctrl+V helper because upstream resolves pbpaste/wl-copy/xclip through PATH;
	// ordinary editor input must never execute a caller-controlled binary.
	area.KeyMap.Paste.SetEnabled(false)

	composer := composerModel{textarea: area}
	composer.Configure(theme, layout)
	composer.Blur()
	return composer
}

func (composer *composerModel) Configure(theme Theme, layout Layout) {
	if composer == nil {
		return
	}
	maxRows := max(1, layout.ComposerMaxRows-2)
	composer.textarea.MinHeight = 1
	composer.textarea.MaxHeight = maxRows
	composer.textarea.MaxContentHeight = composerMaxContentRows
	composer.textarea.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.Focused && info.LineNumber == 0 {
			return theme.Glyphs.Focus + " "
		}
		return "  "
	})

	styles := composer.textarea.Styles()
	styles.Focused.Base = theme.CanvasStyle
	styles.Focused.CursorLine = theme.TextStyle
	styles.Focused.CursorLineNumber = theme.MutedStyle
	styles.Focused.EndOfBuffer = theme.MutedStyle
	styles.Focused.LineNumber = theme.MutedStyle
	styles.Focused.Placeholder = theme.MutedStyle
	styles.Focused.Prompt = theme.AccentStyle
	styles.Focused.Text = theme.TextStyle
	styles.Blurred.Base = theme.CanvasStyle
	styles.Blurred.CursorLine = theme.MutedStyle
	styles.Blurred.CursorLineNumber = theme.MutedStyle
	styles.Blurred.EndOfBuffer = theme.MutedStyle
	styles.Blurred.LineNumber = theme.MutedStyle
	styles.Blurred.Placeholder = theme.MutedStyle
	styles.Blurred.Prompt = theme.MutedStyle
	styles.Blurred.Text = theme.MutedStyle
	styles.Cursor.Color = theme.AccentStyle.GetForeground()
	styles.Cursor.Shape = tea.CursorBlock
	styles.Cursor.Blink = !theme.Mode.ReducedMotion
	styles.Cursor.BlinkSpeed = 600 * time.Millisecond
	composer.textarea.SetStyles(styles)
	composer.textarea.SetWidth(max(1, layout.ContentWidth))
}

func (composer *composerModel) Focus() tea.Cmd {
	if composer == nil {
		return nil
	}
	return composer.textarea.Focus()
}

func (composer *composerModel) Blur() {
	if composer != nil {
		composer.textarea.Blur()
	}
}

func (composer *composerModel) Focused() bool {
	return composer != nil && composer.textarea.Focused()
}

func (composer *composerModel) Value() string {
	if composer == nil {
		return ""
	}
	return composer.textarea.Value()
}

func (composer *composerModel) SetValue(value string) {
	if composer != nil {
		composer.textarea.SetValue(value)
	}
}

func (composer *composerModel) InsertString(value string) {
	if composer != nil {
		composer.textarea.InsertString(value)
	}
}

func (composer *composerModel) Reset() {
	if composer != nil {
		composer.textarea.Reset()
	}
}

func (composer *composerModel) Update(message tea.Msg) tea.Cmd {
	if composer == nil {
		return nil
	}
	key, isKey := message.(tea.KeyPressMsg)
	if !isKey {
		return composer.updateTextarea(message)
	}

	switch key.String() {
	case "left", "ctrl+b":
		return composer.updateAndSnap(key, snapLeft)
	case "right", "ctrl+f":
		return composer.updateAndSnap(key, snapRight)
	case "backspace", "ctrl+h":
		return composer.deleteGrapheme(key, true)
	case "delete", "ctrl+d":
		return composer.deleteGrapheme(key, false)
	default:
		return composer.updateAndSnap(key, snapNearest)
	}
}

type cursorSnap uint8

const (
	snapNearest cursorSnap = iota
	snapLeft
	snapRight
)

func (composer *composerModel) updateTextarea(message tea.Msg) tea.Cmd {
	updated, command := composer.textarea.Update(message)
	composer.textarea = updated
	return command
}

func (composer *composerModel) updateAndSnap(message tea.Msg, direction cursorSnap) tea.Cmd {
	command := composer.updateTextarea(message)
	if !composer.snapCursorToGrapheme(direction) {
		return command
	}
	// SetCursorColumn intentionally does not update the textarea viewport.
	// A message-free update refreshes wrapping and keeps the real terminal
	// cursor inside the visible textarea after the corrected movement.
	return tea.Batch(command, composer.updateTextarea(nil))
}

func (composer *composerModel) deleteGrapheme(message tea.KeyPressMsg, backward bool) tea.Cmd {
	line, column := composer.cursorLine()
	boundaries := graphemeRuneBoundaries(line)
	if len(boundaries) < 2 {
		return composer.updateAndSnap(message, snapNearest)
	}

	// Every cursor transition is normally snapped already. If an embedding
	// caller moved it directly, first move to the outer boundary so deletion
	// still cannot leave half of a grapheme behind.
	direction := snapRight
	if !backward {
		direction = snapLeft
	}
	if snapRuneColumn(boundaries, column, direction) != column {
		composer.textarea.SetCursorColumn(snapRuneColumn(boundaries, column, direction))
		_, column = composer.cursorLine()
	}

	count := 1 // At a line edge, preserve the textarea's newline merge behavior.
	if backward && column > 0 {
		count = column - previousRuneBoundary(boundaries, column)
	} else if !backward && column < len(line) {
		count = nextRuneBoundary(boundaries, column) - column
	}
	count = max(1, count)

	commands := make([]tea.Cmd, 0, count+1)
	for range count {
		commands = append(commands, composer.updateTextarea(message))
	}
	if composer.snapCursorToGrapheme(snapNearest) {
		commands = append(commands, composer.updateTextarea(nil))
	}
	return tea.Batch(commands...)
}

func (composer *composerModel) cursorLine() ([]rune, int) {
	lines := strings.Split(composer.textarea.Value(), "\n")
	row := min(max(0, composer.textarea.Line()), max(0, len(lines)-1))
	line := []rune(lines[row])
	return line, min(max(0, composer.textarea.Column()), len(line))
}

func (composer *composerModel) snapCursorToGrapheme(direction cursorSnap) bool {
	line, column := composer.cursorLine()
	boundaries := graphemeRuneBoundaries(line)
	target := snapRuneColumn(boundaries, column, direction)
	if target == column {
		return false
	}
	composer.textarea.SetCursorColumn(target)
	return true
}

func graphemeRuneBoundaries(line []rune) []int {
	boundaries := []int{0}
	iterator := uniseg.NewGraphemes(string(line))
	offset := 0
	for iterator.Next() {
		offset += len(iterator.Runes())
		boundaries = append(boundaries, offset)
	}
	return boundaries
}

func snapRuneColumn(boundaries []int, column int, direction cursorSnap) int {
	if len(boundaries) == 0 {
		return 0
	}
	column = min(max(0, column), boundaries[len(boundaries)-1])
	previous := 0
	for _, boundary := range boundaries {
		if boundary == column {
			return column
		}
		if boundary > column {
			switch direction {
			case snapLeft:
				return previous
			case snapRight:
				return boundary
			default:
				if column-previous < boundary-column {
					return previous
				}
				return boundary
			}
		}
		previous = boundary
	}
	return boundaries[len(boundaries)-1]
}

func previousRuneBoundary(boundaries []int, column int) int {
	previous := 0
	for _, boundary := range boundaries {
		if boundary >= column {
			return previous
		}
		previous = boundary
	}
	return previous
}

func nextRuneBoundary(boundaries []int, column int) int {
	for _, boundary := range boundaries {
		if boundary > column {
			return boundary
		}
	}
	if len(boundaries) == 0 {
		return column
	}
	return boundaries[len(boundaries)-1]
}

func (composer *composerModel) Height() int {
	if composer == nil {
		return 1
	}
	return max(1, composer.textarea.Height())
}

func (composer *composerModel) Cursor() *tea.Cursor {
	if composer == nil {
		return nil
	}
	return composer.textarea.Cursor()
}

func (composer *composerModel) Lines() []string {
	if composer == nil {
		return nil
	}
	view := strings.TrimSuffix(composer.textarea.View(), "\n")
	if view == "" {
		return []string{""}
	}
	return strings.Split(view, "\n")
}

func (composer *composerModel) SetPlaceholder(value string) {
	if composer != nil {
		composer.textarea.Placeholder = value
	}
}

func (composer *composerModel) SetCharLimit(limit int) {
	if composer != nil {
		composer.textarea.CharLimit = max(0, limit)
	}
}
