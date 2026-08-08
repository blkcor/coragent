package macos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/sandbox"
)

func TestGenerateProfile_Structure(t *testing.T) {
	spec := sandbox.CommandSpec{CWD: "/tmp/workspace"}
	profile := GenerateProfile(spec)

	required := []string{
		"(version 1)",
		"(deny default)",
		"(allow file-read*)",
		"(allow process-exec)",
		"(allow process-fork)",
		"(allow sysctl-read)",
	}
	for _, r := range required {
		if !strings.Contains(profile, r) {
			t.Errorf("profile must contain %q:\n%s", r, profile)
		}
	}
}

func TestGenerateProfile_Workspace(t *testing.T) {
	ws := "/Users/test/project"
	spec := sandbox.CommandSpec{CWD: ws}
	profile := GenerateProfile(spec)

	expected := `(allow file-read* file-write* (subpath "/Users/test/project"))`
	if !strings.Contains(profile, expected) {
		t.Errorf("profile must contain workspace grant:\nwant: %s\ngot:\n%s", expected, profile)
	}
}

func TestGenerateProfile_NoWorkspace(t *testing.T) {
	spec := sandbox.CommandSpec{}
	profile := GenerateProfile(spec)

	if strings.Contains(profile, `(allow file-read* file-write* (subpath "")`) {
		t.Error("profile must not emit empty workspace grant")
	}
}

func TestGenerateProfile_TmpDir(t *testing.T) {
	spec := sandbox.CommandSpec{}
	profile := GenerateProfile(spec)

	// Seatbelt resolves symlinks, so /tmp becomes /private/tmp.
	tmpDir := os.TempDir()
	resolved, err := filepath.EvalSymlinks(tmpDir)
	if err == nil {
		tmpDir = resolved
	}
	expected := `(subpath "` + tmpDir + `")`
	if !strings.Contains(profile, expected) {
		t.Errorf("profile must contain tmp dir:\nwant: %s\ngot:\n%s", expected, profile)
	}
}

func TestGenerateProfile_Grants_WritePaths(t *testing.T) {
	spec := sandbox.CommandSpec{
		Grants: sandbox.Grants{
			AllowedWritePaths: []string{"/data/output", "/logs"},
		},
	}
	profile := GenerateProfile(spec)

	for _, p := range spec.Grants.AllowedWritePaths {
		expected := `(allow file-read* file-write* (subpath "` + p + `"))`
		if !strings.Contains(profile, expected) {
			t.Errorf("profile must contain write grant %q:\n%s", p, profile)
		}
	}
}

func TestGenerateProfile_DefaultDenyNetwork(t *testing.T) {
	spec := sandbox.CommandSpec{}
	profile := GenerateProfile(spec)

	if strings.Contains(profile, "network-outbound") || strings.Contains(profile, "network-inbound") {
		t.Error("profile must deny network by default")
	}
}

func TestGenerateProfile_GlobalReadAllowed(t *testing.T) {
	spec := sandbox.CommandSpec{}
	profile := GenerateProfile(spec)

	// (allow file-read*) without path qualifier means global read access.
	if !strings.Contains(profile, "(allow file-read*)\n") {
		t.Error("profile must allow global file-read*")
	}
}

func TestGenerateProfile_CombinedGrants(t *testing.T) {
	spec := sandbox.CommandSpec{
		CWD: "/workspace",
		Grants: sandbox.Grants{
			AllowedWritePaths: []string{"/tmp/output"},
		},
	}
	profile := GenerateProfile(spec)

	checks := []string{
		`(subpath "/workspace")`,
		`(subpath "/tmp/output")`,
	}
	for _, c := range checks {
		if !strings.Contains(profile, c) {
			t.Errorf("profile missing expected path %q:\n%s", c, profile)
		}
	}
}
