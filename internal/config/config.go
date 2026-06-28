package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Settings configures the coragent harness.
// A single settings file is discovered in the home directory and/or the project
// directory, merged field-by-field with project taking precedence.
type Settings struct {
	// Model configures the default model backend.
	Model *ModelSettings `json:"model,omitempty"`
}

// ModelSettings configures the model backend.
type ModelSettings struct {
	// Name is the default model identifier (e.g., "gpt-4", "deepseek-chat").
	Name string `json:"name,omitempty"`

	// BaseURL is the OpenAI-compatible API endpoint.
	BaseURL string `json:"base_url,omitempty"`

	// APIKey is the API key, resolved from environment if in ${VAR} syntax.
	APIKey string `json:"api_key,omitempty"`

	// Temperature is the default sampling temperature (0.0 to 2.0).
	Temperature *float64 `json:"temperature,omitempty"`

	// MaxTokens is the default maximum reply length.
	MaxTokens *int `json:"max_tokens,omitempty"`

	// RetryMax is the maximum number of retry attempts for transient failures.
	RetryMax *int `json:"retry_max,omitempty"`

	// RetryInitialBackoff is the initial backoff duration in milliseconds.
	RetryInitialBackoff *int `json:"retry_initial_backoff_ms,omitempty"`
}

// Defaults returns a Settings with documented default values.
func Defaults() Settings {
	retryMax := 3
	retryBackoff := 1000
	temperature := 0.7
	return Settings{
		Model: &ModelSettings{
			Name:                "gpt-4",
			BaseURL:             "https://api.openai.com/v1",
			Temperature:         &temperature,
			RetryMax:            &retryMax,
			RetryInitialBackoff: &retryBackoff,
		},
	}
}

// Load discovers and loads settings from home and project directories.
// Project settings override home settings field-by-field.
// If neither file exists, returns documented defaults.
func Load() (Settings, error) {
	homeSettings, homeErr := loadHomeSettings()
	projectSettings, projectErr := loadProjectSettings()

	if homeErr != nil && projectErr != nil {
		// Both failed or neither exist
		if isFileNotFoundError(homeErr) && isFileNotFoundError(projectErr) {
			return Defaults(), nil
		}
		// At least one is a real error
		if !isFileNotFoundError(homeErr) {
			return Settings{}, homeErr
		}
		return Settings{}, projectErr
	}

	// Merge: start with defaults, overlay home, then project
	settings := Defaults()
	if homeErr == nil {
		settings = merge(settings, homeSettings)
	}
	if projectErr == nil {
		settings = merge(settings, projectSettings)
	}

	return settings, nil
}

// LoadFrom loads settings directly from the provided struct, skipping file discovery.
// This is used by SDK embedders who supply configuration in code.
func LoadFrom(s Settings) Settings {
	return merge(Defaults(), s)
}

// loadHomeSettings loads from ~/.coragent/settings.json
func loadHomeSettings() (Settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Settings{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	path := filepath.Join(home, ".coragent", "settings.json")
	return loadFromFile(path)
}

// loadProjectSettings loads from .coragent/settings.json in current directory
func loadProjectSettings() (Settings, error) {
	path := filepath.Join(".coragent", "settings.json")
	return loadFromFile(path)
}

// loadFromFile loads and validates settings from a specific file path
func loadFromFile(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, &FileNotFoundError{Path: path}
		}
		return Settings{}, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, fmt.Errorf("malformed JSON in %s: %w", path, err)
	}

	// Resolve environment variables in credentials
	if err := resolveEnvVars(&settings, path); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

// resolveEnvVars replaces ${VAR} syntax with environment variable values
func resolveEnvVars(settings *Settings, filePath string) error {
	if settings.Model == nil {
		return nil
	}

	envPattern := regexp.MustCompile(`^\$\{([^}]+)\}$`)

	if settings.Model.APIKey != "" {
		if matches := envPattern.FindStringSubmatch(settings.Model.APIKey); len(matches) == 2 {
			envVar := matches[1]
			value := os.Getenv(envVar)
			if value == "" {
				// Leave empty; first API request will fail loudly
				settings.Model.APIKey = ""
			} else {
				settings.Model.APIKey = value
			}
		}
	}

	return nil
}

// merge overlays src onto dst, field-by-field (src wins per overlapping field)
func merge(dst, src Settings) Settings {
	if src.Model != nil {
		if dst.Model == nil {
			dst.Model = &ModelSettings{}
		}
		if src.Model.Name != "" {
			dst.Model.Name = src.Model.Name
		}
		if src.Model.BaseURL != "" {
			dst.Model.BaseURL = src.Model.BaseURL
		}
		if src.Model.APIKey != "" {
			dst.Model.APIKey = src.Model.APIKey
		}
		if src.Model.Temperature != nil {
			dst.Model.Temperature = src.Model.Temperature
		}
		if src.Model.MaxTokens != nil {
			dst.Model.MaxTokens = src.Model.MaxTokens
		}
		if src.Model.RetryMax != nil {
			dst.Model.RetryMax = src.Model.RetryMax
		}
		if src.Model.RetryInitialBackoff != nil {
			dst.Model.RetryInitialBackoff = src.Model.RetryInitialBackoff
		}
	}
	return dst
}

// FileNotFoundError indicates a settings file was not found
type FileNotFoundError struct {
	Path string
}

func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("settings file not found: %s", e.Path)
}

func isFileNotFoundError(err error) bool {
	_, ok := err.(*FileNotFoundError)
	return ok
}
