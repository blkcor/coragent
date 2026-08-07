package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPairingValidationAndOpenCalls(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	call, _ := New("run-1", now, KindToolCall, ToolCallPayload{CallID: "c1", Name: "read", Arguments: []byte(`{"path":"a.go"}`)})
	call.Seq = 1
	if err := ValidateRecords([]Record{call}); err != nil {
		t.Fatalf("ValidateRecords open call: %v", err)
	}
	if err := ValidateTranscript([]Record{call}); !errors.Is(err, ErrUnpairedToolCall) {
		t.Fatalf("ValidateTranscript = %v", err)
	}
	open, err := OpenToolCalls([]Record{call})
	if err != nil || len(open) != 1 || open[0].CallID != "c1" {
		t.Fatalf("OpenToolCalls = %+v, %v", open, err)
	}
	result, _ := New("run-1", now, KindToolResult, ToolResultPayload{CallID: "c1", Outcome: ToolResultSuccess, Content: "ok"})
	result.Seq = 2
	if err := ValidateTranscript([]Record{call, result}); err != nil {
		t.Fatalf("ValidateTranscript paired: %v", err)
	}
}

func TestUnknownPayloadFieldsFailClosed(t *testing.T) {
	record := Record{Seq: 1, Time: time.Now(), RunID: "run-1", Kind: KindRunOutcome, Payload: json.RawMessage(`{"outcome":"completed","credential":"must-not-load"}`)}
	if err := record.Validate(); err == nil {
		t.Fatal("Validate accepted undeclared Transcript payload content")
	}
	if _, err := New("run-1", time.Now(), KindRunOutcome, map[string]string{"outcome": "completed", "credential": "must-not-enter"}); err == nil {
		t.Fatal("New accepted undeclared Transcript payload type")
	}
}

func TestPairingRejectsCrossRunResultAndEarlyOutcome(t *testing.T) {
	now := time.Now()
	call, _ := New("run-1", now, KindToolCall, ToolCallPayload{CallID: "c1", Name: "read", Arguments: json.RawMessage(`{}`)})
	call.Seq = 1
	result, _ := New("run-2", now, KindToolResult, ToolResultPayload{CallID: "c1", Outcome: ToolResultSuccess, Content: "ok"})
	result.Seq = 2
	if err := ValidateRecords([]Record{call, result}); !errors.Is(err, ErrToolRunMismatch) {
		t.Fatalf("cross-run result = %v", err)
	}
	outcome, _ := New("run-1", now, KindRunOutcome, RunOutcomePayload{Outcome: RunOutcomeCompleted})
	outcome.Seq = 2
	if err := ValidateRecords([]Record{call, outcome}); !errors.Is(err, ErrTerminalWithOpenCall) {
		t.Fatalf("early outcome = %v", err)
	}
}

func TestOpenToolCallsForRunRejectsAmbiguousRecovery(t *testing.T) {
	call, _ := New("run-1", time.Now(), KindToolCall, ToolCallPayload{CallID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)})
	call.Seq = 1
	if _, err := OpenToolCallsForRun([]Record{call}, "run-2"); !errors.Is(err, ErrUnpairedToolCall) {
		t.Fatalf("OpenToolCallsForRun ambiguous state = %v", err)
	}
	open, err := OpenToolCallsForRun([]Record{call}, "run-1")
	if err != nil || len(open) != 1 || open[0].CallID != "call-1" {
		t.Fatalf("OpenToolCallsForRun = %+v, %v", open, err)
	}
}

func TestSessionCloseIsTheTerminalTranscriptBoundary(t *testing.T) {
	now := time.Now()
	closed, err := New("", now, KindSessionClosed, SessionClosedPayload{})
	if err != nil {
		t.Fatal(err)
	}
	closed.Seq = 1
	message, err := New("run-1", now, KindUserMessage, UserMessagePayload{Text: "must not follow close"})
	if err != nil {
		t.Fatal(err)
	}
	message.Seq = 2
	if err := ValidateRecords([]Record{closed, message}); !errors.Is(err, ErrRecordsAfterClose) {
		t.Fatalf("record after close = %v", err)
	}

	duplicate := closed
	duplicate.Seq = 2
	if err := ValidateRecords([]Record{closed, duplicate}); !errors.Is(err, ErrDuplicateSessionClose) {
		t.Fatalf("duplicate close = %v", err)
	}
}

// --- S1.5 Action Journal Tests ---

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func newRecord(t *testing.T, runID string, at time.Time, kind Kind, payload any) Record {
	t.Helper()
	rec, err := New(runID, at, kind, payload)
	if err != nil {
		t.Fatalf("New(%s): %v", kind, err)
	}
	return rec
}

func TestActionRecordRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	diff := "--- a.txt\n+++ a.txt\n@@ -1,3 +1,3 @@\n-old\n+new\n"
	digest := sha256hex(diff)

	prepared, err := New("run-1", now, KindActionPrepared, ActionPreparedPayload{
		RequestID:      "req-1",
		ToolCallID:     "call-1",
		Path:           "a.txt",
		SourceSHA256:   sha256hex("old content"),
		ExpectedSHA256: sha256hex("new content"),
		DiffDigest:     digest,
	})
	if err != nil {
		t.Fatalf("New action_prepared: %v", err)
	}
	prepared.Seq = 1

	var decoded ActionPreparedPayload
	if err := prepared.DecodePayload(&decoded); err != nil {
		t.Fatalf("DecodePayload action_prepared: %v", err)
	}
	if decoded.RequestID != "req-1" || decoded.ToolCallID != "call-1" || decoded.DiffDigest != digest {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}

	// Verify strict JSON: unknown fields rejected
	bad := Record{Seq: 1, Time: now, RunID: "run-1", Kind: KindActionPrepared,
		Payload: json.RawMessage(`{"request_id":"r","tool_call_id":"t","path":"p","source_sha256":"s","expected_sha256":"e","diff_digest":"d","extra":"nope"}`)}
	if err := bad.Validate(); err == nil {
		t.Fatal("action_prepared accepted unknown field")
	}
}

func TestActionRecordValidationRejectsMissingFields(t *testing.T) {
	now := time.Now()

	tests := []struct {
		kind    Kind
		payload any
	}{
		{KindActionPrepared, ActionPreparedPayload{}},                                   // all empty
		{KindActionPrepared, ActionPreparedPayload{RequestID: "r"}},                     // missing most
		{KindActionApproved, ActionApprovedPayload{}},                                   // both empty
		{KindActionApproved, ActionApprovedPayload{RequestID: "r"}},                     // missing command_id
		{KindActionDenied, ActionDeniedPayload{}},                                       // both empty
		{KindActionDenied, ActionDeniedPayload{RequestID: "r"}},                         // missing command_id
		{KindActionCommitting, ActionCommittingPayload{}},                               // empty
		{KindActionCommitted, ActionCommittedPayload{}},                                 // both empty
		{KindActionCommitted, ActionCommittedPayload{RequestID: "r"}},                   // missing actual_sha256
		{KindActionAborted, ActionAbortedPayload{}},                                     // empty request_id
		{KindActionAborted, ActionAbortedPayload{RequestID: "r", Reason: "bad-reason"}}, // unknown reason
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			_, err := New("run-1", now, tt.kind, tt.payload)
			if err == nil {
				t.Fatalf("New(%s) accepted invalid payload: %+v", tt.kind, tt.payload)
			}
			if !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("New(%s) error is not ErrInvalidRecord: %v", tt.kind, err)
			}
		})
	}
}

func TestActionLifecycleValidFullChain(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
		withSeq(newRecord(t, "run-1", now, KindActionCommitting, ActionCommittingPayload{
			RequestID: "req-1",
		}), 3),
		withSeq(newRecord(t, "run-1", now, KindActionCommitted, ActionCommittedPayload{
			RequestID: "req-1", ActualSHA256: "abc",
		}), 4),
	}
	if err := ValidateRecords(records); err != nil {
		t.Fatalf("valid full chain rejected: %v", err)
	}
}

func TestActionLifecycleValidDeniedPath(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionDenied, ActionDeniedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
	}
	if err := ValidateRecords(records); err != nil {
		t.Fatalf("valid denied path rejected: %v", err)
	}
}

func TestActionLifecycleValidAbortedAfterPrepared(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionAborted, ActionAbortedPayload{
			RequestID: "req-1", Reason: AbortStale,
		}), 2),
	}
	if err := ValidateRecords(records); err != nil {
		t.Fatalf("valid aborted-after-prepared path rejected: %v", err)
	}
}

func TestActionLifecycleValidAbortedAfterCommitting(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
		withSeq(newRecord(t, "run-1", now, KindActionCommitting, ActionCommittingPayload{
			RequestID: "req-1",
		}), 3),
		withSeq(newRecord(t, "run-1", now, KindActionAborted, ActionAbortedPayload{
			RequestID: "req-1", Reason: AbortCancelled,
		}), 4),
	}
	if err := ValidateRecords(records); err != nil {
		t.Fatalf("valid aborted-after-committing path rejected: %v", err)
	}
}

func TestActionLifecycleRejectsApprovedWithoutPrepared(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 1),
	}
	if err := ValidateRecords(records); !errors.Is(err, ErrOrphanActionRecord) {
		t.Fatalf("approved without prepared = %v", err)
	}
}

func TestActionLifecycleRejectsCommittingWithoutApproved(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionCommitting, ActionCommittingPayload{
			RequestID: "req-1",
		}), 2),
	}
	if err := ValidateRecords(records); !errors.Is(err, ErrActionWithoutApproval) {
		t.Fatalf("committing without approved = %v", err)
	}
}

func TestActionLifecycleRejectsCommittedWithoutCommitting(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
		withSeq(newRecord(t, "run-1", now, KindActionCommitted, ActionCommittedPayload{
			RequestID: "req-1", ActualSHA256: "abc",
		}), 3),
	}
	if err := ValidateRecords(records); !errors.Is(err, ErrCommittedWithoutCommitting) {
		t.Fatalf("committed without committing = %v", err)
	}
}

func TestActionLifecycleRejectsDuplicatePrepared(t *testing.T) {
	now := time.Now()
	prepared := ActionPreparedPayload{
		RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
		SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
	}
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, prepared), 1),
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, prepared), 2),
	}
	if err := ValidateRecords(records); !errors.Is(err, ErrDuplicateActionRequest) {
		t.Fatalf("duplicate prepared = %v", err)
	}
}

func TestActionLifecycleRejectsDuplicateCommitted(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
		withSeq(newRecord(t, "run-1", now, KindActionCommitting, ActionCommittingPayload{
			RequestID: "req-1",
		}), 3),
		withSeq(newRecord(t, "run-1", now, KindActionCommitted, ActionCommittedPayload{
			RequestID: "req-1", ActualSHA256: "abc",
		}), 4),
		withSeq(newRecord(t, "run-1", now, KindActionCommitted, ActionCommittedPayload{
			RequestID: "req-1", ActualSHA256: "def",
		}), 5),
	}
	if err := ValidateRecords(records); !errors.Is(err, ErrDuplicateActionCommitted) {
		t.Fatalf("duplicate committed = %v", err)
	}
}

func TestActionLifecycleRejectsRecordAfterDenied(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionDenied, ActionDeniedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-2",
		}), 3),
	}
	if err := ValidateRecords(records); !errors.Is(err, ErrActionAfterTerminal) {
		t.Fatalf("record after denied = %v", err)
	}
}

func TestActionLifecycleRejectsRecordAfterCommitted(t *testing.T) {
	now := time.Now()
	records := []Record{
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "call-1", Path: "f.txt",
			SourceSHA256: "s", ExpectedSHA256: "e", DiffDigest: "d",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
		withSeq(newRecord(t, "run-1", now, KindActionCommitting, ActionCommittingPayload{
			RequestID: "req-1",
		}), 3),
		withSeq(newRecord(t, "run-1", now, KindActionCommitted, ActionCommittedPayload{
			RequestID: "req-1", ActualSHA256: "abc",
		}), 4),
		withSeq(newRecord(t, "run-1", now, KindActionAborted, ActionAbortedPayload{
			RequestID: "req-1", Reason: AbortStale,
		}), 5),
	}
	if err := ValidateRecords(records); !errors.Is(err, ErrActionAfterTerminal) {
		t.Fatalf("record after committed = %v", err)
	}
}

func TestDiffDigestMatchesPreparePhase(t *testing.T) {
	diff := "--- a.txt\n+++ a.txt\n@@ -1,3 +1,3 @@\n-old\n+new\n"
	expected := sha256hex(diff)

	now := time.Now()
	prepared := withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
		RequestID: "req-1", ToolCallID: "call-1", Path: "a.txt",
		SourceSHA256:   sha256hex("old"),
		ExpectedSHA256: sha256hex("new"),
		DiffDigest:     expected,
	}), 1)

	var decoded ActionPreparedPayload
	if err := prepared.DecodePayload(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.DiffDigest != expected {
		t.Fatalf("diff_digest %q != expected %q", decoded.DiffDigest, expected)
	}

	// Prove a different diff produces a different digest
	otherDigest := sha256hex("different diff content")
	if otherDigest == expected {
		t.Fatal("different diffs must produce different digests")
	}
}

func TestActionLifecycleMultipleIndependentRequests(t *testing.T) {
	now := time.Now()
	records := []Record{
		// Request 1: full chain
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-1", ToolCallID: "c1", Path: "a.txt",
			SourceSHA256: "s1", ExpectedSHA256: "e1", DiffDigest: "d1",
		}), 1),
		withSeq(newRecord(t, "run-1", now, KindActionApproved, ActionApprovedPayload{
			RequestID: "req-1", CommandID: "cmd-1",
		}), 2),
		withSeq(newRecord(t, "run-1", now, KindActionCommitting, ActionCommittingPayload{
			RequestID: "req-1",
		}), 3),
		withSeq(newRecord(t, "run-1", now, KindActionCommitted, ActionCommittedPayload{
			RequestID: "req-1", ActualSHA256: "abc",
		}), 4),
		// Request 2: denied
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-2", ToolCallID: "c2", Path: "b.txt",
			SourceSHA256: "s2", ExpectedSHA256: "e2", DiffDigest: "d2",
		}), 5),
		withSeq(newRecord(t, "run-1", now, KindActionDenied, ActionDeniedPayload{
			RequestID: "req-2", CommandID: "cmd-2",
		}), 6),
		// Request 3: aborted after prepared
		withSeq(newRecord(t, "run-1", now, KindActionPrepared, ActionPreparedPayload{
			RequestID: "req-3", ToolCallID: "c3", Path: "c.txt",
			SourceSHA256: "s3", ExpectedSHA256: "e3", DiffDigest: "d3",
		}), 7),
		withSeq(newRecord(t, "run-1", now, KindActionAborted, ActionAbortedPayload{
			RequestID: "req-3", Reason: AbortPolicyBlock,
		}), 8),
	}
	if err := ValidateRecords(records); err != nil {
		t.Fatalf("multiple independent requests rejected: %v", err)
	}
}

func withSeq(rec Record, seq uint64) Record {
	rec.Seq = seq
	return rec
}
