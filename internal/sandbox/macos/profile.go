// Package macos provides a Sandbox implementation using macOS sandbox-exec with
// Seatbelt profiles for kernel-level (ConfinementKernel) process isolation.
package macos

import (
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/blkcor/coragent/internal/sandbox"
)

// profileTemplate builds a Seatbelt profile in Scheme syntax. The strategy is:
//   - (allow file-read*) globally — read is non-destructive and path-based
//     restrictions on reads break easily across macOS sealed-volume/cryptex mounts
//   - (allow file-read* file-write*) only on workspace, tmp, and declared grants
//   - Network is denied by default (no allow network* rule)
var profileTemplate = template.Must(template.New("seatbelt").Parse(
	`(version 1)
(deny default)
(allow file-read*)
(allow process-exec)
(allow process-fork)
(allow sysctl-read)
{{if .Workspace}}(allow file-read* file-write* (subpath {{printf "%q" .Workspace}}))
{{end}}(allow file-read* file-write* (subpath {{printf "%q" .TmpDir}}))
{{range .Grants.AllowedWritePaths}}(allow file-read* file-write* (subpath {{printf "%q" .}}))
{{end}}`))

type profileData struct {
	Workspace string
	TmpDir    string
	Grants    sandbox.Grants
}

// resolve resolves symlinks to the canonical path. If resolution fails the
// original path is returned — the sandbox will deny the write, which is the
// safe default.
func resolve(path string) string {
	if path == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

// GenerateProfile produces a Seatbelt profile string from the given spec. File
// reads are allowed globally; writes are confined to spec.CWD, TmpDir, and the
// declared AllowedWritePaths. All write paths are resolved to their canonical
// form because Seatbelt resolves symlinks before evaluating subpath rules.
// Network access is denied by default.
func GenerateProfile(spec sandbox.CommandSpec) string {
	writePaths := make([]string, len(spec.Grants.AllowedWritePaths))
	for i, p := range spec.Grants.AllowedWritePaths {
		writePaths[i] = resolve(p)
	}

	data := profileData{
		Workspace: resolve(spec.CWD),
		TmpDir:    resolve(os.TempDir()),
		Grants: sandbox.Grants{
			AllowedWritePaths: writePaths,
		},
	}

	var b strings.Builder
	if err := profileTemplate.Execute(&b, data); err != nil {
		return "(version 1)\n(deny default)\n"
	}
	return b.String()
}
