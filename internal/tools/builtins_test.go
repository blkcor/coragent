package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- read_file --------------------------------------------------------------

func TestReadFileWholeAndWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	whole, err := ReadFile{}.Execute(context.Background(), map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("read whole: %v", err)
	}
	if !strings.Contains(whole, "     1\tone") || !strings.Contains(whole, "     4\tfour") {
		t.Errorf("whole read must be line-referenced, got:\n%s", whole)
	}

	win, err := ReadFile{}.Execute(context.Background(), map[string]interface{}{"path": path, "offset": 2, "limit": 2})
	if err != nil {
		t.Fatalf("read window: %v", err)
	}
	if !strings.Contains(win, "     2\ttwo") || !strings.Contains(win, "     3\tthree") {
		t.Errorf("window must return lines 2-3, got:\n%s", win)
	}
	if strings.Contains(win, "one") || strings.Contains(win, "four") {
		t.Errorf("window must exclude lines outside it, got:\n%s", win)
	}
}

func TestReadFileErrorsDumpNoBytes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.WriteFile(bin, []byte{0x00, 0x01, 0x02, 'a'}, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := map[string]map[string]interface{}{
		"missing":     {"path": filepath.Join(dir, "nope.txt")},
		"directory":   {"path": dir},
		"binary":      {"path": bin},
		"offset-past": {"path": mustWrite(t, dir, "s.txt", "x\n"), "offset": 99},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := ReadFile{}.Execute(context.Background(), args)
			if err == nil {
				t.Fatalf("expected error, got output %q", out)
			}
			if out != "" {
				t.Errorf("no raw bytes may be returned on error, got %q", out)
			}
		})
	}
}

// --- write_file -------------------------------------------------------------

func TestWriteFileCreatesParentsAndReplaces(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "x", "y", "z.txt")

	out, err := WriteFile{}.Execute(context.Background(), map[string]interface{}{
		"path": nested, "content": "hello", "create_parents": true,
	})
	if err != nil {
		t.Fatalf("write with parents: %v", err)
	}
	if strings.Contains(out, "hello") {
		t.Errorf("confirmation must not echo content, got %q", out)
	}
	if got := readFile(t, nested); got != "hello" {
		t.Errorf("file content = %q, want hello", got)
	}

	if _, err := (WriteFile{}).Execute(context.Background(), map[string]interface{}{"path": nested, "content": "world"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got := readFile(t, nested); got != "world" {
		t.Errorf("replace failed, content = %q", got)
	}
}

// --- edit_file --------------------------------------------------------------

func TestEditFileUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	path := mustWrite(t, dir, "e.txt", "alpha beta gamma")
	if _, err := (EditFile{}).Execute(context.Background(), map[string]interface{}{
		"path": path, "old_string": "beta", "new_string": "BETA",
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := readFile(t, path); got != "alpha BETA gamma" {
		t.Errorf("content = %q", got)
	}
}

func TestEditFileAmbiguousLeavesFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	const original = "x x x"
	path := mustWrite(t, dir, "e.txt", original)

	_, err := EditFile{}.Execute(context.Background(), map[string]interface{}{
		"path": path, "old_string": "x", "new_string": "y",
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("want ambiguous error reporting count 3, got %v", err)
	}
	if got := readFile(t, path); got != original {
		t.Errorf("file must be byte-for-byte unchanged, got %q", got)
	}
}

func TestEditFileReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := mustWrite(t, dir, "e.txt", "x x x")
	if _, err := (EditFile{}).Execute(context.Background(), map[string]interface{}{
		"path": path, "old_string": "x", "new_string": "y", "replace_all": true,
	}); err != nil {
		t.Fatalf("replace_all: %v", err)
	}
	if got := readFile(t, path); got != "y y y" {
		t.Errorf("content = %q, want 'y y y'", got)
	}
}

func TestEditFileMissingAndNoOpRejectedUnchanged(t *testing.T) {
	dir := t.TempDir()
	const original = "hello world"

	t.Run("missing target", func(t *testing.T) {
		path := mustWrite(t, dir, "m.txt", original)
		_, err := EditFile{}.Execute(context.Background(), map[string]interface{}{
			"path": path, "old_string": "absent", "new_string": "x",
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("want not-found error, got %v", err)
		}
		if readFile(t, path) != original {
			t.Errorf("file must be unchanged")
		}
	})

	t.Run("no-op", func(t *testing.T) {
		path := mustWrite(t, dir, "n.txt", original)
		_, err := EditFile{}.Execute(context.Background(), map[string]interface{}{
			"path": path, "old_string": "hello", "new_string": "hello",
		})
		if err == nil || !strings.Contains(err.Error(), "identical") {
			t.Fatalf("want no-op error, got %v", err)
		}
		if readFile(t, path) != original {
			t.Errorf("file must be unchanged")
		}
	})
}

// --- run_command ------------------------------------------------------------

func TestShellCombinedOutputAndExitCode(t *testing.T) {
	out, err := ShellCommand{}.Execute(context.Background(), map[string]interface{}{
		"command": "echo out; echo err 1>&2",
	})
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	if !strings.Contains(out, "out") || !strings.Contains(out, "err") {
		t.Errorf("combined stdout+stderr expected, got %q", out)
	}
	if !strings.Contains(out, "exit code: 0") {
		t.Errorf("exit code must be reported, got %q", out)
	}
}

func TestShellNonZeroExitIsError(t *testing.T) {
	out, err := ShellCommand{}.Execute(context.Background(), map[string]interface{}{
		"command": "echo partial; exit 3",
	})
	if err == nil {
		t.Fatalf("non-zero exit must return an error")
	}
	if !strings.Contains(out, "partial") || !strings.Contains(out, "exit code: 3") {
		t.Errorf("error must carry captured output and exit code, got %q", out)
	}
}

func TestShellTimeoutKillsWithPartialOutput(t *testing.T) {
	start := time.Now()
	out, err := ShellCommand{}.Execute(context.Background(), map[string]interface{}{
		"command": "echo before; sleep 5", "timeout_ms": 200,
	})
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("result must note the timeout, got %q", out)
	}
	if elapsed > 3*time.Second {
		t.Errorf("timed-out command was not killed promptly (took %s) — possible orphan", elapsed)
	}
}

func TestShellCancellationStopsChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()

	start := time.Now()
	_, err := ShellCommand{}.Execute(ctx, map[string]interface{}{"command": "sleep 5"})
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("want cancellation error, got %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("cancelled command was not stopped promptly (took %s)", elapsed)
	}
}

// --- search_content ---------------------------------------------------------

func TestSearchContentMatchesAndNoMatches(t *testing.T) {
	if !haveRg() {
		t.Skip("rg not installed; content-search backend unavailable")
	}
	dir := t.TempDir()
	mustWrite(t, dir, "a.go", "package main\n// needle here\n")
	mustWrite(t, dir, "b.txt", "no marker\n")

	out, err := SearchContent{}.Execute(context.Background(), map[string]interface{}{
		"pattern": "needle", "path": dir,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, ":2:") {
		t.Errorf("want file:line:match result, got %q", out)
	}

	none, err := SearchContent{}.Execute(context.Background(), map[string]interface{}{
		"pattern": "zzzznotpresent", "path": dir,
	})
	if err != nil {
		t.Fatalf("no-match search must not error: %v", err)
	}
	if none != "no matches" {
		t.Errorf("want 'no matches', got %q", none)
	}
}

func TestSearchContentGlobScoping(t *testing.T) {
	if !haveRg() {
		t.Skip("rg not installed")
	}
	dir := t.TempDir()
	mustWrite(t, dir, "a.go", "target\n")
	mustWrite(t, dir, "a.txt", "target\n")

	out, err := SearchContent{}.Execute(context.Background(), map[string]interface{}{
		"pattern": "target", "path": dir, "glob": "*.go",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "a.go") || strings.Contains(out, "a.txt") {
		t.Errorf("glob must scope to *.go only, got %q", out)
	}
}

// --- find_files -------------------------------------------------------------

func TestFindFilesStableOrderSkipsNoise(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "b.go", "")
	mustWrite(t, dir, "a.go", "")
	mustWriteNested(t, dir, filepath.Join("pkg", "c.go"), "")
	mustWriteNested(t, dir, filepath.Join(".git", "hooks.go"), "")

	out, err := FindFiles{}.Execute(context.Background(), map[string]interface{}{
		"pattern": "*.go", "root": dir,
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	got := strings.Split(out, "\n")
	want := []string{"a.go", "b.go", filepath.Join("pkg", "c.go")}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v (.git must be skipped)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stable sorted order expected: got %v, want %v", got, want)
		}
	}
}

func TestFindFilesNoMatchAndBadRoot(t *testing.T) {
	dir := t.TempDir()
	out, err := FindFiles{}.Execute(context.Background(), map[string]interface{}{"pattern": "*.zzz", "root": dir})
	if err != nil {
		t.Fatalf("no-match must not error: %v", err)
	}
	if out != "no files matched" {
		t.Errorf("want 'no files matched', got %q", out)
	}

	if _, err := (FindFiles{}).Execute(context.Background(), map[string]interface{}{
		"pattern": "*.go", "root": filepath.Join(dir, "does-not-exist"),
	}); err == nil {
		t.Errorf("bad root must return an error")
	}
}

// --- helpers ----------------------------------------------------------------

func mustWrite(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustWriteNested(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func haveRg() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}
