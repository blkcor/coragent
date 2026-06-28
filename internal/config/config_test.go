package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	settings := Defaults()
	if settings.Model == nil {
		t.Fatal("expected default model settings")
	}
	if settings.Model.Name != "gpt-4" {
		t.Errorf("expected default model name gpt-4, got %s", settings.Model.Name)
	}
	if settings.Model.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("expected default base URL, got %s", settings.Model.BaseURL)
	}
	if settings.Model.RetryMax == nil || *settings.Model.RetryMax != 3 {
		t.Errorf("expected default retry max 3")
	}
}

func TestLoadFromFile_ValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "settings.json")

	content := `{
		"model": {
			"name": "deepseek-chat",
			"base_url": "https://api.deepseek.com/v1"
		}
	}`

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	settings, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.Model == nil {
		t.Fatal("expected model settings")
	}
	if settings.Model.Name != "deepseek-chat" {
		t.Errorf("expected name deepseek-chat, got %s", settings.Model.Name)
	}
	if settings.Model.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("expected base URL, got %s", settings.Model.BaseURL)
	}
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "settings.json")

	content := `{ invalid json }`

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadFromFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	// Check error names the file
	if errStr := err.Error(); !contains(errStr, path) {
		t.Errorf("error should name file %s, got: %s", path, errStr)
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")

	_, err := loadFromFile(path)
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	if !isFileNotFoundError(err) {
		t.Errorf("expected FileNotFoundError, got %T", err)
	}
}

func TestMerge_ProjectOverridesHome(t *testing.T) {
	home := Settings{
		Model: &ModelSettings{
			Name:    "home-model",
			BaseURL: "https://home.example.com",
		},
	}

	project := Settings{
		Model: &ModelSettings{
			Name: "project-model",
		},
	}

	merged := merge(home, project)

	// Project name wins
	if merged.Model.Name != "project-model" {
		t.Errorf("expected project-model, got %s", merged.Model.Name)
	}

	// Home base URL preserved (project didn't override)
	if merged.Model.BaseURL != "https://home.example.com" {
		t.Errorf("expected home base URL preserved, got %s", merged.Model.BaseURL)
	}
}

func TestMerge_NilSource(t *testing.T) {
	dst := Settings{
		Model: &ModelSettings{
			Name: "dst-model",
		},
	}

	src := Settings{}

	merged := merge(dst, src)

	// Destination preserved
	if merged.Model.Name != "dst-model" {
		t.Errorf("expected dst-model preserved, got %s", merged.Model.Name)
	}
}

func TestResolveEnvVars_Set(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret-key-123")

	settings := Settings{
		Model: &ModelSettings{
			APIKey: "${TEST_API_KEY}",
		},
	}

	if err := resolveEnvVars(&settings, "test.json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.Model.APIKey != "secret-key-123" {
		t.Errorf("expected resolved API key, got %s", settings.Model.APIKey)
	}
}

func TestResolveEnvVars_Unset(t *testing.T) {
	settings := Settings{
		Model: &ModelSettings{
			APIKey: "${UNSET_VAR}",
		},
	}

	if err := resolveEnvVars(&settings, "test.json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unset leaves empty (first API request will fail)
	if settings.Model.APIKey != "" {
		t.Errorf("expected empty API key for unset env var, got %s", settings.Model.APIKey)
	}
}

func TestResolveEnvVars_LiteralValue(t *testing.T) {
	settings := Settings{
		Model: &ModelSettings{
			APIKey: "literal-key",
		},
	}

	if err := resolveEnvVars(&settings, "test.json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-${} syntax left as-is
	if settings.Model.APIKey != "literal-key" {
		t.Errorf("expected literal key preserved, got %s", settings.Model.APIKey)
	}
}

func TestLoad_NoFiles_ReturnsDefaults(t *testing.T) {
	// Redirect HOME to a temp dir so we never touch real user files
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Also work from a temp directory to avoid picking up a project settings file.
	// NOTE: os.Chdir is process-wide; this test must not use t.Parallel().
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpProject := t.TempDir()
	if err := os.Chdir(tmpProject); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	settings, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get defaults
	if settings.Model == nil {
		t.Fatal("expected default model settings")
	}
	if settings.Model.Name != "gpt-4" {
		t.Errorf("expected default gpt-4, got %s", settings.Model.Name)
	}
}

func TestLoadFrom_SkipsDiscovery(t *testing.T) {
	custom := Settings{
		Model: &ModelSettings{
			Name:    "custom-model",
			BaseURL: "https://custom.example.com",
		},
	}

	settings := LoadFrom(custom)

	// Custom settings honored
	if settings.Model.Name != "custom-model" {
		t.Errorf("expected custom-model, got %s", settings.Model.Name)
	}

	// Defaults merged for unset fields
	if settings.Model.RetryMax == nil || *settings.Model.RetryMax != 3 {
		t.Errorf("expected default retry max 3")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
