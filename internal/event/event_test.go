package event

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var fixtureTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func samplePayloads() map[Kind]any {
	return map[Kind]any{
		KindRunStarted:       RunStartedPayload{Prompt: "explain what main.go does"},
		KindRunCompleted:     RunCompletedPayload{},
		KindRunFailed:        RunFailedPayload{Cause: CauseProviderPermanent},
		KindRunCancelled:     RunCancelledPayload{},
		KindAssistantText:    AssistantTextPayload{Text: "main.go wires the CLI to the engine."},
		KindAssistantDelta:   AssistantDeltaPayload{Text: "main.go "},
		KindToolStarted:      ToolStartedPayload{CallID: "call-1", Name: "read"},
		KindToolFinished:     ToolFinishedPayload{CallID: "call-1", Outcome: "success"},
		KindWarning:          WarningPayload{Code: "detected_credential_in_prompt"},
		KindRetryScheduled:   RetryScheduledPayload{Attempt: 1, DelayMillis: 500, Class: "rate_limit"},
		KindSessionResumed:   SessionResumedPayload{},
		KindSessionClosed:    SessionClosedPayload{},
		KindApprovalRequired: ApprovalRequiredPayload{RequestID: "req-abc", ToolCallID: "call-1", Path: "a.go", Target: "L3", Diff: "-old\n+new\n", IsSensitive: false},
	}
}

// TestEveryKindRoundTrips constructs one event per registered kind, marshals
// it, unmarshals it, and decodes the payload back into its typed struct
// without losing fields.
func TestEveryKindRoundTrips(t *testing.T) {
	samples := samplePayloads()
	if len(samples) != len(payloadFactories) {
		t.Fatalf("sample payloads cover %d kinds, registry has %d", len(samples), len(payloadFactories))
	}

	for kind, factory := range payloadFactories {
		t.Run(string(kind), func(t *testing.T) {
			payload, ok := samples[kind]
			if !ok {
				t.Fatalf("no sample payload for kind %q", kind)
			}
			runID := "run-1"
			if kind == KindWarning || kind == KindSessionResumed || kind == KindSessionClosed {
				runID = ""
			}
			ev, err := New("sess-1", runID, 7, fixtureTime, kind, payload)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			data, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back Event
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(ev, back) {
				t.Fatalf("envelope round-trip mismatch:\nfirst:  %+v\nsecond: %+v", ev, back)
			}

			decoded := factory()
			if err := back.DecodePayload(decoded); err != nil {
				t.Fatalf("DecodePayload: %v", err)
			}
			if !reflect.DeepEqual(reflect.ValueOf(decoded).Elem().Interface(), payload) {
				t.Errorf("payload round-trip mismatch: got %+v, want %+v", decoded, payload)
			}
		})
	}
}

// TestApprovalRequiredDiffRoundTripsUnicode proves the display diff survives
// multiline text, Unicode, and JSON-significant characters intact.
func TestApprovalRequiredDiffRoundTripsUnicode(t *testing.T) {
	diff := "--- a.go\n+++ b.go\n@@ -1,1 +1,1 @@\n-问候 \"old\"\t\\path\n+✨ 新行 \U0001F600 `quotes`\n"
	ev, err := New("sess-1", "run-1", 3, fixtureTime, KindApprovalRequired, ApprovalRequiredPayload{
		RequestID: "req-u", ToolCallID: "call-u", Path: "目录/文件.go",
		Target: "L1", Diff: diff, IsSensitive: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Event
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var payload ApprovalRequiredPayload
	if err := back.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.Diff != diff {
		t.Errorf("diff corrupted:\ngot:  %q\nwant: %q", payload.Diff, diff)
	}
	if payload.Path != "目录/文件.go" {
		t.Errorf("path corrupted: %q", payload.Path)
	}
}

// TestGoldenFixturesRoundTrip proves the on-disk protocol fixtures decode
// and re-encode without losing fields.
func TestGoldenFixturesRoundTrip(t *testing.T) {
	files := map[string]Kind{
		"run_started.json":       KindRunStarted,
		"run_completed.json":     KindRunCompleted,
		"run_failed.json":        KindRunFailed,
		"run_cancelled.json":     KindRunCancelled,
		"assistant_text.json":    KindAssistantText,
		"approval_required.json": KindApprovalRequired,
	}
	for file, kind := range files {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("fixture is not an object: %v", err)
			}
			for _, key := range []string{"session_id", "run_id", "cursor", "time", "kind", "payload"} {
				if _, ok := raw[key]; !ok {
					t.Errorf("fixture missing required envelope key %q", key)
				}
			}

			var ev Event
			if err := json.Unmarshal(data, &ev); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			if ev.Kind != kind {
				t.Errorf("kind = %q, want %q", ev.Kind, kind)
			}
			if ev.SessionID == "" || ev.RunID == "" || ev.Cursor == 0 || ev.Time.IsZero() {
				t.Errorf("fixture has empty envelope fields: %+v", ev)
			}
			factory, ok := payloadFactories[ev.Kind]
			if !ok {
				t.Fatalf("fixture kind %q is not registered", ev.Kind)
			}
			if err := ev.DecodePayload(factory()); err != nil {
				t.Errorf("payload does not decode: %v", err)
			}

			out, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			assertJSONEqual(t, data, out)
		})
	}
}

// TestPayloadsArePureData walks every registered payload type by reflection
// and proves it is built only from serializable value types: no channels,
// callbacks, closures, credentials sources, internal pointers, interfaces,
// or raw Go errors can enter the envelope.
func TestPayloadsArePureData(t *testing.T) {
	for kind, factory := range payloadFactories {
		t.Run(string(kind), func(t *testing.T) {
			typ := reflect.TypeOf(factory()).Elem()
			assertPureData(t, typ, string(kind))
		})
	}
}

func TestUnknownPayloadFieldsFailClosed(t *testing.T) {
	ev := Event{SessionID: "sess-1", RunID: "run-1", Cursor: 1, Time: fixtureTime, Kind: KindRunCompleted, Payload: json.RawMessage(`{"credential":"must-not-load"}`)}
	if err := ev.Validate(); err == nil {
		t.Fatal("Validate accepted an undeclared Event payload field")
	}
	if _, err := New("sess-1", "run-1", 1, fixtureTime, KindRunCompleted, map[string]string{"credential": "must-not-enter"}); err == nil {
		t.Fatal("New accepted an undeclared Event payload type")
	}
}

func assertPureData(t *testing.T, typ reflect.Type, path string) {
	t.Helper()
	switch typ.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		// Serializable scalar.
	case reflect.Struct:
		if typ == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				t.Errorf("%s.%s: unexported fields are forbidden in Event payloads", path, f.Name)
				continue
			}
			if f.Tag.Get("json") == "-" {
				t.Errorf("%s.%s: non-serialized fields are forbidden in Event payloads", path, f.Name)
			}
			assertPureData(t, f.Type, path+"."+f.Name)
		}
	case reflect.Slice, reflect.Array:
		assertPureData(t, typ.Elem(), path+"[]")
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			t.Errorf("%s: map keys must be strings, got %s", path, typ.Key().Kind())
		}
		assertPureData(t, typ.Elem(), path+"{}")
	default:
		t.Errorf("%s: kind %s is forbidden in Event payloads (channels, functions, pointers, interfaces, raw Go errors, and unsafe pointers cannot be serialized facts)", path, typ.Kind())
	}
}

func assertJSONEqual(t *testing.T, want, got []byte) {
	t.Helper()
	var w, g any
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if !reflect.DeepEqual(w, g) {
		t.Errorf("JSON mismatch:\nwant: %s\ngot:  %s", want, got)
	}
}
