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
		Canvas:               "#0C0D0C",
		Surface:              "#131512",
		Elevated:             "#1A1D19",
		Border:               "#30352E",
		Text:                 "#E7E9E3",
		Muted:                "#90988B",
		Accent:               "#D97757",
		Success:              "#7EBC77",
		Warning:              "#D6A95F",
		Danger:               "#E17A72",
		Info:                 "#8FA9A1",
		DiffAddBackground:    "#16301F",
		DiffRemoveBackground: "#351C1C",
	}
	if got != want {
		t.Fatalf("QuietPalette() = %#v, want %#v", got, want)
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
