package tools_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

func patchArgsJSON(path, target, replacement string) json.RawMessage {
	m := map[string]string{"path": path, "target": target, "replacement": replacement}
	b, _ := json.Marshal(m)
	return json.RawMessage(b)
}

func setupPatch(t *testing.T) (string, action.Tool, *action.Broker) {
	t.Helper()
	dir := t.TempDir()
	mustWrite := func(name, content string) {
		t.Helper()
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("a.go", "package sample\n\nfunc Alpha() {}\nfunc Beta() {}\nfunc Gamma() {}\n")
	mustWrite(".env", "TOKEN=do-not-return\n")
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	catalog := tools.NewCatalog(workspace.NewFileService(w), dataproj.New())
	b, err := action.NewBroker(catalog...)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range catalog {
		if tool.Definition().Name == "patch" {
			return dir, tool, b
		}
	}
	t.Fatal("patch tool not found in catalog")
	return dir, nil, nil
}

func preparePatch(t *testing.T, tool action.Tool, args json.RawMessage) (*action.PreparedPatch, error) {
	t.Helper()
	prepared, err := tool.Prepare(context.Background(), args)
	if err != nil {
		return nil, err
	}
	if prepared.Patch == nil {
		t.Fatal("Prepared.Patch is nil")
	}
	if prepared.Tool != "patch" {
		t.Fatalf("Prepared.Tool = %q, want %q", prepared.Tool, "patch")
	}
	if len(prepared.Effects) != 1 || prepared.Effects[0] != action.EffectWrite {
		t.Fatalf("Prepared.Effects = %v, want [EffectWrite]", prepared.Effects)
	}
	if !regexp.MustCompile(`^req-[0-9a-f]{32}$`).MatchString(prepared.Patch.RequestID) {
		t.Fatalf("RequestID = %q, want req-<hex32>", prepared.Patch.RequestID)
	}
	if prepared.Patch.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero")
	}
	return prepared.Patch, nil
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func sha256Str(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// --- replace range ---

func TestPatchReplaceRange(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3-L4", "func A() {}\nfunc B() {}"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "a.go" {
		t.Fatalf("Path = %q", p.Path)
	}
	want := "package sample\n\nfunc A() {}\nfunc B() {}\nfunc Gamma() {}\n"
	if p.ExpectedSHA256 != sha256Str(want) {
		t.Fatalf("ExpectedSHA256 mismatch")
	}
	if p.SourceSHA256 != sha256File(t, filepath.Join(dir, "a.go")) {
		t.Fatalf("SourceSHA256 mismatch")
	}
	if !strings.Contains(p.Diff, "@@ -3,2 +3,2 @@") {
		t.Fatalf("Diff hunk header missing: %s", p.Diff)
	}
	if !strings.Contains(p.Diff, "-func Alpha() {}") || !strings.Contains(p.Diff, "+func A() {}") {
		t.Fatalf("Diff content wrong: %s", p.Diff)
	}
	if p.DiffDigest != sha256Str(p.Diff) {
		t.Fatalf("DiffDigest = %q, want sha256 of diff content", p.DiffDigest)
	}
	if p.IsSensitive {
		t.Fatal("IsSensitive should be false")
	}
}

// --- target format variants ---

func TestPatchTargetFormatVariants(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	tests := []struct {
		name     string
		target   string
		repl     string
		wantHash string
		wantHunk string
	}{
		{
			"L3 replace single", "L3", "X",
			sha256Str("package sample\n\nX\nfunc Beta() {}\nfunc Gamma() {}\n"), "@@ -3,1 +3,1 @@",
		},
		{
			"L3-L5 replace range", "L3-L5", "X\nY",
			sha256Str("package sample\n\nX\nY\n"), "@@ -3,3 +3,2 @@",
		},
		{
			"L3- replace to EOF", "L3-", "Z",
			sha256Str("package sample\n\nZ\n"), "@@ -3,3 +3,1 @@",
		},
		{
			"L3-L3 insert before", "L3-L3", "INSERTED",
			sha256Str("package sample\n\nINSERTED\nfunc Alpha() {}\nfunc Beta() {}\nfunc Gamma() {}\n"), "@@ -2,0 +3,1 @@",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := "package sample\n\nfunc Alpha() {}\nfunc Beta() {}\nfunc Gamma() {}\n"
			if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			p, err := preparePatch(t, tool, patchArgsJSON("a.go", tt.target, tt.repl))
			if err != nil {
				t.Fatal(err)
			}
			if p.ExpectedSHA256 != tt.wantHash {
				t.Fatalf("ExpectedSHA256 = %s, want %s", p.ExpectedSHA256, tt.wantHash)
			}
			if !strings.Contains(p.Diff, tt.wantHunk) {
				t.Fatalf("Diff missing hunk %q: %s", tt.wantHunk, p.Diff)
			}
		})
	}
}

// --- insert before keeps all lines ---

func TestPatchInsertBeforeKeepsExistingLines(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\nc\nd\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3-L3", "X"))
	if err != nil {
		t.Fatal(err)
	}
	want := sha256Str("a\nb\nX\nc\nd\n")
	if p.ExpectedSHA256 != want {
		t.Fatalf("ExpectedSHA256 mismatch: want hash of %q", "a\nb\nX\nc\nd\n")
	}
	if !strings.Contains(p.Diff, "@@ -2,0 +3,1 @@") {
		t.Fatalf("Diff hunk missing: %s", p.Diff)
	}
}

// --- single line and open-ended ---

func TestPatchSingleLineReplace(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\nc\nd\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3", "X"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("a\nb\nX\nd\n") {
		t.Fatal("ExpectedSHA256 mismatch")
	}
	if !strings.Contains(p.Diff, "@@ -3,1 +3,1 @@") {
		t.Fatalf("Diff hunk: %s", p.Diff)
	}
}

func TestPatchOpenEnded(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\nc\nd\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3-", "X"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("a\nb\nX\n") {
		t.Fatal("ExpectedSHA256 mismatch")
	}
}

// --- trailing newline ---

func TestPatchTrailingNewlinePreserved(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L2", "X"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("a\nX\n") {
		t.Fatal("ExpectedSHA256 mismatch")
	}
}

func TestPatchTrailingNewlineAbsent(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L2", "X"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("a\nX") {
		t.Fatal("ExpectedSHA256 mismatch")
	}
}

// --- empty replacement deletes lines ---

func TestPatchEmptyReplacementDeletesLines(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\nc\nd\ne\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L2-L3", ""))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("a\nd\ne\n") {
		t.Fatalf("ExpectedSHA256 mismatch")
	}
}

// --- file not found ---

func TestPatchFileNotFound(t *testing.T) {
	_, tool, _ := setupPatch(t)
	_, err := preparePatch(t, tool, patchArgsJSON("missing.go", "L1", "x"))
	if err == nil || !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("expected 'file not found', got: %v", err)
	}
}

// --- line out of bounds ---

func TestPatchLineOutOfBounds(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\nc\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []string{"L4", "L2-L4", "L4-"}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			_, err := preparePatch(t, tool, patchArgsJSON("a.go", target, "x"))
			if err == nil || !strings.Contains(err.Error(), "out of bounds") {
				t.Fatalf("expected 'out of bounds' for %s, got: %v", target, err)
			}
		})
	}
}

// --- invalid target formats ---

func TestPatchInvalidTargetFormat(t *testing.T) {
	_, tool, _ := setupPatch(t)
	// Each of these should produce a format error. "L3-1" fails because right
	// side lacks the L prefix. "L0" fails because line numbers start at 1.
	tests := []string{"L", "3", "L0", "L3-1", "L3-L", "L-L3", "abc", "L3x"}
	for _, target := range tests {
		t.Run("target="+target, func(t *testing.T) {
			_, err := preparePatch(t, tool, patchArgsJSON("a.go", target, "x"))
			if err == nil || !strings.Contains(err.Error(), "expected L3") {
				t.Fatalf("expected format error for %q, got: %v", target, err)
			}
		})
	}
}

func TestPatchEndBeforeStart(t *testing.T) {
	_, tool, _ := setupPatch(t)
	_, err := preparePatch(t, tool, patchArgsJSON("a.go", "L5-L3", "x"))
	if err == nil || !strings.Contains(err.Error(), "before start") {
		t.Fatalf("expected 'before start', got: %v", err)
	}
}

// --- required arguments ---

func TestPatchRequiredArguments(t *testing.T) {
	_, tool, _ := setupPatch(t)
	tests := []struct {
		name string
		args json.RawMessage
		want string
	}{
		{"missing path", json.RawMessage(`{"target":"L1","replacement":"x"}`), "path is required"},
		{"missing target", json.RawMessage(`{"path":"a.go","replacement":"x"}`), "target is required"},
		{"missing replacement key", json.RawMessage(`{"path":"a.go","target":"L1"}`), "replacement is required"},
		{"unknown field", json.RawMessage(`{"path":"a.go","target":"L1","replacement":"x","extra":1}`), "arguments do not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := preparePatch(t, tool, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got: %v", tt.want, err)
			}
		})
	}
}

// --- credential detection ---

func TestPatchCredentialInReplacement(t *testing.T) {
	_, tool, _ := setupPatch(t)
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3", "sk-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsSensitive {
		t.Fatal("IsSensitive should be true for replacement with credential")
	}
}

func TestPatchCredentialInSource(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "package sample\n\nfunc Alpha() {}\nsk-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nfunc Gamma() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L2", "clean"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsSensitive {
		t.Fatal("IsSensitive should be true when source has credential even in untouched line")
	}
}

func TestPatchCredentialClean(t *testing.T) {
	_, tool, _ := setupPatch(t)
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3", "normal text"))
	if err != nil {
		t.Fatal(err)
	}
	if p.IsSensitive {
		t.Fatal("IsSensitive should be false for clean content")
	}
}

// --- protected path ---

func TestPatchProtectedPathRefused(t *testing.T) {
	_, tool, _ := setupPatch(t)
	_, err := preparePatch(t, tool, patchArgsJSON(".env", "L1", "SAFE"))
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("expected 'protected' error, got: %v", err)
	}
}

// --- binary and directory ---

func TestPatchBinaryRejected(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	if err := os.WriteFile(filepath.Join(dir, "binary.dat"), []byte{0xff, 0x00, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := preparePatch(t, tool, patchArgsJSON("binary.dat", "L1", "x"))
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("expected 'binary' error, got: %v", err)
	}
}

func TestPatchDirectoryRejected(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := preparePatch(t, tool, patchArgsJSON("subdir", "L1", "x"))
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected 'not a regular file', got: %v", err)
	}
}

// --- no write to disk ---

func TestPatchNoWriteToDisk(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	before, err := os.ReadFile(filepath.Join(dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = preparePatch(t, tool, patchArgsJSON("a.go", "L3", "CHANGED"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("file was modified on disk — Prepare must not write")
	}
}

// --- large file diff truncation ---

func TestPatchLargeFileTruncatesDiff(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	var sourceLines strings.Builder
	for i := 0; i < 5000; i++ {
		sourceLines.WriteString("line content that is long enough to exceed diff budget quickly\n")
	}
	bigContent := sourceLines.String()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(bigContent), 0o600); err != nil {
		t.Fatal(err)
	}
	var replLines strings.Builder
	for i := 0; i < 5000; i++ {
		replLines.WriteString("replacement line equally long content here\n")
	}
	repl := replLines.String()
	p, err := preparePatch(t, tool, patchArgsJSON("big.txt", "L1-", repl))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Diff, "truncated=true") {
		t.Fatal("large diff should be truncated")
	}
	const maxDiffBytes = 64 * 1024
	const marker = "[diff truncated=true; large patch preview]\n"
	if len(p.Diff) > maxDiffBytes+len(marker) {
		t.Fatalf("diff too large: %d bytes (max %d + marker)", len(p.Diff), maxDiffBytes)
	}
	// ExpectedSHA256 must still be correct
	if p.ExpectedSHA256 != sha256Str(repl) {
		t.Fatal("ExpectedSHA256 mismatch for large file")
	}
}

// --- insert into empty file ---

func TestPatchInsertIntoEmptyFile(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	emptyPath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("empty.txt", "L1-L1", "X\nY"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("X\nY") {
		t.Fatalf("ExpectedSHA256 mismatch for insert into empty file")
	}
	if !strings.Contains(p.Diff, "@@ -0,0 +1,2 @@") {
		t.Fatalf("Diff hunk for empty insert: %s", p.Diff)
	}
}

// --- single-line file ---

func TestPatchSingleLineFile(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "only\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("L1-L1 insert", func(t *testing.T) {
		p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L1-L1", "X"))
		if err != nil {
			t.Fatal(err)
		}
		if p.ExpectedSHA256 != sha256Str("X\nonly\n") {
			t.Fatal("ExpectedSHA256 mismatch for insert on single-line file")
		}
	})
	t.Run("L1 replace", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L1", "X"))
		if err != nil {
			t.Fatal(err)
		}
		if p.ExpectedSHA256 != sha256Str("X\n") {
			t.Fatal("ExpectedSHA256 mismatch for replace on single-line file")
		}
	})
}

// --- insert at EOF ---

func TestPatchInsertAtEOF(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\nc\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L4-L4", "appended"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("a\nb\nc\nappended\n") {
		t.Fatal("ExpectedSHA256 mismatch for insert at EOF")
	}
}

// --- last line replacement ---

func TestPatchLastLineReplacement(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	content := "a\nb\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L2", "X"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedSHA256 != sha256Str("a\nX\n") {
		t.Fatal("ExpectedSHA256 mismatch for last line replacement")
	}
}

// --- patch prepares but does not execute through broker ---

func TestPatchPrepareDoesNotExecute(t *testing.T) {
	_, _, broker := setupPatch(t)
	got := broker.Execute(context.Background(), provider.ToolCall{
		ID: "c1", Name: "patch", Arguments: patchArgsJSON("a.go", "L1", "x"),
	})
	if got.Outcome != transcript.ToolResultSuccess {
		t.Fatalf("expected ToolResultSuccess (prepared), got %s", got.Outcome)
	}
	if !strings.Contains(got.Content, "awaiting execution") {
		t.Fatalf("expected 'awaiting execution', got: %s", got.Content)
	}
}

// --- catalog includes patch ---

func TestPatchCatalogIncludesPatch(t *testing.T) {
	_, _, broker := setupPatch(t)
	names := make(map[string]bool)
	for _, def := range broker.Catalog() {
		names[def.Name] = true
	}
	if !names["patch"] {
		t.Fatal("catalog missing patch tool")
	}
	if len(names) != 4 {
		t.Fatalf("catalog has %d tools, want 4", len(names))
	}
}

// --- SHA256 round-trip ---

func TestPatchSHA256RoundTrip(t *testing.T) {
	dir, tool, _ := setupPatch(t)
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3-L4", "func X() {}"))
	if err != nil {
		t.Fatal(err)
	}
	fileHash := sha256File(t, filepath.Join(dir, "a.go"))
	if p.SourceSHA256 != fileHash {
		t.Fatalf("SourceSHA256 = %s, file hash = %s", p.SourceSHA256, fileHash)
	}
}

// --- S1.3: ExecutePrepared and stale detection ---

func TestPatchExecutePreparedWritesFile(t *testing.T) {
	dir, tool, broker := setupPatch(t)
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3", "func NewFunc() {}"))
	if err != nil {
		t.Fatal(err)
	}
	got := broker.ExecutePrepared(context.Background(), action.Prepared{
		Tool:      "patch",
		Arguments: patchArgsJSON("a.go", "L3", "func NewFunc() {}"),
		Effects:   []action.Effect{action.EffectWrite},
		Patch:     p,
	}, "call-1")
	if got.Outcome != transcript.ToolResultSuccess {
		t.Fatalf("ExecutePrepared = %+v", got)
	}
	content, err := os.ReadFile(filepath.Join(dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := "package sample\n\nfunc NewFunc() {}\nfunc Beta() {}\nfunc Gamma() {}\n"
	if string(content) != want {
		t.Fatalf("file content = %q, want %q", content, want)
	}
}

func TestPatchStaleDetectionRejectsChangedSource(t *testing.T) {
	dir, tool, broker := setupPatch(t)
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3", "func NewFunc() {}"))
	if err != nil {
		t.Fatal(err)
	}
	// Modify the file after preparation
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("changed content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := broker.ExecutePrepared(context.Background(), action.Prepared{
		Tool:      "patch",
		Arguments: patchArgsJSON("a.go", "L3", "func NewFunc() {}"),
		Effects:   []action.Effect{action.EffectWrite},
		Patch:     p,
	}, "call-1")
	if got.Outcome != transcript.ToolResultError {
		t.Fatalf("expected error due to stale detection, got %s: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "stale") {
		t.Fatalf("expected 'stale' in error message, got: %s", got.Content)
	}
	// File must not be overwritten
	content, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if string(content) != "changed content\n" {
		t.Fatal("file was modified despite stale detection")
	}
}

func TestPatchExecutePreparedSHA256MismatchFails(t *testing.T) {
	_, tool, broker := setupPatch(t)
	p, err := preparePatch(t, tool, patchArgsJSON("a.go", "L3", "func NewFunc() {}"))
	if err != nil {
		t.Fatal(err)
	}
	// Tamper with ExpectedSHA256
	p.ExpectedSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	got := broker.ExecutePrepared(context.Background(), action.Prepared{
		Tool:      "patch",
		Arguments: patchArgsJSON("a.go", "L3", "func NewFunc() {}"),
		Effects:   []action.Effect{action.EffectWrite},
		Patch:     p,
	}, "call-1")
	if got.Outcome != transcript.ToolResultError {
		t.Fatalf("expected error due to SHA256 mismatch, got %s: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "content mismatch") {
		t.Fatalf("expected 'content mismatch', got: %s", got.Content)
	}
}
