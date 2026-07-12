package agent

import (
	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/skill"
)

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

// Model backend seam. A successful Provider stream ends with exactly one
// ReplyEndedEvent carrying a non-nil ReplyEnded with one of the defined reasons,
// then closes. No event may follow ReplyEndedEvent.
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
	HookOutcome    = core.HookOutcome
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

// Hooks.
type (
	HookMoment          = core.HookMoment
	HookAction          = core.HookAction
	HookScope           = core.HookScope
	HookEvent           = core.HookEvent
	HookVerdict         = core.HookVerdict
	HookFunc            = core.HookFunc
	HookRegistration    = core.HookRegistration
	ExternalHook        = core.ExternalHook
	HookLifecycleResult = core.HookLifecycleResult
	LifecycleHooks      = core.LifecycleHooks
)

// Tool authoring. Command-running tools also implement CommandToolHandler and
// launch processes only through the supplied CommandRunner.
type (
	ToolHandler        = core.ToolHandler
	CommandToolHandler = core.CommandToolHandler
	CommandRunner      = core.CommandRunner
	CommandSpec        = core.CommandSpec
)

// Action classification. A ToolHandler may optionally implement ActionClassifier
// to declare its ActionKind, which the soft permission gate uses for mode-aware
// decisions (plan mode blocks mutation; auto-accept-edits allows only edits).
type (
	ActionKind       = core.ActionKind
	ActionClassifier = core.ActionClassifier
)

// Sandbox reporting.
type (
	SandboxStatus    = sandbox.Status
	ConfinementLevel = sandbox.ConfinementLevel
	SandboxGrants    = core.SandboxGrants
)

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
	HookOutcomeEvent         = core.HookOutcomeEvent
)

// HookMoment values.
const (
	HookSessionStart = core.HookSessionStart
	HookPromptSubmit = core.HookPromptSubmit
	HookBeforeTool   = core.HookBeforeTool
	HookAfterTool    = core.HookAfterTool
	HookRunFinished  = core.HookRunFinished
	HookSessionStop  = core.HookSessionStop
)

// HookAction values.
const (
	HookAllowed  = core.HookAllowed
	HookBlocked  = core.HookBlocked
	HookReplaced = core.HookReplaced
	HookInjected = core.HookInjected
)

// Status values for StatusChange events.
const (
	StatusThinking         = core.StatusThinking
	StatusCallingTool      = core.StatusCallingTool
	StatusIdle             = core.StatusIdle
	StatusSubagentStarted  = core.StatusSubagentStarted
	StatusSubagentFinished = core.StatusSubagentFinished
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

// ActionKind values.
const (
	ActionUnknown = core.ActionUnknown
	ActionRead    = core.ActionRead
	ActionEdit    = core.ActionEdit
	ActionCommand = core.ActionCommand
)

// ConfinementLevel values.
const (
	ConfinementOSEnforced     = sandbox.ConfinementOSEnforced
	ConfinementPolicyFallback = sandbox.ConfinementPolicyFallback
)

// SkillInfo is a frontend-safe snapshot of a loaded skill.
type SkillInfo struct {
	Name        string
	Description string
	Type        string
	Source      string
}

// SkillInfoFromInternal converts an internal skill to a public SkillInfo.
func SkillInfoFromInternal(s *skill.Skill) SkillInfo {
	if s == nil {
		return SkillInfo{}
	}
	return SkillInfo{
		Name:        s.Name,
		Description: s.Description,
		Type:        s.Type,
		Source:      string(s.Source),
	}
}
