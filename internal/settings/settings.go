// Package settings loads the home and project settings paths shared by the
// terminal product. Runtime credentials are referenced by environment-variable
// name and are never read while parsing or listing sessions.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Provider struct {
	Endpoint        string   `json:"endpoint"`
	Model           string   `json:"model"`
	ContextWindow   int      `json:"context_window"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	APIKeyEnv       string   `json:"api_key_env"`
	Temperature     *float64 `json:"temperature,omitempty"`
	Seed            *int64   `json:"seed,omitempty"`
	ToolChoice      string   `json:"tool_choice,omitempty"`
}

type Settings struct {
	Provider        Provider `json:"provider"`
	UserPreferences string   `json:"user_preferences,omitempty"`
}

// Load resolves ~/.coragent/settings.json then workspace-local
// .coragent/settings.json. Provider transport authority and credential-source
// selection are trusted startup inputs and may only come from the home file;
// an untrusted repository may override model tuning but cannot redirect a
// credential to another endpoint or select another ambient environment value.
func Load(workspace string) (Settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Settings{}, fmt.Errorf("settings: resolve home: %w", err)
	}
	return LoadFiles(filepath.Join(home, ".coragent", "settings.json"), filepath.Join(workspace, ".coragent", "settings.json"))
}

func LoadFiles(homePath, projectPath string) (Settings, error) {
	home, err := loadOne(homePath)
	if err != nil {
		return Settings{}, err
	}
	project, err := loadOne(projectPath)
	if err != nil {
		return Settings{}, err
	}
	if project.Provider.Endpoint != "" || project.Provider.APIKeyEnv != "" {
		return Settings{}, errors.New("settings: project settings may not override provider endpoint or api_key_env")
	}
	return merge(home, project), nil
}

func (s Settings) Validate() error {
	if s.Provider.Endpoint == "" || s.Provider.Model == "" || s.Provider.APIKeyEnv == "" {
		return errors.New("settings: provider endpoint, model, and api_key_env are required")
	}
	if s.Provider.ContextWindow <= 0 || s.Provider.MaxOutputTokens <= 0 {
		return errors.New("settings: explicit positive context_window and max_output_tokens are required")
	}
	return nil
}

func loadOne(name string) (Settings, error) {
	//nolint:gosec // reads the operator-selected settings file path
	data, err := os.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("settings: read %s: %w", name, err)
	}
	var out Settings
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Settings{}, fmt.Errorf("settings: decode %s: %w", name, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Settings{}, fmt.Errorf("settings: decode %s: trailing data", name)
	}
	return out, nil
}

func merge(base, override Settings) Settings {
	out := base
	if override.Provider.Model != "" {
		out.Provider.Model = override.Provider.Model
	}
	if override.Provider.ContextWindow != 0 {
		out.Provider.ContextWindow = override.Provider.ContextWindow
	}
	if override.Provider.MaxOutputTokens != 0 {
		out.Provider.MaxOutputTokens = override.Provider.MaxOutputTokens
	}
	if override.Provider.Temperature != nil {
		out.Provider.Temperature = override.Provider.Temperature
	}
	if override.Provider.Seed != nil {
		out.Provider.Seed = override.Provider.Seed
	}
	if override.Provider.ToolChoice != "" {
		out.Provider.ToolChoice = override.Provider.ToolChoice
	}
	if override.UserPreferences != "" {
		out.UserPreferences = override.UserPreferences
	}
	return out
}
