package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type overlayKind uint8

const (
	overlayHelp overlayKind = iota + 1
	overlayInspector
	overlayBypass
)

type overlayState struct {
	Kind       overlayKind
	Cursor     int
	Scroll     int
	Submitting bool
	Feedback   string
}

type inspectorEntry struct {
	Text            string
	BlockID         string
	Reasoning       bool
	Expandable      bool
	CurrentlyOpened bool
}

func (model *AppModel) openOverlay(kind overlayKind) tea.Cmd {
	if model.permission != nil && kind != overlayBypass {
		return nil
	}
	model.overlay = &overlayState{Kind: kind}
	model.composer.Blur()
	return nil
}

func (model *AppModel) closeOverlay() tea.Cmd {
	model.overlay = nil
	return model.syncComposerFocus()
}

func (model *AppModel) handleOverlayKey(_ tea.KeyPressMsg, key string) tea.Cmd {
	if model.overlay == nil {
		return nil
	}
	if model.overlay.Kind == overlayBypass {
		if model.overlay.Submitting {
			return nil
		}
		switch key {
		case "esc", "n":
			return model.closeOverlay()
		case "enter", "y":
			if !model.info.ModeChangeable || model.mode == ModeExternal || model.mode == ModeUnsupported || model.port == nil {
				model.overlay.Feedback = "permission mode is controlled externally"
				return nil
			}
			model.overlay.Submitting = true
			model.modeChangePending = true
			return setModeCmd(model.port, ModeBypass)
		default:
			return nil
		}
	}

	entries := model.overlayEntries()
	maxCursor := max(0, len(entries)-1)
	switch key {
	case "esc":
		return model.closeOverlay()
	case "up", "k":
		model.overlay.Cursor = max(0, model.overlay.Cursor-1)
	case "down", "j":
		model.overlay.Cursor = min(maxCursor, model.overlay.Cursor+1)
	case "pgup", "ctrl+u":
		model.overlay.Cursor = max(0, model.overlay.Cursor-max(1, model.layout.TranscriptRows/2))
	case "pgdown", "ctrl+d":
		model.overlay.Cursor = min(maxCursor, model.overlay.Cursor+max(1, model.layout.TranscriptRows/2))
	case "home":
		model.overlay.Cursor = 0
	case "end", "G":
		model.overlay.Cursor = maxCursor
	case "enter":
		if model.overlay.Kind != overlayInspector || len(entries) == 0 {
			return nil
		}
		entry := entries[min(model.overlay.Cursor, len(entries)-1)]
		if !entry.Expandable {
			return nil
		}
		if entry.Reasoning {
			model.transcript.ToggleReasoning(entry.BlockID)
		} else {
			model.transcript.ToggleExpanded(entry.BlockID)
		}
	}
	return nil
}

func (model *AppModel) overlayEntries() []inspectorEntry {
	if model.overlay == nil {
		return nil
	}
	if model.overlay.Kind == overlayHelp {
		return []inspectorEntry{
			{Text: "Compose"},
			{Text: "  Enter  send"},
			{Text: "  Ctrl+J  newline"},
			{Text: "  Shift+Enter / Alt+Enter  newline with enhanced keyboard reporting"},
			{Text: "Run"},
			{Text: "  Shift+Tab  cycle DEFAULT / AUTO EDIT / PLAN"},
			{Text: "  Ctrl+B  review BYPASS"},
			{Text: "  Esc / Ctrl+C  interrupt active work"},
			{Text: "Inspect"},
			{Text: "  Ctrl+I  open run inspector"},
			{Text: "  Ctrl+/  open this key index"},
			{Text: "Terminal"},
			{Text: "  Wheel  browse task history"},
			{Text: "  Drag  select and copy in this pane"},
			{Text: "  Shift/Option+drag  terminal-native selection fallback"},
			{Text: "  Ctrl+Q  bounded shutdown"},
		}
	}
	return model.inspectorEntries()
}

func (model *AppModel) inspectorEntries() []inspectorEntry {
	contextLabel, _ := model.contextUsageLabel()
	entries := []inspectorEntry{
		{Text: "Session"},
		{Text: "  model: " + firstNonEmpty(model.info.Model, "unknown")},
		{Text: "  provider: " + firstNonEmpty(model.info.Provider, "unknown")},
		{Text: "  permission: " + sessionModeLabel(model.mode) + " · owner " + firstNonEmpty(model.info.PermissionOwner, "unknown")},
		{Text: "  sandbox: " + firstNonEmpty(model.info.Sandbox, "unknown")},
	}
	if model.info.SandboxReason != "" {
		entries = append(entries, inspectorEntry{Text: "    " + model.info.SandboxReason})
	}
	entries = append(entries,
		inspectorEntry{Text: "  context: " + contextLabel + model.contextSourceDetail()},
		inspectorEntry{Text: "  reasoning summaries: " + string(model.info.ReasoningSummarySupport)},
		inspectorEntry{Text: "  provider usage: " + string(model.info.UsageSupport)},
		inspectorEntry{Text: "Capabilities"},
	)
	for _, category := range model.info.Capabilities {
		label := "  " + firstNonEmpty(category.Kind, "unknown") + ": " + string(category.Support)
		if category.Source != "" {
			label += " · " + category.Source
		}
		if category.Support == SupportSupported && len(category.Items) == 0 {
			label += " · none loaded"
		} else if category.Support == SupportUnsupported {
			label += " · not reported"
		}
		entries = append(entries, inspectorEntry{Text: label})
		for _, item := range category.Items {
			line := fmt.Sprintf("    %s · %s", firstNonEmpty(item.Name, "unnamed"), item.Availability)
			if item.Source != "" {
				line += " · " + item.Source
			}
			if item.Detail != "" {
				line += " · " + item.Detail
			}
			entries = append(entries, inspectorEntry{Text: line})
		}
	}
	entries = append(entries, inspectorEntry{Text: "Inspectable transcript details"})
	for _, block := range model.transcript.Blocks {
		if block.Kind == BlockAssistant && block.Reasoning != "" {
			marker := "[+]"
			if block.ReasoningExpanded {
				marker = "[-]"
			}
			entries = append(entries, inspectorEntry{
				Text: marker + " reasoning summary · " + block.ID, BlockID: block.ID,
				Reasoning: true, Expandable: true, CurrentlyOpened: block.ReasoningExpanded,
			})
			if block.ReasoningExpanded {
				for _, line := range strings.Split(block.Reasoning, "\n") {
					entries = append(entries, inspectorEntry{Text: "    " + line})
				}
			}
		}
		if block.Kind == BlockTool && (block.Result != "" || block.Preview != nil) {
			marker := "[+]"
			if block.Expanded {
				marker = "[-]"
			}
			entries = append(entries, inspectorEntry{
				Text:    marker + " tool " + firstNonEmpty(block.ToolName, "tool") + " · " + block.ID,
				BlockID: block.ID, Expandable: true, CurrentlyOpened: block.Expanded,
			})
			if block.Expanded {
				if block.Duration > 0 {
					entries = append(entries, inspectorEntry{Text: "    duration: " + formatDuration(block.Duration)})
				}
				if block.Arguments != "" {
					entries = append(entries, inspectorEntry{Text: "    arguments: " + block.Arguments})
				}
				for _, line := range inspectPreviewLines(block.Preview, block.Revision) {
					entries = append(entries, inspectorEntry{Text: "    " + line})
				}
				if block.Result != "" {
					for _, line := range strings.Split(block.Result, "\n") {
						entries = append(entries, inspectorEntry{Text: "    " + line})
					}
				}
			}
		}
		for _, omission := range block.Omissions {
			entries = append(entries, inspectorEntry{Text: "[!] " + formatOmissionNotice(omission)})
		}
	}
	return entries
}

func inspectPreviewLines(preview *ActionPreview, revision uint64) []string {
	if preview == nil {
		return nil
	}
	lines := []string{fmt.Sprintf("%s preview r%d", firstNonEmpty(preview.Operation, "action"), revision)}
	if preview.FileDiff != nil {
		lines = append(lines, firstNonEmpty(preview.FileDiff.Path, strings.Join(preview.Targets, ", ")))
		for _, hunk := range preview.FileDiff.Hunks {
			lines = append(lines, fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines))
			for _, line := range hunk.Lines {
				marker := " "
				switch line.Kind {
				case "added":
					marker = "+"
				case "removed":
					marker = "-"
				}
				lines = append(lines, marker+line.Text)
			}
		}
	} else if preview.Text != "" {
		lines = append(lines, strings.Split(preview.Text, "\n")...)
	} else if preview.Summary != "" {
		lines = append(lines, preview.Summary)
	} else if preview.UnavailableReason != "" {
		lines = append(lines, "preview unavailable: "+preview.UnavailableReason)
	}
	if preview.Omission != nil {
		lines = append(lines, formatOmissionNotice(*preview.Omission))
	}
	return lines
}

func (model *AppModel) contextSourceDetail() string {
	if model.usage == nil {
		return ""
	}
	switch model.usage.Source {
	case "estimated":
		return " · estimated"
	case "provider":
		return " · provider-reported"
	default:
		return " · source unknown"
	}
}

func (model *AppModel) renderOverlay(width, rows int) []string {
	if model.overlay == nil || rows <= 0 {
		return make([]string, max(0, rows))
	}
	if model.overlay.Kind == overlayBypass {
		return model.renderBypassConfirmation(width, rows)
	}
	entries := model.overlayEntries()
	title := "HELP / KEYS"
	if model.overlay.Kind == overlayInspector {
		title = "INSPECT / RUN LEDGER"
	}
	bodyRows := max(1, rows-3)
	maxTop := max(0, len(entries)-bodyRows)
	if model.overlay.Cursor < model.overlay.Scroll {
		model.overlay.Scroll = model.overlay.Cursor
	}
	if model.overlay.Cursor >= model.overlay.Scroll+bodyRows {
		model.overlay.Scroll = model.overlay.Cursor - bodyRows + 1
	}
	model.overlay.Scroll = min(max(0, model.overlay.Scroll), maxTop)
	end := min(len(entries), model.overlay.Scroll+bodyRows)
	rule := strings.Repeat(model.theme.Border.Top, width)
	lines := []string{
		renderElevatedLine(model.theme, model.theme.BorderStyle, rule, width),
		renderElevatedLine(model.theme, model.theme.AccentStyle, ansi.Truncate(title, width, ""), width),
	}
	for index := model.overlay.Scroll; index < end; index++ {
		prefix := "  "
		style := model.theme.TextStyle
		if model.overlay.Kind == overlayInspector && index == model.overlay.Cursor {
			prefix = model.theme.Glyphs.Focus + " "
			style = model.theme.AccentStyle
		}
		lines = append(lines, renderElevatedLine(model.theme, style, ansi.Truncate(prefix+SanitizeString(entries[index].Text), width, model.theme.Glyphs.Ellipsis), width))
	}
	for len(lines) < rows-1 {
		lines = append(lines, renderElevatedLine(model.theme, model.theme.TextStyle, "", width))
	}
	footer := "Esc  close"
	if model.overlay.Kind == overlayInspector {
		footer = "↑/↓  inspect   Enter  expand   Esc  close"
	}
	lines = append(lines, renderElevatedLine(model.theme, model.theme.MutedStyle, ansi.Truncate(footer, width, ""), width))
	if len(lines) > rows {
		lines = append(lines[:rows-1], lines[len(lines)-1])
	}
	return lines
}

func (model *AppModel) renderBypassConfirmation(width, rows int) []string {
	content := []string{
		"REVIEW / BYPASS",
		"Permission prompts may be skipped for tool calls.",
		"Hard hooks still block unconditionally, and sandbox confinement still applies.",
		"This applies to later tool decisions until you leave bypass.",
		"",
		"[y/Enter] Enable bypass    [n/Esc] Keep current mode",
	}
	if model.overlay.Feedback != "" {
		content = append(content, "! "+model.overlay.Feedback)
	}
	if model.overlay.Submitting {
		content = append(content, "Applying mode...")
	}
	lines := make([]string, 0, rows)
	for index, value := range content {
		style := model.theme.TextStyle
		if index == 0 {
			style = model.theme.DangerStyle
		} else if strings.HasPrefix(value, "!") {
			style = model.theme.WarningStyle
		}
		for _, line := range wrapProse(value, width) {
			lines = append(lines, style.Render(ansi.Truncate(line, width, model.theme.Glyphs.Ellipsis)))
		}
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return lines
}
