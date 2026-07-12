package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	MinimumTerminalWidth  = 60
	MinimumTerminalHeight = 20
	MaximumProseWidth     = 92
)

// LayoutClass is ordered from the fail-safe view to the widest supported view.
type LayoutClass uint8

const (
	LayoutTooSmall LayoutClass = iota
	LayoutMinimal
	LayoutCompact
	LayoutStandard
	LayoutWide
)

func (class LayoutClass) String() string {
	switch class {
	case LayoutMinimal:
		return "minimal"
	case LayoutCompact:
		return "compact"
	case LayoutStandard:
		return "standard"
	case LayoutWide:
		return "wide"
	default:
		return "too-small"
	}
}

// Layout contains the cell geometry and priority decisions for one resize.
// Semantic state lives elsewhere, so recomputing a Layout cannot lose drafts,
// selections, or transcript ordering.
type Layout struct {
	Class             LayoutClass
	Width             int
	Height            int
	HorizontalPadding int
	ContentWidth      int
	TranscriptRows    int
	ComposerMinRows   int
	ComposerMaxRows   int
	PermissionWidth   int
	ProseWidth        int
	ShowModel         bool
	ShowFullPath      bool
	ShowFullMetadata  bool
	TwoColumnDetail   bool
	MinimalBorders    bool
}

// ClassifyLayout maps terminal cells to the five approved responsive classes.
func ClassifyLayout(width, height int) LayoutClass {
	switch {
	case width < MinimumTerminalWidth || height < MinimumTerminalHeight:
		return LayoutTooSmall
	case width >= 160 && height >= 48:
		return LayoutWide
	case width >= 120 && height >= 36:
		return LayoutStandard
	case width >= 80 && height >= 24:
		return LayoutCompact
	default:
		return LayoutMinimal
	}
}

// LayoutForSize derives terminal-cell geometry for one resize event.
func LayoutForSize(width, height int) Layout {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}

	class := ClassifyLayout(width, height)
	layout := Layout{Class: class, Width: width, Height: height}
	if class == LayoutTooSmall {
		layout.ContentWidth = width
		layout.TranscriptRows = height
		return layout
	}

	padding := 1
	if width >= 80 {
		padding = 2
	}
	contentWidth := max(0, width-padding*2)

	layout.HorizontalPadding = padding
	layout.ContentWidth = contentWidth
	layout.ComposerMinRows = 3
	layout.ProseWidth = min(contentWidth, MaximumProseWidth)
	layout.PermissionWidth = contentWidth
	layout.MinimalBorders = class == LayoutMinimal

	switch class {
	case LayoutWide:
		layout.ComposerMaxRows = 6
		layout.PermissionWidth = min(contentWidth, 112)
		layout.ShowModel = true
		layout.ShowFullPath = true
		layout.ShowFullMetadata = true
		layout.TwoColumnDetail = true
	case LayoutStandard:
		layout.ComposerMaxRows = 6
		layout.PermissionWidth = min(contentWidth, 84)
		layout.ShowModel = true
		layout.ShowFullPath = true
		layout.ShowFullMetadata = true
	case LayoutCompact:
		layout.ComposerMaxRows = 4
		layout.ShowModel = true
	case LayoutMinimal:
		layout.ComposerMaxRows = 3
	}

	// One ledger header, one quiet footer, and the minimum composer stay fixed.
	// Help is an overlay, never a permanently reserved hint row.
	layout.TranscriptRows = max(1, height-2-layout.ComposerMinRows)
	return layout
}

// CellWidth sanitizes untrusted text before asking Lip Gloss for its Unicode
// grapheme and terminal-cell width.
func CellWidth(value string) int {
	return lipgloss.Width(SanitizeString(value))
}

// CompressPath compacts a path by complete segments with the filename kept at
// the right edge. It uses the normal Unicode ellipsis.
func CompressPath(path string, maxWidth int) string {
	return CompressPathForMode(path, maxWidth, TrueColorMode())
}

// CompressPathForMode uses a width-accounted ASCII marker when requested.
func CompressPathForMode(path string, maxWidth int, mode VisualMode) string {
	path = SanitizeString(path)
	if maxWidth <= 0 || path == "" {
		return ""
	}
	if CellWidth(path) <= maxWidth {
		return path
	}

	normalized := strings.ReplaceAll(path, "\\", "/")
	rawSegments := strings.Split(normalized, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return ""
	}

	marker := "…"
	if mode.ASCII {
		marker = "..."
	}
	filename := segments[len(segments)-1]
	best := ""
	for start := len(segments) - 1; start >= 0; start-- {
		candidate := marker + "/" + strings.Join(segments[start:], "/")
		if CellWidth(candidate) > maxWidth {
			break
		}
		best = candidate
	}
	if best != "" {
		return best
	}

	// If the marker itself would hide an otherwise complete filename, prefer
	// the whole filename. Only an intrinsically over-wide filename needs the
	// grapheme-safe last-resort truncation.
	if CellWidth(filename) <= maxWidth {
		return filename
	}
	markerWidth := CellWidth(marker)
	if maxWidth <= markerWidth {
		return ansi.Truncate(filename, maxWidth, "")
	}
	remove := CellWidth(filename) - (maxWidth - markerWidth)
	return ansi.TruncateLeft(filename, remove, marker)
}

// FitMetricLabel keeps a metric or safety label whole. Callers can drop the
// empty result according to chrome priority instead of displaying an ambiguous
// tail ellipsis.
func FitMetricLabel(label string, maxWidth int) string {
	label = SanitizeString(label)
	if maxWidth >= 0 && CellWidth(label) <= maxWidth {
		return label
	}
	return ""
}

// TruncateMetric is a compatibility name for whole-label metric fitting.
func TruncateMetric(label string, maxWidth int) string {
	return FitMetricLabel(label, maxWidth)
}

func padCells(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = ansi.Truncate(SanitizeString(value), width, "")
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}
