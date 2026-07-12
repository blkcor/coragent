package permission

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

func TestFingerprintKeyRepresentationsAreAlwaysRedacted(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef")
	key, err := NewFingerprintKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	call := core.ToolCall{ToolName: "custom_tool", Arguments: map[string]interface{}{"pin": "1234"}}
	fingerprint, err := exactCallFingerprint(key, core.ActionUnknown, call)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := ParseRule("exact-v2:unknown:hmac-sha256:" + fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{FingerprintKey: key, Rules: RuleSet{Allow: []Rule{rule}}}
	jsonValue, err := json.Marshal(struct {
		Key   FingerprintKey `json:"key"`
		Rules RuleSet        `json:"rules"`
	}{Key: key, Rules: config.Rules})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	logger.Info("permission config", "config", config, "key", key)
	joined := strings.Join([]string{
		fmt.Sprint(key), fmt.Sprintf("%+v", key), fmt.Sprintf("%#v", key),
		fmt.Sprintf("%+v", config), string(jsonValue), logs.String(),
	}, "\n")
	for _, secret := range []string{string(raw), hex.EncodeToString(raw)} {
		if strings.Contains(joined, secret) {
			t.Fatalf("fingerprint key leaked through a descriptor: %q in %s", secret, joined)
		}
	}
	if !strings.Contains(strings.ToLower(joined), "redacted") {
		t.Fatalf("representations did not make redaction explicit: %s", joined)
	}
}
