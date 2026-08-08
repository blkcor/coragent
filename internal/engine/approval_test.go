package engine_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

func newSessionWithWorkspace(t *testing.T, p provider.Provider, dir string) *engine.Session {
	t.Helper()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	fs := workspace.NewFileService(w)
	projector := dataproj.New()
	broker, err := action.NewBrokerWithProjector(projector, tools.NewCatalog(fs, projector)...)
	if err != nil {
		t.Fatalf("NewBrokerWithProjector: %v", err)
	}
	s, err := engine.NewSession("sess-test", engine.Config{
		Provider: p, Now: fixedClock(), Broker: broker, FileService: fs,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return s
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func sha256Str(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// --- Approval Flow Tests ---

func TestApprovalNormalApprovePath(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nline2\nline3\n")

	turn1 := provider.Turn{
		Text: "I'll patch the file", Reason: provider.ReasonStop,
		ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "patch",
			Arguments: json.RawMessage(`{"path":"f.txt","target":"L2","replacement":"new line2"}`),
		}},
	}
	turn2 := provider.Turn{Text: "patch applied", Reason: provider.ReasonStop}
	fake := provider.NewScripted(turn1, turn2)
	s := newSessionWithWorkspace(t, fake, dir)

	submit, _ := sessioncommand.NewSubmit("cmd-1", "change line 2")
	if err := s.Apply(context.Background(), submit.ForSession(s.ID())); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}

	// Wait for approval_required event
	events, unsub := s.Subscribe()
	defer unsub()
	var approvalEvent event.Event
	for ev := range events {
		if ev.Kind == event.KindApprovalRequired {
			approvalEvent = ev
			break
		}
	}
	if approvalEvent.Kind != event.KindApprovalRequired {
		t.Fatal("never received approval_required event")
	}
	var approvalPayload event.ApprovalRequiredPayload
	if err := approvalEvent.DecodePayload(&approvalPayload); err != nil {
		t.Fatalf("decode approval payload: %v", err)
	}
	if approvalPayload.Path != "f.txt" {
		t.Fatalf("approval path = %q, want f.txt", approvalPayload.Path)
	}

	// Send approve command
	approve, _ := sessioncommand.NewApprove("cmd-2", approvalPayload.RequestID)
	if err := s.Apply(context.Background(), approve.ForSession(s.ID())); err != nil {
		t.Fatalf("Apply approve: %v", err)
	}

	// Wait for run completion
	if err := s.WaitIdle(context.Background()); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}

	// Verify file was modified
	content, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(content) != "line1\nnew line2\nline3\n" {
		t.Fatalf("file content = %q", content)
	}

	// Verify transcript has full action lifecycle
	records := s.Transcript()
	hasPrepared := false
	hasApproved := false
	hasCommitting := false
	hasCommitted := false
	hasToolResult := false
	for _, rec := range records {
		switch rec.Kind {
		case transcript.KindActionPrepared:
			hasPrepared = true
		case transcript.KindActionApproved:
			hasApproved = true
		case transcript.KindActionCommitting:
			hasCommitting = true
		case transcript.KindActionCommitted:
			hasCommitted = true
		case transcript.KindToolResult:
			var p transcript.ToolResultPayload
			_ = rec.DecodePayload(&p)
			if p.Outcome == transcript.ToolResultSuccess && p.CallID == "call-1" {
				hasToolResult = true
			}
		}
	}
	if !hasPrepared || !hasApproved || !hasCommitting || !hasCommitted || !hasToolResult {
		t.Fatalf("incomplete lifecycle: prepared=%v approved=%v committing=%v committed=%v result=%v",
			hasPrepared, hasApproved, hasCommitting, hasCommitted, hasToolResult)
	}
}

func TestApprovalDenyPath(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nline2\nline3\n")

	turn1 := provider.Turn{
		Text: "I'll patch the file", Reason: provider.ReasonStop,
		ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "patch",
			Arguments: json.RawMessage(`{"path":"f.txt","target":"L2","replacement":"new line2"}`),
		}},
	}
	turn2 := provider.Turn{Text: "patch denied", Reason: provider.ReasonStop}
	fake := provider.NewScripted(turn1, turn2)
	s := newSessionWithWorkspace(t, fake, dir)

	submit, _ := sessioncommand.NewSubmit("cmd-1", "change line 2")
	if err := s.Apply(context.Background(), submit.ForSession(s.ID())); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}

	events, unsub := s.Subscribe()
	defer unsub()
	var approvalEvent event.Event
	for ev := range events {
		if ev.Kind == event.KindApprovalRequired {
			approvalEvent = ev
			break
		}
	}
	var approvalPayload event.ApprovalRequiredPayload
	_ = approvalEvent.DecodePayload(&approvalPayload)

	// Send deny command
	deny, _ := sessioncommand.NewDeny("cmd-2", approvalPayload.RequestID)
	if err := s.Apply(context.Background(), deny.ForSession(s.ID())); err != nil {
		t.Fatalf("Apply deny: %v", err)
	}

	if err := s.WaitIdle(context.Background()); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}

	// File must NOT be modified
	content, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(content) != "line1\nline2\nline3\n" {
		t.Fatal("file was modified despite denial")
	}

	// Transcript must have action_denied
	records := s.Transcript()
	hasDenied := false
	for _, rec := range records {
		if rec.Kind == transcript.KindActionDenied {
			hasDenied = true
		}
	}
	if !hasDenied {
		t.Fatal("transcript missing action_denied")
	}
}

func TestApprovalCancelDuringWait(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nline2\nline3\n")

	turn1 := provider.Turn{
		Text: "I'll patch the file", Reason: provider.ReasonStop,
		ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "patch",
			Arguments: json.RawMessage(`{"path":"f.txt","target":"L2","replacement":"new line2"}`),
		}},
	}
	fake := provider.NewScripted(turn1)
	s := newSessionWithWorkspace(t, fake, dir)

	submit, _ := sessioncommand.NewSubmit("cmd-1", "change line 2")
	if err := s.Apply(context.Background(), submit.ForSession(s.ID())); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}

	events, unsub := s.Subscribe()
	defer unsub()
	for ev := range events {
		if ev.Kind == event.KindApprovalRequired {
			break
		}
	}

	// Cancel instead of approve/deny
	cancel, _ := sessioncommand.NewCancel("cmd-2")
	if err := s.Apply(context.Background(), cancel.ForSession(s.ID())); err != nil {
		t.Fatalf("Apply cancel: %v", err)
	}

	if err := s.WaitIdle(context.Background()); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}

	// File must NOT be modified
	content, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(content) != "line1\nline2\nline3\n" {
		t.Fatal("file was modified despite cancellation")
	}

	// Transcript must have action_aborted with reason=cancelled
	records := s.Transcript()
	hasAborted := false
	for _, rec := range records {
		if rec.Kind == transcript.KindActionAborted {
			var p transcript.ActionAbortedPayload
			_ = rec.DecodePayload(&p)
			if p.Reason == transcript.AbortCancelled {
				hasAborted = true
			}
		}
	}
	if !hasAborted {
		t.Fatal("transcript missing action_aborted with reason=cancelled")
	}
}

// --- Crash Recovery Tests ---

func TestCrashRecoveryCommittedWithoutToolResult(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nline2\nline3\n")
	sourceSHA := sha256Str("line1\nline2\nline3\n")
	expectedSHA := sha256Str("line1\nnew line2\nline3\n")
	// Write the file to expected state (simulating post-crash where write happened)
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("line1\nnew line2\nline3\n"), 0o600)

	root := t.TempDir()
	durable, err := store.Create(root, "sess-crash", dir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	runID, err := durable.BeginRun("cmd-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Write full action lifecycle up to committed, but no tool_result
	appendRec(t, durable, runID, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "patch"})
	appendRec(t, durable, runID, transcript.KindToolCall, transcript.ToolCallPayload{
		CallID: "call-1", Name: "patch",
		Arguments: json.RawMessage(`{"path":"f.txt","target":"L2","replacement":"new line2"}`),
	})
	appendRec(t, durable, runID, transcript.KindActionPrepared, transcript.ActionPreparedPayload{
		RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
		SourceSHA256: sourceSHA, ExpectedSHA256: expectedSHA, DiffDigest: "d",
	})
	appendRec(t, durable, runID, transcript.KindActionApproved, transcript.ActionApprovedPayload{
		RequestID: "req-1", CommandID: "cmd-2",
	})
	appendRec(t, durable, runID, transcript.KindActionCommitting, transcript.ActionCommittingPayload{
		RequestID: "req-1",
	})
	appendRec(t, durable, runID, transcript.KindActionCommitted, transcript.ActionCommittedPayload{
		RequestID: "req-1", ActualSHA256: expectedSHA,
	})
	// No tool_result — simulating crash after committed but before result persisted

	// Reopen and recover
	reopened, err := store.Open(root, "sess-crash")
	if err != nil {
		t.Fatal(err)
	}

	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	fs := workspace.NewFileService(w)
	projector := dataproj.New()
	broker, _ := action.NewBrokerWithProjector(projector, tools.NewCatalog(fs, projector)...)

	fake := provider.NewScripted(provider.Turn{Text: "follow-up", Reason: provider.ReasonStop})
	recovered, err := engine.NewSession("sess-crash", engine.Config{
		Provider: fake, Durable: reopened, Broker: broker, FileService: fs,
	})
	if err != nil {
		t.Fatalf("NewSession recovery: %v", err)
	}

	// Verify tool_result(success) was added
	records := recovered.Transcript()
	hasRecoveredResult := false
	for _, rec := range records {
		if rec.Kind == transcript.KindToolResult {
			var p transcript.ToolResultPayload
			_ = rec.DecodePayload(&p)
			if p.CallID == "call-1" && p.Outcome == transcript.ToolResultSuccess &&
				strings.Contains(p.Content, "already committed") {
				hasRecoveredResult = true
			}
		}
	}
	if !hasRecoveredResult {
		t.Fatal("recovery did not add tool_result(success) for committed action")
	}
	if recovered.State() != engine.StateIdle {
		t.Fatalf("state = %s, want idle", recovered.State())
	}
}

func TestCrashRecoveryCommittingDiskMatchesExpected(t *testing.T) {
	dir := t.TempDir()
	sourceSHA := sha256Str("line1\nline2\nline3\n")
	expectedSHA := sha256Str("line1\nnew line2\nline3\n")
	// Write the file to expected state (write already happened)
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("line1\nnew line2\nline3\n"), 0o600)

	root := t.TempDir()
	durable, err := store.Create(root, "sess-crash", dir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
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
	appendRec(t, durable, runID, transcript.KindActionApproved, transcript.ActionApprovedPayload{
		RequestID: "req-1", CommandID: "cmd-2",
	})
	appendRec(t, durable, runID, transcript.KindActionCommitting, transcript.ActionCommittingPayload{
		RequestID: "req-1",
	})
	// No committed and no tool_result — crash after committing, write happened

	reopened, err := store.Open(root, "sess-crash")
	if err != nil {
		t.Fatal(err)
	}

	w, _ := workspace.Open(dir)
	defer func() { _ = w.Close() }()
	fs := workspace.NewFileService(w)
	projector := dataproj.New()
	broker, _ := action.NewBrokerWithProjector(projector, tools.NewCatalog(fs, projector)...)

	fake := provider.NewScripted(provider.Turn{Text: "follow-up", Reason: provider.ReasonStop})
	recovered, err := engine.NewSession("sess-crash", engine.Config{
		Provider: fake, Durable: reopened, Broker: broker, FileService: fs,
	})
	if err != nil {
		t.Fatalf("NewSession recovery: %v", err)
	}

	records := recovered.Transcript()
	hasCommitted := false
	hasRecoveredResult := false
	for _, rec := range records {
		if rec.Kind == transcript.KindActionCommitted {
			hasCommitted = true
		}
		if rec.Kind == transcript.KindToolResult {
			var p transcript.ToolResultPayload
			_ = rec.DecodePayload(&p)
			if p.CallID == "call-1" && p.Outcome == transcript.ToolResultSuccess {
				hasRecoveredResult = true
			}
		}
	}
	if !hasCommitted {
		t.Fatal("recovery should add action_committed (disk = expected)")
	}
	if !hasRecoveredResult {
		t.Fatal("recovery should add tool_result(success)")
	}
	if recovered.State() != engine.StateIdle {
		t.Fatalf("state = %s, want idle", recovered.State())
	}
}

func TestCrashRecoveryCommittingDiskMatchesSource(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nline2\nline3\n")
	sourceSHA := sha256Str("line1\nline2\nline3\n")
	expectedSHA := sha256Str("line1\nnew line2\nline3\n")

	root := t.TempDir()
	durable, err := store.Create(root, "sess-crash", dir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
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
	appendRec(t, durable, runID, transcript.KindActionApproved, transcript.ActionApprovedPayload{
		RequestID: "req-1", CommandID: "cmd-2",
	})
	appendRec(t, durable, runID, transcript.KindActionCommitting, transcript.ActionCommittingPayload{
		RequestID: "req-1",
	})
	// No committed — crash after committing, write did NOT happen, source unchanged

	reopened, err := store.Open(root, "sess-crash")
	if err != nil {
		t.Fatal(err)
	}

	w, _ := workspace.Open(dir)
	defer func() { _ = w.Close() }()
	fs := workspace.NewFileService(w)
	projector := dataproj.New()
	broker, _ := action.NewBrokerWithProjector(projector, tools.NewCatalog(fs, projector)...)

	fake := provider.NewScripted(provider.Turn{Text: "follow-up", Reason: provider.ReasonStop})
	recovered, err := engine.NewSession("sess-crash", engine.Config{
		Provider: fake, Durable: reopened, Broker: broker, FileService: fs,
	})
	if err != nil {
		t.Fatalf("NewSession recovery: %v", err)
	}

	// File should now be patched (auto-retry succeeded)
	content, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(content) != "line1\nnew line2\nline3\n" {
		t.Fatalf("auto-retry should have patched file, got: %q", content)
	}

	records := recovered.Transcript()
	hasCommitted := false
	hasRecoveredResult := false
	for _, rec := range records {
		if rec.Kind == transcript.KindActionCommitted {
			hasCommitted = true
		}
		if rec.Kind == transcript.KindToolResult {
			var p transcript.ToolResultPayload
			_ = rec.DecodePayload(&p)
			if p.CallID == "call-1" && p.Outcome == transcript.ToolResultSuccess &&
				strings.Contains(p.Content, "auto-retry") {
				hasRecoveredResult = true
			}
		}
	}
	if !hasCommitted {
		t.Fatal("auto-retry should add action_committed")
	}
	if !hasRecoveredResult {
		t.Fatal("auto-retry should add tool_result(success)")
	}
	if recovered.State() != engine.StateIdle {
		t.Fatalf("state = %s, want idle", recovered.State())
	}
}

func TestCrashRecoveryCommittingDiskMismatch(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "externally modified content\n")
	sourceSHA := sha256Str("line1\nline2\nline3\n")
	expectedSHA := sha256Str("line1\nnew line2\nline3\n")

	root := t.TempDir()
	durable, err := store.Create(root, "sess-crash", dir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
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
	appendRec(t, durable, runID, transcript.KindActionApproved, transcript.ActionApprovedPayload{
		RequestID: "req-1", CommandID: "cmd-2",
	})
	appendRec(t, durable, runID, transcript.KindActionCommitting, transcript.ActionCommittingPayload{
		RequestID: "req-1",
	})
	// Disk content matches neither source nor expected (externally modified)

	reopened, err := store.Open(root, "sess-crash")
	if err != nil {
		t.Fatal(err)
	}

	w, _ := workspace.Open(dir)
	defer func() { _ = w.Close() }()
	fs := workspace.NewFileService(w)
	projector := dataproj.New()
	broker, _ := action.NewBrokerWithProjector(projector, tools.NewCatalog(fs, projector)...)

	fake := provider.NewScripted(provider.Turn{Text: "follow-up", Reason: provider.ReasonStop})
	recovered, err := engine.NewSession("sess-crash", engine.Config{
		Provider: fake, Durable: reopened, Broker: broker, FileService: fs,
	})
	if err != nil {
		t.Fatalf("NewSession recovery: %v", err)
	}

	// File must NOT be modified
	content, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(content) != "externally modified content\n" {
		t.Fatal("file was modified despite stale detection")
	}

	records := recovered.Transcript()
	hasAborted := false
	hasStaleResult := false
	for _, rec := range records {
		if rec.Kind == transcript.KindActionAborted {
			var p transcript.ActionAbortedPayload
			_ = rec.DecodePayload(&p)
			if p.Reason == transcript.AbortStale {
				hasAborted = true
			}
		}
		if rec.Kind == transcript.KindToolResult {
			var p transcript.ToolResultPayload
			_ = rec.DecodePayload(&p)
			if p.CallID == "call-1" && p.Outcome == transcript.ToolResultError &&
				strings.Contains(p.Content, "externally") {
				hasStaleResult = true
			}
		}
	}
	if !hasAborted {
		t.Fatal("recovery should add action_aborted with reason=stale")
	}
	if !hasStaleResult {
		t.Fatal("recovery should add tool_result(error) for stale state")
	}
	if recovered.State() != engine.StateIdle {
		t.Fatalf("state = %s, want idle", recovered.State())
	}
}

func TestCrashRecoveryPreparedWithoutApproved(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "f.txt", "line1\nline2\nline3\n")

	root := t.TempDir()
	durable, err := store.Create(root, "sess-crash", dir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
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
		SourceSHA256:   sha256Str("line1\nline2\nline3\n"),
		ExpectedSHA256: sha256Str("line1\nnew line2\nline3\n"),
		DiffDigest:     "d",
	})
	// No approved — crash before user could respond

	reopened, _ := store.Open(root, "sess-crash")
	w, _ := workspace.Open(dir)
	defer func() { _ = w.Close() }()
	fs := workspace.NewFileService(w)
	projector := dataproj.New()
	broker, _ := action.NewBrokerWithProjector(projector, tools.NewCatalog(fs, projector)...)

	fake := provider.NewScripted(provider.Turn{Text: "follow-up", Reason: provider.ReasonStop})
	recovered, err := engine.NewSession("sess-crash", engine.Config{
		Provider: fake, Durable: reopened, Broker: broker, FileService: fs,
	})
	if err != nil {
		t.Fatalf("NewSession recovery: %v", err)
	}

	// File must NOT be modified
	content, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(content) != "line1\nline2\nline3\n" {
		t.Fatal("file was modified despite no approval")
	}

	records := recovered.Transcript()
	hasAborted := false
	hasCancelledResult := false
	for _, rec := range records {
		if rec.Kind == transcript.KindActionAborted {
			var p transcript.ActionAbortedPayload
			_ = rec.DecodePayload(&p)
			if p.Reason == transcript.AbortCancelled {
				hasAborted = true
			}
		}
		if rec.Kind == transcript.KindToolResult {
			var p transcript.ToolResultPayload
			_ = rec.DecodePayload(&p)
			if p.CallID == "call-1" && p.Outcome == transcript.ToolResultCancelled {
				hasCancelledResult = true
			}
		}
	}
	if !hasAborted {
		t.Fatal("recovery should add action_aborted for unapproved prepared")
	}
	if !hasCancelledResult {
		t.Fatal("recovery should add tool_result(cancelled) for unapproved prepared")
	}
}

func appendRec(t *testing.T, durable *store.Session, runID string, kind transcript.Kind, payload any) {
	t.Helper()
	rec, err := transcript.New(runID, time.Now(), kind, payload)
	if err != nil {
		t.Fatalf("transcript.New(%s): %v", kind, err)
	}
	if _, err := durable.AppendTranscript(rec); err != nil {
		t.Fatalf("AppendTranscript(%s): %v", kind, err)
	}
}
