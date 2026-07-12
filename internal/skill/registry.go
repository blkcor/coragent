package skill

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Registry holds loaded skills in stable order.
type Registry struct {
	skills []*Skill
	byName map[string]*Skill
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]*Skill),
	}
}

// Load loads skills from both roots, with project overriding user for
// same-named skills. It returns a populated Registry ready for use. Malformed
// skills are logged and skipped; the remaining valid skills still load.
func Load(userRoot, projectRoot string) *Registry {
	r := NewRegistry()

	var projectSkills, userSkills []*Skill

	if projectRoot != "" {
		projectSkills = loadFromRoot(projectRoot, SourceProject)
	}
	if userRoot != "" {
		userSkills = loadFromRoot(userRoot, SourceUser)
	}

	if len(projectSkills) > 0 || len(userSkills) > 0 {
		slog.Info("skill: scanning roots",
			"project_root", projectRoot,
			"project_candidates", len(projectSkills),
			"user_root", userRoot,
			"user_candidates", len(userSkills),
		)
	}

	// Project skills first, then user skills. Project overrides same-named user.
	seen := make(map[string]struct{})
	for _, s := range projectSkills {
		if err := Validate(s); err != nil {
			slog.Warn("skill: skipping invalid project skill", "path", s.Path, "error", err)
			continue
		}
		if _, exists := seen[s.Name]; exists {
			slog.Warn("skill: skipping duplicate project skill", "name", s.Name, "path", s.Path)
			continue
		}
		seen[s.Name] = struct{}{}
		r.skills = append(r.skills, s)
		r.byName[s.Name] = s
	}

	for _, s := range userSkills {
		if err := Validate(s); err != nil {
			slog.Warn("skill: skipping invalid user skill", "path", s.Path, "error", err)
			continue
		}
		if _, exists := seen[s.Name]; exists {
			continue // project overrides user
		}
		if _, exists := r.byName[s.Name]; exists {
			continue
		}
		seen[s.Name] = struct{}{}
		r.skills = append(r.skills, s)
		r.byName[s.Name] = s
	}

	// Sort: project before user, alphabetical within each group.
	sort.SliceStable(r.skills, func(i, j int) bool {
		si, sj := r.skills[i], r.skills[j]
		if si.Source != sj.Source {
			return si.Source == SourceProject
		}
		return si.Name < sj.Name
	})

	if len(r.skills) > 0 {
		names := make([]string, len(r.skills))
		for i, s := range r.skills {
			names[i] = s.Name
		}
		slog.Info("skill: registry ready", "count", len(r.skills), "skills", names)
	}

	return r
}

// List returns all loaded skills in stable order.
func (r *Registry) List() []*Skill {
	return r.skills
}

// Lookup returns the skill with the given name, or nil if not found.
func (r *Registry) Lookup(name string) *Skill {
	return r.byName[name]
}

// Len returns the number of loaded skills.
func (r *Registry) Len() int {
	return len(r.skills)
}

// loadFromRoot walks a root directory and parses every SKILL.md found.
func loadFromRoot(root string, source SkillSource) []*Skill {
	var skills []*Skill

	info, err := os.Stat(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("skill: cannot access skill root", "path", root, "error", err)
		}
		return nil
	}
	if !info.IsDir() {
		slog.Warn("skill: skill root is not a directory", "path", root)
		return nil
	}

	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("skill: error walking skill root", "path", path, "error", err)
			return nil
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "skill.md") {
			dirName := filepath.Base(filepath.Dir(path))
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				slog.Warn("skill: cannot read SKILL.md", "path", path, "error", readErr)
				return nil
			}
			skill, parseErr := ParseSKILL(content, dirName, source, path)
			if parseErr != nil {
				slog.Warn("skill: failed to parse SKILL.md", "path", path, "error", parseErr)
				return nil
			}
			skills = append(skills, skill)
		}
		return nil
	})

	return skills
}

// DefaultUserRoot returns the default user skill root path.
func DefaultUserRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".coragent", "skills")
}

// DefaultProjectRoot returns the default project skill root path.
func DefaultProjectRoot() string {
	return filepath.Join(".coragent", "skills")
}

// RootsFromSettings returns the resolved user and project skill root paths from
// the given setting values. Empty strings fall back to defaults.
func RootsFromSettings(userSetting, projectSetting string) (userRoot, projectRoot string) {
	userRoot = userSetting
	if userRoot == "" {
		userRoot = DefaultUserRoot()
	}
	projectRoot = projectSetting
	if projectRoot == "" {
		projectRoot = DefaultProjectRoot()
	}
	return userRoot, projectRoot
}

// ParseError describes a skill that failed to parse.
type ParseError struct {
	Path  string
	Cause error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("skill %q: %v", e.Path, e.Cause)
}

func (e *ParseError) Unwrap() error {
	return e.Cause
}
