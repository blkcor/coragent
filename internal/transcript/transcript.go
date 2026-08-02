// Package transcript defines the durable, append-only semantic history of a
// Session.
//
// The Transcript records facts: user messages, completed assistant blocks,
// ToolCalls, ToolResults, cancellation boundaries, and terminal run outcomes.
// It is not a UI log: streaming text deltas are never persisted, only the
// completed assistant block is.
//
// A Record is a serializable envelope with a kind discriminator and a JSON
// payload, in the same style as event.Event and sessioncommand.Command. New
// record kinds (prepared action references, permission records, context
// checkpoints, steering) are added as new Kind values with their own payload
// structs registered in payloadFactories; the envelope shape and existing
// records never change. Records are never updated or deleted: corrections
// append a new record.
package transcript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Kind identifies the fact a Record persists.
type Kind string

const (
	// KindUserMessage records one submitted user prompt.
	KindUserMessage Kind = "user_message"
	// KindAssistantBlock records one completed assistant content block.
	KindAssistantBlock Kind = "assistant_block"
	// KindToolCall records one complete tool call proposed by the model.
	KindToolCall Kind = "tool_call"
	// KindToolResult records the single terminal result of a tool call.
	KindToolResult Kind = "tool_result"
	// KindCancellationBoundary marks where cancellation interrupted the
	// session history.
	KindCancellationBoundary Kind = "cancellation_boundary"
	// KindRunOutcome records the terminal outcome of a run.
	KindRunOutcome Kind = "run_outcome"
	// KindInstructionsLoaded records the project-instruction sources and
	// scopes used for a run. Content is already projected before persistence.
	KindInstructionsLoaded Kind = "instructions_loaded"
	// KindSessionClosed records the non-destructive closure of a session.
	KindSessionClosed Kind = "session_closed"
)

// Record is the serializable Transcript envelope. Seq is a session-wide
// contiguous sequence number starting at 1, assigned by the store on append.
// RunID is empty for facts that do not belong to a run. Session ownership is
// implicit: records live inside one session's storage.
type Record struct {
	Seq     uint64          `json:"seq"`
	Time    time.Time       `json:"time"`
	RunID   string          `json:"run_id,omitempty"`
	Kind    Kind            `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// UserMessagePayload carries the text of one submitted user prompt.
type UserMessagePayload struct {
	Text string `json:"text"`
}

// AssistantBlockPayload carries one completed assistant text block.
type AssistantBlockPayload struct {
	Text string `json:"text"`
}

// ToolCallPayload carries one complete tool call proposed by the model. The
// Action Broker arrives in S1.5; the record type exists now so the schema
// and the pairing invariant are settled.
type ToolCallPayload struct {
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResultOutcome is the terminal outcome of one tool call. Later slices
// add outcomes (denied, blocked, stale, interrupted, recovered); every value
// is terminal: a call with a result is closed.
type ToolResultOutcome string

const (
	// ToolResultSuccess means the call executed successfully.
	ToolResultSuccess ToolResultOutcome = "success"
	// ToolResultError means the call executed and reported an error.
	ToolResultError ToolResultOutcome = "error"
	// ToolResultCancelled means the call was cancelled before completing.
	ToolResultCancelled ToolResultOutcome = "cancelled"
	// ToolResultSkipped means the call never executed (steering or an
	// earlier non-success result in the batch).
	ToolResultSkipped ToolResultOutcome = "skipped"
	// ToolResultInterrupted repairs an open read-only call after process exit.
	ToolResultInterrupted ToolResultOutcome = "interrupted"
	// ToolResultBlocked means hard data or authority policy prevented execution.
	ToolResultBlocked ToolResultOutcome = "blocked"
)

// ToolResultPayload carries the single terminal result of a tool call.
type ToolResultPayload struct {
	CallID  string            `json:"call_id"`
	Outcome ToolResultOutcome `json:"outcome"`
	Content string            `json:"content"`
}

// CancellationBoundaryPayload marks the point where a cancel SessionCommand
// interrupted the session. It carries no data in M1; the struct exists so
// later fields extend the payload without changing the envelope.
type CancellationBoundaryPayload struct{}

// RunOutcome is the terminal outcome of a run.
type RunOutcome string

// FailureCause is the durable Transcript taxonomy. It intentionally does not
// import the frontend Event protocol; Engine maps the equal wire values at the
// projection boundary.
type FailureCause string

const (
	CauseProviderPermanent FailureCause = "provider_permanent"
	CauseProviderProtocol  FailureCause = "provider_protocol"
	CauseProviderTransient FailureCause = "provider_transient_exhausted"
	CauseEngineInvariant   FailureCause = "engine_invariant"
	CauseBudgetExhausted   FailureCause = "budget_exhausted"
	CauseContextLimit      FailureCause = "context_limit"
	CauseProviderOutput    FailureCause = "provider_output_limit"
	CauseInterrupted       FailureCause = "interrupted"
	CauseSensitiveContent  FailureCause = "sensitive_content_blocked"
)

const (
	// RunOutcomeCompleted means the run finished with a final assistant
	// answer.
	RunOutcomeCompleted RunOutcome = "completed"
	// RunOutcomeFailed means the run stopped with a typed failure cause.
	RunOutcomeFailed RunOutcome = "failed"
	// RunOutcomeCancelled means the run was interrupted by cancellation.
	RunOutcomeCancelled   RunOutcome = "cancelled"
	RunOutcomeInterrupted RunOutcome = "interrupted"
)

// RunOutcomePayload records the terminal outcome of a run. Cause is set only
// for RunOutcomeFailed and reuses the typed event failure taxonomy.
type RunOutcomePayload struct {
	Outcome RunOutcome   `json:"outcome"`
	Cause   FailureCause `json:"cause,omitempty"`
}

// InstructionSource is durable provenance for one effective instruction
// document. Sources contains every same-content path collapsed by hash.
type InstructionSource struct {
	Sources    []string `json:"sources"`
	Scope      string   `json:"scope"`
	SHA256     string   `json:"sha256"`
	Precedence int      `json:"precedence"`
}

// InstructionsLoadedPayload records provenance without duplicating full
// instruction content in this fact. The projected content is assembled from
// the workspace again for each new request.
type InstructionsLoadedPayload struct {
	Sources []InstructionSource `json:"sources"`
}

// SessionClosedPayload marks a non-destructive close request.
type SessionClosedPayload struct{}

var (
	// ErrUnknownRecordKind rejects a record whose kind is not registered.
	// Unknown kinds fail closed: a record this build cannot interpret is
	// never skipped or rewritten.
	ErrUnknownRecordKind = errors.New("transcript: unknown record kind")
	// ErrInvalidRecord rejects a record whose payload fails semantic checks
	// for its kind.
	ErrInvalidRecord = errors.New("transcript: invalid record")
)

// payloadFactories registers one zero-value constructor per Kind. It is the
// single list of record kinds; tests use it to prove every kind round-trips
// through JSON and every payload stays pure data.
var payloadFactories = map[Kind]func() any{
	KindUserMessage:          func() any { return &UserMessagePayload{} },
	KindAssistantBlock:       func() any { return &AssistantBlockPayload{} },
	KindToolCall:             func() any { return &ToolCallPayload{} },
	KindToolResult:           func() any { return &ToolResultPayload{} },
	KindCancellationBoundary: func() any { return &CancellationBoundaryPayload{} },
	KindRunOutcome:           func() any { return &RunOutcomePayload{} },
	KindInstructionsLoaded:   func() any { return &InstructionsLoadedPayload{} },
	KindSessionClosed:        func() any { return &SessionClosedPayload{} },
}

// New builds a Record, marshaling the kind-specific payload into the
// envelope. Seq is left zero; the store assigns it on append.
func New(runID string, at time.Time, kind Kind, payload any) (Record, error) {
	if _, ok := payloadFactories[kind]; !ok {
		return Record{}, fmt.Errorf("%w: %q", ErrUnknownRecordKind, kind)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Record{}, fmt.Errorf("transcript: marshal %s payload: %w", kind, err)
	}
	rec := Record{
		Time:    at,
		RunID:   runID,
		Kind:    kind,
		Payload: raw,
	}
	if err := rec.Validate(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// DecodePayload unmarshals the kind-specific payload into v.
func (r Record) DecodePayload(v any) error {
	decoder := json.NewDecoder(strings.NewReader(string(r.Payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("transcript: decode %s payload: %w", r.Kind, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("transcript: decode %s payload: trailing data", r.Kind)
	}
	return nil
}

// Validate checks one record in isolation: a registered kind, a decodable
// payload, and the semantic checks of that kind. Cross-record invariants
// (sequence contiguity, tool-call pairing) live in ValidateTranscript.
func (r Record) Validate() error {
	if r.Time.IsZero() {
		return fmt.Errorf("%w: record has zero timestamp", ErrInvalidRecord)
	}
	factory, ok := payloadFactories[r.Kind]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownRecordKind, r.Kind)
	}
	payload := factory()
	if err := r.DecodePayload(payload); err != nil {
		return err
	}
	if r.Kind == KindSessionClosed {
		if r.RunID != "" {
			return fmt.Errorf("%w: session_closed must not have a run ID", ErrInvalidRecord)
		}
	} else if r.RunID == "" {
		return fmt.Errorf("%w: %s requires a run ID", ErrInvalidRecord, r.Kind)
	}
	switch p := payload.(type) {
	case *UserMessagePayload:
		if p.Text == "" {
			return fmt.Errorf("%w: user_message at seq %d has empty text", ErrInvalidRecord, r.Seq)
		}
	case *AssistantBlockPayload:
		if p.Text == "" {
			return fmt.Errorf("%w: assistant_block at seq %d has empty text", ErrInvalidRecord, r.Seq)
		}
	case *ToolCallPayload:
		if p.CallID == "" || p.Name == "" || !json.Valid(p.Arguments) {
			return fmt.Errorf("%w: tool_call at seq %d requires call_id and name", ErrInvalidRecord, r.Seq)
		}
	case *ToolResultPayload:
		if p.CallID == "" {
			return fmt.Errorf("%w: tool_result at seq %d requires call_id", ErrInvalidRecord, r.Seq)
		}
		switch p.Outcome {
		case ToolResultSuccess, ToolResultError, ToolResultCancelled, ToolResultSkipped, ToolResultInterrupted, ToolResultBlocked:
		default:
			return fmt.Errorf("%w: tool_result at seq %d has unknown outcome %q", ErrInvalidRecord, r.Seq, p.Outcome)
		}
	case *RunOutcomePayload:
		switch p.Outcome {
		case RunOutcomeCompleted, RunOutcomeFailed, RunOutcomeCancelled, RunOutcomeInterrupted:
		default:
			return fmt.Errorf("%w: run_outcome at seq %d has unknown outcome %q", ErrInvalidRecord, r.Seq, p.Outcome)
		}
		if (p.Outcome == RunOutcomeFailed || p.Outcome == RunOutcomeInterrupted) && p.Cause == "" {
			return fmt.Errorf("%w: failed run_outcome at seq %d requires a cause", ErrInvalidRecord, r.Seq)
		}
		if (p.Outcome == RunOutcomeCompleted || p.Outcome == RunOutcomeCancelled) && p.Cause != "" {
			return fmt.Errorf("%w: successful/cancelled run_outcome at seq %d must not have a cause", ErrInvalidRecord, r.Seq)
		}
		if p.Cause != "" && !validFailureCause(p.Cause) {
			return fmt.Errorf("%w: run_outcome at seq %d has unknown cause %q", ErrInvalidRecord, r.Seq, p.Cause)
		}
	case *InstructionsLoadedPayload:
		if len(p.Sources) == 0 {
			return fmt.Errorf("%w: instructions_loaded at seq %d has no sources", ErrInvalidRecord, r.Seq)
		}
		for _, source := range p.Sources {
			if len(source.Sources) == 0 || source.Scope == "" || source.SHA256 == "" {
				return fmt.Errorf("%w: incomplete instruction provenance at seq %d", ErrInvalidRecord, r.Seq)
			}
		}
	}
	return nil
}

func validFailureCause(cause FailureCause) bool {
	switch cause {
	case CauseProviderPermanent, CauseProviderProtocol, CauseProviderTransient,
		CauseEngineInvariant, CauseBudgetExhausted, CauseContextLimit,
		CauseProviderOutput, CauseInterrupted, CauseSensitiveContent:
		return true
	default:
		return false
	}
}
