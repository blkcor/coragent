package tui

import tea "charm.land/bubbletea/v2"

const transcriptMouseWheelDelta = 3

func (model *AppModel) currentTranscriptRows() int {
	if model == nil || model.layout.Class == LayoutTooSmall {
		return max(1, model.layout.Height)
	}
	composerRows := model.composerHeight(model.layout.ContentWidth)
	if model.permission != nil || model.overlay != nil {
		composerRows = 0
	}
	fixedRows := 3 + composerRows
	if model.permission == nil && model.overlay == nil {
		fixedRows++
	}
	return max(1, model.layout.Height-fixedRows)
}

func transcriptRenderWidth(width int) int {
	return max(1, width-1)
}

func (model *AppModel) renderedTranscriptRows(width int) []RenderedTranscriptLine {
	return model.transcript.RenderRows(model.theme, transcriptRenderWidth(width), model.frame)
}

func (model *AppModel) resolvedScrollTop(lines []RenderedTranscriptLine, visibleRows int) int {
	maxTop := max(0, len(lines)-max(1, visibleRows))
	if model.scroll.Mode == ScrollPinnedBottom {
		return maxTop
	}
	top := min(max(0, model.scroll.Top), maxTop)
	if model.scroll.AnchorBlockID == "" {
		return top
	}

	best := -1
	bestDistance := int(^uint(0) >> 1)
	for index, line := range lines {
		if line.BlockID != model.scroll.AnchorBlockID {
			continue
		}
		if line.Line == model.scroll.AnchorLine {
			return min(max(0, index-model.scroll.AnchorScreenRow), maxTop)
		}
		distance := line.Line - model.scroll.AnchorLine
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			best = index
			bestDistance = distance
		}
	}
	if best >= 0 {
		return min(max(0, best-model.scroll.AnchorScreenRow), maxTop)
	}
	return top
}

func (model *AppModel) browseAt(lines []RenderedTranscriptLine, visibleRows, top int) {
	visibleRows = max(1, visibleRows)
	maxTop := max(0, len(lines)-visibleRows)
	if maxTop == 0 {
		model.pinTranscriptBottom()
		return
	}
	top = min(max(0, top), maxTop)
	model.scroll.Mode = ScrollBrowsingHistory
	model.scroll.Top = top
	model.scroll.AnchorBlockID = ""
	model.scroll.AnchorLine = 0
	model.scroll.AnchorScreenRow = 0
	if len(lines) > 0 && top < len(lines) {
		model.scroll.AnchorBlockID = lines[top].BlockID
		model.scroll.AnchorLine = lines[top].Line
	}
}

func (model *AppModel) pinTranscriptBottom() {
	model.scroll = ScrollState{Mode: ScrollPinnedBottom}
}

func (model *AppModel) scrollTranscriptBy(delta int) bool {
	before := model.scroll
	rows := model.currentTranscriptRows()
	lines := model.renderedTranscriptRows(model.layout.ContentWidth)
	maxTop := max(0, len(lines)-rows)
	if maxTop == 0 {
		model.pinTranscriptBottom()
		return model.scroll != before
	}
	top := model.resolvedScrollTop(lines, rows)
	next := min(max(0, top+delta), maxTop)
	if delta > 0 && next >= maxTop {
		model.pinTranscriptBottom()
		return model.scroll != before
	}
	model.browseAt(lines, rows, next)
	return model.scroll != before
}

func (model *AppModel) reconcileTranscriptScroll() {
	if model == nil || model.layout.Class == LayoutTooSmall {
		return
	}
	rows := model.currentTranscriptRows()
	lines := model.renderedTranscriptRows(model.layout.ContentWidth)
	if len(lines) > rows {
		return
	}
	model.pinTranscriptBottom()
}

func (model *AppModel) handleMouseWheel(message tea.MouseWheelMsg) tea.Cmd {
	if model.permission != nil {
		if model.permission.View == permissionDecision {
			switch message.Mouse().Button {
			case tea.MouseWheelUp:
				model.permission.Scroll = max(0, model.permission.Scroll-transcriptMouseWheelDelta)
			case tea.MouseWheelDown:
				model.permission.Scroll += transcriptMouseWheelDelta
			}
		}
		return nil
	}
	if model.overlay != nil {
		entries := model.overlayEntries()
		if len(entries) == 0 {
			return nil
		}
		switch message.Mouse().Button {
		case tea.MouseWheelUp:
			model.overlay.Cursor = max(0, model.overlay.Cursor-transcriptMouseWheelDelta)
		case tea.MouseWheelDown:
			model.overlay.Cursor = min(len(entries)-1, model.overlay.Cursor+transcriptMouseWheelDelta)
		}
		return nil
	}
	if model.layout.Class == LayoutTooSmall {
		return nil
	}
	mouse := message.Mouse()
	// Wheel history browsing is global within the app and remains orthogonal to
	// composer focus, so the draft and real caret stay available while browsing.
	lines := model.renderedTranscriptRows(model.layout.ContentWidth)
	if len(lines) <= model.currentTranscriptRows() {
		model.reconcileTranscriptScroll()
		return nil
	}

	changed := false
	switch mouse.Button {
	case tea.MouseWheelUp:
		changed = model.scrollTranscriptBy(-transcriptMouseWheelDelta)
	case tea.MouseWheelDown:
		changed = model.scrollTranscriptBy(transcriptMouseWheelDelta)
	default:
		return nil
	}
	if !changed {
		return nil
	}
	return model.syncComposerFocus()
}

func renderTranscriptScrollbar(theme Theme, totalRows, visibleRows, top int) []string {
	visibleRows = max(0, visibleRows)
	result := make([]string, visibleRows)
	if visibleRows == 0 || totalRows <= visibleRows {
		return result
	}

	track := "│"
	thumb := "┃"
	if theme.Mode.ASCII {
		track = "|"
		thumb = "#"
	}
	thumbRows := max(1, visibleRows*visibleRows/totalRows)
	thumbRows = min(visibleRows, thumbRows)
	maxTop := max(1, totalRows-visibleRows)
	maxThumbTop := max(0, visibleRows-thumbRows)
	thumbTop := top * maxThumbTop / maxTop

	for row := range visibleRows {
		glyph := theme.MutedStyle.Render(track)
		if row >= thumbTop && row < thumbTop+thumbRows {
			glyph = theme.AccentStyle.Render(thumb)
		}
		result[row] = glyph
	}
	return result
}
