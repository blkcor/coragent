package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var textSelectionStyle = lipgloss.NewStyle().Reverse(true)

type cellPoint struct {
	X int
	Y int
}

// textSelection owns a pane-local snapshot of the cells Coragent rendered.
// tmux translates mouse reports into pane coordinates, and the snapshot is
// additionally clipped to the current layout, so adjacent panes can never
// become part of application-managed copy text.
type textSelection struct {
	anchor   cellPoint
	focus    cellPoint
	dragging bool
	moved    bool
	snapshot []string
}

func (selection *textSelection) begin(mouse tea.Mouse, content string, width, height int) bool {
	if mouse.Button != tea.MouseLeft || !pointInsidePane(mouse.X, mouse.Y, width, height) {
		return false
	}

	point := cellPoint{X: mouse.X, Y: mouse.Y}
	selection.anchor = point
	selection.focus = point
	selection.dragging = true
	selection.moved = false
	selection.snapshot = captureSelectionScreen(content, width, height)
	return true
}

func (selection *textSelection) update(mouse tea.Mouse, content string, width, height int) bool {
	if !selection.dragging || width <= 0 || height <= 0 {
		return false
	}

	point := cellPoint{
		X: min(max(mouse.X, 0), width-1),
		Y: min(max(mouse.Y, 0), height-1),
	}
	selection.focus = point
	selection.moved = selection.moved || point != selection.anchor
	selection.snapshot = captureSelectionScreen(content, width, height)
	return true
}

func (selection *textSelection) finish(mouse tea.Mouse, content string, width, height int) (string, bool) {
	if !selection.update(mouse, content, width, height) {
		return "", false
	}
	selection.dragging = false
	if !selection.moved {
		selection.clear()
		return "", false
	}

	text := selection.text()
	if text == "" {
		selection.clear()
		return "", false
	}
	return text, true
}

func (selection *textSelection) clear() {
	*selection = textSelection{}
}

func (selection textSelection) render(content string, width, height int) string {
	if !selection.moved || len(selection.snapshot) == 0 {
		return content
	}
	// Do not leave a highlight attached to different text if streaming output,
	// animation, or another state transition changes the screen after release.
	if current := captureSelectionScreen(content, width, height); !slices.Equal(current, selection.snapshot) {
		return content
	}

	start, end := selection.orderedBounds()
	lines := strings.Split(content, "\n")
	for row := start.Y; row <= end.Y && row < len(lines); row++ {
		left, right := selection.columnsForRow(row, start, end, width)
		lines[row] = highlightCellRange(lines[row], left, right)
	}
	return strings.Join(lines, "\n")
}

func (selection textSelection) text() string {
	start, end := selection.orderedBounds()
	if start.Y < 0 || end.Y >= len(selection.snapshot) {
		return ""
	}

	lines := make([]string, 0, end.Y-start.Y+1)
	for row := start.Y; row <= end.Y; row++ {
		left, right := selection.columnsForRow(row, start, end, ansi.StringWidth(selection.snapshot[row]))
		line := ansi.Cut(selection.snapshot[row], left, right)
		// Every normal screen row is padded to the pane width. Terminal copy
		// conventions omit that layout-only padding from clipboard text.
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return strings.Join(lines, "\n")
}

func (selection textSelection) orderedBounds() (cellPoint, cellPoint) {
	start, end := selection.anchor, selection.focus
	if end.Y < start.Y || (end.Y == start.Y && end.X < start.X) {
		start, end = end, start
	}
	return start, end
}

func (selection textSelection) columnsForRow(row int, start, end cellPoint, width int) (int, int) {
	left := 0
	right := max(0, width)
	if row == start.Y {
		left = min(max(0, start.X), right)
	}
	if row == end.Y {
		right = min(max(0, end.X+1), right)
	}
	if right < left {
		right = left
	}
	return left, right
}

func captureSelectionScreen(content string, width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	lines := strings.Split(ansi.Strip(content), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], width, "")
	}
	if len(lines) < height {
		lines = append(lines, make([]string, height-len(lines))...)
	}
	return lines
}

func highlightCellRange(line string, left, right int) string {
	width := ansi.StringWidth(line)
	left = min(max(0, left), width)
	right = min(max(left, right), width)
	if left == right {
		return line
	}

	prefix := ansi.Cut(line, 0, left)
	selected := ansi.Strip(ansi.Cut(line, left, right))
	suffix := ansi.Cut(line, right, width)
	return prefix + textSelectionStyle.Render(selected) + suffix
}

func pointInsidePane(x, y, width, height int) bool {
	return width > 0 && height > 0 && x >= 0 && x < width && y >= 0 && y < height
}
