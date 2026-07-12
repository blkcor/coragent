package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// RichEvent is the harness's single frontend-neutral event envelope. The loop,
// lifecycle hooks, executor adapters, and session runtime all feed this shape.
// Public observed events are a direct projection of it; Legacy is the exact
// pre-Phase-7 RunEvent projection when the fact has one. Rich-only facts leave
// Legacy nil instead of encoding themselves into legacy strings.
//
// RunID, Sequence, Timestamp, and a root Origin are filled by sessionrun at the
// one run boundary. Producers may set a delegated Origin for the few child facts
// that are allowed to cross the root isolation boundary.
type RichEvent struct {
	RunID     RunID
	Sequence  uint64
	Timestamp time.Time
	Origin    Origin
	Kind      ObservedEventKind
	Payload   ObservedEventPayload
	Legacy    *RunEvent
}

// Clone returns an independent event value while retaining live reply
// operations embedded in permission payloads.
func (e RichEvent) Clone() RichEvent {
	out := e
	out.Payload = cloneObservedPayload(e.Payload)
	if e.Legacy != nil {
		legacy := cloneRunEvent(*e.Legacy)
		out.Legacy = &legacy
	}
	return out
}

// Observed projects the internal envelope onto the public schema-v1 envelope.
func (e RichEvent) Observed() ObservedEvent {
	return ObservedEvent{
		SchemaVersion: ObservedSchemaV1,
		RunID:         e.RunID,
		Sequence:      e.Sequence,
		Timestamp:     e.Timestamp,
		Origin:        e.Origin,
		Kind:          e.Kind,
		Payload:       cloneObservedPayload(e.Payload),
	}
}

// RichProvider is an optional extension. The required Provider interface stays
// unchanged; the loop selects this method by type assertion and performs only
// one provider request for a model round.
type RichProvider interface {
	StreamRichReply(ctx context.Context, conv Conversation, tools []Tool, opts StreamOptions) <-chan RichProviderEvent
}

// RichProviderEventType is the closed internal provider-stream vocabulary.
type RichProviderEventType uint8

const (
	RichProviderTextDelta RichProviderEventType = iota
	RichProviderReasoningSummaryDelta
	RichProviderToolCall
	RichProviderUsage
	RichProviderWarning
	RichProviderReplyEnded
	RichProviderError
)

// RichProviderEvent carries one ordered provider fact. A successful stream has
// exactly one ReplyEnded event followed by channel closure.
type RichProviderEvent struct {
	Type                  RichProviderEventType
	TextDelta             string
	ReasoningSummaryDelta string
	ToolCall              *ToolCall
	Usage                 *ProviderUsage
	WarningCode           string
	Warning               string
	ReplyEnded            *RichReplyEnded
	Error                 error
}

// RichReplyEnded preserves termination detail unavailable in ReplyEnded.
type RichReplyEnded struct {
	Reason             ProviderTerminationReason
	ProviderReasonCode string
}

// RichDispatchResult is the optional executor's structured completion. The
// existing Dispatcher result remains authoritative for legacy implementations.
type RichDispatchResult struct {
	Result   ToolResult
	Revision PreviewRevision
	Outcome  ToolOutcome
}

// RichDispatcher optionally reports prepared/executing facts through the same
// internal event path. Dispatcher remains the required compatibility seam.
type RichDispatcher interface {
	DispatchRich(ctx context.Context, call ToolCall, callID CallID, origin Origin, emit func(RichEvent) error) (RichDispatchResult, error)
}

// NewOpaqueID returns a process-local opaque identifier suitable for event
// correlation. Randomness is preferred; the monotonic fallback remains unique
// within the process if the platform random source is unavailable.
func NewOpaqueID(prefix string) string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%s-%d", prefix, opaqueIDFallback.Add(1))
}

var opaqueIDFallback atomic.Uint64

func cloneRunEvent(event RunEvent) RunEvent {
	out := event
	if event.ToolCall != nil {
		call := cloneToolCall(*event.ToolCall)
		out.ToolCall = &call
	}
	if event.ReplyEnded != nil {
		value := *event.ReplyEnded
		out.ReplyEnded = &value
	}
	if event.ToolResult != nil {
		value := *event.ToolResult
		out.ToolResult = &value
	}
	if event.RunFinished != nil {
		value := *event.RunFinished
		out.RunFinished = &value
	}
	if event.Permission != nil {
		value := *event.Permission
		value.ToolCall = cloneToolCall(value.ToolCall)
		out.Permission = &value
	}
	if event.HookOutcome != nil {
		value := *event.HookOutcome
		out.HookOutcome = &value
	}
	return out
}
