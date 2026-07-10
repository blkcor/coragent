package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	timeout := 100
	home := Settings{
		Model: &ModelSettings{
			Name:    "home-model",
			BaseURL: "https://home.example.com",
		},
		Hooks: []HookSettings{
			{Name: "home-only", Moment: "before-tool", Command: []string{"/bin/true"}, TimeoutMillis: &timeout},
			{Name: "shared", Moment: "before-tool", Command: []string{"/bin/true"}},
		},
	}

	project := Settings{
		Model: &ModelSettings{
			Name: "project-model",
		},
		Hooks: []HookSettings{
			{Name: "shared", Moment: "after-tool", Command: []string{"/bin/true"}},
			{Name: "project-only", Moment: "prompt-submit", Command: []string{"/bin/true"}},
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
	if len(merged.Hooks) != 3 {
		t.Fatalf("expected home-only, overridden shared, and project-only hooks, got %+v", merged.Hooks)
	}
	if merged.Hooks[0].Name != "home-only" {
		t.Errorf("non-overlapping home hook should be preserved, got %+v", merged.Hooks)
	}
	if merged.Hooks[1].Name != "shared" || merged.Hooks[1].Moment != "after-tool" {
		t.Errorf("project hook should override same-name home hook in place, got %+v", merged.Hooks)
	}
	if merged.Hooks[2].Name != "project-only" {
		t.Errorf("project-only hook should append, got %+v", merged.Hooks)
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

func TestLoadFromFile_HookSettings(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "settings.json")
	content := `{
		"hooks": [{
			"name": "notify",
			"moment": "run-finished",
			"command": ["/bin/sh", "guard.sh"],
			"timeout_ms": 250
		}]
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	settings, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	hooks := settings.ExternalHooks()
	if len(hooks) != 1 {
		t.Fatalf("want one hook, got %+v", hooks)
	}
	if hooks[0].Name != "notify" || hooks[0].Moment != "run-finished" || hooks[0].Timeout != 250*time.Millisecond {
		t.Fatalf("bad hook conversion: %+v", hooks[0])
	}
}

func TestLoadFromFile_InvalidHookSettings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "moment", content: `{"hooks":[{"name":"h","moment":"later","command":["/bin/true"]}]}`, want: "moment"},
		{name: "pattern", content: `{"hooks":[{"name":"h","moment":"before-tool","command":["/bin/true"],"pattern":"["}]}`, want: "pattern"},
		{name: "timeout", content: `{"hooks":[{"name":"h","moment":"before-tool","command":["/bin/true"],"timeout_ms":-1}]}`, want: "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := loadFromFile(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), path) {
				t.Fatalf("want error naming %q and path, got %v", tc.want, err)
			}
		})
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// --- permission settings ----------------------------------------------------

func TestDefaults_PermissionMode(t *testing.T) {
	settings := Defaults()
	if settings.Permission == nil {
		t.Fatal("expected default permission settings")
	}
	if settings.Permission.Mode != "default" {
		t.Errorf("expected default mode 'default', got %q", settings.Permission.Mode)
	}
	if len(settings.Permission.Allow) != 0 || len(settings.Permission.Deny) != 0 {
		t.Errorf("expected empty default rule lists, got allow=%v deny=%v",
			settings.Permission.Allow, settings.Permission.Deny)
	}
}

func TestLoadFromFile_PermissionSection(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "settings.json")
	content := `{
		"permission": {
			"mode": "plan",
			"allow": ["command:git status"],
			"deny": ["command:rm"]
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	settings, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings.Permission == nil {
		t.Fatal("expected permission settings parsed")
	}
	if settings.Permission.Mode != "plan" {
		t.Errorf("expected mode plan, got %q", settings.Permission.Mode)
	}
	if len(settings.Permission.Allow) != 1 || settings.Permission.Allow[0] != "command:git status" {
		t.Errorf("expected one allow rule, got %v", settings.Permission.Allow)
	}
	if len(settings.Permission.Deny) != 1 || settings.Permission.Deny[0] != "command:rm" {
		t.Errorf("expected one deny rule, got %v", settings.Permission.Deny)
	}
}

func TestMerge_PermissionModeOverrideAndListAppend(t *testing.T) {
	home := Settings{
		Permission: &PermissionSettings{
			Mode:  "default",
			Allow: []string{"command:git status"},
			Deny:  []string{"command:rm -rf"},
		},
	}
	project := Settings{
		Permission: &PermissionSettings{
			Mode:  "auto-accept-edits",
			Allow: []string{"command:ls"},
			Deny:  []string{"command:curl"},
		},
	}

	merged := merge(home, project)

	if merged.Permission.Mode != "auto-accept-edits" {
		t.Errorf("project mode must win, got %q", merged.Permission.Mode)
	}
	// Lists append home-then-project (both layers apply).
	wantAllow := []string{"command:git status", "command:ls"}
	wantDeny := []string{"command:rm -rf", "command:curl"}
	if strings.Join(merged.Permission.Allow, "|") != strings.Join(wantAllow, "|") {
		t.Errorf("allow lists must append home-then-project, got %v", merged.Permission.Allow)
	}
	if strings.Join(merged.Permission.Deny, "|") != strings.Join(wantDeny, "|") {
		t.Errorf("deny lists must append home-then-project, got %v", merged.Permission.Deny)
	}
}

func TestMerge_PermissionEmptyModePreservesExisting(t *testing.T) {
	home := Settings{Permission: &PermissionSettings{Mode: "bypass"}}
	project := Settings{Permission: &PermissionSettings{Allow: []string{"command:ls"}}}

	merged := merge(home, project)
	if merged.Permission.Mode != "bypass" {
		t.Errorf("empty project mode must preserve home mode, got %q", merged.Permission.Mode)
	}
}

func TestAppendPermissionRule_PreservesUnrelated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{
		"model": {"name": "deepseek-chat"},
		"permission": {"mode": "default", "allow": ["command:ls"]}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AppendPermissionRule(path, true, "command:git status"); err != nil {
		t.Fatalf("append: %v", err)
	}

	reloaded, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// Unrelated settings preserved.
	if reloaded.Model == nil || reloaded.Model.Name != "deepseek-chat" {
		t.Errorf("model settings must be preserved, got %+v", reloaded.Model)
	}
	// New rule appended alongside the existing one.
	want := []string{"command:ls", "command:git status"}
	if strings.Join(reloaded.Permission.Allow, "|") != strings.Join(want, "|") {
		t.Errorf("allow rules = %v, want %v", reloaded.Permission.Allow, want)
	}
}

func TestAppendPermissionRule_DenyAndCreateWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")

	if err := AppendPermissionRule(path, false, "command:rm"); err != nil {
		t.Fatalf("append to missing file: %v", err)
	}

	reloaded, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Permission == nil || len(reloaded.Permission.Deny) != 1 || reloaded.Permission.Deny[0] != "command:rm" {
		t.Errorf("deny rule must be persisted in a freshly created file, got %+v", reloaded.Permission)
	}
}

// --- sandbox settings ------------------------------------------------------

func TestLoadFromFile_SandboxSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	content := `{
		"sandbox": {
			"extra_read_roots": ["../shared"],
			"extra_write_roots": ["/tmp/coragent-extra"],
			"network": true
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	settings, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if settings.Sandbox == nil {
		t.Fatal("expected sandbox settings")
	}
	if strings.Join(settings.Sandbox.ExtraReadRoots, "|") != "../shared" {
		t.Fatalf("read roots = %v", settings.Sandbox.ExtraReadRoots)
	}
	if strings.Join(settings.Sandbox.ExtraWriteRoots, "|") != "/tmp/coragent-extra" {
		t.Fatalf("write roots = %v", settings.Sandbox.ExtraWriteRoots)
	}
	if settings.Sandbox.Network == nil || !*settings.Sandbox.Network {
		t.Fatalf("network grant should parse as true")
	}
}

func TestMerge_SandboxProjectOverridesOverlappingFields(t *testing.T) {
	homeNetwork := false
	projectNetwork := true
	home := Settings{Sandbox: &SandboxSettings{
		ExtraReadRoots:  []string{"/home/read"},
		ExtraWriteRoots: []string{"/home/write"},
		Network:         &homeNetwork,
	}}
	project := Settings{Sandbox: &SandboxSettings{
		ExtraReadRoots: []string{"/project/read"},
		Network:        &projectNetwork,
	}}

	merged := merge(home, project)
	if strings.Join(merged.Sandbox.ExtraReadRoots, "|") != "/project/read" {
		t.Fatalf("project read roots should override, got %v", merged.Sandbox.ExtraReadRoots)
	}
	if strings.Join(merged.Sandbox.ExtraWriteRoots, "|") != "/home/write" {
		t.Fatalf("non-overlapping home write roots should be preserved, got %v", merged.Sandbox.ExtraWriteRoots)
	}
	if merged.Sandbox.Network == nil || !*merged.Sandbox.Network {
		t.Fatalf("project network setting should override home")
	}
}

func TestLoadFromFile_InvalidSandboxSettings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "read path", content: `{"sandbox":{"extra_read_roots":[""]}}`, want: "extra_read_roots"},
		{name: "write path", content: `{"sandbox":{"extra_write_roots":[""]}}`, want: "extra_write_roots"},
		{name: "network type", content: `{"sandbox":{"network":"yes"}}`, want: "sandbox.network"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := loadFromFile(path)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error naming %q and path, got %v", tc.want, err)
			}
		})
	}
}
