package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	legacyExactRuleVersion = "exact-v1"
	keyedExactRuleVersion  = "exact-v2"
)

var permissionSettingsWriteMu sync.Mutex

type permissionRuleMutation struct {
	removeVersions map[string]bool
	appendAllow    *string
	appendDeny     *string
}

type PermissionRuleScrubSummary struct {
	ByVersion map[string]int
}

func (summary *PermissionRuleScrubSummary) add(version string) {
	if summary.ByVersion == nil {
		summary.ByVersion = make(map[string]int)
	}
	summary.ByVersion[version]++
}

// HomeSettingsPath returns the canonical per-user settings path without loading
// or resolving any values from it.
func HomeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".coragent", "settings.json"), nil
}

// ScrubLegacyExactPermissionRules removes unsafe unkeyed selectors from the raw
// home and project settings before environment placeholders are resolved.
func ScrubLegacyExactPermissionRules() error {
	return scrubDiscoveredPermissionRules(map[string]bool{legacyExactRuleVersion: true})
}

// ScrubAllExactPermissionRules removes every persisted exact selector after a
// fingerprint key is created or rotated. It runs before publishing the new key,
// so a scrub failure cannot leave stale selectors paired with that key.
func ScrubAllExactPermissionRules() error {
	return scrubDiscoveredPermissionRules(map[string]bool{
		legacyExactRuleVersion: true,
		keyedExactRuleVersion:  true,
	})
}

func scrubDiscoveredPermissionRules(versions map[string]bool) error {
	home, err := HomeSettingsPath()
	if err != nil {
		return err
	}
	for _, path := range []string{home, ProjectSettingsPath()} {
		summary, err := mutatePermissionRules(path, false, permissionRuleMutation{removeVersions: versions})
		if err != nil {
			return err
		}
		warnScrubbedPermissionRules(path, summary)
	}
	return nil
}

func warnScrubbedPermissionRules(path string, summary PermissionRuleScrubSummary) {
	for _, version := range []string{legacyExactRuleVersion, keyedExactRuleVersion} {
		count := summary.ByVersion[version]
		if count == 0 {
			continue
		}
		slog.Warn(
			"removed unsafe exact-call permission rules; rotate credentials that may have appeared in remembered calls",
			"path", path,
			"count", count,
			"version", version,
		)
	}
}

// FilterPermissionRulesAfterKeyReset removes exact selectors from caller-owned
// in-memory lists when a fresh or rotated key invalidates their identity.
func FilterPermissionRulesAfterKeyReset(entries []string) []string {
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		if exactRuleVersion(entry) == legacyExactRuleVersion || exactRuleVersion(entry) == keyedExactRuleVersion {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func appendPermissionRuleRaw(path string, allow bool, rule string) error {
	mutation := permissionRuleMutation{removeVersions: map[string]bool{legacyExactRuleVersion: true}}
	unsafeLegacyAppend := exactRuleVersion(rule) == legacyExactRuleVersion
	if !unsafeLegacyAppend {
		if allow {
			mutation.appendAllow = &rule
		} else {
			mutation.appendDeny = &rule
		}
	}
	summary, err := mutatePermissionRules(path, true, mutation)
	if err != nil {
		return err
	}
	warnScrubbedPermissionRules(path, summary)
	if unsafeLegacyAppend {
		return fmt.Errorf("refusing to persist unsafe %s exact-call permission rule", legacyExactRuleVersion)
	}
	return nil
}

func mutatePermissionRules(path string, create bool, mutation permissionRuleMutation) (PermissionRuleScrubSummary, error) {
	permissionSettingsWriteMu.Lock()
	defer permissionSettingsWriteMu.Unlock()

	var summary PermissionRuleScrubSummary
	top := make(map[string]json.RawMessage)
	mode := os.FileMode(0o644)
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return summary, fmt.Errorf("failed to update %s: settings must be a regular file", path)
		}
		mode = info.Mode().Perm()
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return summary, fmt.Errorf("failed to read %s: %w", path, readErr)
		}
		if err := json.Unmarshal(data, &top); err != nil {
			return summary, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		if top == nil {
			top = make(map[string]json.RawMessage)
		}
	case os.IsNotExist(err):
		if !create {
			return summary, nil
		}
	default:
		return summary, fmt.Errorf("failed to read %s: %w", path, err)
	}

	permissionObject := make(map[string]json.RawMessage)
	if raw, exists := top["permission"]; exists && strings.TrimSpace(string(raw)) != "null" {
		if err := json.Unmarshal(raw, &permissionObject); err != nil {
			return summary, fmt.Errorf("failed to parse %s permission settings: %w", path, err)
		}
		if permissionObject == nil {
			permissionObject = make(map[string]json.RawMessage)
		}
	}

	changed := false
	for _, field := range []string{"allow", "deny"} {
		raw, exists := permissionObject[field]
		if !exists || strings.TrimSpace(string(raw)) == "null" {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return summary, fmt.Errorf("failed to parse %s permission.%s: %w", path, field, err)
		}
		filtered := make([]json.RawMessage, 0, len(entries))
		for _, entry := range entries {
			var rule string
			if err := json.Unmarshal(entry, &rule); err == nil {
				version := exactRuleVersion(rule)
				if mutation.removeVersions[version] {
					summary.add(version)
					changed = true
					continue
				}
			}
			filtered = append(filtered, append(json.RawMessage(nil), entry...))
		}
		if changedField := len(filtered) != len(entries); changedField {
			encoded, err := json.Marshal(filtered)
			if err != nil {
				return summary, fmt.Errorf("failed to encode %s permission.%s: %w", path, field, err)
			}
			permissionObject[field] = encoded
		}
	}

	appendRule := func(field string, rule *string) error {
		if rule == nil {
			return nil
		}
		var entries []json.RawMessage
		if raw, exists := permissionObject[field]; exists && strings.TrimSpace(string(raw)) != "null" {
			if err := json.Unmarshal(raw, &entries); err != nil {
				return fmt.Errorf("failed to parse %s permission.%s: %w", path, field, err)
			}
		}
		encodedRule, err := json.Marshal(*rule)
		if err != nil {
			return err
		}
		entries = append(entries, encodedRule)
		encodedEntries, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		permissionObject[field] = encodedEntries
		changed = true
		return nil
	}
	if err := appendRule("allow", mutation.appendAllow); err != nil {
		return summary, err
	}
	if err := appendRule("deny", mutation.appendDeny); err != nil {
		return summary, err
	}
	if !changed {
		return summary, nil
	}

	permissionRaw, err := json.Marshal(permissionObject)
	if err != nil {
		return summary, fmt.Errorf("failed to encode permission settings: %w", err)
	}
	top["permission"] = permissionRaw
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return summary, fmt.Errorf("failed to encode settings: %w", err)
	}
	out = append(out, '\n')
	if err := atomicWriteSettings(path, out, mode); err != nil {
		return summary, err
	}
	return summary, nil
}

func exactRuleVersion(rule string) string {
	trimmed := strings.TrimSpace(rule)
	for _, version := range []string{legacyExactRuleVersion, keyedExactRuleVersion} {
		if strings.HasPrefix(trimmed, version+":") {
			return version
		}
	}
	return ""
}

func atomicWriteSettings(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("failed to create settings directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".settings-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary settings file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
	}()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("failed to preserve settings permissions: %w", err)
	}
	written, err := temporary.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write temporary settings: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("failed to write temporary settings: short write %d of %d bytes", written, len(data))
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary settings: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("failed to close temporary settings: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("failed to atomically replace %s: %w", path, err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("failed to open settings directory for sync: %w", err)
	}
	if err := directoryHandle.Sync(); err != nil {
		_ = directoryHandle.Close()
		return fmt.Errorf("failed to sync settings directory: %w", err)
	}
	if err := directoryHandle.Close(); err != nil {
		return fmt.Errorf("failed to close settings directory: %w", err)
	}
	return nil
}
