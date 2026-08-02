package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanRejectsEscape(t *testing.T) {
	w, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("close workspace: %v", err)
		}
	})
	for _, name := range []string{"../x", "/absolute"} {
		if _, err := w.Clean(name); !errors.Is(err, ErrEscape) {
			t.Errorf("Clean(%q) = %v", name, err)
		}
	}
}

func TestOpenFileRejectsOutsideSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("close workspace: %v", err)
		}
	})
	if _, _, err := w.OpenFile("link"); !errors.Is(err, ErrEscape) {
		t.Fatalf("OpenFile = %v", err)
	}
}

func TestOpenFileAndStatRejectInWorkspaceSymlinkAlias(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=must-not-open"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".env", filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if file, _, err := w.OpenFile("AGENTS.md"); !errors.Is(err, ErrEscape) {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("OpenFile alias = %v", err)
	}
	if _, _, err := w.Stat("AGENTS.md"); !errors.Is(err, ErrEscape) {
		t.Fatalf("Stat alias = %v", err)
	}
}

func TestOpenFileRejectsSymlinkDirectoryComponent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "file.txt"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if file, _, err := w.OpenFile("alias/file.txt"); !errors.Is(err, ErrEscape) {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("OpenFile directory alias = %v", err)
	}
}

func TestPreparedPathFailsClosedAfterSymlinkReplacement(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(inside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("must-not-open"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("close workspace: %v", err)
		}
	})
	prepared, err := w.Clean("target.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(inside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, inside); err != nil {
		t.Fatal(err)
	}
	if file, _, err := w.OpenFile(prepared); !errors.Is(err, ErrEscape) {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("replacement OpenFile = %v", err)
	}
}
