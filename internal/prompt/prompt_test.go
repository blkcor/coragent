package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

func TestInstructionDiscoveryPrecedenceScopeAndDedup(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("CLAUDE.md", "root claude")
	write("AGENTS.md", "root agents")
	write("pkg/CLAUDE.md", "same content")
	write("pkg/AGENTS.md", "pkg agents")
	write("pkg/deep/AGENTS.md", "same content")
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("close workspace: %v", err)
		}
	})
	docs, err := DiscoverInstructions(context.Background(), w, "pkg/deep", dataproj.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 4 {
		t.Fatalf("docs = %+v", docs)
	}
	if docs[0].Sources[0] != "CLAUDE.md" || docs[1].Sources[0] != "AGENTS.md" || docs[0].Precedence >= docs[1].Precedence {
		t.Fatalf("root order = %+v", docs[:2])
	}
	var dedup *Instruction
	for i := range docs {
		if docs[i].Content == "same content" {
			dedup = &docs[i]
		}
	}
	if dedup == nil || len(dedup.Sources) != 2 || dedup.Scope != "pkg/deep" {
		t.Fatalf("deduplicated doc = %+v", dedup)
	}
	payload := TranscriptPayload(docs)
	if len(payload.Sources) != 4 || payload.Sources[0].SHA256 == "" {
		t.Fatalf("transcript provenance = %+v", payload)
	}
}

func TestInstructionDiscoveryRejectsSymlinkAndRedactsWholePrivateKey(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=must-not-load"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(".env", filepath.Join(dir, "AGENTS.md")); err != nil {
			t.Fatal(err)
		}
		w, err := workspace.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = w.Close() }()
		if _, err := DiscoverInstructions(context.Background(), w, ".", dataproj.New()); err == nil {
			t.Fatal("instruction discovery followed a symlink alias")
		}
	})

	t.Run("private key", func(t *testing.T) {
		dir := t.TempDir()
		secretBody := "synthetic-private-body-in-instructions"
		content := "policy before\n-----BEGIN PRIVATE KEY-----\n" + secretBody + "\n-----END PRIVATE KEY-----\npolicy after"
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		w, err := workspace.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = w.Close() }()
		docs, err := DiscoverInstructions(context.Background(), w, ".", dataproj.New())
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 1 || strings.Contains(docs[0].Content, secretBody) || !strings.Contains(docs[0].Content, "REDACTED") || !strings.Contains(docs[0].Content, "policy after") {
			t.Fatalf("instruction projection = %+v", docs)
		}
	})
}

func TestInstructionDiscoveryAllowsNoInstructionFiles(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	docs, err := DiscoverInstructions(context.Background(), w, ".", dataproj.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Fatalf("unexpected instructions = %+v", docs)
	}
}

func TestAssemblerSeparatesSectionsAndPairsTools(t *testing.T) {
	a, err := NewAssembler(Config{Workspace: "/workspace", ActivePath: ".", ContextWindow: 32000, MaxOutputTokens: 8000})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	record := func(seq uint64, kind transcript.Kind, payload any) transcript.Record {
		r, err := transcript.New("run-1", now, kind, payload)
		if err != nil {
			t.Fatal(err)
		}
		r.Seq = seq
		return r
	}
	records := []transcript.Record{
		record(1, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "find Alpha"}),
		record(2, transcript.KindToolCall, transcript.ToolCallPayload{CallID: "c1", Name: "search", Arguments: []byte(`{"pattern":"Alpha"}`)}),
		record(3, transcript.KindToolResult, transcript.ToolResultPayload{CallID: "c1", Outcome: transcript.ToolResultSuccess, Content: "a.go:3"}),
	}
	req, err := a.Build("find Alpha", []Instruction{{Sources: []string{"AGENTS.md"}, Scope: ".", SHA256: "abc", Precedence: 11, Content: "cite files"}}, records,
		[]provider.ToolDefinition{{Name: "search", Schema: []byte(`{"type":"object"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.StablePrompt, "Hard runtime policy") || strings.Contains(req.StablePrompt, "cite files") {
		t.Fatalf("stable section = %q", req.StablePrompt)
	}
	if !strings.Contains(req.DynamicPrompt, "cite files") || !strings.Contains(req.DynamicPrompt, "Current explicit user request") {
		t.Fatalf("dynamic section = %q", req.DynamicPrompt)
	}
	if len(req.Messages) != 3 || len(req.Messages[1].ToolCalls) != 1 || req.Messages[2].ToolCallID != "c1" {
		t.Fatalf("messages = %+v", req.Messages)
	}
	req2, err := a.Build("find Alpha", nil, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(req2.DynamicPrompt, "cite files") {
		t.Fatal("assembler accumulated a prior dynamic instruction")
	}
}

func TestAssemblerFailsExplicitlyAtContextLimit(t *testing.T) {
	a, err := NewAssembler(Config{Workspace: "/w", ContextWindow: 100, MaxOutputTokens: 90})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Build(strings.Repeat("x", 100), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Build = %v", err)
	}
}
