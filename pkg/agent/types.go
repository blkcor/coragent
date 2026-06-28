package agent

import (
	"context"
	"encoding/json"
)

// Conversation represents a sequence of turns between user and assistant.
// A conversation accumulates turns over time, forming the context the model
// sees when generating its next reply.
type Conversation struct {
	// Turns is the ordered sequence of exchanges in this conversation.
	Turns []Turn
}

// Turn represents a single exchange in a conversation.
// A turn may be a user message, an assistant reply, or a tool interaction.
type Turn struct {
	// Role identifies who produced this turn (user, assistant, tool).
	Role string

	// Content is the text of the turn (user message, assistant reply, tool result).
	Content string

	// ToolCalls are the tool requests made by the assistant in this turn, if any.
	ToolCalls []ToolCall

	// ToolResults are the outcomes of tool calls, returned to the assistant.
	ToolResults []ToolResult
}

// Tool describes a capability the assistant can invoke.
// Each tool has a name, a description the model reads, and a parameter schema.
type Tool struct {
	// Name is the identifier the assistant uses to invoke this tool.
	Name string

	// Description explains what the tool does, read by the model.
	Description string

	// Parameters is a JSON schema describing the tool's arguments.
	Parameters json.RawMessage
}

// ToolCall represents a request from the assistant to invoke a specific tool.
// The assistant specifies the tool name and provides arguments.
type ToolCall struct {
	// ID uniquely identifies this tool call within a conversation turn.
	ID string

	// ToolName is the name of the tool to invoke.
	ToolName string

	// Arguments are the parsed arguments for the tool call.
	Arguments map[string]interface{}
}

// ToolResult represents the outcome of a tool call, returned to the assistant.
type ToolResult struct {
	// ToolCallID is the ID of the tool call this result is for.
	ToolCallID string

	// Result is the outcome of the tool execution (success or error message).
	Result string

	// IsError indicates whether the tool execution failed.
	IsError bool
}

// Provider is the interface for model backends.
// Implementations speak the OpenAI-compatible protocol or provide faked replies for testing.
type Provider interface {
	// StreamReply takes a conversation and available tools, and returns a channel
	// of RunEvents streamed incrementally as the model produces its reply.
	// The channel closes when the reply ends or an error occurs.
	// The provided context controls cancellation; cancelling it aborts the request.
	StreamReply(ctx context.Context, conv Conversation, tools []Tool, opts StreamOptions) <-chan RunEvent
}

// StreamOptions controls per-request model and sampling options.
// Omitted fields fall back to configured defaults.
type StreamOptions struct {
	// Model overrides the configured default model for this request.
	Model string

	// Temperature controls randomness in generation (0.0 to 2.0).
	Temperature *float64

	// MaxTokens limits the length of the generated reply.
	MaxTokens *int
}

// RunEvent is a typed event streamed during a run.
// The consumer handles each event type explicitly.
type RunEvent struct {
	// Type identifies the kind of event.
	Type RunEventType

	// TextDelta is the incremental text chunk (for TextDelta events).
	TextDelta string

	// ToolCall is the complete tool call (for ToolCall events).
	ToolCall *ToolCall

	// Status is the current status (for StatusChange events).
	Status string

	// ReplyEnded indicates how the reply ended (for ReplyEnded events).
	ReplyEnded *ReplyEnded

	// Error is the error that occurred (for Error events).
	Error error
}

// RunEventType identifies the kind of run event.
type RunEventType int

const (
	// TextDelta indicates incremental assistant text.
	TextDelta RunEventType = iota

	// ToolCallEvent indicates a complete, dispatchable tool call.
	ToolCallEvent

	// StatusChange indicates a status update (thinking, calling tool, idle).
	StatusChange

	// ReplyEndedEvent indicates the reply has finished.
	ReplyEndedEvent

	// ErrorEvent indicates an error occurred.
	ErrorEvent
)

// ReplyEnded describes how a reply ended.
type ReplyEnded struct {
	// Reason explains why the reply ended.
	Reason ReplyEndReason
}

// ReplyEndReason explains why a reply ended.
type ReplyEndReason int

const (
	// Finished indicates the reply completed normally without tool calls.
	Finished ReplyEndReason = iota

	// StoppedToCallTools indicates the reply completed and requested tool calls.
	StoppedToCallTools

	// CutOff indicates the reply was cut off at a length limit.
	CutOff
)

// PermissionRequest represents a human-in-the-loop moment.
// The harness pauses and waits for a decision before proceeding.
// This shape is declared in Phase 0 for stability, though no component
// emits or acts on it until Phase 3.
type PermissionRequest struct {
	// ToolCall is the tool call requiring permission.
	ToolCall ToolCall

	// ReplyPath is how to send the decision back.
	ReplyPath chan<- PermissionDecision
}

// PermissionDecision is the human's response to a permission request.
type PermissionDecision struct {
	// Allow indicates whether to allow or deny the tool call.
	Allow bool

	// Remember indicates whether to remember this decision for similar future calls.
	Remember bool

	// EditedArguments are modified arguments for the tool call, if any.
	EditedArguments map[string]interface{}
}
