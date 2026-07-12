package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadScrubsLegacyExactRulesFromHomeAndProjectRawSettings(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CORAGENT_TEST_API_KEY", "resolved-secret-must-not-be-written")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	homePath, err := HomeSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := ProjectSettingsPath()
	legacyAllow := "exact-v1:read:sha256:" + strings.Repeat("a", 64)
	legacyDeny := "exact-v1:command:sha256:" + strings.Repeat("b", 64)
	writeRawSettings(t, homePath, 0o640, `{
  "model": {"api_key": "${CORAGENT_TEST_API_KEY}", "unknown_model_field": "keep-home"},
  "unknown_top": {"keep": true},
  "permission": {"mode": "default", "allow": ["`+legacyAllow+`", "read:README.md"], "deny": ["`+legacyDeny+`", "command:rm"]}
}`)
	writeRawSettings(t, projectPath, 0o600, `{
  "project_unknown": "keep-project",
  "permission": {"allow": ["`+legacyAllow+`", "command:git status"], "deny": ["`+legacyDeny+`", "edit:notes.txt"]}
}`)

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Model == nil || loaded.Model.APIKey != "resolved-secret-must-not-be-written" {
		t.Fatalf("resolved settings API key = %q", loaded.Model.APIKey)
	}

	for path, mode := range map[string]os.FileMode{homePath: 0o640, projectPath: 0o600} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "exact-v1") || strings.Contains(text, strings.Repeat("a", 64)) || strings.Contains(text, strings.Repeat("b", 64)) {
			t.Fatalf("legacy digest survived in %s: %s", path, text)
		}
		if strings.Contains(path, home) && (!strings.Contains(text, "${CORAGENT_TEST_API_KEY}") || strings.Contains(text, "resolved-secret-must-not-be-written") || !strings.Contains(text, "unknown_top") || !strings.Contains(text, "unknown_model_field")) {
			t.Fatalf("home raw settings were not preserved: %s", text)
		}
		if strings.Contains(path, project) && !strings.Contains(text, "project_unknown") {
			t.Fatalf("project unknown field was lost: %s", text)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
		}
		assertNoSettingsBackup(t, filepath.Dir(path))
	}
	logText := logs.String()
	if !strings.Contains(logText, homePath) || !strings.Contains(logText, projectPath) || !strings.Contains(logText, "count=2") || !strings.Contains(logText, "version=exact-v1") {
		t.Fatalf("migration warning lacks safe path/count/version fields: %s", logText)
	}
	for _, forbidden := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), "resolved-secret-must-not-be-written"} {
		if strings.Contains(logText, forbidden) {
			t.Fatalf("migration warning leaked %q: %s", forbidden, logText)
		}
	}
}

func TestAppendPermissionRuleScrubsLegacyWithoutResolvingOrResurrecting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := "exact-v1:unknown:sha256:" + strings.Repeat("c", 64)
	writeRawSettings(t, path, 0o640, `{
  "model": {"api_key": "${STILL_A_PLACEHOLDER}"},
  "unknown_top": [1, 2, 3],
  "permission": {"allow": ["`+legacy+`"], "deny": ["`+legacy+`", "command:rm"]}
}`)
	if err := AppendPermissionRule(path, true, "command:git status"); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := AppendPermissionRule(path, false, "edit:/tmp/blocked"); err != nil {
		t.Fatalf("second append: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{"${STILL_A_PLACEHOLDER}", "unknown_top", "command:git status", "command:rm", "edit:/tmp/blocked"} {
		if !strings.Contains(text, required) {
			t.Fatalf("raw settings lost %q: %s", required, text)
		}
	}
	if strings.Contains(text, "exact-v1") || strings.Contains(text, strings.Repeat("c", 64)) {
		t.Fatalf("legacy rule resurrected after save: %s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 0640", info.Mode().Perm())
	}
	assertNoSettingsBackup(t, filepath.Dir(path))
}

func TestAppendPermissionRuleRefusesLegacyInputAfterScrubbingDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := "exact-v1:read:sha256:" + strings.Repeat("f", 64)
	writeRawSettings(t, path, 0o600, `{"permission":{"allow":["`+legacy+`","read:README.md"]}}`)
	err := AppendPermissionRule(path, true, legacy)
	if err == nil || !strings.Contains(err.Error(), "exact-v1") || strings.Contains(err.Error(), strings.Repeat("f", 64)) {
		t.Fatalf("legacy append error = %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if bytes.Contains(data, []byte("exact-v1")) || bytes.Contains(data, []byte(strings.Repeat("f", 64))) || !bytes.Contains(data, []byte("read:README.md")) {
		t.Fatalf("legacy input survived or family rule was lost: %s", data)
	}
}

func writeRawSettings(t *testing.T, path string, mode os.FileMode, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertNoSettingsBackup(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, ".bak") || strings.HasPrefix(name, ".settings-") {
			t.Fatalf("unexpected settings backup or temporary file %q", name)
		}
	}
}
