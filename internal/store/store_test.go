package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/platform/fileid"
	"github.com/blkcor/coragent/internal/transcript"
)

var testTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func testProviderBinding() ProviderBinding {
	binding := ProviderBinding{
		Adapter: "scripted-offline", WireProtocol: "scripted-v1", EndpointSHA256: strings.Repeat("a", 64),
		CredentialSourceSHA256: strings.Repeat("c", 64),
		Model:                  "scripted-offline", ContextWindow: 32000, MaxOutputTokens: 8000, ToolChoice: "auto",
		UserPreferencesSHA256: strings.Repeat("b", 64), PromptVersion: "test-prompt-v1",
	}
	binding.Digest = binding.ComputeDigest()
	return binding
}

func createTestSession(t *testing.T) (*Session, string) {
	t.Helper()
	root := t.TempDir()
	workspace := t.TempDir()
	s, err := Create(root, "sess-1", workspace, "projection-v1", testProviderBinding(), testTime)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s, root
}

func TestCreateManifestRecordsWorkspaceIdentityAuthorityAndCursor(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	s, err := Create(root, "sess-manifest", workspace, "projection-v9", testProviderBinding(), testTime)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := s.Manifest()
	if m.FormatVersion != FormatVersion {
		t.Errorf("format version = %d, want %d", m.FormatVersion, FormatVersion)
	}
	if m.SessionID != "sess-manifest" {
		t.Errorf("session id = %q", m.SessionID)
	}
	cleanWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	cleanWorkspace, err = filepath.Abs(cleanWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if m.Workspace != cleanWorkspace {
		t.Errorf("workspace = %q, want %q", m.Workspace, cleanWorkspace)
	}
	info, err := os.Stat(cleanWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if m.WorkspaceIdentity != fileid.FromInfo(info) {
		t.Errorf("workspace identity = %q", m.WorkspaceIdentity)
	}
	if m.ProjectionVersion != "projection-v9" {
		t.Errorf("projection version = %q, want projection-v9", m.ProjectionVersion)
	}
	if !m.Authority.WorkspaceRead {
		t.Error("authority is not workspace read-only")
	}
	if m.EventCursor != 0 || m.TranscriptSeq != 0 {
		t.Errorf("initial cursor = %d, transcript seq = %d, want 0", m.EventCursor, m.TranscriptSeq)
	}
}

func TestCreateAppendReopen(t *testing.T) {
	s, root := createTestSession(t)
	rec, err := transcript.New("run-1", testTime, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "question"})
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := s.AppendTranscript(rec)
	if err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if assigned[0].Seq != 1 || rec.Seq != 0 {
		t.Fatalf("assigned seq = %d, input seq = %d", assigned[0].Seq, rec.Seq)
	}
	ev, err := event.New("sess-1", "run-1", 1, testTime, event.KindRunStarted, event.RunStartedPayload{Prompt: "question"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	reopened, err := Open(root, "sess-1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := reopened.Transcript(); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("transcript = %+v", got)
	}
	if got := reopened.Events(); len(got) != 1 || got[0].Cursor != 1 {
		t.Fatalf("events = %+v", got)
	}
	m := reopened.Manifest()
	if m.EventCursor != 1 || m.TranscriptSeq != 1 || !m.Authority.WorkspaceRead {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestAppendOnlyPreservesPriorRecordBytes(t *testing.T) {
	s, _ := createTestSession(t)
	first, _ := transcript.New("run-1", testTime, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "first"})
	if _, err := s.AppendTranscript(first); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, transcriptName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := transcript.New("run-2", testTime.Add(time.Second), transcript.KindUserMessage, transcript.UserMessagePayload{Text: "second"})
	if _, err := s.AppendTranscript(second); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) || string(after[:len(before)]) != string(before) {
		t.Fatal("later append changed prior transcript bytes")
	}
}

func TestCorruptAndUnsupportedDataFailClosed(t *testing.T) {
	_, root := createTestSession(t)
	manifestPath := filepath.Join(root, "sess-1", manifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m.FormatVersion = 99
	data, _ = json.Marshal(m)
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "sess-1"); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Open unsupported = %v", err)
	}
	after, _ := os.ReadFile(manifestPath)
	if string(after) != string(data) {
		t.Fatal("Open rewrote unsupported manifest")
	}
}

func TestProviderBindingCorruptionFailsClosedWithoutRewrite(t *testing.T) {
	_, root := createTestSession(t)
	manifestPath := filepath.Join(root, "sess-1", manifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ProviderBinding.Model = "silently-changed-model"
	corrupt, _ := json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "sess-1"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open corrupt Provider binding = %v", err)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Fatal("Open rewrote a corrupt Provider binding")
	}
}

func TestPartialTranscriptRecordFailsClosed(t *testing.T) {
	s, root := createTestSession(t)
	if err := os.WriteFile(filepath.Join(s.dir, transcriptName), []byte(`{"seq":1`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "sess-1"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open partial = %v", err)
	}
}

func TestPartialEventsRecordFailsClosed(t *testing.T) {
	s, root := createTestSession(t)
	if err := os.WriteFile(filepath.Join(s.dir, eventsName), []byte(`{"cursor":1`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "sess-1"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open partial events = %v", err)
	}
}

func TestLifecycleAndDurableBudget(t *testing.T) {
	s, root := createTestSession(t)
	runID, err := s.BeginRun("cmd-1", testTime)
	if err != nil {
		t.Fatal(err)
	}
	if runID != "run-1" {
		t.Fatalf("run ID = %q", runID)
	}
	if _, err := s.BeginRun("cmd-1", testTime); !errors.Is(err, ErrDuplicateCommand) && err == nil {
		t.Fatalf("duplicate BeginRun = %v", err)
	}
	if _, err := s.ReserveBudget(runID, BudgetLogicalModelCall, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveBudget(runID, BudgetTransportAttempt, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveBudget(runID, BudgetRetryDelay, 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	b := reopened.Manifest().Budgets[runID]
	if b.LogicalModelCalls != 1 || b.TransportAttempts != 1 || b.RetryDelay != 500*time.Millisecond {
		t.Fatalf("budget after reopen = %+v", b)
	}
	if err := reopened.FinishRun(runID, testTime); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(testTime); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(testTime); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if _, err := reopened.BeginRun("cmd-2", testTime); !errors.Is(err, ErrClosed) {
		t.Fatalf("submit to closed session = %v", err)
	}
}

func TestListIgnoresInvalidEntriesAndIsStable(t *testing.T) {
	root := t.TempDir()
	w1, w2 := t.TempDir(), t.TempDir()
	if _, err := Create(root, "old", w1, "v1", testProviderBinding(), testTime); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, "new", w2, "v1", testProviderBinding(), testTime.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "broken"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SessionID != "new" || got[1].SessionID != "old" {
		t.Fatalf("List = %+v", got)
	}
}

func TestSessionIDTraversalAndSymlinkEntryFailClosed(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	for _, id := range []string{"../escape", "nested/id", "..", "bad id"} {
		if _, err := Create(root, id, workspace, "v1", testProviderBinding(), testTime); err == nil {
			t.Errorf("Create accepted %q", id)
		}
		if _, err := Open(root, id); err == nil {
			t.Errorf("Open accepted %q", id)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "linked"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open symlink entry = %v", err)
	}
}

func TestDefaultRootIsDirectAndCreateNeverCollides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".coragent", "sessions")
	if root != want || filepath.Base(root) == "v2" {
		t.Fatalf("DefaultRoot = %q, want %q", root, want)
	}
	workspace := t.TempDir()
	if _, err := Create(root, "same-id", workspace, "v1", testProviderBinding(), testTime); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, "same-id", workspace, "v1", testProviderBinding(), testTime); !errors.Is(err, ErrExists) {
		t.Fatalf("colliding Create = %v", err)
	}
}

func TestCrashMatrixRepairsLogAheadOfManifestButNeverCursorAheadOfLog(t *testing.T) {
	s, root := createTestSession(t)
	manifestPath := filepath.Join(s.dir, manifestName)
	staleManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := transcript.New("run-1", testTime, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "durable"})
	if _, err := s.AppendTranscript(record); err != nil {
		t.Fatal(err)
	}
	ev, _ := event.New("sess-1", "run-1", 1, testTime, event.KindRunStarted, event.RunStartedPayload{Prompt: "durable"})
	if err := s.AppendEvent(ev); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after the complete log records were synced but
	// before either manifest cursor checkpoint became durable.
	if err := os.WriteFile(manifestPath, staleManifest, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Manifest(); got.TranscriptSeq != 1 || got.EventCursor != 1 {
		t.Fatalf("repaired high-water marks = %+v", got)
	}
	afterOpen, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterOpen) != string(staleManifest) {
		t.Fatal("Open rewrote the stale manifest during read-only recovery")
	}
	if err := reopened.RecordCommand("checkpoint", testTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	persisted, err := Open(root, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.Manifest(); got.TranscriptSeq != 1 || got.EventCursor != 1 {
		t.Fatalf("persisted repaired high-water marks = %+v", got)
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var ahead Manifest
	if err := json.Unmarshal(data, &ahead); err != nil {
		t.Fatal(err)
	}
	ahead.TranscriptSeq++
	aheadData, _ := json.Marshal(ahead)
	if err := os.WriteFile(manifestPath, aheadData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "sess-1"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("manifest cursor ahead of log = %v", err)
	}
}

func TestDurableBudgetCrashMatrixAndExactBounds(t *testing.T) {
	s, root := createTestSession(t)
	runID, err := s.BeginRun("budget-command", testTime)
	if err != nil {
		t.Fatal(err)
	}
	for want := uint64(1); want <= MaxLogicalModelCalls; want++ {
		if _, err := s.ReserveBudget(runID, BudgetLogicalModelCall, 0); err != nil {
			t.Fatalf("logical reservation %d: %v", want, err)
		}
		s, err = Open(root, "sess-1")
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Manifest().Budgets[runID].LogicalModelCalls; got != want {
			t.Fatalf("logical count after reopen = %d, want %d", got, want)
		}
	}
	if _, err := s.ReserveBudget(runID, BudgetLogicalModelCall, 0); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("logical bound = %v", err)
	}
	for want := uint64(1); want <= MaxTransportAttempts; want++ {
		if _, err := s.ReserveBudget(runID, BudgetTransportAttempt, 0); err != nil {
			t.Fatalf("transport reservation %d: %v", want, err)
		}
	}
	if _, err := s.ReserveBudget(runID, BudgetTransportAttempt, 0); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("transport bound = %v", err)
	}
	if _, err := s.ReserveBudget(runID, BudgetRetryDelay, MaxRetryDelay); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveBudget(runID, BudgetRetryDelay, time.Nanosecond); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("retry-delay bound = %v", err)
	}
	reopened, err := Open(root, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	bounded := reopened.Manifest().Budgets[runID]
	if bounded.LogicalModelCalls != MaxLogicalModelCalls || bounded.TransportAttempts != MaxTransportAttempts || bounded.RetryDelay != MaxRetryDelay {
		t.Fatalf("bounded budget after restart = %+v", bounded)
	}
	if err := reopened.FinishRun(runID, testTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	newRun, err := reopened.BeginRun("fresh-command", testTime.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if newRun == runID || reopened.Manifest().Budgets[newRun] != (RunBudget{}) || reopened.Manifest().Budgets[runID] != bounded {
		t.Fatalf("old/new budgets = old:%+v new:%+v", reopened.Manifest().Budgets[runID], reopened.Manifest().Budgets[newRun])
	}
}
