package permission

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/blkcor/coragent/internal/core"
)

const (
	// FingerprintKeySize is the exact size accepted for keyed exact-call
	// fingerprints. A fixed size keeps the bootstrap secret surface simple and
	// makes malformed key material fail closed.
	FingerprintKeySize = sha256.Size

	exactFingerprintDomain = "coragent.permission.exact-call-fingerprint.v2\x00"
)

// FingerprintKey is secret key material for exact-call permission rules. Its
// underlying bytes are intentionally private, and every standard formatting,
// JSON, and structured-logging representation is redacted.
type FingerprintKey struct {
	material [FingerprintKeySize]byte
	valid    bool
}

// NewFingerprintKey copies exactly 32 bytes of caller-owned secret material.
func NewFingerprintKey(raw []byte) (FingerprintKey, error) {
	if len(raw) != FingerprintKeySize {
		return FingerprintKey{}, fmt.Errorf("permission: fingerprint key must be %d bytes", FingerprintKeySize)
	}
	var key FingerprintKey
	copy(key.material[:], raw)
	key.valid = true
	return key, nil
}

func newEphemeralFingerprintKey() (FingerprintKey, error) {
	raw := make([]byte, FingerprintKeySize)
	if _, err := rand.Read(raw); err != nil {
		return FingerprintKey{}, fmt.Errorf("permission: generate ephemeral fingerprint key: %w", err)
	}
	key, err := NewFingerprintKey(raw)
	clear(raw)
	return key, err
}

// Valid reports whether the key was constructed from valid secret material.
func (k FingerprintKey) Valid() bool { return k.valid }

// String deliberately withholds key material.
func (k FingerprintKey) String() string { return "PermissionFingerprintKey{redacted}" }

// GoString deliberately withholds key material from %#v formatting.
func (k FingerprintKey) GoString() string { return k.String() }

// MarshalJSON deliberately withholds key material from JSON descriptors.
func (k FingerprintKey) MarshalJSON() ([]byte, error) { return json.Marshal("[REDACTED]") }

// LogValue deliberately withholds key material from structured logs.
func (k FingerprintKey) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// exactCallFingerprint returns a domain-separated HMAC identity for one
// effective call. JSON object keys are emitted in stable lexical order by
// encoding/json; call IDs are intentionally excluded so equivalent calls match.
func exactCallFingerprint(key FingerprintKey, kind core.ActionKind, call core.ToolCall) (string, error) {
	if !key.Valid() {
		return "", fmt.Errorf("permission: exact-call fingerprint key is unavailable")
	}
	canonical, err := canonicalExactCall(kind, call)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key.material[:])
	_, _ = mac.Write([]byte(exactFingerprintDomain))
	_, _ = mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func canonicalExactCall(kind core.ActionKind, call core.ToolCall) ([]byte, error) {
	arguments := call.Arguments
	if arguments == nil {
		arguments = map[string]interface{}{}
	}
	payload := struct {
		Version   string                 `json:"version"`
		Action    string                 `json:"action"`
		Tool      string                 `json:"tool"`
		Arguments map[string]interface{} `json:"arguments"`
	}{
		Version: exactRuleVersion, Action: kindLabel(kind), Tool: call.ToolName, Arguments: arguments,
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("canonicalize exact call: %w", err)
	}
	return canonical, nil
}
