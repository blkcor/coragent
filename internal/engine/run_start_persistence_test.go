package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/prompt"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/transcript"
)

func TestRunStartPersistenceFailuresFaultClosedUntilRestart(t *testing.T) {
	type startFailure struct {
		name      string
		configure func(*Session, error)
	}
	failures := []startFailure{
		{name: "user_message", configure: func(session *Session, injected error) {
			session.transcriptHook = func(kind transcript.Kind) error {
				if kind == transcript.KindUserMessage {
					return injected
				}
				return nil
			}
		}},
		{name: "instructions", configure: func(session *Session, injected error) {
			session.docs = []prompt.Instruction{{Sources: []string{"AGENTS.md"}, Scope: ".", SHA256: "digest", Precedence: 1, Content: "read only"}}
			session.transcriptHook = func(kind transcript.Kind) error {
				if kind == transcript.KindInstructionsLoaded {
					return injected
				}
				return nil
			}
		}},
		{name: "run_started_event", configure: func(session *Session, injected error) {
			session.eventHook = func(kind event.Kind) error {
				if kind == event.KindRunStarted {
					return injected
				}
				return nil
			}
		}},
	}
	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			root := t.TempDir()
			workspace := t.TempDir()
			durable, err := store.Create(root, "sess-start-fault", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			fake := provider.NewScripted(provider.Turn{Text: "must wait for explicit restart"})
			session, err := NewSession("sess-start-fault", Config{Provider: fake, Durable: durable})
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected run-start persistence failure")
			failure.configure(session, injected)
			command, _ := sessioncommand.NewSubmit("cmd-start", "inspect")
			if err := session.Apply(context.Background(), command.ForSession(session.ID())); !errors.Is(err, injected) {
				t.Fatalf("run-start error = %v", err)
			}
			if session.State() != StateFaulted || durable.Manifest().ActiveRun == nil || len(fake.Requests()) != 0 {
				t.Fatalf("start fault state=%s manifest=%+v requests=%d", session.State(), durable.Manifest(), len(fake.Requests()))
			}

			reopened, err := store.Open(root, "sess-start-fault")
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := NewSession("sess-start-fault", Config{Provider: fake, Durable: reopened})
			if err != nil {
				t.Fatal(err)
			}
			if recovered.State() != StateIdle || reopened.Manifest().ActiveRun != nil || len(fake.Requests()) != 0 {
				t.Fatalf("recovered start state=%s manifest=%+v requests=%d", recovered.State(), reopened.Manifest(), len(fake.Requests()))
			}
			if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
				t.Fatalf("recovered start transcript: %v", err)
			}
		})
	}
}
