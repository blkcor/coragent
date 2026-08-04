package config

import (
	"os"
	"path/filepath"
	"testing"
)

func intPointer(value int) *int { return &value }

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.json")
	project := filepath.Join(dir, "project.json")
	if err := os.WriteFile(user, []byte(`{"queue_size":11,"retries":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"queue_size":12,"retries":5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(user, project, map[string]string{"MERCURY_QUEUE_SIZE": "13", "MERCURY_RETRIES": "6"}, FlagValues{QueueSize: intPointer(14), Retries: intPointer(7)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QueueSize != 14 || cfg.Retries != 7 {
		t.Fatalf("configuration = %+v", cfg)
	}
}

func TestValidationRunsAfterLoading(t *testing.T) {
	cfg, err := Load("", "", map[string]string{"MERCURY_QUEUE_SIZE": "-1"}, FlagValues{})
	if err != nil {
		t.Fatalf("Load validates too early: %v", err)
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("Validate accepted negative queue size")
	}
}

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.QueueSize != 10 || cfg.Retries != 3 {
		t.Fatalf("defaults = %+v", cfg)
	}
}
