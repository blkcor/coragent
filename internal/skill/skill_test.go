package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSKILL_Valid(t *testing.T) {
	content := `---
name: my-skill
description: A test skill
type: project
---

This is the skill body.
`
	s, err := ParseSKILL([]byte(content), "my-skill", SourceProject, "test/SKILL.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "my-skill" {
		t.Errorf("expected name 'my-skill', got %q", s.Name)
	}
	if s.Description != "A test skill" {
		t.Errorf("expected description, got %q", s.Description)
	}
	if s.Type != "project" {
		t.Errorf("expected type 'project', got %q", s.Type)
	}
	if s.Body != "This is the skill body." {
		t.Errorf("expected body, got %q", s.Body)
	}
	if s.Source != SourceProject {
		t.Errorf("expected source 'project', got %q", s.Source)
	}
}

func TestParseSKILL_NoFrontmatter(t *testing.T) {
	content := `Just a markdown file with no frontmatter.`
	s, err := ParseSKILL([]byte(content), "nosettings", SourceUser, "test/SKILL.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "nosettings" {
		t.Errorf("expected directory name as fallback, got %q", s.Name)
	}
	if s.Type != "user" {
		t.Errorf("expected default type 'user', got %q", s.Type)
	}
	if s.Body != "Just a markdown file with no frontmatter." {
		t.Errorf("expected full content as body, got %q", s.Body)
	}
}

func TestParseSKILL_NameOnly(t *testing.T) {
	content := `---
name: minimal
---
Body text.
`
	s, err := ParseSKILL([]byte(content), "myskill", SourceUser, "test/SKILL.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "myskill" {
		t.Errorf("expected directory name 'myskill', got %q", s.Name)
	}
	if s.Type != "user" {
		t.Errorf("expected default type 'user', got %q", s.Type)
	}
	if s.Description != "" {
		t.Errorf("expected empty description, got %q", s.Description)
	}
}

func TestParseSKILL_InvalidYAML(t *testing.T) {
	content := "---\nname: [unclosed\n---\nBody."
	_, err := ParseSKILL([]byte(content), "test", SourceUser, "test/SKILL.md")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseSKILL_EmptyBody(t *testing.T) {
	content := "---\nname: empty-body\n---\n"
	s, err := ParseSKILL([]byte(content), "empty-body", SourceUser, "test/SKILL.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Body != "" {
		t.Errorf("expected empty body, got %q", s.Body)
	}
}

func TestValidate_EmptyName(t *testing.T) {
	s := &Skill{Name: "  ", Body: "x"}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidate_ReservedName(t *testing.T) {
	s := &Skill{Name: "bash", Body: "x"}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for reserved name 'bash'")
	}
}

func TestValidate_Valid(t *testing.T) {
	s := &Skill{Name: "my-skill", Body: "x"}
	err := Validate(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseInvocations(t *testing.T) {
	reg := NewRegistry()
	reg.skills = []*Skill{{Name: "linter", Body: "lint instructions"}}
	reg.byName["linter"] = reg.skills[0]

	cleaned, names := ParseInvocations("/linter fix this code", reg)
	if len(names) != 1 || names[0] != "linter" {
		t.Errorf("expected [linter], got %v", names)
	}
	if cleaned != "fix this code" {
		t.Errorf("expected 'fix this code', got %q", cleaned)
	}
}

func TestParseInvocations_MultipleSkills(t *testing.T) {
	reg := NewRegistry()
	reg.skills = []*Skill{
		{Name: "linter", Body: "lint"},
		{Name: "formatter", Body: "fmt"},
	}
	reg.byName["linter"] = reg.skills[0]
	reg.byName["formatter"] = reg.skills[1]

	cleaned, names := ParseInvocations("/linter /formatter fix this", reg)
	if len(names) != 2 {
		t.Errorf("expected 2 skills, got %v", names)
	}
	if cleaned != "fix this" {
		t.Errorf("expected 'fix this', got %q", cleaned)
	}
}

func TestParseInvocations_UnknownSkill(t *testing.T) {
	reg := NewRegistry()
	reg.skills = []*Skill{{Name: "linter", Body: "lint"}}
	reg.byName["linter"] = reg.skills[0]

	cleaned, names := ParseInvocations("/unknown do something", reg)
	if len(names) != 0 {
		t.Errorf("expected no skills, got %v", names)
	}
	if cleaned != "/unknown do something" {
		t.Errorf("expected full input preserved, got %q", cleaned)
	}
}

func TestParseInvocations_NilRegistry(t *testing.T) {
	cleaned, names := ParseInvocations("/linter fix this", nil)
	if len(names) != 0 {
		t.Errorf("expected no skills with nil registry, got %v", names)
	}
	if cleaned != "/linter fix this" {
		t.Errorf("expected input unchanged, got %q", cleaned)
	}
}

func TestParseInvocations_EmptyRegistry(t *testing.T) {
	reg := NewRegistry()
	cleaned, names := ParseInvocations("/linter fix this", reg)
	if len(names) != 0 {
		t.Errorf("expected no skills with empty registry, got %v", names)
	}
	if cleaned != "/linter fix this" {
		t.Errorf("expected input unchanged, got %q", cleaned)
	}
}

func TestSkillBodies(t *testing.T) {
	reg := NewRegistry()
	reg.skills = []*Skill{{Name: "linter", Body: "lint body"}}
	reg.byName["linter"] = reg.skills[0]

	bodies := SkillBodies([]string{"linter"}, reg)
	if len(bodies) != 1 || bodies[0] != "lint body" {
		t.Errorf("expected ['lint body'], got %v", bodies)
	}
}

func TestRegistryLoad_TempDir(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: my-skill\ndescription: A skill\n---\nBody text.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := Load("", dir)
	if reg.Len() != 1 {
		t.Fatalf("expected 1 skill, got %d", reg.Len())
	}
	s := reg.Lookup("my-skill")
	if s == nil {
		t.Fatal("expected to find 'my-skill'")
	}
	if s.Description != "A skill" {
		t.Errorf("expected description 'A skill', got %q", s.Description)
	}
}

func TestRegistryLoad_ProjectOverridesUser(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	writeSkill(t, userDir, "shared", "user version", "user body")
	writeSkill(t, projectDir, "shared", "project version", "project body")

	reg := Load(userDir, projectDir)
	skills := reg.List()
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Description != "project version" {
		t.Errorf("expected project version to override, got %q", skills[0].Description)
	}
}

func TestRegistryLoad_MissingRoots(t *testing.T) {
	reg := Load("/nonexistent/path/12345", "/another/nonexistent/67890")
	if reg.Len() != 0 {
		t.Errorf("expected empty registry for missing roots, got %d skills", reg.Len())
	}
}

func TestHandler_Descriptor(t *testing.T) {
	s := &Skill{Name: "test", Description: "test desc", Body: "body", Type: "user"}
	h := NewHandler(s)
	desc := h.Descriptor()
	if desc.Name != "test" {
		t.Errorf("expected name 'test', got %q", desc.Name)
	}
	if desc.Description != "test desc" {
		t.Errorf("expected description 'test desc', got %q", desc.Description)
	}
}

func TestHandler_Execute(t *testing.T) {
	s := &Skill{Name: "test", Body: "skill body content"}
	h := NewHandler(s)
	result, err := h.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "skill body content" {
		t.Errorf("expected 'skill body content', got %q", result)
	}
}

func writeSkill(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
