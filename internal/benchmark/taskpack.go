package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Seed struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Old         string `json:"old"`
	New         string `json:"new"`
	FocusedTest string `json:"focused_test"`
	TestName    string `json:"test_name"`
}

func LoadSeed(name string) (Seed, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return Seed{}, err
	}
	var seed Seed
	if err := json.Unmarshal(data, &seed); err != nil {
		return Seed{}, err
	}
	if seed.ID == "" || seed.Path == "" || seed.Old == "" || seed.New == "" {
		return Seed{}, errors.New("benchmark: incomplete seed")
	}
	return seed, nil
}

func ApplySeed(workspace string, seed Seed) error {
	name := filepath.Join(workspace, filepath.FromSlash(seed.Path))
	data, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if strings.Count(string(data), seed.Old) != 1 {
		return fmt.Errorf("benchmark: seed %s target count is not one", seed.ID)
	}
	changed := strings.Replace(string(data), seed.Old, seed.New, 1)
	return os.WriteFile(name, []byte(changed), 0o600)
}

type SearchTrigger struct {
	mu          sync.Mutex
	activated   bool
	activations int
}

// BeforeCall fails only the first matching content-search request.
func (t *SearchTrigger) BeforeCall(toolName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if toolName == "search" && !t.activated {
		t.activated = true
		t.activations++
		return errors.New("primary search backend unavailable")
	}
	return nil
}

func (t *SearchTrigger) Activations() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activations
}

type PreparedPreview struct {
	Revision string
	Paths    []string
}

type StalePatchTrigger struct {
	mu        sync.Mutex
	activated bool
}

// Observe activates exactly once for the queue-size target, mutates only
// attempt-owned state through the supplied callback, and returns the stale
// revision the runner will approve in M2.
func (t *StalePatchTrigger) Observe(preview PreparedPreview, mutate func() error) (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.activated || preview.Revision == "" || len(preview.Paths) != 1 || preview.Paths[0] != "internal/config/config.go" {
		return "", false, nil
	}
	if err := mutate(); err != nil {
		return "", false, err
	}
	t.activated = true
	return preview.Revision, true, nil
}

type PermissionScript struct {
	WorkspaceRead        bool `json:"workspace_read"`
	WorkspaceWrite       bool `json:"workspace_write"`
	Process              bool `json:"process"`
	Network              bool `json:"network"`
	ExternalRoots        bool `json:"external_roots"`
	EnvironmentExpansion bool `json:"environment_expansion"`
}

func LoadPermissionScript(name string) (PermissionScript, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return PermissionScript{}, err
	}
	var script PermissionScript
	if err := json.Unmarshal(data, &script); err != nil {
		return PermissionScript{}, err
	}
	return script, nil
}

func (p PermissionScript) Allows(capability string) bool {
	switch capability {
	case "workspace_read":
		return p.WorkspaceRead
	case "workspace_write":
		return p.WorkspaceWrite
	case "process":
		return p.Process
	case "network":
		return p.Network
	case "external_roots":
		return p.ExternalRoots
	case "environment_expansion":
		return p.EnvironmentExpansion
	default:
		return false
	}
}
