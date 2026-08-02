// Package provider defines the internal model request/response contract and
// the failure classification every provider adapter reports through.
//
// Provider-specific wire types never escape an adapter: adapters translate
// into Request, Response, ToolCall, and Failure. The scripted offline
// replacement used by tests lives in this package as Scripted.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Role is an internal conversational role, independent of Provider wire data.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one bounded model-context item.
type Message struct {
	Role       Role
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

// ToolDefinition is the model-facing read-only tool contract.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Request is one model request assembled from current runtime facts.
type Request struct {
	// Prompt is retained as a convenience for simple scripted tests. Runtime
	// requests use StablePrompt, DynamicPrompt, and Messages.
	Prompt        string
	StablePrompt  string
	DynamicPrompt string
	Messages      []Message
	Tools         []ToolDefinition
	MaxOutput     int
}

// ToolCall is one complete tool call requested by the model. The S1.1 loop
// never receives any; the field exists so the scripted provider can emit
// tool calls in S1.5 without breaking this interface.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Response is one completed provider turn.
type Response struct {
	Text string
	// ToolCalls holds complete tool calls in provider order. The S1.1 loop
	// handles text only; the Action Broker consumes these from S1.5.
	ToolCalls []ToolCall
	Usage     Usage
	Reason    TerminalReason
}

// Usage is optional Provider-reported token accounting.
type Usage struct {
	InputTokens  uint64
	OutputTokens uint64
}

// TerminalReason preserves useful finish metadata without controlling whether
// completed ToolCalls enter the tool phase.
type TerminalReason string

const (
	ReasonStop      TerminalReason = "stop"
	ReasonToolCalls TerminalReason = "tool_calls"
	ReasonLength    TerminalReason = "length"
	ReasonOther     TerminalReason = "other"
)

// FailureClass classifies a provider failure before any recovery decision.
type FailureClass string

const (
	// ClassPermanent is an unrecoverable failure such as authentication or
	// an invalid request. No retry.
	ClassPermanent FailureClass = "permanent"
	// ClassProtocol is a malformed provider stream. No retry.
	ClassProtocol FailureClass = "protocol"
	// ClassRateLimit, ClassTransient, and ClassOverloaded are the only M1
	// classes eligible for bounded retry.
	ClassRateLimit  FailureClass = "rate_limit"
	ClassTransient  FailureClass = "transient"
	ClassOverloaded FailureClass = "overloaded"
	// ClassContextOverflow and ClassOutputLimit are classified for later
	// milestones but are not retried by the M1 transient path.
	ClassContextOverflow FailureClass = "context_overflow"
	ClassOutputLimit     FailureClass = "output_limit"
	ClassCancelled       FailureClass = "cancelled"
)

// Failure is a classified provider failure.
type Failure struct {
	Class   FailureClass
	Message string
	// RetryAfter is safe timing metadata parsed from the response header.
	RetryAfter time.Duration
}

// Error implements error.
func (f *Failure) Error() string {
	return fmt.Sprintf("provider failure (%s): %s", f.Class, f.Message)
}

// Provider produces one completed turn for a request. Every implementation
// must honor context cancellation and return promptly when ctx is done.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Identity is the non-secret, immutable Provider configuration bound to a
// durable session. EndpointSHA256 identifies transport authority without
// persisting a potentially sensitive URL. Runtime credentials are never part
// of this value.
type Identity struct {
	Adapter                string
	WireProtocol           string
	EndpointSHA256         string
	CredentialSourceSHA256 string
	Model                  string
	ContextWindow          int
	MaxOutputTokens        int
	Temperature            *float64
	Seed                   *int64
	ToolChoice             string
}

// IdentityProvider exposes the stable profile needed for resume validation.
// Every Provider used by Engine must implement it.
type IdentityProvider interface {
	Identity() Identity
}

// StreamProvider emits raw Provider text deltas. The Engine projects each
// delta before creating a frontend Event and persists only the completed block.
type StreamProvider interface {
	Provider
	Stream(ctx context.Context, req Request, onText func(string) error) (Response, error)
}
