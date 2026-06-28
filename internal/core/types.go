// Package core holds the domain type definitions shared by the harness
// machinery and re-exported by the public pkg/agent facade.
//
// These types physically live here (not in pkg/agent) so the internal packages
// — loop, context, executor, provider — can depend on the domain vocabulary
// without importing pkg/agent, which would create an import cycle (pkg/agent
// composes those same internal packages). pkg/agent re-exports every name here
// as a type alias, so the public contract (agent.Conversation, agent.RunEvent,
// …) is byte-identical and unchanged.
package core

import (
	"context"
	"encoding/json"
)

// Conversation represents a sequence of turns between user and assistant.
type Conversation struct {
	// Turns is the ordered sequence of exchanges in this conversation.
	Turns []Turn
}

// Turn represents a single exchange in a conversation.
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
type Tool struct {
	// Name is the identifier the assistant uses to invoke this tool.
	Name string

	// Description explains what the tool does, read by the model.
	Description string

	// Parameters is a JSON schema describing the tool's arguments.
	Parameters json.RawMessage
}

// ToolCall represents a request from the assistant to invoke a specific tool.
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
type Provider interface {
	// StreamReply takes a conversation and available tools, and returns a channel
	// of RunEvents streamed incrementally as the model produces its reply.
	StreamReply(ctx context.Context, conv Conversation, tools []Tool, opts StreamOptions) <-chan RunEvent
}

// StreamOptions controls per-request model and sampling options.
type StreamOptions struct {
	// Model overrides the configured default model for this request.
	Model string

	// Temperature controls randomness in generation (0.0 to 2.0).
	Temperature *float64

	// MaxTokens limits the length of the generated reply.
	MaxTokens *int
}

// RunEvent is a typed event streamed during a run.
//
// RunEvent is the single vocabulary for two producers. A Provider emits the
// streaming subset (TextDelta, ToolCallEvent, StatusChange, ReplyEndedEvent,
// ErrorEvent). The agent loop consumes those and emits the richer run-level set
// (adding ToolStartedEvent, ToolFinishedEvent, RunFinishedEvent,
// PermissionRequestedEvent, OverBudgetWarningEvent) on the one run stream a
// frontend drains.
type RunEvent struct {
	// Type identifies the kind of event.
	Type RunEventType

	// TextDelta is the incremental text chunk (for TextDelta events).
	TextDelta string

	// ToolCall is the complete tool call. A provider populates it on a
	// ToolCallEvent (provider stream); the loop consumes that and re-emits the
	// request on the run stream as a ToolStartedEvent.
	ToolCall *ToolCall

	// Status is the current status (for StatusChange events).
	Status string

	// ReplyEnded indicates how the reply ended (for ReplyEnded events).
	ReplyEnded *ReplyEnded

	// Error is the error that occurred (for Error events).
	Error error

	// ToolResult is the outcome of a tool (for ToolFinishedEvent events).
	ToolResult *ToolResult

	// RunFinished carries the single terminal stop reason (for RunFinishedEvent events).
	RunFinished *RunFinished

	// Permission is the wait-for-a-human question (for PermissionRequestedEvent events).
	Permission *PermissionRequest

	// Warning is an advisory message (for OverBudgetWarningEvent events).
	Warning string
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

	// ToolStartedEvent indicates a tool invocation began (carries ToolCall).
	ToolStartedEvent

	// ToolFinishedEvent indicates a tool invocation finished (carries ToolResult).
	ToolFinishedEvent

	// RunFinishedEvent indicates the whole run ended (carries RunFinished).
	RunFinishedEvent

	// PermissionRequestedEvent carries a wait-for-a-human question (carries Permission).
	PermissionRequestedEvent

	// OverBudgetWarningEvent is an advisory that the conversation exceeds the
	// context budget (carries Warning); the run proceeds anyway.
	OverBudgetWarningEvent
)

// Status values for StatusChange events. They are advisory and carry no control
// semantics; the named stop reasons are the authoritative lifecycle.
const (
	// StatusThinking marks the agent consulting the model.
	StatusThinking = "thinking"

	// StatusCallingTool marks the agent running a tool.
	StatusCallingTool = "calling_tool"

	// StatusIdle marks the run ending.
	StatusIdle = "idle"
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

// StopReason is the single terminal outcome of a run. Every run ends with
// exactly one of these, never two and never none.
type StopReason int

const (
	// StopCompleted means the model returned a reply with no tool request.
	StopCompleted StopReason = iota

	// StopReachedStepLimit means the configured maximum number of model rounds
	// was reached. It is a normal stop, not an error.
	StopReachedStepLimit

	// StopCancelled means the run was cancelled by the caller.
	StopCancelled

	// StopFailed means the run ended on an unrecoverable error.
	StopFailed
)

// IsError reports whether the stop reason represents a failure. Only StopFailed
// is an error; completed, reached-the-step-limit, and cancelled are not.
func (r StopReason) IsError() bool {
	return r == StopFailed
}

// RunFinished is the payload of a RunFinishedEvent: the one named stop reason
// and, for StopFailed, the cause.
type RunFinished struct {
	// Reason is the single terminal outcome of the run.
	Reason StopReason

	// Err names the cause when Reason is StopFailed; nil otherwise.
	Err error
}

// Dispatcher is the single seam through which every tool call flows. Phase 1
// ships an inert stand-in; later phases fill the path with permission, hooks,
// and sandbox stages — the loop is untouched because the seam is unchanged.
//
// A recoverable tool failure is reported as a ToolResult with IsError set and a
// nil error: the loop surfaces it and continues. A truly unrecoverable condition
// is reported as a non-nil error: the loop ends the run with StopFailed.
//
// The emit callback hands the dispatcher the same live run stream the frontend
// is draining, so it can raise a PermissionRequestedEvent and block on the reply
// path. emit returns a non-nil error when the context is cancelled.
type Dispatcher interface {
	Dispatch(ctx context.Context, call ToolCall, emit func(RunEvent) error) (ToolResult, error)
}
