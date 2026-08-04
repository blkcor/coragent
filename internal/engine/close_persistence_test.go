package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/transcript"
)

func TestSessionCloseLogAheadRecoveryIsIdempotent(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	durable, err := store.Create(root, "sess-close-recovery", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	closeRecord, err := transcript.New("", time.Now(), transcript.KindSessionClosed, transcript.SessionClosedPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := durable.AppendTranscript(closeRecord); err != nil {
		t.Fatal(err)
	}
	if durable.Manifest().Closed {
		t.Fatal("close manifest advanced before recovery")
	}

	fake := provider.NewScripted(provider.Turn{Text: "must not run"})
	reopened, err := store.Open(root, "sess-close-recovery")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := NewSession("sess-close-recovery", Config{Provider: fake, Durable: reopened})
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveredClose(t, recovered, reopened, fake)

	againStore, err := store.Open(root, "sess-close-recovery")
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewSession("sess-close-recovery", Config{Provider: fake, Durable: againStore})
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveredClose(t, again, againStore, fake)
}

func TestSessionCloseManifestWithoutFactFailsClosed(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	durable, err := store.Create(root, "sess-close-corrupt", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := durable.Close(time.Now()); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(root, "sess-close-corrupt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSession("sess-close-corrupt", Config{Provider: provider.NewScripted(), Durable: reopened}); !errors.Is(err, store.ErrCorrupt) {
		t.Fatalf("manifest-only close = %v", err)
	}
}

func TestSessionClosePersistenceFailuresFaultUntilRestart(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Session, error)
		closed    bool
	}{
		{
			name: "transcript",
			configure: func(session *Session, injected error) {
				session.transcriptHook = func(kind transcript.Kind) error {
					if kind == transcript.KindSessionClosed {
						return injected
					}
					return nil
				}
			},
		},
		{
			name: "event",
			configure: func(session *Session, injected error) {
				session.eventHook = func(kind event.Kind) error {
					if kind == event.KindSessionClosed {
						return injected
					}
					return nil
				}
			},
			closed: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workspace := t.TempDir()
			durable, err := store.Create(root, "sess-close-fault", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			session, err := NewSession("sess-close-fault", Config{Provider: provider.NewScripted(), Durable: durable})
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected close persistence failure")
			test.configure(session, injected)
			closeCommand, _ := sessioncommand.NewClose("cmd-close")
			if err := session.Apply(context.Background(), closeCommand.ForSession(session.ID())); !errors.Is(err, injected) {
				t.Fatalf("close error = %v", err)
			}
			if session.State() != StateFaulted {
				t.Fatalf("close failure state = %s", session.State())
			}

			reopened, err := store.Open(root, "sess-close-fault")
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := NewSession("sess-close-fault", Config{Provider: provider.NewScripted(), Durable: reopened})
			if err != nil {
				t.Fatal(err)
			}
			if test.closed {
				if recovered.State() != StateClosed || !reopened.Manifest().Closed || countCloseEvents(recovered.Events()) != 1 {
					t.Fatalf("event-failure recovery state=%s manifest=%+v events=%+v", recovered.State(), reopened.Manifest(), recovered.Events())
				}
			} else if recovered.State() != StateIdle || reopened.Manifest().Closed {
				t.Fatalf("transcript-failure recovery state=%s manifest=%+v", recovered.State(), reopened.Manifest())
			}
		})
	}
}

func TestSensitivePromptWarningPersistenceFailureFaultsSession(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	durable, err := store.Create(root, "sess-warning-fault", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession("sess-warning-fault", Config{Provider: provider.NewScripted(), Durable: durable})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected warning persistence failure")
	session.eventHook = func(kind event.Kind) error {
		if kind == event.KindWarning {
			return injected
		}
		return nil
	}
	command, _ := sessioncommand.NewSubmit("cmd-sensitive", "token sk-test-abcdefghijklmnop")
	if err := session.Apply(context.Background(), command.ForSession(session.ID())); !errors.Is(err, injected) {
		t.Fatalf("sensitive warning error = %v", err)
	}
	if session.State() != StateFaulted {
		t.Fatalf("warning failure state = %s", session.State())
	}
}

func assertRecoveredClose(t *testing.T, session *Session, durable *store.Session, fake *provider.Scripted) {
	t.Helper()
	if session.State() != StateClosed || !durable.Manifest().Closed {
		t.Fatalf("recovered close state=%s manifest=%+v", session.State(), durable.Manifest())
	}
	if countCloseEvents(session.Events()) != 1 {
		t.Fatalf("close events = %+v", session.Events())
	}
	if len(fake.Requests()) != 0 {
		t.Fatalf("close recovery called Provider %d times", len(fake.Requests()))
	}
	submit, _ := sessioncommand.NewSubmit("cmd-after-close", "must reject")
	if err := session.Apply(context.Background(), submit.ForSession(session.ID())); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("submit after recovered close = %v", err)
	}
}

func countCloseEvents(events []event.Event) int {
	count := 0
	for _, current := range events {
		if current.Kind == event.KindSessionClosed {
			count++
		}
	}
	return count
}
