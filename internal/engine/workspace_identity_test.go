package engine_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/platform/fileid"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/transcript"
)

type identifiedProvider struct {
	scripted *provider.Scripted
	identity provider.Identity
}

func (p identifiedProvider) Complete(ctx context.Context, request provider.Request) (provider.Response, error) {
	return p.scripted.Complete(ctx, request)
}

func (p identifiedProvider) Identity() provider.Identity { return p.identity }

func testProviderIdentity(endpoint, model string) provider.Identity {
	digest := sha256.Sum256([]byte(endpoint))
	credentialDigest := sha256.Sum256([]byte("test-credential-source"))
	return provider.Identity{
		Adapter: "test-adapter", WireProtocol: "test-wire-v1", EndpointSHA256: hex.EncodeToString(digest[:]), CredentialSourceSHA256: hex.EncodeToString(credentialDigest[:]),
		Model: model, ContextWindow: 32000, MaxOutputTokens: 8000, ToolChoice: "auto",
	}
}

func TestLoadRejectsReplacedWorkspaceIdentity(t *testing.T) {
	parent := t.TempDir()
	workspacePath := filepath.Join(parent, "workspace")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime, err := engine.New(engine.EngineConfig{
		StoreRoot: t.TempDir(), Provider: provider.NewScripted(), ContextWindow: 32000, MaxOutputTokens: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := runtime.Create(context.Background(), workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := session.ID()
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(parent, "workspace-original")
	if err := os.Rename(workspacePath, original); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(parent, "replacement")
	if err := os.Mkdir(replacement, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, workspacePath); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Load(context.Background(), sessionID); err == nil {
		t.Fatal("Load accepted a workspace path replaced with another identity")
	}
}

func TestLoadRejectsDirectoryRecreatedAtSameCanonicalPath(t *testing.T) {
	parent := t.TempDir()
	workspacePath := filepath.Join(parent, "workspace")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if !fileid.HasBirthTime(originalInfo) {
		t.Skip("platform does not expose stable filesystem birth time")
	}
	runtime, err := engine.New(engine.EngineConfig{
		StoreRoot: t.TempDir(), Provider: provider.NewScripted(), ContextWindow: 32000, MaxOutputTokens: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := runtime.Create(context.Background(), workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := session.ID()
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(workspacePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Load(context.Background(), sessionID); err == nil {
		t.Fatal("Load accepted a different directory recreated at the same path")
	}
}

func TestSubmitRefreshesProjectInstructionsAndRecordsActualProvenance(t *testing.T) {
	workspacePath := t.TempDir()
	instructions := filepath.Join(workspacePath, "AGENTS.md")
	if err := os.WriteFile(instructions, []byte("old instruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := provider.NewScripted(provider.Turn{Text: "done", Reason: provider.ReasonStop})
	runtime, err := engine.New(engine.EngineConfig{
		StoreRoot: t.TempDir(), Provider: fake, ContextWindow: 32000, MaxOutputTokens: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := runtime.Create(context.Background(), workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Shutdown(context.Background()) }()
	if err := os.WriteFile(instructions, []byte("new instruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	command, _ := sessioncommand.NewSubmit("cmd-refresh", "inspect")
	if err := session.Apply(context.Background(), command.ForSession(session.ID())); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	requests := fake.Requests()
	if len(requests) != 1 || !strings.Contains(requests[0].DynamicPrompt, "new instruction") || strings.Contains(requests[0].DynamicPrompt, "old instruction") {
		t.Fatalf("refreshed request = %+v", requests)
	}
	loaded := false
	for _, record := range session.Transcript() {
		if record.Kind != transcript.KindInstructionsLoaded {
			continue
		}
		var payload transcript.InstructionsLoadedPayload
		if record.DecodePayload(&payload) == nil && len(payload.Sources) == 1 && payload.Sources[0].Sources[0] == "AGENTS.md" {
			loaded = true
		}
	}
	if !loaded {
		t.Fatal("actual refreshed instruction provenance was not persisted")
	}
}

func TestLoadRejectsChangedProviderRuntimeBinding(t *testing.T) {
	workspacePath := t.TempDir()
	storeRoot := t.TempDir()
	firstProvider := identifiedProvider{scripted: provider.NewScripted(), identity: testProviderIdentity("https://first.example/v1", "model-2026-08-01")}
	first, err := engine.New(engine.EngineConfig{
		StoreRoot: storeRoot, Provider: firstProvider, ContextWindow: 32000, MaxOutputTokens: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := first.Create(context.Background(), workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := session.ID()
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	changedProvider := identifiedProvider{scripted: provider.NewScripted(), identity: testProviderIdentity("https://second.example/v1", "model-2026-08-02")}
	changed, err := engine.New(engine.EngineConfig{StoreRoot: storeRoot, Provider: changedProvider, ContextWindow: 32000, MaxOutputTokens: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.Load(context.Background(), sessionID); err == nil {
		t.Fatal("Load accepted changed Provider/runtime settings")
	}
}
