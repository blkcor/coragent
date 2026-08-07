// Package event defines the serializable observation envelope.
//
// An Event reports a fact that already occurred. Events never contain
// channels, callbacks, closures, runtime credentials, internal pointers, or
// raw Go errors: every payload is a pure data struct of exported value
// fields, and TestPayloadsArePureData enforces that contract by reflection.
// A frontend answers an event (such as a later approval request) by sending
// a new SessionCommand, never through a channel embedded in an Event.
//
// JSON is the durable encoding. New event kinds are added as new Kind values
// with their own payload structs registered in payloadFactories; the envelope
// shape does not change.
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrUnknownKind = errors.New("event: unknown kind")
	ErrInvalid     = errors.New("event: invalid envelope")
)

// Kind identifies the fact an Event reports.
type Kind string

const (
	// KindRunStarted reports that a run began executing.
	KindRunStarted Kind = "run_started"
	// KindRunCompleted is the terminal outcome of a run that finished with a
	// final assistant answer.
	KindRunCompleted Kind = "run_completed"
	// KindRunFailed is the terminal outcome of a run that stopped with a
	// typed failure cause.
	KindRunFailed Kind = "run_failed"
	// KindRunCancelled is the terminal outcome of a run interrupted by a
	// cancel SessionCommand.
	KindRunCancelled Kind = "run_cancelled"
	// KindAssistantText reports a completed assistant text block. Streaming
	// deltas are separate display Events; the completed block is the durable
	// Transcript fact.
	KindAssistantText Kind = "assistant_text"
	// KindAssistantDelta is a projected streaming display fragment. Deltas are
	// Events, not Transcript records.
	KindAssistantDelta Kind = "assistant_delta"
	KindToolStarted    Kind = "tool_started"
	KindToolFinished   Kind = "tool_finished"
	KindWarning        Kind = "warning"
	KindRetryScheduled Kind = "retry_scheduled"
	KindSessionResumed Kind = "session_resumed"
	KindSessionClosed  Kind = "session_closed"
	// KindApprovalRequired is emitted when a prepared action needs user approval.
	KindApprovalRequired Kind = "approval_required"
)

// FailureCause is the typed reason a run failed. Failure causes are
// classified values, never raw error strings.
type FailureCause string

const (
	// CauseProviderPermanent is an unrecoverable provider failure such as
	// authentication or an invalid request.
	CauseProviderPermanent FailureCause = "provider_permanent"
	// CauseProviderProtocol is a malformed provider stream or wire-level
	// protocol violation.
	CauseProviderProtocol  FailureCause = "provider_protocol"
	CauseProviderTransient FailureCause = "provider_transient_exhausted"
	// CauseEngineInvariant is an engine-side invariant or programming error.
	CauseEngineInvariant  FailureCause = "engine_invariant"
	CauseBudgetExhausted  FailureCause = "budget_exhausted"
	CauseContextLimit     FailureCause = "context_limit"
	CauseProviderOutput   FailureCause = "provider_output_limit"
	CauseInterrupted      FailureCause = "interrupted"
	CauseSensitiveContent FailureCause = "sensitive_content_blocked"
)

// Event is the serializable observation envelope. Cursor is a session-wide
// monotonic sequence number that increases across runs; its high-water mark
// becomes durable with the session store. RunID is empty for facts that do
// not belong to a run.
type Event struct {
	SessionID string          `json:"session_id"`
	RunID     string          `json:"run_id"`
	Cursor    uint64          `json:"cursor"`
	Time      time.Time       `json:"time"`
	Kind      Kind            `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
}

// RunStartedPayload records the prompt that started the run.
type RunStartedPayload struct {
	Prompt string `json:"prompt"`
}

// RunCompletedPayload marks successful run termination.
type RunCompletedPayload struct{}

// RunFailedPayload marks failed run termination with a typed cause.
type RunFailedPayload struct {
	Cause FailureCause `json:"cause"`
}

// RunCancelledPayload marks run termination by cancellation.
type RunCancelledPayload struct{}

// AssistantTextPayload carries one completed assistant text block.
type AssistantTextPayload struct {
	Text string `json:"text"`
}

type AssistantDeltaPayload struct {
	Text string `json:"text"`
}

type ToolStartedPayload struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
}

type ToolFinishedPayload struct {
	CallID  string `json:"call_id"`
	Outcome string `json:"outcome"`
}

type WarningPayload struct {
	Code string `json:"code"`
}

type RetryScheduledPayload struct {
	Attempt     int    `json:"attempt"`
	DelayMillis int64  `json:"delay_ms"`
	Class       string `json:"class"`
}

// ApprovalRequiredPayload carries a prepared patch for user approval.
// The diff field is for display only and is never persisted to transcript.
type ApprovalRequiredPayload struct {
	RequestID   string `json:"request_id"`
	ToolCallID  string `json:"tool_call_id"`
	Path        string `json:"path"`
	Target      string `json:"target"`
	Diff        string `json:"diff"`
	IsSensitive bool   `json:"is_sensitive"`
}

type SessionResumedPayload struct{}
type SessionClosedPayload struct{}

// payloadFactories registers one zero-value constructor per Kind. It is the
// single list of event kinds; tests use it to prove every kind round-trips
// through JSON and every payload stays pure data.
var payloadFactories = map[Kind]func() any{
	KindRunStarted:       func() any { return &RunStartedPayload{} },
	KindRunCompleted:     func() any { return &RunCompletedPayload{} },
	KindRunFailed:        func() any { return &RunFailedPayload{} },
	KindRunCancelled:     func() any { return &RunCancelledPayload{} },
	KindAssistantText:    func() any { return &AssistantTextPayload{} },
	KindAssistantDelta:   func() any { return &AssistantDeltaPayload{} },
	KindToolStarted:      func() any { return &ToolStartedPayload{} },
	KindToolFinished:     func() any { return &ToolFinishedPayload{} },
	KindWarning:          func() any { return &WarningPayload{} },
	KindRetryScheduled:   func() any { return &RetryScheduledPayload{} },
	KindSessionResumed:   func() any { return &SessionResumedPayload{} },
	KindSessionClosed:    func() any { return &SessionClosedPayload{} },
	KindApprovalRequired: func() any { return &ApprovalRequiredPayload{} },
}

// New builds an Event, marshaling the kind-specific payload into the
// envelope.
func New(sessionID, runID string, cursor uint64, at time.Time, kind Kind, payload any) (Event, error) {
	if _, ok := payloadFactories[kind]; !ok {
		return Event{}, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("event: marshal %s payload: %w", kind, err)
	}
	ev := Event{
		SessionID: sessionID,
		RunID:     runID,
		Cursor:    cursor,
		Time:      at,
		Kind:      kind,
		Payload:   raw,
	}
	if err := ev.Validate(); err != nil {
		return Event{}, err
	}
	return ev, nil
}

// Validate fails closed on unknown kinds, malformed payloads, or missing
// envelope identity. It never skips an Event during durable replay.
func (e Event) Validate() error {
	if e.SessionID == "" || e.Cursor == 0 || e.Time.IsZero() {
		return ErrInvalid
	}
	factory, ok := payloadFactories[e.Kind]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKind, e.Kind)
	}
	payload := factory()
	if err := e.DecodePayload(payload); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	runFact := e.Kind != KindWarning && e.Kind != KindSessionResumed && e.Kind != KindSessionClosed
	if runFact && e.RunID == "" {
		return fmt.Errorf("%w: %s requires a run ID", ErrInvalid, e.Kind)
	}
	if !runFact && e.RunID != "" {
		return fmt.Errorf("%w: %s must not have a run ID", ErrInvalid, e.Kind)
	}
	switch p := payload.(type) {
	case *RunStartedPayload:
		if p.Prompt == "" {
			return fmt.Errorf("%w: empty run prompt", ErrInvalid)
		}
	case *RunFailedPayload:
		if !validFailureCause(p.Cause) {
			return fmt.Errorf("%w: unknown failure cause %q", ErrInvalid, p.Cause)
		}
	case *AssistantTextPayload:
		if p.Text == "" {
			return fmt.Errorf("%w: empty assistant text", ErrInvalid)
		}
	case *AssistantDeltaPayload:
		if p.Text == "" {
			return fmt.Errorf("%w: empty assistant delta", ErrInvalid)
		}
	case *ToolStartedPayload:
		if p.CallID == "" || p.Name == "" {
			return fmt.Errorf("%w: incomplete tool start", ErrInvalid)
		}
	case *ToolFinishedPayload:
		if p.CallID == "" || !validToolOutcome(p.Outcome) {
			return fmt.Errorf("%w: incomplete tool finish", ErrInvalid)
		}
	case *WarningPayload:
		if p.Code == "" {
			return fmt.Errorf("%w: empty warning code", ErrInvalid)
		}
	case *RetryScheduledPayload:
		if p.Attempt <= 0 || p.DelayMillis < 0 || p.Class == "" {
			return fmt.Errorf("%w: invalid retry metadata", ErrInvalid)
		}
	case *ApprovalRequiredPayload:
		if p.RequestID == "" || p.ToolCallID == "" || p.Path == "" || p.Target == "" {
			return fmt.Errorf("%w: incomplete approval required payload", ErrInvalid)
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

func validToolOutcome(outcome string) bool {
	switch outcome {
	case "success", "error", "cancelled", "skipped", "interrupted", "blocked":
		return true
	default:
		return false
	}
}

// DecodePayload unmarshals the kind-specific payload into v.
func (e Event) DecodePayload(v any) error {
	decoder := json.NewDecoder(strings.NewReader(string(e.Payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("event: decode %s payload: %w", e.Kind, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("event: decode %s payload: trailing data", e.Kind)
	}
	return nil
}
