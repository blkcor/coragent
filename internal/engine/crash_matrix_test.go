package engine_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/transcript"
)

func TestInterruptedRunCrashMatrixBeforeAndAfterDurableBoundaries(t *testing.T) {
	type phase struct {
		name    string
		prepare func(*testing.T, *store.Session, string)
	}
	appendUser := func(t *testing.T, durable *store.Session, runID string) {
		t.Helper()
		user, _ := transcript.New(runID, time.Now(), transcript.KindUserMessage, transcript.UserMessagePayload{Text: "inspect"})
		if _, err := durable.AppendTranscript(user); err != nil {
			t.Fatal(err)
		}
	}
	appendCall := func(t *testing.T, durable *store.Session, runID string) {
		t.Helper()
		call, _ := transcript.New(runID, time.Now(), transcript.KindToolCall, transcript.ToolCallPayload{CallID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)})
		if _, err := durable.AppendTranscript(call); err != nil {
			t.Fatal(err)
		}
	}
	appendResult := func(t *testing.T, durable *store.Session, runID string) {
		t.Helper()
		result, _ := transcript.New(runID, time.Now(), transcript.KindToolResult, transcript.ToolResultPayload{CallID: "call-1", Outcome: transcript.ToolResultSuccess, Content: "ok"})
		if _, err := durable.AppendTranscript(result); err != nil {
			t.Fatal(err)
		}
	}
	appendStarted := func(t *testing.T, durable *store.Session, runID string) {
		t.Helper()
		started, _ := event.New("sess-crash-matrix", runID, durable.Manifest().EventCursor+1, time.Now(), event.KindRunStarted, event.RunStartedPayload{Prompt: "inspect"})
		if err := durable.AppendEvent(started); err != nil {
			t.Fatal(err)
		}
	}
	appendOutcome := func(t *testing.T, durable *store.Session, runID string) {
		t.Helper()
		outcome, _ := transcript.New(runID, time.Now(), transcript.KindRunOutcome, transcript.RunOutcomePayload{Outcome: transcript.RunOutcomeInterrupted, Cause: transcript.CauseInterrupted})
		if _, err := durable.AppendTranscript(outcome); err != nil {
			t.Fatal(err)
		}
	}
	appendTerminal := func(t *testing.T, durable *store.Session, runID string) {
		t.Helper()
		terminal, _ := event.New("sess-crash-matrix", runID, durable.Manifest().EventCursor+1, time.Now(), event.KindRunFailed, event.RunFailedPayload{Cause: event.CauseInterrupted})
		if err := durable.AppendEvent(terminal); err != nil {
			t.Fatal(err)
		}

	}
	phases := []phase{
		{name: "before_transcript_append", prepare: func(*testing.T, *store.Session, string) {}},
		{name: "after_transcript_append", prepare: appendUser},
		{name: "before_tool_result", prepare: func(t *testing.T, durable *store.Session, runID string) {
			appendUser(t, durable, runID)
			appendCall(t, durable, runID)
		}},
		{name: "after_tool_result", prepare: func(t *testing.T, durable *store.Session, runID string) {
			appendUser(t, durable, runID)
			appendCall(t, durable, runID)
			appendResult(t, durable, runID)
		}},
		{name: "before_cursor_checkpoint", prepare: appendUser},
		{name: "after_cursor_checkpoint", prepare: func(t *testing.T, durable *store.Session, runID string) {
			appendUser(t, durable, runID)
			appendStarted(t, durable, runID)
		}},
		{name: "before_terminal_outcome", prepare: func(t *testing.T, durable *store.Session, runID string) {
			appendUser(t, durable, runID)
			appendStarted(t, durable, runID)
		}},
		{name: "after_terminal_outcome", prepare: func(t *testing.T, durable *store.Session, runID string) {
			appendUser(t, durable, runID)
			appendStarted(t, durable, runID)
			appendOutcome(t, durable, runID)
		}},
		{name: "after_terminal_event_before_finish", prepare: func(t *testing.T, durable *store.Session, runID string) {
			appendUser(t, durable, runID)
			appendStarted(t, durable, runID)
			appendOutcome(t, durable, runID)
			appendTerminal(t, durable, runID)
		}},
	}

	for _, phase := range phases {
		t.Run(phase.name, func(t *testing.T) {
			root := t.TempDir()
			workspace := t.TempDir()
			durable, err := store.Create(root, "sess-crash-matrix", workspace, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			runID, err := durable.BeginRun("cmd-crash", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			phase.prepare(t, durable, runID)
			fake := provider.NewScripted(provider.Turn{Text: "explicit follow-up", Reason: provider.ReasonStop})
			reopened, err := store.Open(root, "sess-crash-matrix")
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := engine.NewSession("sess-crash-matrix", engine.Config{Provider: fake, Durable: reopened})
			if err != nil {
				t.Fatal(err)
			}
			assertReconciledCrashState(t, recovered, reopened, runID)
			firstRecordCount, firstEventCount := len(recovered.Transcript()), len(recovered.Events())

			againStore, err := store.Open(root, "sess-crash-matrix")
			if err != nil {
				t.Fatal(err)
			}
			again, err := engine.NewSession("sess-crash-matrix", engine.Config{Provider: fake, Durable: againStore})
			if err != nil {
				t.Fatal(err)
			}
			if len(again.Transcript()) != firstRecordCount || len(again.Events()) != firstEventCount || len(fake.Requests()) != 0 {
				t.Fatal("second reconciliation changed facts or contacted Provider")
			}
			followUp, _ := sessioncommand.NewSubmit("cmd-follow-up", "continue")
			if err := again.Apply(context.Background(), followUp.ForSession(again.ID())); err != nil {
				t.Fatal(err)
			}
			waitIdle(t, again)
			if len(fake.Requests()) != 1 {
				t.Fatalf("explicit follow-up requests = %d", len(fake.Requests()))
			}
		})
	}
}

func assertReconciledCrashState(t *testing.T, session *engine.Session, durable *store.Session, runID string) {
	t.Helper()
	if session.State() != engine.StateIdle || durable.Manifest().ActiveRun != nil {
		t.Fatalf("reconciled state=%s manifest=%+v", session.State(), durable.Manifest())
	}
	if err := transcript.ValidateTranscript(session.Transcript()); err != nil {
		t.Fatalf("reconciled transcript: %v", err)
	}
	outcomes := 0
	for _, record := range session.Transcript() {
		if record.RunID == runID && record.Kind == transcript.KindRunOutcome {
			outcomes++
		}
	}
	terminals := 0
	for _, ev := range session.Events() {
		if ev.RunID == runID && (ev.Kind == event.KindRunCompleted || ev.Kind == event.KindRunFailed || ev.Kind == event.KindRunCancelled) {
			terminals++
		}
	}
	if outcomes != 1 || terminals != 1 {
		t.Fatalf("reconciled outcomes=%d terminals=%d", outcomes, terminals)
	}
}
