package agent

import "github.com/blkcor/coragent/internal/core"

// This file re-exports the domain vocabulary from internal/core as type aliases.
// The definitions live in internal/core so the harness machinery (loop, context,
// executor, provider) can depend on them without importing this public facade —
// which would form an import cycle, since pkg/agent composes those packages.
//
// Because these are aliases, the public contract is identical to the original
// definitions: agent.Conversation IS core.Conversation, fully interchangeable.

// Domain concepts.
type (
	Conversation = core.Conversation
	Turn         = core.Turn
	Tool         = core.Tool
	ToolCall     = core.ToolCall
	ToolResult   = core.ToolResult
)

// Model backend seam.
type (
	Provider      = core.Provider
	StreamOptions = core.StreamOptions
)

// Event stream.
type (
	RunEvent       = core.RunEvent
	RunEventType   = core.RunEventType
	ReplyEnded     = core.ReplyEnded
	ReplyEndReason = core.ReplyEndReason
)

// Run lifecycle.
type (
	StopReason  = core.StopReason
	RunFinished = core.RunFinished
)

// Human-in-the-loop.
type (
	PermissionRequest  = core.PermissionRequest
	PermissionDecision = core.PermissionDecision
)

// Tool-dispatch seam.
type Dispatcher = core.Dispatcher

// Tool authoring. An SDK developer implements ToolHandler to add a custom
// capability; it travels the identical execution path as the built-ins.
type ToolHandler = core.ToolHandler

// Execution-chain stage seams. Phase 2 ships inert placeholders for these; later
// phases supply real implementations injected at executor construction.
type (
	PreToolCheck     = core.PreToolCheck
	Permission       = core.Permission
	Sandbox          = core.Sandbox
	PostToolCheck    = core.PostToolCheck
	StageDecision    = core.StageDecision
	PermissionResult = core.PermissionResult
)

// RunEventType values.
const (
	TextDelta                = core.TextDelta
	ToolCallEvent            = core.ToolCallEvent
	StatusChange             = core.StatusChange
	ReplyEndedEvent          = core.ReplyEndedEvent
	ErrorEvent               = core.ErrorEvent
	ToolStartedEvent         = core.ToolStartedEvent
	ToolFinishedEvent        = core.ToolFinishedEvent
	RunFinishedEvent         = core.RunFinishedEvent
	PermissionRequestedEvent = core.PermissionRequestedEvent
	OverBudgetWarningEvent   = core.OverBudgetWarningEvent
)

// Status values for StatusChange events.
const (
	StatusThinking    = core.StatusThinking
	StatusCallingTool = core.StatusCallingTool
	StatusIdle        = core.StatusIdle
)

// ReplyEndReason values.
const (
	Finished           = core.Finished
	StoppedToCallTools = core.StoppedToCallTools
	CutOff             = core.CutOff
)

// StopReason values.
const (
	StopCompleted        = core.StopCompleted
	StopReachedStepLimit = core.StopReachedStepLimit
	StopCancelled        = core.StopCancelled
	StopFailed           = core.StopFailed
)
