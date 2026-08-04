package sessioncommand

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func goldenCases() []struct {
	file string
	kind Kind
} {
	return []struct {
		file string
		kind Kind
	}{
		{"submit.json", KindSubmit},
		{"cancel.json", KindCancel},
		{"resume.json", KindResume},
		{"close.json", KindClose},
	}
}

// TestGoldenFixturesRoundTrip proves every M1 command kind round-trips
// through JSON without losing fields, using the on-disk fixtures.
func TestGoldenFixturesRoundTrip(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("fixture is not an object: %v", err)
			}
			for _, key := range []string{"id", "kind", "payload"} {
				if _, ok := raw[key]; !ok {
					t.Errorf("fixture missing required envelope key %q", key)
				}
			}

			var cmd Command
			if err := json.Unmarshal(data, &cmd); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			if cmd.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", cmd.Kind, tc.kind)
			}
			if err := cmd.Validate(); err != nil {
				t.Errorf("fixture does not validate: %v", err)
			}

			out, err := json.Marshal(cmd)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			assertJSONEqual(t, data, out)

			var again Command
			if err := json.Unmarshal(out, &again); err != nil {
				t.Fatalf("unmarshal re-encoded: %v", err)
			}
			if again.ID != cmd.ID || again.Kind != cmd.Kind {
				t.Errorf("envelope round-trip mismatch: got %+v, want %+v", again, cmd)
			}
			// Raw payload bytes may differ in whitespace; compare semantically.
			assertJSONEqual(t, cmd.Payload, again.Payload)
		})
	}
}

func TestConstructorsRoundTrip(t *testing.T) {
	submit, err := NewSubmit("cmd-1", "explain what main.go does")
	if err != nil {
		t.Fatalf("NewSubmit: %v", err)
	}
	data, err := json.Marshal(submit)
	if err != nil {
		t.Fatalf("marshal submit: %v", err)
	}
	var decoded Command
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal submit: %v", err)
	}
	sp, err := decoded.DecodeSubmit()
	if err != nil {
		t.Fatalf("DecodeSubmit: %v", err)
	}
	if sp.Prompt != "explain what main.go does" {
		t.Errorf("prompt = %q", sp.Prompt)
	}

	cancel, err := NewCancel("cmd-2")
	if err != nil {
		t.Fatalf("NewCancel: %v", err)
	}
	data, err = json.Marshal(cancel)
	if err != nil {
		t.Fatalf("marshal cancel: %v", err)
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal cancel: %v", err)
	}
	if _, err := decoded.DecodeCancel(); err != nil {
		t.Fatalf("DecodeCancel: %v", err)
	}
}

func TestValidateRejectsMalformedCommands(t *testing.T) {
	submit, err := NewSubmit("cmd-1", "prompt")
	if err != nil {
		t.Fatalf("NewSubmit: %v", err)
	}

	cases := map[string]Command{
		"empty id":     {ID: "", Kind: KindSubmit, Payload: submit.Payload},
		"unknown kind": {ID: "cmd-x", Kind: "steer", Payload: json.RawMessage(`{}`)},
		"no payload":   {ID: "cmd-x", Kind: KindSubmit},
		"bad payload":  {ID: "cmd-x", Kind: KindSubmit, Payload: json.RawMessage(`{"prompt": 42}`)},
		"empty prompt": {ID: "cmd-x", Kind: KindSubmit, Payload: json.RawMessage(`{"prompt": ""}`)},
		"huge prompt":  {ID: "cmd-x", Kind: KindSubmit, Payload: mustPayload(t, strings.Repeat("x", 256*1024+1))},
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cmd.Validate(); err == nil {
				t.Error("Validate succeeded, want error")
			}
		})
	}
}

func mustPayload(t *testing.T, prompt string) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(SubmitPayload{Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestDecodeRejectsWrongKind(t *testing.T) {
	cancel, err := NewCancel("cmd-1")
	if err != nil {
		t.Fatalf("NewCancel: %v", err)
	}
	if _, err := cancel.DecodeSubmit(); err == nil {
		t.Error("DecodeSubmit on a cancel command succeeded, want error")
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
