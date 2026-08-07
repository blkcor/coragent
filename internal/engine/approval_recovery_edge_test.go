package engine_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

// These tests cover crash windows the S1.6 recovery matrix does not list
// explicitly: the action lifecycle reached a terminal journal state AND the
// tool result was persisted, but the run outcome was not (crash during the
// next provider turn, or during a previous recovery). Recovery must not
// append a second tool result or a second lifecycle record for the call.

func recoverCrashedSession(t *testing.T, sessionID, dir, root string) *engine.Session {
	t.Helper()
	reopened, err := store.Open(root, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	fs := workspace.NewFileService(w)
	projector := dataproj.New()
	broker, err := action.NewBrokerWithProjector(projector, tools.NewCatalog(fs, projector)...)
	if err != nil {
		t.Fatal(err)
	}
	fake := provider.NewScripted(provider.Turn{Text: "follow-up", Reason: provider.ReasonStop})
	recovered, err := engine.NewSession(sessionID, engine.Config{
		Provider: fake, Durable: reopened, Broker: broker, FileService: fs,
	})
	if err != nil {
		t.Fatalf("NewSession recovery: %v", err)
	}
	return recovered
}

func countCallResults(t *testing.T, records []transcript.Record, callID string) int {
	t.Helper()
	count := 0
	for _, rec := range records {
		if rec.Kind != transcript.KindToolResult {
			continue
		}
		var p transcript.ToolResultPayload
		if err := rec.DecodePayload(&p); err != nil {
			t.Fatal(err)
		}
		if p.CallID == callID {
			count++
		}
	}
	return count
}

// setupTerminalActionCrash writes a transcript whose action lifecycle ended in
// the given terminal records, with the tool result already persisted, and no
// run outcome (crash after the result, before the next turn completed).
func setupTerminalActionCrash(t *testing.T, terminal []transcript.Kind, result transcript.ToolResultPayload) (dir, root, sessionID string) {
	t.Helper()
	dir = t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nnew line2\nline3\n")
	sourceSHA := sha256Str("line1\nline2\nline3\n")
	expectedSHA := sha256Str("line1\nnew line2\nline3\n")

	sessionID = "sess-terminal-crash"
	root = t.TempDir()
	durable, err := store.Create(root, sessionID, dir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	runID, err := durable.BeginRun("cmd-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	appendRec(t, durable, runID, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "patch"})
	appendRec(t, durable, runID, transcript.KindToolCall, transcript.ToolCallPayload{
		CallID: "call-1", Name: "patch",
		Arguments: json.RawMessage(`{"path":"f.txt","target":"L2","replacement":"new line2"}`),
	})
	appendRec(t, durable, runID, transcript.KindActionPrepared, transcript.ActionPreparedPayload{
		RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
		SourceSHA256: sourceSHA, ExpectedSHA256: expectedSHA, DiffDigest: "d",
	})
	for _, kind := range terminal {
		switch kind {
		case transcript.KindActionApproved:
			appendRec(t, durable, runID, kind, transcript.ActionApprovedPayload{RequestID: "req-1", CommandID: "cmd-2"})
		case transcript.KindActionDenied:
			appendRec(t, durable, runID, kind, transcript.ActionDeniedPayload{RequestID: "req-1", CommandID: "cmd-2"})
		case transcript.KindActionCommitting:
			appendRec(t, durable, runID, kind, transcript.ActionCommittingPayload{RequestID: "req-1"})
		case transcript.KindActionCommitted:
			appendRec(t, durable, runID, kind, transcript.ActionCommittedPayload{RequestID: "req-1", ActualSHA256: expectedSHA})
		case transcript.KindActionAborted:
			appendRec(t, durable, runID, kind, transcript.ActionAbortedPayload{RequestID: "req-1", Reason: transcript.AbortCancelled})
		default:
			t.Fatalf("unsupported terminal kind %s", kind)
		}
	}
	result.CallID = "call-1"
	appendRec(t, durable, runID, transcript.KindToolResult, result)
	return dir, root, sessionID
}

func TestCrashRecoveryCommittedWithExistingToolResult(t *testing.T) {
	dir, root, sessionID := setupTerminalActionCrash(t,
		[]transcript.Kind{transcript.KindActionApproved, transcript.KindActionCommitting, transcript.KindActionCommitted},
		transcript.ToolResultPayload{Outcome: transcript.ToolResultSuccess, Content: "patch applied successfully"})
	recovered := recoverCrashedSession(t, sessionID, dir, root)
	if n := countCallResults(t, recovered.Transcript(), "call-1"); n != 1 {
		t.Fatalf("tool_result count = %d, want exactly 1 (recovery duplicated it)", n)
	}
	if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
		t.Fatalf("recovered transcript invalid: %v", err)
	}
}

func TestCrashRecoveryDeniedWithExistingToolResult(t *testing.T) {
	dir, root, sessionID := setupTerminalActionCrash(t,
		[]transcript.Kind{transcript.KindActionDenied},
		transcript.ToolResultPayload{Outcome: transcript.ToolResultBlocked, Content: "action denied by user"})
	recovered := recoverCrashedSession(t, sessionID, dir, root)
	if n := countCallResults(t, recovered.Transcript(), "call-1"); n != 1 {
		t.Fatalf("tool_result count = %d, want exactly 1 (recovery duplicated it)", n)
	}
	if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
		t.Fatalf("recovered transcript invalid: %v", err)
	}
}

func TestCrashRecoveryAbortedWithExistingToolResult(t *testing.T) {
	dir, root, sessionID := setupTerminalActionCrash(t,
		[]transcript.Kind{transcript.KindActionAborted},
		transcript.ToolResultPayload{Outcome: transcript.ToolResultCancelled, Content: "approval cancelled"})
	recovered := recoverCrashedSession(t, sessionID, dir, root)
	if n := countCallResults(t, recovered.Transcript(), "call-1"); n != 1 {
		t.Fatalf("tool_result count = %d, want exactly 1 (recovery duplicated it)", n)
	}
	if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
		t.Fatalf("recovered transcript invalid: %v", err)
	}
}

// The mirror windows: terminal journal state persisted but the tool result
// was not. Recovery must close the call with a matching result exactly once.
func TestCrashRecoveryDeniedWithoutToolResult(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nline2\nline3\n")
	sourceSHA := sha256Str("line1\nline2\nline3\n")

	sessionID := "sess-denied-noresult"
	root := t.TempDir()
	durable, err := store.Create(root, sessionID, dir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	runID, err := durable.BeginRun("cmd-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	appendRec(t, durable, runID, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "patch"})
	appendRec(t, durable, runID, transcript.KindToolCall, transcript.ToolCallPayload{
		CallID: "call-1", Name: "patch",
		Arguments: json.RawMessage(`{"path":"f.txt","target":"L2","replacement":"new line2"}`),
	})
	appendRec(t, durable, runID, transcript.KindActionPrepared, transcript.ActionPreparedPayload{
		RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
		SourceSHA256: sourceSHA, ExpectedSHA256: sha256Str("x"), DiffDigest: "d",
	})
	appendRec(t, durable, runID, transcript.KindActionDenied, transcript.ActionDeniedPayload{RequestID: "req-1", CommandID: "cmd-2"})

	recovered := recoverCrashedSession(t, sessionID, dir, root)
	if n := countCallResults(t, recovered.Transcript(), "call-1"); n != 1 {
		t.Fatalf("tool_result count = %d, want exactly 1 (recovery did not close the call)", n)
	}
	if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
		t.Fatalf("recovered transcript invalid: %v", err)
	}
}
