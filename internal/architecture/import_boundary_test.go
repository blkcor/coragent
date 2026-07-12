package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/blkcor/coragent"

func TestFrontendImportsOnlyPublicHarnessSurface(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"tui", filepath.Join("cmd", "coragent")} {
		assertNoImports(t, filepath.Join(root, relative), func(path string) bool {
			return strings.HasPrefix(path, modulePath+"/internal/")
		})
	}
}

func TestHarnessDoesNotImportFrontend(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal", filepath.Join("pkg", "agent")} {
		assertNoImports(t, filepath.Join(root, relative), func(path string) bool {
			return path == modulePath+"/tui" || strings.HasPrefix(path, modulePath+"/tui/") ||
				path == modulePath+"/cmd/coragent" || strings.HasPrefix(path, modulePath+"/cmd/coragent/")
		})
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate import-boundary test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func assertNoImports(t *testing.T, root string, forbidden func(string) bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Errorf("decode import in %s: %v", path, err)
				continue
			}
			if forbidden(importPath) {
				t.Errorf("%s imports forbidden package %q", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
