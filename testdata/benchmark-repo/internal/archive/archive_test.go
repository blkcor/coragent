package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func makeArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "input.zip")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for path, content := range entries {
		entry, err := w.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestExtractValidNestedFile(t *testing.T) {
	destination := t.TempDir()
	if err := Extract(makeArchive(t, map[string]string{"nested/report.txt": "ok"}), destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "nested", "report.txt"))
	if err != nil || string(content) != "ok" {
		t.Fatalf("content = %q, %v", content, err)
	}
}

func TestExtractRejectsTraversalAndAbsolutePath(t *testing.T) {
	for _, name := range []string{"../escape.txt", filepath.Join(string(filepath.Separator), "absolute.txt")} {
		if err := Extract(makeArchive(t, map[string]string{name: "bad"}), t.TempDir()); err == nil {
			t.Fatalf("Extract accepted %q", name)
		}
	}
}

func TestExtractRejectsParentSymlinkAtCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := Extract(makeArchive(t, map[string]string{"linked/escape.txt": "bad"}), destination); err == nil {
		t.Fatal("Extract followed parent symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists: %v", err)
	}
}
