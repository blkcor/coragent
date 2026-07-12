package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEnhancedNewlineAliasWorksWithoutPersistentHintRow(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	updated, _ := model.Update(tea.KeyboardEnhancementsMsg{Flags: 1})
	model = updated.(*AppModel)
	model.composer.SetValue("first")
	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if got := model.composer.Value(); got != "first\n" {
		t.Fatalf("enhanced Shift+Enter draft = %q", got)
	}
	if view := stripANSI(model.View().Content); strings.Contains(view, "Shift+Enter") || strings.Contains(view, "Ctrl+J newline") {
		t.Fatalf("persistent shortcut hint returned:\n%s", view)
	}
}

func TestIdleShellOmitsPersistentShortcutLegend(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	view := stripANSI(model.View().Content)
	for _, noise := range []string{"Enter send", "Ctrl+J newline", "Shift+Tab mode", "wheel history", "drag auto-copy", "Shift/Option+drag"} {
		if strings.Contains(view, noise) {
			t.Fatalf("idle shell still renders %q:\n%s", noise, view)
		}
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
