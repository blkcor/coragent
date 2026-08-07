package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestFileService(t *testing.T) (FileService, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return NewFileService(w), dir
}

func mustWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFileServiceReadAndIdentity(t *testing.T) {
	fsvc, dir := newTestFileService(t)
	mustWriteFile(t, dir, "hello.txt", "hello world")
	mustWriteFile(t, dir, "sub/note.txt", "nested content")

	f, clean, err := fsvc.Read("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" || clean != "hello.txt" {
		t.Fatalf("Read = %q, %q", string(data), clean)
	}

	identity, err := fsvc.Identity("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("hello world"))
	if identity != hex.EncodeToString(h[:]) {
		t.Fatalf("Identity = %s, want %s", identity, hex.EncodeToString(h[:]))
	}
}

func TestFileServiceReadRejectsEscape(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	for _, name := range []string{"../outside", "/etc/passwd"} {
		if _, _, err := fsvc.Read(name); !errors.Is(err, ErrEscape) {
			t.Errorf("Read(%q) = %v, want ErrEscape", name, err)
		}
	}
}

func TestFileServiceReadRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	fsvc := NewFileService(w)

	if _, _, err := fsvc.Read("link"); !errors.Is(err, ErrEscape) {
		t.Fatalf("Read(symlink) = %v, want ErrEscape", err)
	}
}

func TestFileServiceReadFileNotFound(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	if _, _, err := fsvc.Read("missing.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read(missing) = %v, want ErrNotExist", err)
	}
}

func TestFileServiceReadPermissionError(t *testing.T) {
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "unreadable.txt")
	if err := os.WriteFile(unreadable, []byte("must-not-return"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	fsvc := NewFileService(w)

	f, _, err := fsvc.Read("unreadable.txt")
	if err == nil {
		_ = f.Close()
		t.Skip("test process can read a mode-000 file")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Read(unreadable) = %v, want ErrPermission", err)
	}
}

func TestFileServiceListAndSearchValidatePath(t *testing.T) {
	fsvc, dir := newTestFileService(t)
	mustWriteFile(t, dir, "a.txt", "a")
	mustWriteFile(t, dir, "sub/b.txt", "b")

	walkFS, root, err := fsvc.List("sub")
	if err != nil {
		t.Fatal(err)
	}
	if root != "sub" {
		t.Fatalf("List clean = %q", root)
	}
	var entries []string
	if err := fs.WalkDir(walkFS, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name != root {
			entries = append(entries, name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "sub/b.txt" {
		t.Fatalf("List walk entries = %q", entries)
	}

	walkFS, root, err = fsvc.Search(".")
	if err != nil {
		t.Fatal(err)
	}
	if root != "." {
		t.Fatalf("Search clean = %q", root)
	}
	entries = nil
	if err := fs.WalkDir(walkFS, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name != root {
			entries = append(entries, name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0] != "a.txt" || entries[1] != "sub" || entries[2] != "sub/b.txt" {
		t.Fatalf("Search walk entries = %q", entries)
	}
}

func TestFileServiceListRejectsEscape(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	for _, name := range []string{"../outside", "/etc"} {
		if _, _, err := fsvc.List(name); !errors.Is(err, ErrEscape) {
			t.Errorf("List(%q) = %v, want ErrEscape", name, err)
		}
	}
}

func TestFileServiceListFileNotFound(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	if _, _, err := fsvc.List("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("List(missing) = %v, want ErrNotExist", err)
	}
}

func TestFileServiceSearchRejectsEscape(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	for _, name := range []string{"../outside", "/etc"} {
		if _, _, err := fsvc.Search(name); !errors.Is(err, ErrEscape) {
			t.Errorf("Search(%q) = %v, want ErrEscape", name, err)
		}
	}
}

func TestFileServiceSearchFileNotFound(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	if _, _, err := fsvc.Search("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Search(missing) = %v, want ErrNotExist", err)
	}
}

func TestFileServiceWriteWritesAndVerifiesSHA256(t *testing.T) {
	fsvc, dir := newTestFileService(t)
	const name = "target.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256Hex([]byte("world\n"))
	gotHash, err := fsvc.Write(name, []byte("world\n"), wantHash)
	if err != nil {
		t.Fatalf("Write = %v, want nil", err)
	}
	if gotHash != wantHash {
		t.Fatalf("Write returned hash %s, want %s", gotHash, wantHash)
	}
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "world\n" {
		t.Fatalf("content = %q, want %q", content, "world\n")
	}
}

func TestFileServiceWriteRejectsWrongSHA256(t *testing.T) {
	fsvc, dir := newTestFileService(t)
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fsvc.Write("x.txt", []byte("world\n"), "abc123")
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("Write with wrong SHA256 = %v, want SHA256 mismatch", err)
	}
}

func TestFileServiceWriteRejectsEscapeAndSymlink(t *testing.T) {
	fsvc, dir := newTestFileService(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", "link"} {
		if _, err := fsvc.Write(name, []byte("x"), ""); !errors.Is(err, ErrEscape) {
			t.Errorf("Write(%q) = %v, want ErrEscape", name, err)
		}
	}
}

func TestFileServiceCleanRejectsEscape(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	for _, name := range []string{"../x", "/absolute"} {
		if _, err := fsvc.Clean(name); !errors.Is(err, ErrEscape) {
			t.Errorf("Clean(%q) = %v, want ErrEscape", name, err)
		}
	}
}

func TestFileServiceIdentityFileNotFound(t *testing.T) {
	fsvc, _ := newTestFileService(t)
	if _, err := fsvc.Identity("missing.txt"); err == nil {
		t.Fatal("Identity(missing) returned nil error")
	}
}
