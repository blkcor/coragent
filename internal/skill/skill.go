// Package skill discovers, loads, and manages skill definitions from the
// filesystem. Each skill is a directory containing an SKILL.md file with YAML
// frontmatter and a markdown body.
package skill

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is a parsed skill definition ready for registration.
type Skill struct {
	Name        string
	Description string
	Type        string
	Body        string
	Source      SkillSource
	Path        string
}

// SkillSource records which root a skill was loaded from.
type SkillSource string

const (
	SourceUser    SkillSource = "user"
	SourceProject SkillSource = "project"
)

// frontmatter is the parsed YAML header of an SKILL.md file.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
}

// ReservedToolNames is the set of built-in tool names that skills cannot use.
var ReservedToolNames = map[string]bool{
	"read_file":      true,
	"write_file":     true,
	"edit_file":      true,
	"bash":           true,
	"search_content": true,
	"find_files":     true,
	"task":           true,
}

// ParseSKILL parses an SKILL.md file's raw content and returns a Skill.
// dirName is used as the fallback name when frontmatter omits it. sourcePath
// identifies the file for error messages.
func ParseSKILL(content []byte, dirName string, source SkillSource, sourcePath string) (*Skill, error) {
	fm, body, err := splitFrontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("skill %q (%s): %w", dirName, sourcePath, err)
	}

	name := dirName
	if fm.Name != "" {
		name = fm.Name
	}
	if name == "" {
		return nil, fmt.Errorf("skill at %s: empty name (no frontmatter name and no directory name)", sourcePath)
	}

	skillType := fm.Type
	if skillType == "" {
		skillType = "user"
	}

	description := fm.Description

	return &Skill{
		Name:        name,
		Description: description,
		Type:        skillType,
		Body:        strings.TrimSpace(body),
		Source:      source,
		Path:        sourcePath,
	}, nil
}

// Validate checks a parsed Skill against registration rules. It returns an
// error for empty names, names that collide with reserved built-in tool names,
// or other invalid states.
func Validate(s *Skill) error {
	if s == nil {
		return fmt.Errorf("skill: nil skill")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("skill: empty name")
	}
	if ReservedToolNames[s.Name] {
		return fmt.Errorf("skill %q: name collides with a reserved built-in tool name", s.Name)
	}
	return nil
}

// splitFrontmatter extracts YAML frontmatter and body from markdown content.
// Frontmatter is delimited by --- fences at the very start of the file.
// Returns zero-value frontmatter and empty body when no frontmatter is present.
func splitFrontmatter(content string) (frontmatter, string, error) {
	var fm frontmatter

	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return fm, content, nil
	}

	// Strip the opening ---
	rest, found := strings.CutPrefix(content, "---\n")
	if !found {
		rest, _ = strings.CutPrefix(content, "---\r\n")
	}

	// Find closing ---
	endIdx := strings.Index(rest, "\n---\n")
	if endIdx == -1 {
		endIdx = strings.Index(rest, "\n---\r\n")
	}
	if endIdx == -1 {
		return fm, content, fmt.Errorf("unclosed YAML frontmatter: opening --- without closing ---")
	}

	yamlBlock := rest[:endIdx]
	bodyStart := endIdx + 5 // len("\n---\n")

	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return fm, content, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	return fm, rest[bodyStart:], nil
}
