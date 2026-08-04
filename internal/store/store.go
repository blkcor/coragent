// Package store persists M1 sessions without exposing runtime internals to a
// frontend. Session directories live directly below the configured root. The
// production root is ~/.coragent/sessions; tests always inject t.TempDir().
package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/platform/fileid"
	"github.com/blkcor/coragent/internal/transcript"
)

const FormatVersion = 2

var (
	ErrExists            = errors.New("store: session already exists")
	ErrNotFound          = errors.New("store: session not found")
	ErrUnsupportedFormat = errors.New("store: unsupported session format")
	ErrCorrupt           = errors.New("store: corrupt session data")
	ErrDuplicateCommand  = errors.New("store: duplicate command ID")
	ErrClosed            = errors.New("store: session is closed")
	ErrBudgetExhausted   = errors.New("store: run budget exhausted")
)

const (
	manifestName   = "manifest.json"
	transcriptName = "transcript.jsonl"
	eventsName     = "events.jsonl"
)

// Authority is the immutable M1 maximum authority. No mutation, process, or
// network capability exists in this representation.
type Authority struct {
	WorkspaceRead bool `json:"workspace_read"`
}

// RunBudget contains the M1 durable counters and their fixed limits.
type RunBudget struct {
	LogicalModelCalls uint64        `json:"logical_model_calls"`
	TransportAttempts uint64        `json:"transport_attempts"`
	RetryDelay        time.Duration `json:"retry_delay_ns"`
}

const (
	MaxLogicalModelCalls = 64
	MaxTransportAttempts = 96
	MaxRetryDelay        = 10 * time.Minute
)

// ActiveRun identifies work that must be reconciled after process exit.
type ActiveRun struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
}

// ProviderBinding is the non-secret immutable runtime identity for a session.
// Digest covers every field except itself. Credentials and raw endpoint URLs
// are deliberately absent.
type ProviderBinding struct {
	Digest                 string   `json:"digest"`
	Adapter                string   `json:"adapter"`
	WireProtocol           string   `json:"wire_protocol"`
	EndpointSHA256         string   `json:"endpoint_sha256"`
	CredentialSourceSHA256 string   `json:"credential_source_sha256"`
	Model                  string   `json:"model"`
	ContextWindow          int      `json:"context_window"`
	MaxOutputTokens        int      `json:"max_output_tokens"`
	Temperature            *float64 `json:"temperature,omitempty"`
	Seed                   *int64   `json:"seed,omitempty"`
	ToolChoice             string   `json:"tool_choice"`
	UserPreferencesSHA256  string   `json:"user_preferences_sha256"`
	PromptVersion          string   `json:"prompt_version"`
}

func (b ProviderBinding) ComputeDigest() string {
	b.Digest = ""
	data, _ := json.Marshal(b)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (b ProviderBinding) Validate() error {
	if b.Adapter == "" || b.WireProtocol == "" || b.Model == "" || b.ContextWindow <= 0 || b.MaxOutputTokens <= 0 || b.ToolChoice == "" || b.PromptVersion == "" || !validSHA256(b.EndpointSHA256) || !validSHA256(b.CredentialSourceSHA256) || !validSHA256(b.UserPreferencesSHA256) || !validSHA256(b.Digest) || b.Digest != b.ComputeDigest() {
		return errors.New("store: invalid Provider binding")
	}
	return nil
}

// Manifest is the versioned, atomically replaced session metadata.
type Manifest struct {
	FormatVersion     int                  `json:"format_version"`
	SessionID         string               `json:"session_id"`
	Workspace         string               `json:"workspace"`
	WorkspaceIdentity string               `json:"workspace_identity"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	Closed            bool                 `json:"closed"`
	ProjectionVersion string               `json:"projection_version"`
	ProviderBinding   ProviderBinding      `json:"provider_binding"`
	Authority         Authority            `json:"authority"`
	TranscriptSeq     uint64               `json:"transcript_seq"`
	EventCursor       uint64               `json:"event_cursor"`
	NextRun           uint64               `json:"next_run"`
	SeenCommands      map[string]bool      `json:"seen_commands"`
	Budgets           map[string]RunBudget `json:"budgets"`
	ActiveRun         *ActiveRun           `json:"active_run,omitempty"`
}

// Summary is safe to produce without contacting a Provider.
type Summary struct {
	SessionID    string
	Workspace    string
	LastActivity time.Time
	Closed       bool
}

// Session is one opened durable session.
type Session struct {
	mu         sync.Mutex
	dir        string
	manifest   Manifest
	transcript []transcript.Record
	events     []event.Event
}

// DefaultRoot resolves the production storage root. Tests should not call it.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: resolve user home: %w", err)
	}
	return filepath.Join(home, ".coragent", "sessions"), nil
}

// Create creates an opaque session directory and initial manifest.
func Create(root, id, workspace, projectionVersion string, binding ProviderBinding, now time.Time) (*Session, error) {
	if root == "" || id == "" || workspace == "" {
		return nil, errors.New("store: root, session ID, and workspace are required")
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("store: clean root: %w", err)
	}
	cleanWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, fmt.Errorf("store: resolve workspace: %w", err)
	}
	cleanWorkspace, err = filepath.Abs(cleanWorkspace)
	if err != nil {
		return nil, fmt.Errorf("store: clean workspace: %w", err)
	}
	workspaceInfo, err := os.Stat(cleanWorkspace)
	if err != nil {
		return nil, fmt.Errorf("store: stat workspace identity: %w", err)
	}
	workspaceIdentity := fileid.FromInfo(workspaceInfo)
	if err := os.MkdirAll(cleanRoot, 0o700); err != nil {
		return nil, fmt.Errorf("store: create root: %w", err)
	}
	cleanRoot, err = filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("store: resolve root: %w", err)
	}
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	dir := filepath.Join(cleanRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s", ErrExists, id)
		}
		return nil, fmt.Errorf("store: create session: %w", err)
	}
	m := Manifest{
		FormatVersion:     FormatVersion,
		SessionID:         id,
		Workspace:         cleanWorkspace,
		WorkspaceIdentity: workspaceIdentity,
		CreatedAt:         now.UTC(),
		UpdatedAt:         now.UTC(),
		ProjectionVersion: projectionVersion,
		ProviderBinding:   binding,
		Authority:         Authority{WorkspaceRead: true},
		SeenCommands:      make(map[string]bool),
		Budgets:           make(map[string]RunBudget),
	}
	s := &Session{dir: dir, manifest: m}
	if err := s.writeManifestLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// Open loads and validates a session. It never rewrites unknown or corrupt
// data. Complete log records are authoritative high-water marks after a crash.
func Open(root, id string) (*Session, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("store: clean root: %w", err)
	}
	cleanRoot, err = filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("store: resolve root: %w", err)
	}
	dir := filepath.Join(cleanRoot, id)
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("store: inspect session directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: session entry is not a real directory", ErrCorrupt)
	}
	//nolint:gosec // dir is cleanRoot/id where id passed validateSessionID
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("store: read manifest: %w", err)
	}
	var m Manifest
	if err := decodeStrict(data, &m); err != nil {
		return nil, fmt.Errorf("%w: manifest: %v", ErrCorrupt, err)
	}
	if m.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("%w: got %d, support %d", ErrUnsupportedFormat, m.FormatVersion, FormatVersion)
	}
	if m.SessionID != id || m.Workspace == "" || !filepath.IsAbs(m.Workspace) || m.WorkspaceIdentity == "" || m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() || m.ProjectionVersion == "" || !m.Authority.WorkspaceRead || m.ProviderBinding.Validate() != nil {
		return nil, fmt.Errorf("%w: manifest identity", ErrCorrupt)
	}
	if m.SeenCommands == nil {
		m.SeenCommands = make(map[string]bool)
	}
	if m.Budgets == nil {
		m.Budgets = make(map[string]RunBudget)
	}
	if m.ActiveRun != nil {
		if _, ok := m.Budgets[m.ActiveRun.ID]; !ok || m.ActiveRun.ID == "" || m.ActiveRun.StartedAt.IsZero() {
			return nil, fmt.Errorf("%w: active run has no durable budget", ErrCorrupt)
		}
	}
	records, err := readJSONLines[transcript.Record](filepath.Join(dir, transcriptName))
	if err != nil {
		return nil, err
	}
	if err := transcript.ValidateRecords(records); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	events, err := readJSONLines[event.Event](filepath.Join(dir, eventsName))
	if err != nil {
		return nil, err
	}
	for i, ev := range events {
		if err := ev.Validate(); err != nil || ev.SessionID != id || ev.Cursor != uint64(i+1) {
			return nil, fmt.Errorf("%w: event at position %d", ErrCorrupt, i)
		}
	}
	if uint64(len(records)) < m.TranscriptSeq || uint64(len(events)) < m.EventCursor {
		return nil, fmt.Errorf("%w: manifest high-water mark exceeds log", ErrCorrupt)
	}
	// A crash may leave a complete synced log record before the atomic
	// manifest replacement. Keep the larger in-memory marks; the next normal
	// metadata write persists them, never reducing a counter.
	m.TranscriptSeq = uint64(len(records))
	m.EventCursor = uint64(len(events))
	return &Session{dir: dir, manifest: m, transcript: records, events: events}, nil
}

func validateSessionID(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return errors.New("store: invalid session ID")
	}
	for _, char := range id {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return errors.New("store: invalid session ID")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

// List returns valid session summaries in stable newest-first order. Unknown
// or corrupt entries are ignored and left untouched.
func List(root string) ([]Summary, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: list root: %w", err)
	}
	var out []Summary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		s, err := Open(root, entry.Name())
		if err != nil {
			continue
		}
		m := s.Manifest()
		out = append(out, Summary{SessionID: m.SessionID, Workspace: m.Workspace, LastActivity: m.UpdatedAt, Closed: m.Closed})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastActivity.Equal(out[j].LastActivity) {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].LastActivity.After(out[j].LastActivity)
	})
	return out, nil
}

func (s *Session) Manifest() Manifest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneManifest(s.manifest)
}

func (s *Session) Transcript() []transcript.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]transcript.Record(nil), s.transcript...)
}

func (s *Session) Events() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.events...)
}

// AppendTranscript appends complete newline-delimited records and syncs them
// before advancing the manifest. A partial final line is rejected on reopen.
func (s *Session) AppendTranscript(records ...transcript.Record) ([]transcript.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	assigned := make([]transcript.Record, len(records))
	for i := range records {
		records[i].Seq = s.manifest.TranscriptSeq + uint64(i) + 1
		if err := records[i].Validate(); err != nil {
			return nil, err
		}
		assigned[i] = records[i]
	}
	if err := appendJSONLines(filepath.Join(s.dir, transcriptName), assigned); err != nil {
		return nil, err
	}
	s.transcript = append(s.transcript, assigned...)
	s.manifest.TranscriptSeq += uint64(len(assigned))
	if len(assigned) > 0 {
		s.manifest.UpdatedAt = assigned[len(assigned)-1].Time.UTC()
	}
	if err := s.writeManifestLocked(); err != nil {
		return nil, err
	}
	return append([]transcript.Record(nil), assigned...), nil
}

// AppendEvent persists a cursor-assigned Event before it is shown to a
// frontend.
func (s *Session) AppendEvent(ev event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.SessionID != s.manifest.SessionID || ev.Cursor != s.manifest.EventCursor+1 {
		return fmt.Errorf("%w: event cursor or session", ErrCorrupt)
	}
	if err := appendJSONLines(filepath.Join(s.dir, eventsName), []event.Event{ev}); err != nil {
		return err
	}
	s.events = append(s.events, ev)
	s.manifest.EventCursor = ev.Cursor
	s.manifest.UpdatedAt = ev.Time.UTC()
	return s.writeManifestLocked()
}

// BeginRun atomically consumes the command ID, allocates a run ID and creates
// its fresh durable budget before Provider work begins.
func (s *Session) BeginRun(commandID string, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest.Closed {
		return "", ErrClosed
	}
	if s.manifest.SeenCommands[commandID] {
		return "", fmt.Errorf("%w: %s", ErrDuplicateCommand, commandID)
	}
	if s.manifest.ActiveRun != nil {
		return "", errors.New("store: active run already exists")
	}
	s.manifest.SeenCommands[commandID] = true
	s.manifest.NextRun++
	runID := fmt.Sprintf("run-%d", s.manifest.NextRun)
	s.manifest.Budgets[runID] = RunBudget{}
	s.manifest.ActiveRun = &ActiveRun{ID: runID, StartedAt: now.UTC()}
	s.manifest.UpdatedAt = now.UTC()
	if err := s.writeManifestLocked(); err != nil {
		return "", err
	}
	return runID, nil
}

// RecordCommand consumes a non-submit command ID durably.
func (s *Session) RecordCommand(commandID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest.SeenCommands[commandID] {
		return fmt.Errorf("%w: %s", ErrDuplicateCommand, commandID)
	}
	s.manifest.SeenCommands[commandID] = true
	s.manifest.UpdatedAt = now.UTC()
	return s.writeManifestLocked()
}

func (s *Session) FinishRun(runID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest.ActiveRun == nil || s.manifest.ActiveRun.ID != runID {
		return fmt.Errorf("%w: finish non-active run %s", ErrCorrupt, runID)
	}
	s.manifest.ActiveRun = nil
	s.manifest.UpdatedAt = now.UTC()
	return s.writeManifestLocked()
}

// ReconcileRun clears exactly the persisted active run after its interrupted
// Transcript facts have been appended.
func (s *Session) ReconcileRun(runID string, now time.Time) error {
	return s.FinishRun(runID, now)
}

// Close marks a session closed after the caller appends the durable close fact.
func (s *Session) Close(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest.Closed {
		return nil
	}
	if s.manifest.ActiveRun != nil {
		return errors.New("store: cannot close an active session")
	}
	s.manifest.Closed = true
	s.manifest.UpdatedAt = now.UTC()
	return s.writeManifestLocked()
}

type BudgetCounter string

const (
	BudgetLogicalModelCall BudgetCounter = "logical_model_call"
	BudgetTransportAttempt BudgetCounter = "transport_attempt"
	BudgetRetryDelay       BudgetCounter = "retry_delay"
)

// ReserveBudget persists a monotonic reservation before work begins.
func (s *Session) ReserveBudget(runID string, counter BudgetCounter, amount time.Duration) (RunBudget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.manifest.Budgets[runID]
	if !ok {
		return b, fmt.Errorf("%w: unknown run %s", ErrCorrupt, runID)
	}
	switch counter {
	case BudgetLogicalModelCall:
		if b.LogicalModelCalls >= MaxLogicalModelCalls {
			return b, fmt.Errorf("%w: logical model calls", ErrBudgetExhausted)
		}
		b.LogicalModelCalls++
	case BudgetTransportAttempt:
		if b.TransportAttempts >= MaxTransportAttempts {
			return b, fmt.Errorf("%w: transport attempts", ErrBudgetExhausted)
		}
		b.TransportAttempts++
	case BudgetRetryDelay:
		if amount < 0 || b.RetryDelay > MaxRetryDelay-amount {
			return b, fmt.Errorf("%w: retry delay", ErrBudgetExhausted)
		}
		b.RetryDelay += amount
	default:
		return b, fmt.Errorf("store: unknown budget counter %q", counter)
	}
	s.manifest.Budgets[runID] = b
	if err := s.writeManifestLocked(); err != nil {
		return RunBudget{}, err
	}
	return b, nil
}

func (s *Session) writeManifestLocked() error {
	data, err := json.MarshalIndent(s.manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode manifest: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".manifest-*")
	if err != nil {
		return fmt.Errorf("store: create manifest temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store: chmod manifest temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store: write manifest temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store: sync manifest temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close manifest temp: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(s.dir, manifestName)); err != nil {
		return fmt.Errorf("store: replace manifest: %w", err)
	}
	dir, err := os.Open(s.dir)
	if err == nil {
		err = dir.Sync()
		_ = dir.Close()
	}
	if err != nil {
		return fmt.Errorf("store: sync session directory: %w", err)
	}
	return nil
}

func appendJSONLines[T any](path string, values []T) error {
	if len(values) == 0 {
		return nil
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, value := range values {
		if err := enc.Encode(value); err != nil {
			return fmt.Errorf("store: encode log record: %w", err)
		}
	}
	//nolint:gosec // path is under a validated session directory
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("store: open log: %w", err)
	}
	if _, err := io.WriteString(f, b.String()); err != nil {
		_ = f.Close()
		return fmt.Errorf("store: append log: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("store: sync log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("store: close log: %w", err)
	}
	return nil
}

func readJSONLines[T any](path string) ([]T, error) {
	//nolint:gosec // path is under a validated session directory
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: open log: %w", err)
	}
	defer func() { _ = f.Close() }()
	var out []T
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		data := append([]byte(nil), scanner.Bytes()...)
		var value T
		if err := decodeStrict(data, &value); err != nil {
			return nil, fmt.Errorf("%w: %s line %d: %v", ErrCorrupt, filepath.Base(path), line, err)
		}
		out = append(out, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: scan %s: %v", ErrCorrupt, filepath.Base(path), err)
	}
	// Scanner accepts a final line without newline. Treat that as a partial
	// append so a torn record can never appear valid.
	//nolint:gosec // path is under a validated session directory
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("store: inspect log: %w", err)
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("%w: partial final record in %s", ErrCorrupt, filepath.Base(path))
	}
	return out, nil
}

func decodeStrict(data []byte, out any) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func cloneManifest(m Manifest) Manifest {
	m.SeenCommands = cloneMap(m.SeenCommands)
	m.Budgets = cloneMap(m.Budgets)
	if m.ProviderBinding.Temperature != nil {
		value := *m.ProviderBinding.Temperature
		m.ProviderBinding.Temperature = &value
	}
	if m.ProviderBinding.Seed != nil {
		value := *m.ProviderBinding.Seed
		m.ProviderBinding.Seed = &value
	}
	if m.ActiveRun != nil {
		r := *m.ActiveRun
		m.ActiveRun = &r
	}
	return m
}

func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
