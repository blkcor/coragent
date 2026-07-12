package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// ColorMode describes the semantic color capability selected by the caller.
// Theme construction never reads the process environment implicitly.
type ColorMode uint8

const (
	ColorTrueColor ColorMode = iota
	ColorANSI256
	ColorNoColor
)

// VisualMode contains the orthogonal terminal presentation fallbacks. ASCII
// glyphs and reduced motion can be selected independently of color depth.
type VisualMode struct {
	Color         ColorMode
	ASCII         bool
	ReducedMotion bool
}

// VisualOptions is an explicit input to ResolveVisualMode. NoColor follows the
// NO_COLOR contract: every non-empty value disables semantic color.
type VisualOptions struct {
	Color         ColorMode
	NoColor       string
	Term          string
	ASCII         bool
	ReducedMotion bool
}

// ResolveVisualMode turns caller-supplied terminal facts into a visual mode.
// TERM=dumb selects the static, ASCII, no-color fallback.
func ResolveVisualMode(options VisualOptions) VisualMode {
	colorMode := options.Color
	if colorMode > ColorNoColor {
		colorMode = ColorTrueColor
	}

	dumb := strings.EqualFold(strings.TrimSpace(options.Term), "dumb")
	if options.NoColor != "" || dumb {
		colorMode = ColorNoColor
	}

	return VisualMode{
		Color:         colorMode,
		ASCII:         options.ASCII || dumb,
		ReducedMotion: options.ReducedMotion || dumb,
	}
}

// TrueColorMode returns the default Terminal Narrative visual mode.
func TrueColorMode() VisualMode {
	return VisualMode{Color: ColorTrueColor}
}

// NoColorMode returns a mode with semantic colors disabled.
func NoColorMode() VisualMode {
	return VisualMode{Color: ColorNoColor}
}

// ASCIIMode returns the most conservative static visual mode.
func ASCIIMode() VisualMode {
	return VisualMode{Color: ColorNoColor, ASCII: true, ReducedMotion: true}
}

// Palette records the versioned Terminal Narrative semantic color roles.
// Hex values remain available to renderers and golden tests even in no-color
// mode, while Theme styles decide whether to emit them.
type Palette struct {
	Canvas               string
	Surface              string
	Elevated             string
	Border               string
	Text                 string
	Muted                string
	Accent               string
	Success              string
	Warning              string
	Danger               string
	Info                 string
	DiffAddBackground    string
	DiffRemoveBackground string
}

// QuietPalette returns a fresh copy of the approved Phase 7 palette. The name
// remains stable while the layout direction evolves independently of colors.
func QuietPalette() Palette {
	return Palette{
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
}

// GlyphSet contains every geometry-bearing symbol used by the visual
// foundation. ActiveFrames are all one cell wide, so animation cannot move
// adjacent content.
type GlyphSet struct {
	Proposed     string
	ActiveFrames [4]string
	Permission   string
	Success      string
	Warning      string
	Error        string
	Cancelled    string
	HookBlocked  string
	Cursor       string
	Ellipsis     string
	Focus        string
}

// Theme is the single visual-token package for the TUI. Styles intentionally
// use the host terminal font and terminal-cell geometry.
type Theme struct {
	Mode    VisualMode
	Palette Palette
	Glyphs  GlyphSet
	Border  lipgloss.Border

	CanvasStyle     lipgloss.Style
	SurfaceStyle    lipgloss.Style
	ElevatedStyle   lipgloss.Style
	BorderStyle     lipgloss.Style
	TextStyle       lipgloss.Style
	MutedStyle      lipgloss.Style
	AccentStyle     lipgloss.Style
	SuccessStyle    lipgloss.Style
	WarningStyle    lipgloss.Style
	DangerStyle     lipgloss.Style
	InfoStyle       lipgloss.Style
	DiffAddStyle    lipgloss.Style
	DiffRemoveStyle lipgloss.Style
	StrongStyle     lipgloss.Style
}

// DefaultTheme returns the approved truecolor theme.
func DefaultTheme() Theme {
	return ThemeForMode(TrueColorMode())
}

// ThemeForMode builds a theme solely from the explicit visual mode.
func ThemeForMode(mode VisualMode) Theme {
	if mode.Color > ColorNoColor {
		mode.Color = ColorTrueColor
	}

	palette := QuietPalette()
	glyphs := unicodeGlyphs()
	border := lipgloss.NormalBorder()
	if mode.ASCII {
		glyphs = asciiGlyphs()
		border = lipgloss.ASCIIBorder()
	}

	theme := Theme{
		Mode:    mode,
		Palette: palette,
		Glyphs:  glyphs,
		Border:  border,
	}

	if mode.Color == ColorNoColor {
		plain := lipgloss.NewStyle()
		theme.CanvasStyle = plain
		theme.SurfaceStyle = plain
		theme.ElevatedStyle = plain
		theme.BorderStyle = plain
		theme.TextStyle = plain
		theme.MutedStyle = plain
		theme.AccentStyle = plain
		theme.SuccessStyle = plain
		theme.WarningStyle = plain
		theme.DangerStyle = plain
		theme.InfoStyle = plain
		theme.DiffAddStyle = plain
		theme.DiffRemoveStyle = plain
		theme.StrongStyle = plain
		return theme
	}

	canvas := semanticColor(mode.Color, palette.Canvas, 232)
	surface := semanticColor(mode.Color, palette.Surface, 233)
	elevated := semanticColor(mode.Color, palette.Elevated, 234)
	borderColor := semanticColor(mode.Color, palette.Border, 237)
	textColor := semanticColor(mode.Color, palette.Text, 254)
	muted := semanticColor(mode.Color, palette.Muted, 102)
	accent := semanticColor(mode.Color, palette.Accent, 173)
	success := semanticColor(mode.Color, palette.Success, 114)
	warning := semanticColor(mode.Color, palette.Warning, 179)
	danger := semanticColor(mode.Color, palette.Danger, 174)
	info := semanticColor(mode.Color, palette.Info, 109)
	diffAdd := semanticColor(mode.Color, palette.DiffAddBackground, 22)
	diffRemove := semanticColor(mode.Color, palette.DiffRemoveBackground, 52)

	theme.CanvasStyle = lipgloss.NewStyle().Background(canvas).Foreground(textColor)
	theme.SurfaceStyle = lipgloss.NewStyle().Background(surface).Foreground(textColor)
	theme.ElevatedStyle = lipgloss.NewStyle().Background(elevated).Foreground(textColor)
	theme.BorderStyle = lipgloss.NewStyle().Foreground(borderColor)
	theme.TextStyle = lipgloss.NewStyle().Foreground(textColor)
	theme.MutedStyle = lipgloss.NewStyle().Foreground(muted)
	theme.AccentStyle = lipgloss.NewStyle().Foreground(accent)
	theme.SuccessStyle = lipgloss.NewStyle().Foreground(success)
	theme.WarningStyle = lipgloss.NewStyle().Foreground(warning)
	theme.DangerStyle = lipgloss.NewStyle().Foreground(danger)
	theme.InfoStyle = lipgloss.NewStyle().Foreground(info)
	theme.DiffAddStyle = lipgloss.NewStyle().Foreground(success).Background(diffAdd)
	theme.DiffRemoveStyle = lipgloss.NewStyle().Foreground(danger).Background(diffRemove)
	theme.StrongStyle = lipgloss.NewStyle().Foreground(textColor).Bold(true)

	return theme
}

func semanticColor(mode ColorMode, hex string, ansi256 uint8) color.Color {
	if mode == ColorANSI256 {
		return lipgloss.ANSIColor(ansi256)
	}
	return lipgloss.Color(hex)
}

func unicodeGlyphs() GlyphSet {
	return GlyphSet{
		Proposed:     "○",
		ActiveFrames: [4]string{"◐", "◓", "◑", "◒"},
		Permission:   "◆",
		Success:      "●",
		Warning:      "▲",
		Error:        "×",
		Cancelled:    "∅",
		HookBlocked:  "■",
		Cursor:       "▍",
		Ellipsis:     "…",
		Focus:        "›",
	}
}

func asciiGlyphs() GlyphSet {
	return GlyphSet{
		Proposed:     "o",
		ActiveFrames: [4]string{">", ">", ">", ">"},
		Permission:   "?",
		Success:      "+",
		Warning:      "!",
		Error:        "x",
		Cancelled:    "-",
		HookBlocked:  "#",
		Cursor:       "|",
		Ellipsis:     "...",
		Focus:        ">",
	}
}

func visualSeparator(theme Theme) string {
	if theme.Mode.ASCII {
		return " | "
	}
	return " · "
}

// RailState is the complete one-cell execution-marker state vocabulary.
type RailState uint8

const (
	RailProposed RailState = iota
	RailActive
	RailPermission
	RailSuccess
	RailWarning
	RailError
	RailCancelled
	RailHookBlocked
)

// RailLabelWidth is wide enough for every rail state label. Rendered rows keep
// this width across state changes.
const RailLabelWidth = 15

// RailVisual is the color-independent glyph and label for one rail state.
type RailVisual struct {
	State RailState
	Glyph string
	Label string
	Style lipgloss.Style
}

// RailVisual returns the current frame for a rail state. frame is ignored for
// static states and reduced-motion modes.
func (theme Theme) RailVisual(state RailState, frame int) RailVisual {
	glyph := theme.Glyphs.Proposed
	label := "preparing"
	style := theme.MutedStyle

	switch state {
	case RailActive:
		if theme.Mode.ReducedMotion {
			frame = 0
		}
		if frame < 0 {
			frame = -frame
		}
		glyph = theme.Glyphs.ActiveFrames[frame%len(theme.Glyphs.ActiveFrames)]
		label = "running"
		style = theme.AccentStyle
	case RailPermission:
		glyph = theme.Glyphs.Permission
		label = "approval needed"
		style = theme.AccentStyle
	case RailSuccess:
		glyph = theme.Glyphs.Success
		label = "succeeded"
		style = theme.SuccessStyle
	case RailWarning:
		glyph = theme.Glyphs.Warning
		label = "warning"
		style = theme.WarningStyle
	case RailError:
		glyph = theme.Glyphs.Error
		label = "failed"
		style = theme.DangerStyle
	case RailCancelled:
		glyph = theme.Glyphs.Cancelled
		label = "cancelled"
		style = theme.MutedStyle
	case RailHookBlocked:
		glyph = theme.Glyphs.HookBlocked
		label = "hook blocked"
		style = theme.DangerStyle
	}

	return RailVisual{State: state, Glyph: glyph, Label: label, Style: style}
}

// Render renders a geometry-stable rail state using RailLabelWidth.
func (visual RailVisual) Render() string {
	return visual.RenderWidth(RailLabelWidth)
}

// RenderWidth renders a rail glyph plus a fixed-width label field.
func (visual RailVisual) RenderWidth(labelWidth int) string {
	if labelWidth < 0 {
		labelWidth = 0
	}
	body := visual.Glyph
	if labelWidth > 0 {
		body += " " + padCells(visual.Label, labelWidth)
	}
	return visual.Style.Render(body)
}
