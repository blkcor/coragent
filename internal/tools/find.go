package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blkcor/coragent/internal/core"
)

// noiseDirs are directories skipped during a file-pattern walk so version-control
// internals and dependency caches do not drown out project structure. Phase 2
// ships this minimal default; making it configurable is a possible follow-up.
var noiseDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".idea":        true,
	".vscode":      true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"__pycache__":  true,
}

// FindFiles is the built-in that lists file paths matching a glob under a root,
// skipping common noise directories, in stable order. It is a pure file
// operation.
type FindFiles struct{}

func (FindFiles) Descriptor() core.Tool {
	return core.Tool{
		Name: "find_files",
		Description: "List file paths whose name matches a glob pattern under a root directory, " +
			"in stable order, skipping version-control and dependency directories.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern": {"type": "string", "description": "Glob to match against file names, e.g. '*.go'."},
				"root": {"type": "string", "description": "Root directory to search (default '.')."}
			},
			"required": ["pattern"]
		}`),
	}
}

func (FindFiles) RunsCommands() bool { return false }

func (FindFiles) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	pattern, ok := stringArg(args, "pattern")
	if !ok || pattern == "" {
		return "", fmt.Errorf("find_files: pattern is required")
	}
	// Validate the glob up front so a bad pattern is a clear error, not a silent
	// empty result.
	if _, err := filepath.Match(pattern, ""); err != nil {
		return "", fmt.Errorf("find_files: invalid pattern %q: %w", pattern, err)
	}

	root := "."
	if r, ok := stringArg(args, "root"); ok && r != "" {
		root = r
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("find_files: %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("find_files: %s is not a directory", root)
	}

	var matches []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && noiseDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if ok, _ := filepath.Match(pattern, d.Name()); ok {
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				rel = p
			}
			matches = append(matches, rel)
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("find_files: walking %s: %w", root, walkErr)
	}

	if len(matches) == 0 {
		return "no files matched", nil
	}
	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}
