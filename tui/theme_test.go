package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestQuietPalette(t *testing.T) {
	t.Parallel()

	got := QuietPalette()
	want := Palette{
		Canvas:               "#090C0A",
		Surface:              "#121711",
		Elevated:             "#192018",
		Border:               "#2C352A",
		Text:                 "#E8EBDD",
		Muted:                "#8C9484",
		Accent:               "#C8D45A",
		Success:              "#82B67E",
		Warning:              "#D6AA58",
		Danger:               "#E0766E",
		Info:                 "#91AAA0",
		DiffAddBackground:    "#16291B",
		DiffRemoveBackground: "#301A19",
	}
	if got != want {
		t.Fatalf("QuietPalette() = %#v, want %#v", got, want)
	}
}

func TestRunLedgerToolReceiptIsSingleLineUntilExpanded(t *testing.T) {
	t.Parallel()

	block := TranscriptBlock{
		Kind:       BlockTool,
		ToolName:   "read_file",
		Arguments:  `{"path":"tui/app.go"}`,
		Result:     "a long successful result that belongs in the inspector",
		ToolState:  ToolDone,
		Duration:   42,
		ledgerTask: 1,
		ledgerStep: 3,
	}
	lines := renderTranscriptBlock(ThemeForMode(NoColorMode()), block, 80, 0)
	if len(lines) != 1 {
		t.Fatalf("collapsed success receipt rendered %d lines, want 1: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "STEP 03") || !strings.Contains(lines[0], "succeeded") {
		t.Fatalf("receipt lost its stable ordinal or outcome: %q", lines[0])
	}

	block.Expanded = true
	if expanded := renderTranscriptBlock(ThemeForMode(NoColorMode()), block, 80, 0); len(expanded) < 2 {
		t.Fatalf("expanded receipt did not expose its result: %q", expanded)
	}
}

func TestUnavailablePreviewStaysOutOfRunLedger(t *testing.T) {
	t.Parallel()

	preview := ActionPreview{Kind: "unavailable", UnavailableReason: "handler has no preparation interface"}
	block := TranscriptBlock{
		Kind:       BlockTool,
		ToolName:   "custom_tool",
		ToolState:  ToolRunning,
		Preview:    &preview,
		ledgerTask: 1,
		ledgerStep: 1,
	}
	main := strings.Join(renderTranscriptBlock(ThemeForMode(NoColorMode()), block, 80, 0), "\n")
	if strings.Contains(strings.ToLower(main), "unavailable") || strings.Contains(main, preview.UnavailableReason) {
		t.Fatalf("run ledger leaked preview plumbing: %q", main)
	}
	inspector := strings.Join(inspectPreviewLines(&preview, 1), "\n")
	if !strings.Contains(inspector, preview.UnavailableReason) {
		t.Fatalf("inspector lost preview diagnosis: %q", inspector)
	}
}

func TestRunLedgerOrdinalsStayScopedToTask(t *testing.T) {
	t.Parallel()

	store := NewTranscriptStore()
	store.Blocks = []TranscriptBlock{
		{ID: "user-1", RunID: "run-1", Kind: BlockUser, Text: "first task"},
		{ID: "tool-1", RunID: "run-1", Kind: BlockTool, ToolName: "read_file", ToolState: ToolDone},
		{ID: "tool-2", RunID: "run-1", Kind: BlockTool, ToolName: "search_content", ToolState: ToolDone},
		{ID: "user-2", RunID: "run-2", Kind: BlockUser, Text: "second task"},
		{ID: "tool-3", RunID: "run-2", Kind: BlockTool, ToolName: "edit_file", ToolState: ToolDone},
	}
	view := strings.Join(store.RenderLines(ThemeForMode(NoColorMode()), 80, 0), "\n")
	for _, want := range []string{"first task", "STEP 01  ", "STEP 02  ", "second task"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ledger lost stable ordinal %q:\n%s", want, view)
		}
	}
	if got := store.CurrentTaskNumber(); got != 2 {
		t.Fatalf("current task number = %d, want 2", got)
	}
}

func TestResolveVisualMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options VisualOptions
		want    VisualMode
	}{
		{
			name:    "default truecolor",
			options: VisualOptions{},
			want:    VisualMode{Color: ColorTrueColor},
		},
		{
			name:    "ansi 256 stays selected",
			options: VisualOptions{Color: ColorANSI256},
			want:    VisualMode{Color: ColorANSI256},
		},
		{
			name:    "every non-empty no color value wins",
			options: VisualOptions{Color: ColorTrueColor, NoColor: "false"},
			want:    VisualMode{Color: ColorNoColor},
		},
		{
			name:    "ascii is orthogonal to color",
			options: VisualOptions{Color: ColorANSI256, ASCII: true},
			want:    VisualMode{Color: ColorANSI256, ASCII: true},
		},
		{
			name:    "dumb terminal is static ascii no color",
			options: VisualOptions{Term: "DUMB"},
			want:    ASCIIMode(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveVisualMode(test.options); got != test.want {
				t.Fatalf("ResolveVisualMode(%#v) = %#v, want %#v", test.options, got, test.want)
			}
		})
	}
}

func TestThemeDoesNotReadEnvironmentImplicitly(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	theme := DefaultTheme()
	if theme.Mode.Color != ColorTrueColor {
		t.Fatalf("DefaultTheme color = %v, want explicit truecolor", theme.Mode.Color)
	}
	if rendered := theme.AccentStyle.Render("focus"); !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("truecolor accent did not render a terminal style: %q", rendered)
	}
}

func TestNoColorThemeEmitsNoANSI(t *testing.T) {
	t.Parallel()

	theme := ThemeForMode(NoColorMode())
	styles := []lipgloss.Style{
		theme.CanvasStyle,
		theme.SurfaceStyle,
		theme.ElevatedStyle,
		theme.BorderStyle,
		theme.TextStyle,
		theme.MutedStyle,
		theme.AccentStyle,
		theme.SuccessStyle,
		theme.WarningStyle,
		theme.DangerStyle,
		theme.InfoStyle,
		theme.DiffAddStyle,
		theme.DiffRemoveStyle,
		theme.StrongStyle,
	}
	for index, style := range styles {
		if rendered := style.Render("state"); strings.Contains(rendered, "\x1b") {
			t.Fatalf("style %d emitted ANSI in no-color mode: %q", index, rendered)
		}
	}
}

func TestRailVisualsHaveStableGeometry(t *testing.T) {
	t.Parallel()

	modes := []VisualMode{TrueColorMode(), {Color: ColorANSI256}, NoColorMode(), ASCIIMode()}
	states := []RailState{
		RailProposed,
		RailActive,
		RailPermission,
		RailSuccess,
		RailWarning,
		RailError,
		RailCancelled,
		RailHookBlocked,
	}
	wantWidth := 1 + 1 + RailLabelWidth

	for _, mode := range modes {
		theme := ThemeForMode(mode)
		for _, state := range states {
			visual := theme.RailVisual(state, 3)
			if visual.Label == "" {
				t.Fatalf("mode %#v state %d has no text label", mode, state)
			}
			if width := lipgloss.Width(visual.Glyph); width != 1 {
				t.Fatalf("mode %#v state %d glyph %q width = %d, want 1", mode, state, visual.Glyph, width)
			}
			if width := lipgloss.Width(visual.Render()); width != wantWidth {
				t.Fatalf("mode %#v state %d rail width = %d, want %d", mode, state, width, wantWidth)
			}
		}
	}
}

func TestActiveRailFramesNeverChangeWidth(t *testing.T) {
	t.Parallel()

	for _, mode := range []VisualMode{TrueColorMode(), ASCIIMode()} {
		theme := ThemeForMode(mode)
		for frame := 0; frame < 12; frame++ {
			if width := lipgloss.Width(theme.RailVisual(RailActive, frame).Render()); width != RailLabelWidth+2 {
				t.Fatalf("mode %#v frame %d changed rail width to %d", mode, frame, width)
			}
		}
	}
}

func TestRenderedToolNarrativeKeepsStateVisibleWithoutFixedStatusColumn(t *testing.T) {
	theme := DefaultTheme()
	states := []ToolBlockState{
		ToolPreparing,
		ToolRunning,
		ToolAwaitingPermission,
		ToolDone,
		ToolError,
		ToolWasCancelled,
		ToolWasHookBlocked,
	}
	for _, state := range states {
		lines := renderTranscriptBlock(theme, TranscriptBlock{
			Kind:      BlockTool,
			ToolName:  "run_command",
			Arguments: `{"command":"go test ./..."}`,
			ToolState: state,
		}, 80, 1)
		if len(lines) == 0 {
			t.Fatalf("state %v rendered no lines", state)
		}
		if got := ansi.StringWidth(lines[0]); got >= 80 {
			t.Fatalf("state %v still renders a padded telemetry row of %d cells", state, got)
		}
		if !strings.Contains(lines[0], theme.RailVisual(toolStateRail(state), 1).Label) {
			t.Fatalf("state %v lost its textual label: %q", state, lines[0])
		}
	}
}

func toolStateRail(state ToolBlockState) RailState {
	switch state {
	case ToolRunning:
		return RailActive
	case ToolAwaitingPermission:
		return RailPermission
	case ToolDone:
		return RailSuccess
	case ToolError:
		return RailError
	case ToolWasCancelled:
		return RailCancelled
	case ToolWasHookBlocked:
		return RailHookBlocked
	default:
		return RailProposed
	}
}
