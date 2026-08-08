package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type approvalRecord struct {
	ApprovedAt     time.Time
	ScopeDigest    string
	IdentityDigest string
}

// SessionMemory remembers workspace-command prefixes approved during one
// in-process session. It is deliberately not durable: reopening the process
// creates a fresh memory and requires approval again.
type SessionMemory struct {
	mu               sync.RWMutex
	approvedPrefixes map[string]approvalRecord
	now              func() time.Time
}

// NewSessionMemory returns empty, process-local approval memory.
func NewSessionMemory() *SessionMemory {
	return newSessionMemory(time.Now)
}

func newSessionMemory(now func() time.Time) *SessionMemory {
	if now == nil {
		now = time.Now
	}
	return &SessionMemory{
		approvedPrefixes: make(map[string]approvalRecord),
		now:              now,
	}
}

// IsApproved reports whether the command's session-memory prefix was approved.
func (m *SessionMemory) IsApproved(cmd string, args []string) bool {
	if m == nil {
		return false
	}
	prefix := ApprovalPrefix(cmd, args)
	if prefix == "" {
		return false
	}
	m.mu.RLock()
	_, ok := m.approvedPrefixes[prefix]
	m.mu.RUnlock()
	return ok
}

// MarkApproved records the command prefix for the lifetime of this memory.
func (m *SessionMemory) MarkApproved(cmd string, args []string) {
	m.markApproved(CommandSpec{Command: cmd, Args: append([]string(nil), args...)}, "")
}

// isApprovedSpec requires the remembered prefix and its authority-sensitive
// scope to match. The command's non-prefix argument tail is intentionally not
// part of this scope: S2.6 explicitly allows, for example, go:test approval to
// cover both ./pkg/a and ./pkg/b. The exact full identity remains attached to
// the prepared action and is stored alongside the memory record.
func (m *SessionMemory) isApprovedSpec(spec CommandSpec) bool {
	if m == nil {
		return false
	}
	prefix := ApprovalPrefix(spec.Command, spec.Args)
	if prefix == "" {
		return false
	}
	scopeDigest := commandApprovalScopeDigest(spec)
	m.mu.RLock()
	record, ok := m.approvedPrefixes[prefix]
	m.mu.RUnlock()
	return ok && record.ScopeDigest == scopeDigest
}

func (m *SessionMemory) markApproved(spec CommandSpec, identityDigest string) {
	if m == nil {
		return
	}
	prefix := ApprovalPrefix(spec.Command, spec.Args)
	if prefix == "" {
		return
	}
	m.mu.Lock()
	m.approvedPrefixes[prefix] = approvalRecord{
		ApprovedAt:     m.now(),
		ScopeDigest:    commandApprovalScopeDigest(spec),
		IdentityDigest: identityDigest,
	}
	m.mu.Unlock()
}

func (m *SessionMemory) identityDigest(cmd string, args []string) (string, bool) {
	if m == nil {
		return "", false
	}
	prefix := ApprovalPrefix(cmd, args)
	m.mu.RLock()
	record, ok := m.approvedPrefixes[prefix]
	m.mu.RUnlock()
	return record.IdentityDigest, ok
}

// ApprovalPrefix returns the stable prefix used by session approval memory.
// Most command families are scoped to the binary and first argument, so
// "go test ./pkg/a" and "go test ./pkg/b" intentionally share "go:test".
// Git checkout/restore/reset include the next argument because their target is
// material to the operation being approved.
func ApprovalPrefix(cmd string, args []string) string {
	cmd = strings.TrimSpace(filepath.Base(cmd))
	if cmd == "" || cmd == "." {
		return ""
	}
	parts := []string{cmd}
	if len(args) == 0 {
		return strings.Join(parts, ":")
	}
	parts = append(parts, args[0])
	if cmd == "git" && len(args) > 1 {
		switch args[0] {
		case "checkout", "restore", "reset":
			parts = append(parts, args[1])
		}
	}
	return strings.Join(parts, ":")
}

// SessionState contains policy state owned by exactly one runtime Session.
// Future session policy fields can be added here without making PolicyEngine
// persistent or giving it store access.
type SessionState struct {
	mu     sync.Mutex
	Memory *SessionMemory
}

// NewSessionState returns isolated policy state for one Session.
func NewSessionState() *SessionState {
	return &SessionState{Memory: NewSessionMemory()}
}

func (s *SessionState) memory() *SessionMemory {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Memory == nil {
		s.Memory = NewSessionMemory()
	}
	return s.Memory
}

type commandIdentityInput struct {
	Command        string
	Args           []string
	CWD            string
	Env            []string
	Timeout        time.Duration
	MaxOutputBytes int64
	PTY            bool
	ReadPaths      []string
	WritePaths     []string
	Network        bool
	PolicyVersion  string
}

// CommandIdentityDigest binds an approval to the complete effective command.
// S2.7 extends this identity with the selected sandbox confinement level before
// a prepared command can execute.
func CommandIdentityDigest(spec CommandSpec) string {
	return digestCommandIdentity(spec, append([]string(nil), spec.Args...))
}

func commandApprovalScopeDigest(spec CommandSpec) string {
	prefixArgs := []string(nil)
	if len(spec.Args) > 0 {
		prefixArgs = append(prefixArgs, spec.Args[0])
	}
	if filepath.Base(spec.Command) == "git" && len(spec.Args) > 1 {
		switch spec.Args[0] {
		case "checkout", "restore", "reset":
			prefixArgs = append(prefixArgs, spec.Args[1])
		}
	}
	return digestCommandIdentity(spec, prefixArgs)
}

func digestCommandIdentity(spec CommandSpec, args []string) string {
	// Preserve Env order. os/exec permits duplicate keys and the last value
	// wins, so sorting could make two commands with different execution
	// semantics share an identity.
	env := append([]string(nil), spec.Env...)
	readPaths := append([]string(nil), spec.Grants.AllowedReadPaths...)
	writePaths := append([]string(nil), spec.Grants.AllowedWritePaths...)
	sort.Strings(readPaths)
	sort.Strings(writePaths)
	raw, err := json.Marshal(commandIdentityInput{
		Command: spec.Command, Args: args, CWD: spec.CWD, Env: env,
		Timeout: spec.Timeout, MaxOutputBytes: spec.MaxOutputBytes, PTY: spec.PTY,
		ReadPaths: readPaths, WritePaths: writePaths, Network: spec.Grants.Network,
		PolicyVersion: PolicyVersion,
	})
	if err != nil {
		panic("policy: command identity contains unsupported values")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
