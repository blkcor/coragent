package action

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/blkcor/coragent/internal/policy"
	"github.com/blkcor/coragent/internal/sandbox"
)

// PreparedPatch is the validated, side-effect-free result of the patch tool's
// Prepare phase. It carries content identity and a display diff for approval
// and never touches disk.
type PreparedPatch struct {
	RequestID      string
	Path           string
	Target         string
	SourceSHA256   string
	ExpectedSHA256 string
	Diff           string
	DiffDigest     string
	IsSensitive    bool
	CreatedAt      time.Time
}

// ExecutionIdentity binds every execution-relevant command field to the
// prepared action. Inputs are canonicalized by the command tool before this
// value is constructed.
type ExecutionIdentity struct {
	Command        string
	Args           []string
	CWD            string
	EnvKeys        []string
	EnvValues      []string
	Timeout        time.Duration
	MaxOutputBytes int64
	PTY            bool
	ReadPaths      []string
	WritePaths     []string
	Network        bool
	SandboxLevel   sandbox.ConfinementLevel
	PolicyVersion  string
}

// Digest returns the SHA-256 digest of a deterministic, NUL-delimited
// serialization. Section labels and counts keep adjacent slices unambiguous.
func (i ExecutionIdentity) Digest() string {
	var raw bytes.Buffer
	writeIdentityField(&raw, "command", i.Command)
	writeIdentitySlice(&raw, "args", i.Args)
	writeIdentityField(&raw, "cwd", i.CWD)
	writeIdentitySlice(&raw, "env_keys", i.EnvKeys)
	writeIdentitySlice(&raw, "env_values", i.EnvValues)
	writeIdentityField(&raw, "timeout_ns", strconv.FormatInt(int64(i.Timeout), 10))
	writeIdentityField(&raw, "max_output_bytes", strconv.FormatInt(i.MaxOutputBytes, 10))
	writeIdentityField(&raw, "pty", strconv.FormatBool(i.PTY))
	writeIdentitySlice(&raw, "read_paths", i.ReadPaths)
	writeIdentitySlice(&raw, "write_paths", i.WritePaths)
	writeIdentityField(&raw, "network", strconv.FormatBool(i.Network))
	writeIdentityField(&raw, "sandbox_level", strconv.Itoa(int(i.SandboxLevel)))
	writeIdentityField(&raw, "policy_version", i.PolicyVersion)
	digest := sha256.Sum256(raw.Bytes())
	return hex.EncodeToString(digest[:])
}

func writeIdentityField(raw *bytes.Buffer, label, value string) {
	raw.WriteString(label)
	raw.WriteByte(0)
	raw.WriteString(value)
	raw.WriteByte(0)
}

func writeIdentitySlice(raw *bytes.Buffer, label string, values []string) {
	writeIdentityField(raw, label+"_count", strconv.Itoa(len(values)))
	for _, value := range values {
		writeIdentityField(raw, label, value)
	}
}

// PreparedCommand is the side-effect-free result of command preparation.
type PreparedCommand struct {
	CommandSpec sandbox.CommandSpec
	Effect      policy.EffectClassification
	Decision    policy.PolicyDecision
	Identity    ExecutionIdentity
	Preview     string
	RevisionID  string
	CreatedAt   time.Time
}

// PatchID returns the RequestID from Patch when non-nil, otherwise the
// empty string. This is the stable identifier for execute/approval flows.
func (p Prepared) PatchID() string {
	if p.Patch == nil {
		return ""
	}
	return p.Patch.RequestID
}
