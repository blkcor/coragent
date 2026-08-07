package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

func TestToolEventPersistenceFailureLeavesCallsForRestartReconciliation(t *testing.T) {
	for _, failedKind := range []event.Kind{event.KindToolStarted, event.KindToolFinished} {
		t.Run(string(failedKind), func(t *testing.T) {
			root := t.TempDir()
			workspaceDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspaceDir, "main.go"), []byte("package main\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			durable, err := store.Create(root, "sess-tool-fault", workspaceDir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			workspaceFS, err := workspace.Open(workspaceDir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspaceFS.Close() }()
			broker, err := action.NewBroker(tools.NewCatalog(workspace.NewFileService(workspaceFS), dataproj.New())...)
			if err != nil {
				t.Fatal(err)
			}
			fake := provider.NewScripted(provider.Turn{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)}}})
			session, err := NewSession("sess-tool-fault", Config{Provider: fake, Durable: durable, Broker: broker})
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected tool event failure")
			session.eventHook = func(kind event.Kind) error {
				if kind == failedKind {
					return injected
				}
				return nil
			}
			command, _ := sessioncommand.NewSubmit("cmd-tool", "read main")
			if err := session.Apply(context.Background(), command.ForSession(session.ID())); err != nil {
				t.Fatal(err)
			}
			waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := session.WaitIdle(waitCtx); !errors.Is(err, ErrSessionFaulted) || !errors.Is(err, injected) {
				t.Fatalf("tool event persistence failure = %v", err)
			}
			for _, record := range session.Transcript() {
				if record.Kind == transcript.KindRunOutcome {
					t.Fatal("faulted run appended an outcome before pairing open tool calls")
				}
			}

			reopened, err := store.Open(root, "sess-tool-fault")
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := NewSession("sess-tool-fault", Config{Provider: fake, Durable: reopened, Broker: broker})
			if err != nil {
				t.Fatal(err)
			}
			if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
				t.Fatalf("reconciled tool transcript: %v", err)
			}
			if len(fake.Requests()) != 1 || reopened.Manifest().ActiveRun != nil {
				t.Fatalf("restart requests=%d manifest=%+v", len(fake.Requests()), reopened.Manifest())
			}
		})
	}
}
