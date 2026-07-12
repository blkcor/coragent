//go:build darwin

package tui

import (
	"path/filepath"
	"testing"
)

func TestSystemClipboardUsesPlatformOwnedAbsoluteHelper(t *testing.T) {
	if !filepath.IsAbs(systemClipboardProgram) || filepath.Clean(systemClipboardProgram) != "/usr/bin/pbcopy" {
		t.Fatalf("clipboard helper = %q, want platform-owned absolute path", systemClipboardProgram)
	}
}
