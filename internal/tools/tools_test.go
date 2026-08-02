package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func setupBroker(t *testing.T) (string, *action.Broker) {
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
	mustWrite("a.go", "package sample\n\nfunc Alpha() {}\n")
	mustWrite("nested/b.go", "package nested\n\nfunc Beta() { Alpha() }\n")
	mustWrite(".hidden/note.txt", "Alpha hidden\n")
	mustWrite(".env", "TOKEN=do-not-return\n")
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	b, err := action.NewBroker(tools.NewCatalog(w, dataproj.New())...)
	if err != nil {
		t.Fatal(err)
	}
	return dir, b
}

func call(id, name, arguments string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(arguments)}
}

func TestReadUsesLineNumbersProjectionAndBounds(t *testing.T) {
	dir, broker := setupBroker(t)
	got := broker.Execute(context.Background(), call("c1", "read", `{"path":"a.go","start_line":2,"end_line":3}`))
	if got.Outcome != transcript.ToolResultSuccess || got.Content != "2: \n3: func Alpha() {}" {
		t.Fatalf("read = %+v", got)
	}
	protected := broker.Execute(context.Background(), call("c2", "read", `{"path":".env"}`))
	if protected.Outcome != transcript.ToolResultSuccess || !strings.Contains(protected.Content, "[REDACTED:PROTECTED_PATH]") || strings.Contains(protected.Content, "do-not-return") {
		t.Fatalf("protected read = %+v", protected)
	}
	var lines strings.Builder
	for i := 0; i < 1002; i++ {
		lines.WriteString("line\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	large := broker.Execute(context.Background(), call("c3", "read", `{"path":"large.txt"}`))
	if large.Outcome != transcript.ToolResultSuccess || !strings.Contains(large.Content, "truncated=true") {
		t.Fatalf("large read = %s", large.Content[len(large.Content)-100:])
	}
}

func TestListAndSearchAreDeterministic(t *testing.T) {
	_, broker := setupBroker(t)
	first := broker.Execute(context.Background(), call("c1", "list", `{}`))
	second := broker.Execute(context.Background(), call("c2", "list", `{}`))
	if first.Outcome != transcript.ToolResultSuccess || first.Content != second.Content {
		t.Fatalf("list results differ:\n%s\n%s", first.Content, second.Content)
	}
	if !strings.Contains(first.Content, ".hidden/") || !strings.Contains(first.Content, ".env") {
		t.Fatalf("hidden names absent from list: %s", first.Content)
	}
	search := broker.Execute(context.Background(), call("c3", "search", `{"pattern":"Alpha"}`))
	if search.Outcome != transcript.ToolResultSuccess || !strings.Contains(search.Content, "a.go:3:") || !strings.Contains(search.Content, "nested/b.go:3:") || !strings.Contains(search.Content, "protected_paths_skipped=1") {
		t.Fatalf("search = %+v", search)
	}
	bad := broker.Execute(context.Background(), call("c4", "search", `{"pattern":"["}`))
	if bad.Outcome != transcript.ToolResultError || !strings.Contains(bad.Content, "valid Go regular expression") {
		t.Fatalf("invalid search = %+v", bad)
	}
}

func TestWorkspaceEscapeAndSymlinkEscapeFailClosed(t *testing.T) {
	dir, broker := setupBroker(t)
	abs := broker.Execute(context.Background(), call("c1", "read", `{"path":"/etc/passwd"}`))
	traversal := broker.Execute(context.Background(), call("c2", "read", `{"path":"../outside"}`))
	if abs.Outcome != transcript.ToolResultError || traversal.Outcome != transcript.ToolResultError {
		t.Fatalf("escape outcomes = %+v, %+v", abs, traversal)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("sensitive-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	linked := broker.Execute(context.Background(), call("c3", "read", `{"path":"link"}`))
	if linked.Outcome != transcript.ToolResultError || strings.Contains(linked.Content, "sensitive-payload") {
		t.Fatalf("symlink escape = %+v", linked)
	}
	for _, tc := range []provider.ToolCall{
		call("c4", "list", `{"path":"link"}`),
		call("c5", "search", `{"path":"link","pattern":"sensitive"}`),
	} {
		got := broker.Execute(context.Background(), tc)
		if got.Outcome != transcript.ToolResultError || strings.Contains(got.Content, "sensitive-payload") {
			t.Errorf("%s symlink start path = %+v", tc.Name, got)
		}
	}
}

func TestUnreadablePathsProduceExplicitBoundedErrors(t *testing.T) {
	dir, broker := setupBroker(t)
	unreadableFile := filepath.Join(dir, "unreadable.txt")
	if err := os.WriteFile(unreadableFile, []byte("must-not-return"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadableFile, 0o600) })

	read := broker.Execute(context.Background(), call("read-unreadable", "read", `{"path":"unreadable.txt"}`))
	if read.Outcome == transcript.ToolResultSuccess {
		// Privileged test runners can bypass Unix mode bits, so this fixture
		// cannot prove the ordinary permission path in that environment.
		t.Skip("test process can read a mode-000 file")
	}
	if read.Content != "path is unreadable" || strings.Contains(read.Content, "must-not-return") {
		t.Fatalf("unreadable read = %+v", read)
	}

	unreadableDir := filepath.Join(dir, "unreadable-dir")
	if err := os.Mkdir(unreadableDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadableDir, 0o700) })
	for _, tc := range []provider.ToolCall{
		call("list-unreadable", "list", `{"path":"unreadable-dir"}`),
		call("search-unreadable", "search", `{"path":"unreadable-dir","pattern":"x"}`),
	} {
		got := broker.Execute(context.Background(), tc)
		if got.Outcome != transcript.ToolResultError || got.Content != "path is unreadable" {
			t.Errorf("%s unreadable directory = %+v", tc.Name, got)
		}
	}
}

func TestBrokerUnknownCancellationAndBatchPairing(t *testing.T) {
	_, broker := setupBroker(t)
	unknown := broker.Execute(context.Background(), call("c1", "write", `{}`))
	if unknown.Outcome != transcript.ToolResultError {
		t.Fatalf("unknown = %+v", unknown)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := broker.Execute(ctx, call("c2", "read", `{"path":"a.go"}`))
	if cancelled.Outcome != transcript.ToolResultCancelled {
		t.Fatalf("cancelled = %+v", cancelled)
	}
	results := broker.ExecuteBatch(context.Background(), []provider.ToolCall{
		call("c3", "missing", `{}`), call("c4", "read", `{"path":"a.go"}`),
	})
	if len(results) != 2 || results[0].CallID != "c3" || results[1].CallID != "c4" || results[1].Outcome != transcript.ToolResultSkipped {
		t.Fatalf("batch = %+v", results)
	}
	if got := broker.Catalog(); len(got) != 3 || got[0].Name != "list" || got[1].Name != "read" || got[2].Name != "search" {
		t.Fatalf("catalog = %+v", got)
	}
}

func TestListBoundsContinuationAndAllToolPathPolicies(t *testing.T) {
	dir, broker := setupBroker(t)
	for i := 0; i < 2005; i++ {
		name := filepath.Join(dir, "many", fmt.Sprintf("entry-%04d.txt", i))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first := broker.Execute(context.Background(), call("list-1", "list", `{"path":"many"}`))
	if first.Outcome != transcript.ToolResultSuccess || !strings.Contains(first.Content, "truncated=true") {
		t.Fatalf("bounded list = %+v", first)
	}
	continuation := regexp.MustCompile(`continue with after="([^"]+)"`).FindStringSubmatch(first.Content)
	if len(continuation) != 2 {
		t.Fatalf("list continuation absent: %s", first.Content[len(first.Content)-100:])
	}
	arguments, err := json.Marshal(map[string]any{"path": "many", "after": continuation[1]})
	if err != nil {
		t.Fatal(err)
	}
	second := broker.Execute(context.Background(), provider.ToolCall{ID: "list-2", Name: "list", Arguments: arguments})
	if second.Outcome != transcript.ToolResultSuccess || strings.Contains(second.Content, continuation[1]+"\n") || !strings.Contains(second.Content, "entry-2004.txt") {
		t.Fatalf("continued list = %+v", second)
	}

	for _, tc := range []provider.ToolCall{
		call("list-abs", "list", `{"path":"/etc"}`),
		call("list-up", "list", `{"path":"../outside"}`),
		call("search-abs", "search", `{"path":"/etc","pattern":"x"}`),
		call("search-up", "search", `{"path":"../outside","pattern":"x"}`),
	} {
		if got := broker.Execute(context.Background(), tc); got.Outcome != transcript.ToolResultError {
			t.Errorf("%s accepted escaping path: %+v", tc.ID, got)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []provider.ToolCall{
		call("list-cancel", "list", `{"path":"many"}`),
		call("search-cancel", "search", `{"path":"many","pattern":"entry"}`),
	} {
		if got := broker.Execute(ctx, tc); got.Outcome != transcript.ToolResultCancelled {
			t.Errorf("%s cancellation = %+v", tc.ID, got)
		}
	}
}

func TestReadExactThousandLineBoundary(t *testing.T) {
	dir, broker := setupBroker(t)
	writeLines := func(name string, count int) {
		t.Helper()
		var content strings.Builder
		for range count {
			content.WriteString("line\n")
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeLines("exact.txt", 1000)
	exact := broker.Execute(context.Background(), call("read-1000", "read", `{"path":"exact.txt"}`))
	if exact.Outcome != transcript.ToolResultSuccess || strings.Contains(exact.Content, "truncated=true") {
		t.Fatalf("1000-line read truncated: %+v", exact)
	}
	if count := strings.Count(exact.Content, ": line"); count != 1000 {
		t.Fatalf("1000-line read entries = %d, want 1000", count)
	}

	writeLines("over.txt", 1001)
	over := broker.Execute(context.Background(), call("read-1001", "read", `{"path":"over.txt"}`))
	if over.Outcome != transcript.ToolResultSuccess || !strings.Contains(over.Content, "truncated=true") {
		t.Fatalf("1001-line read not truncated: %+v", over)
	}
	if count := strings.Count(over.Content, ": line"); count != 1000 {
		t.Fatalf("1001-line read entries = %d, want 1000", count)
	}
	if !strings.Contains(over.Content, "start_line=1001") {
		t.Fatalf("1001-line read continuation hint absent: %s", over.Content[len(over.Content)-80:])
	}
}

func TestSearchBoundsAndNonTextInputs(t *testing.T) {
	dir, broker := setupBroker(t)
	var matches strings.Builder
	for i := 0; i < 205; i++ {
		matches.WriteString("needle\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "matches.txt"), []byte(matches.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binary.dat"), []byte{0xff, 0x00, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	search := broker.Execute(context.Background(), call("search-bound", "search", `{"pattern":"needle|binary"}`))
	if search.Outcome != transcript.ToolResultSuccess || !strings.Contains(search.Content, "truncated=true") {
		t.Fatalf("bounded search = %+v", search)
	}
	if count := strings.Count(search.Content, "matches.txt:"); count != 200 {
		t.Fatalf("search match count = %d, want 200", count)
	}

	binary := broker.Execute(context.Background(), call("read-binary", "read", `{"path":"binary.dat"}`))
	missing := broker.Execute(context.Background(), call("read-missing", "read", `{"path":"missing.txt"}`))
	directory := broker.Execute(context.Background(), call("read-directory", "read", `{"path":"nested"}`))
	for _, result := range []transcript.ToolResultPayload{binary, missing, directory} {
		if result.Outcome != transcript.ToolResultError || len(result.Content) == 0 || len(result.Content) > 64*1024 {
			t.Errorf("bounded read failure = %+v", result)
		}
	}
}

func TestReadAndSearchNeverExposeOrMatchPrivateKeyBody(t *testing.T) {
	dir, broker := setupBroker(t)
	secretBody := "synthetic-private-body-for-oracle-test"
	content := "before\n-----BEGIN PRIVATE KEY-----\n" + secretBody + "\n-----END PRIVATE KEY-----\nafter\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	read := broker.Execute(context.Background(), call("read-private", "read", `{"path":"fixture.txt"}`))
	if read.Outcome != transcript.ToolResultSuccess || strings.Contains(read.Content, secretBody) || !strings.Contains(read.Content, "REDACTED") || !strings.Contains(read.Content, "after") {
		t.Fatalf("private-key read projection = %+v", read)
	}
	searchBody := broker.Execute(context.Background(), call("search-private-body", "search", `{"path":"fixture.txt","pattern":"synthetic-private-body"}`))
	if searchBody.Outcome != transcript.ToolResultSuccess || strings.Contains(searchBody.Content, "fixture.txt:") || strings.Contains(searchBody.Content, secretBody) {
		t.Fatalf("search used raw private-key content as an oracle = %+v", searchBody)
	}
	searchMarker := broker.Execute(context.Background(), call("search-private-marker", "search", `{"path":"fixture.txt","pattern":"BEGIN PRIVATE KEY"}`))
	if searchMarker.Outcome != transcript.ToolResultSuccess || strings.Contains(searchMarker.Content, "fixture.txt:") {
		t.Fatalf("search used raw private-key marker as an oracle = %+v", searchMarker)
	}
}
