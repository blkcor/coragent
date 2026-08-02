package transcript

import (
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
