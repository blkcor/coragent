package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/blkcor/coragent/internal/core"
)

// Settings configures the coragent harness.
// A single settings file is discovered in the home directory and/or the project
// directory, merged field-by-field with project taking precedence.
type Settings struct {
	// Model configures the default model backend.
	Model *ModelSettings `json:"model,omitempty"`

	// Hooks configures external command hooks.
	Hooks []HookSettings `json:"hooks,omitempty"`

	// Permission configures the soft human-in-the-loop gate: starting mode and the
	// allow/deny rule lists.
	Permission *PermissionSettings `json:"permission,omitempty"`

	// Sandbox configures additive sandbox policy grants. Missing sandbox settings
	// keep the safe baseline: project/scratch writes and network denied.
	Sandbox *SandboxSettings `json:"sandbox,omitempty"`

	// SkillRoots configures skill discovery paths. When absent, defaults are used.
	SkillRoots *SkillRootSettings `json:"skill_roots,omitempty"`
}

// SkillRootSettings configures where skills are discovered.
type SkillRootSettings struct {
	// User is the path to user-level skills. Default: ~/.coragent/skills/
	User string `json:"user,omitempty"`

	// Project is the path to project-level skills. Default: .coragent/skills/
	Project string `json:"project,omitempty"`
}

// PermissionSettings configures the permission engine. Each allow/deny entry is
// either a legacy "<kind>:<match>" family rule or a versioned keyed exact-call
// fingerprint, so typed rules live in a flat JSON list without persisting exact
// arguments or fingerprint key material.
type PermissionSettings struct {
	// Mode is the starting mode: default, auto-accept-edits, plan, or bypass.
	Mode string `json:"mode,omitempty"`

	// Allow lists rules that run an action without asking.
	Allow []string `json:"allow,omitempty"`

	// Deny lists rules that refuse an action without asking. Deny beats allow.
	Deny []string `json:"deny,omitempty"`
}

// SandboxSettings configures additive grants for the command sandbox. It does
// not accept backend-specific profile text; profiles are generated internally
// from these structured fields.
type SandboxSettings struct {
	// ExtraReadRoots adds paths readable by sandboxed commands.
	ExtraReadRoots []string `json:"extra_read_roots,omitempty"`

	// ExtraWriteRoots adds paths writable by sandboxed commands.
	ExtraWriteRoots []string `json:"extra_write_roots,omitempty"`

	// Network grants outbound network access when true. Nil means unspecified,
	// preserving the inherited or default value during merge.
	Network *bool `json:"network,omitempty"`
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

// HookSettings configures one external command hook in settings.
type HookSettings struct {
	Name          string   `json:"name,omitempty"`
	Moment        string   `json:"moment,omitempty"`
	Command       []string `json:"command,omitempty"`
	Tool          string   `json:"tool,omitempty"`
	Pattern       string   `json:"pattern,omitempty"`
	TimeoutMillis *int     `json:"timeout_ms,omitempty"`
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
		Permission: &PermissionSettings{
			Mode: "default",
		},
	}
}

// Load discovers and loads settings from home and project directories. Before
// resolution it atomically scrubs unsafe legacy exact-call selectors from both
// raw files without expanding environment placeholders. Project settings
// override home settings field-by-field.
// If neither file exists, returns documented defaults.
func Load() (Settings, error) {
	if err := ScrubLegacyExactPermissionRules(); err != nil {
		return Settings{}, err
	}
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
	path, err := HomeSettingsPath()
	if err != nil {
		return Settings{}, err
	}
	return loadFromFile(path)
}

// loadProjectSettings loads from .coragent/settings.json in current directory
func loadProjectSettings() (Settings, error) {
	return loadFromFile(ProjectSettingsPath())
}

// ProjectSettingsPath is the project-local settings file path, where remembered
// permission rules are persisted by default.
func ProjectSettingsPath() string {
	return filepath.Join(".coragent", "settings.json")
}

// AppendPermissionRule durably records a remembered rule by read-modify-write of
// the raw settings file at path: it treats a missing file as empty, scrubs unsafe
// legacy exact selectors, appends the family rule or keyed fingerprint, and
// atomically writes the raw JSON back without resolving placeholders or dropping
// unknown fields. Parent directories are created as needed.
func AppendPermissionRule(path string, allow bool, rule string) error {
	return appendPermissionRuleRaw(path, allow, rule)
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
	if err := validateHooks(settings.Hooks, path); err != nil {
		return Settings{}, err
	}
	if err := validateSandbox(settings.Sandbox, path); err != nil {
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
	if src.Hooks != nil {
		dst.Hooks = mergeHooks(dst.Hooks, src.Hooks)
	}
	if src.Permission != nil {
		if dst.Permission == nil {
			dst.Permission = &PermissionSettings{}
		}
		// Mode overrides when set; an empty project mode preserves the home mode.
		if src.Permission.Mode != "" {
			dst.Permission.Mode = src.Permission.Mode
		}
		// Rule lists append home-then-project so both layers apply; deny-beats-allow
		// at resolution time makes the order safe regardless.
		dst.Permission.Allow = append(dst.Permission.Allow, src.Permission.Allow...)
		dst.Permission.Deny = append(dst.Permission.Deny, src.Permission.Deny...)
	}
	if src.SkillRoots != nil {
		if dst.SkillRoots == nil {
			dst.SkillRoots = &SkillRootSettings{}
		}
		if src.SkillRoots.User != "" {
			dst.SkillRoots.User = src.SkillRoots.User
		}
		if src.SkillRoots.Project != "" {
			dst.SkillRoots.Project = src.SkillRoots.Project
		}
	}
	if src.Sandbox != nil {
		if dst.Sandbox == nil {
			dst.Sandbox = &SandboxSettings{}
		}
		if src.Sandbox.ExtraReadRoots != nil {
			dst.Sandbox.ExtraReadRoots = append([]string(nil), src.Sandbox.ExtraReadRoots...)
		}
		if src.Sandbox.ExtraWriteRoots != nil {
			dst.Sandbox.ExtraWriteRoots = append([]string(nil), src.Sandbox.ExtraWriteRoots...)
		}
		if src.Sandbox.Network != nil {
			v := *src.Sandbox.Network
			dst.Sandbox.Network = &v
		}
	}
	return dst
}

func mergeHooks(dst, src []HookSettings) []HookSettings {
	out := append([]HookSettings(nil), dst...)
	byName := make(map[string]int)
	for i, h := range out {
		if h.Name != "" {
			byName[h.Name] = i
		}
	}
	for _, h := range src {
		if h.Name != "" {
			if i, ok := byName[h.Name]; ok {
				out[i] = h
				continue
			}
			byName[h.Name] = len(out)
		}
		out = append(out, h)
	}
	return out
}

func validateHooks(hooks []HookSettings, filePath string) error {
	for i, h := range hooks {
		name := h.Name
		if name == "" {
			name = fmt.Sprintf("hook[%d]", i)
		}
		switch core.HookMoment(h.Moment) {
		case core.HookSessionStart, core.HookPromptSubmit, core.HookBeforeTool, core.HookAfterTool, core.HookRunFinished, core.HookSessionStop:
		default:
			return fmt.Errorf("invalid hook %q in %s: invalid moment %q", name, filePath, h.Moment)
		}
		if len(h.Command) == 0 || h.Command[0] == "" {
			return fmt.Errorf("invalid hook %q in %s: command is required", name, filePath)
		}
		if h.Pattern != "" {
			if _, err := regexp.Compile(h.Pattern); err != nil {
				return fmt.Errorf("invalid hook %q in %s: invalid pattern: %w", name, filePath, err)
			}
		}
		if h.TimeoutMillis != nil && *h.TimeoutMillis < 0 {
			return fmt.Errorf("invalid hook %q in %s: timeout_ms must be non-negative", name, filePath)
		}
	}
	return nil
}

func validateSandbox(s *SandboxSettings, filePath string) error {
	if s == nil {
		return nil
	}
	for _, p := range s.ExtraReadRoots {
		if p == "" {
			return fmt.Errorf("invalid sandbox in %s: extra_read_roots contains an empty path", filePath)
		}
		if filepath.IsAbs(p) {
			continue
		}
		if filepath.Clean(p) != p {
			return fmt.Errorf("invalid sandbox in %s: invalid extra_read_roots path %q", filePath, p)
		}
	}
	for _, p := range s.ExtraWriteRoots {
		if p == "" {
			return fmt.Errorf("invalid sandbox in %s: extra_write_roots contains an empty path", filePath)
		}
		if filepath.IsAbs(p) {
			continue
		}
		if filepath.Clean(p) != p {
			return fmt.Errorf("invalid sandbox in %s: invalid extra_write_roots path %q", filePath, p)
		}
	}
	return nil
}

// ExternalHooks converts settings hook declarations to the core engine shape.
func (s Settings) ExternalHooks() []core.ExternalHook {
	out := make([]core.ExternalHook, 0, len(s.Hooks))
	for _, h := range s.Hooks {
		timeout := time.Duration(0)
		if h.TimeoutMillis != nil {
			timeout = time.Duration(*h.TimeoutMillis) * time.Millisecond
		}
		out = append(out, core.ExternalHook{
			Name:    h.Name,
			Moment:  core.HookMoment(h.Moment),
			Command: append([]string(nil), h.Command...),
			Scope: core.HookScope{
				ToolName: h.Tool,
				Pattern:  h.Pattern,
			},
			Timeout: timeout,
		})
	}
	return out
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
