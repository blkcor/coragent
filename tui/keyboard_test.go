package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewlineAliasHelpRequiresKeyboardDisambiguation(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	if hints := model.renderHints(model.layout.ContentWidth); strings.Contains(hints, "Shift+Enter") {
		t.Fatalf("legacy help advertised enhanced alias: %q", hints)
	}

	updated, _ := model.Update(tea.KeyboardEnhancementsMsg{Flags: 1})
	model = updated.(*AppModel)
	if hints := model.renderHints(model.layout.ContentWidth); !strings.Contains(hints, "Shift+Enter") {
		t.Fatalf("enhanced help omitted supported alias: %q", hints)
	}
}

func TestIdleHintsAdvertiseWheelHistoryAndTerminalSelection(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	hints := stripANSI(model.renderHints(200))
	for _, want := range []string{"wheel history", "drag auto-copy", "Shift/Option+drag"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("idle hints omitted %q: %q", want, hints)
		}
	}
	if strings.Contains(hints, "Cmd+C copy") {
		t.Fatalf("idle hints promised an OS-owned shortcut: %q", hints)
	}
	if strings.Contains(hints, "PgUp") || strings.Contains(hints, "PgDown") {
		t.Fatalf("idle hints still advertise keyboard history scrolling: %q", hints)
	}
}

func TestComposerDisablesPathResolvedClipboardPaste(t *testing.T) {
	composer := newComposerModel(DefaultTheme(), LayoutForSize(80, 24))
	if composer.textarea.KeyMap.Paste.Enabled() {
		t.Fatal("textarea Ctrl+V clipboard helper is enabled")
	}
	composer.SetValue("safe")
	if command := composer.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}); command != nil {
		t.Fatal("disabled Ctrl+V returned a clipboard command")
	}
	if got := composer.Value(); got != "safe" {
		t.Fatalf("disabled Ctrl+V changed composer to %q", got)
	}
}
