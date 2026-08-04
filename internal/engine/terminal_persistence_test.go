package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/transcript"
)

func TestTerminalPersistenceFailureFaultsClosedAndRestartReconciles(t *testing.T) {
	stages := []terminalStage{terminalOutcome, terminalEvent, terminalFinish}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			root := t.TempDir()
			workspace := t.TempDir()
			durable, err := store.Create(root, "sess-terminal", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			fake := provider.NewScripted(provider.Turn{Text: "durable answer", Reason: provider.ReasonStop})
			session, err := NewSession("sess-terminal", Config{Provider: fake, Durable: durable})
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected terminal persistence failure")
			session.terminalHook = func(current terminalStage) error {
				if current == stage {
					return injected
				}
				return nil
			}
			observation, unsubscribe := session.Observe(session.HighWaterMark())
			defer unsubscribe()
			command, _ := sessioncommand.NewSubmit("cmd-terminal", "complete once")
			if err := session.Apply(context.Background(), command.ForSession(session.ID())); err != nil {
				t.Fatal(err)
			}
			waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := session.WaitIdle(waitCtx); !errors.Is(err, ErrSessionFaulted) || !errors.Is(err, injected) {
				t.Fatalf("WaitIdle durability error = %v", err)
			}
			if session.State() != StateFaulted || durable.Manifest().ActiveRun == nil {
				t.Fatalf("faulted session state=%s manifest=%+v", session.State(), durable.Manifest())
			}
			select {
			case _, ok := <-observation.Events:
				for ok {
					select {
					case _, ok = <-observation.Events:
					case <-time.After(time.Second):
						t.Fatal("faulted observation did not close")
					}
				}
			case <-time.After(time.Second):
				t.Fatal("faulted observation remained open")
			}
			before := len(session.Transcript())
			followUp, _ := sessioncommand.NewSubmit("cmd-after-fault", "must fail")
			if err := session.Apply(context.Background(), followUp.ForSession(session.ID())); !errors.Is(err, ErrSessionFaulted) {
				t.Fatalf("submit after durability fault = %v", err)
			}
			if len(session.Transcript()) != before {
				t.Fatal("faulted session accepted new Transcript state")
			}

			reopened, err := store.Open(root, "sess-terminal")
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := NewSession("sess-terminal", Config{Provider: fake, Durable: reopened})
			if err != nil {
				t.Fatal(err)
			}
			if recovered.State() != StateIdle || reopened.Manifest().ActiveRun != nil {
				t.Fatalf("recovered state=%s manifest=%+v", recovered.State(), reopened.Manifest())
			}
			if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
				t.Fatalf("recovered transcript: %v", err)
			}
			if got := len(fake.Requests()); got != 1 {
				t.Fatalf("restart repeated Provider request: %d", got)
			}
		})
	}
}

func TestCancellationBoundaryPersistenceFailureFaultsClosed(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	durable, err := store.Create(root, "sess-cancel-terminal", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fake := provider.NewScripted(provider.Turn{BlockUntilCancel: true})
	session, err := NewSession("sess-cancel-terminal", Config{Provider: fake, Durable: durable})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected cancellation-boundary failure")
	session.terminalHook = func(stage terminalStage) error {
		if stage == terminalCancellation {
			return injected
		}
		return nil
	}
	submit, _ := sessioncommand.NewSubmit("cmd-submit", "wait")
	if err := session.Apply(context.Background(), submit.ForSession(session.ID())); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(fake.Requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancelCommand, _ := sessioncommand.NewCancel("cmd-cancel")
	if err := session.Apply(context.Background(), cancelCommand.ForSession(session.ID())); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.WaitIdle(waitCtx); !errors.Is(err, injected) || session.State() != StateFaulted {
		t.Fatalf("cancel persistence fault state=%s err=%v", session.State(), err)
	}
}
