package agent

import "github.com/blkcor/coragent/internal/core"

// Observed-run schema, envelope, correlation, and validation types.
type (
	ObservedSchemaVersion          = core.ObservedSchemaVersion
	RunID                          = core.RunID
	AgentID                        = core.AgentID
	CallID                         = core.CallID
	RequestID                      = core.RequestID
	PreviewRevision                = core.PreviewRevision
	Origin                         = core.Origin
	ObservedEventKind              = core.ObservedEventKind
	ObservedEvent                  = core.ObservedEvent
	ObservedEventPayload           = core.ObservedEventPayload
	UnsupportedObservedSchemaError = core.UnsupportedObservedSchemaError
	UnknownObservedKindError       = core.UnknownObservedKindError
	ObservedPayloadMismatchError   = core.ObservedPayloadMismatchError
	InvalidObservedEventError      = core.InvalidObservedEventError
)

// Schema-v1 kinds are closed. An incompatible addition uses a new version.
const (
	ObservedSchemaV1 = core.ObservedSchemaV1

	ObservedKindRunStarted                     = core.ObservedKindRunStarted
	ObservedKindStatusChanged                  = core.ObservedKindStatusChanged
	ObservedKindAssistantStarted               = core.ObservedKindAssistantStarted
	ObservedKindAssistantTextDelta             = core.ObservedKindAssistantTextDelta
	ObservedKindAssistantReasoningSummaryDelta = core.ObservedKindAssistantReasoningSummaryDelta
	ObservedKindAssistantFinished              = core.ObservedKindAssistantFinished
	ObservedKindToolProposed                   = core.ObservedKindToolProposed
	ObservedKindToolPrepared                   = core.ObservedKindToolPrepared
	ObservedKindPermissionRequested            = core.ObservedKindPermissionRequested
	ObservedKindToolExecuting                  = core.ObservedKindToolExecuting
	ObservedKindToolFinished                   = core.ObservedKindToolFinished
	ObservedKindContextUsageUpdated            = core.ObservedKindContextUsageUpdated
	ObservedKindOmissionReported               = core.ObservedKindOmissionReported
	ObservedKindHookOutcome                    = core.ObservedKindHookOutcome
	ObservedKindSubagentStarted                = core.ObservedKindSubagentStarted
	ObservedKindSubagentFinished               = core.ObservedKindSubagentFinished
	ObservedKindWarning                        = core.ObservedKindWarning
	ObservedKindError                          = core.ObservedKindError
	ObservedKindRunFinished                    = core.ObservedKindRunFinished
)

// Frontend-neutral observed values.
type (
	OptionalUint64             = core.OptionalUint64
	CapabilitySupport          = core.CapabilitySupport
	CapabilityAvailability     = core.CapabilityAvailability
	CapabilityKind             = core.CapabilityKind
	Capability                 = core.Capability
	CapabilityCategory         = core.CapabilityCategory
	ContextUsageSource         = core.ContextUsageSource
	ContextUsage               = core.ContextUsage
	ProviderUsage              = core.ProviderUsage
	OmissionKind               = core.OmissionKind
	OmissionScope              = core.OmissionScope
	Recoverability             = core.Recoverability
	ContinuationMode           = core.ContinuationMode
	Omission                   = core.Omission
	ActionOperation            = core.ActionOperation
	ActionPreviewKind          = core.ActionPreviewKind
	DiffLineKind               = core.DiffLineKind
	DiffLine                   = core.DiffLine
	DiffHunk                   = core.DiffHunk
	FileDiffPreview            = core.FileDiffPreview
	ActionPreview              = core.ActionPreview
	SandboxGrantOptions        = core.SandboxGrantOptions
	SubagentOutcome            = core.SubagentOutcome
	SubagentProvenance         = core.SubagentProvenance
	ActivityStatus             = core.ActivityStatus
	ProviderTerminationReason  = core.ProviderTerminationReason
	ToolOutcome                = core.ToolOutcome
	RunOutcome                 = core.RunOutcome
	ObservedError              = core.ObservedError
	PermissionProtocol         = core.PermissionProtocol
	PermissionCapabilities     = core.PermissionCapabilities
	RememberedRuleScope        = core.RememberedRuleScope
	ObservedPermissionRequest  = core.ObservedPermissionRequest
	PermissionReplyAction      = core.PermissionReplyAction
	PermissionReplyStatus      = core.PermissionReplyStatus
	PermissionReplyFeedback    = core.PermissionReplyFeedback
	ObservedPermissionDecision = core.ObservedPermissionDecision
	PermissionReplyResult      = core.PermissionReplyResult
)

const (
	CapabilitySupportUnknown     = core.CapabilitySupportUnknown
	CapabilitySupportUnsupported = core.CapabilitySupportUnsupported
	CapabilitySupportSupported   = core.CapabilitySupportSupported

	CapabilityAvailabilityUnknown     = core.CapabilityAvailabilityUnknown
	CapabilityAvailabilityUnavailable = core.CapabilityAvailabilityUnavailable
	CapabilityAvailabilityAvailable   = core.CapabilityAvailabilityAvailable

	CapabilityKindUnknown  = core.CapabilityKindUnknown
	CapabilityKindTool     = core.CapabilityKindTool
	CapabilityKindHook     = core.CapabilityKindHook
	CapabilityKindSandbox  = core.CapabilityKindSandbox
	CapabilityKindSubagent = core.CapabilityKindSubagent
	CapabilityKindSkill    = core.CapabilityKindSkill
	CapabilityKindMCP      = core.CapabilityKindMCP

	ContextUsageSourceUnknown = core.ContextUsageSourceUnknown
	ContextUsageEstimated     = core.ContextUsageEstimated
	ContextUsageProvider      = core.ContextUsageProvider

	OmissionKindUnknown       = core.OmissionKindUnknown
	OmissionOutputBudget      = core.OmissionOutputBudget
	OmissionPreviewBudget     = core.OmissionPreviewBudget
	OmissionProviderLength    = core.OmissionProviderLength
	OmissionContentFilter     = core.OmissionContentFilter
	OmissionRedacted          = core.OmissionRedacted
	OmissionContextCompaction = core.OmissionContextCompaction

	OmissionScopeUnknown        = core.OmissionScopeUnknown
	OmissionScopeAssistantReply = core.OmissionScopeAssistantReply
	OmissionScopeToolOutput     = core.OmissionScopeToolOutput
	OmissionScopeActionPreview  = core.OmissionScopeActionPreview
	OmissionScopeConversation   = core.OmissionScopeConversation
	OmissionScopePublicPayload  = core.OmissionScopePublicPayload

	RecoverabilityUnknown       = core.RecoverabilityUnknown
	RecoverabilityRecoverable   = core.RecoverabilityRecoverable
	RecoverabilityUnrecoverable = core.RecoverabilityUnrecoverable

	ContinuationUnknown     = core.ContinuationUnknown
	ContinuationUnavailable = core.ContinuationUnavailable
	ContinuationNewUserTurn = core.ContinuationNewUserTurn

	ActionOperationUnknown = core.ActionOperationUnknown
	ActionOperationCreate  = core.ActionOperationCreate
	ActionOperationModify  = core.ActionOperationModify
	ActionOperationDelete  = core.ActionOperationDelete
	ActionOperationCommand = core.ActionOperationCommand
	ActionOperationCustom  = core.ActionOperationCustom

	ActionPreviewKindUnknown = core.ActionPreviewKindUnknown
	ActionPreviewUnavailable = core.ActionPreviewUnavailable
	ActionPreviewText        = core.ActionPreviewText
	ActionPreviewFileDiff    = core.ActionPreviewFileDiff
	ActionPreviewMetadata    = core.ActionPreviewMetadata

	DiffLineContext = core.DiffLineContext
	DiffLineAdded   = core.DiffLineAdded
	DiffLineRemoved = core.DiffLineRemoved

	SubagentOutcomeUnknown          = core.SubagentOutcomeUnknown
	SubagentOutcomeCompleted        = core.SubagentOutcomeCompleted
	SubagentOutcomeFailed           = core.SubagentOutcomeFailed
	SubagentOutcomeCancelled        = core.SubagentOutcomeCancelled
	SubagentOutcomeReachedStepLimit = core.SubagentOutcomeReachedStepLimit

	ActivityUnknown           = core.ActivityUnknown
	ActivityThinking          = core.ActivityThinking
	ActivityPreparingTool     = core.ActivityPreparingTool
	ActivityWaitingPermission = core.ActivityWaitingPermission
	ActivityCallingTool       = core.ActivityCallingTool
	ActivityCancelling        = core.ActivityCancelling
	ActivityIdle              = core.ActivityIdle

	ProviderTerminationUnknown          = core.ProviderTerminationUnknown
	ProviderTerminationStop             = core.ProviderTerminationStop
	ProviderTerminationToolCalls        = core.ProviderTerminationToolCalls
	ProviderTerminationLength           = core.ProviderTerminationLength
	ProviderTerminationContentFilter    = core.ProviderTerminationContentFilter
	ProviderTerminationFailure          = core.ProviderTerminationFailure
	ProviderTerminationProviderSpecific = core.ProviderTerminationProviderSpecific

	ToolOutcomeUnknown     = core.ToolOutcomeUnknown
	ToolOutcomeSucceeded   = core.ToolOutcomeSucceeded
	ToolOutcomeFailed      = core.ToolOutcomeFailed
	ToolOutcomeDenied      = core.ToolOutcomeDenied
	ToolOutcomeCancelled   = core.ToolOutcomeCancelled
	ToolOutcomeHookBlocked = core.ToolOutcomeHookBlocked

	RunOutcomeUnknown          = core.RunOutcomeUnknown
	RunOutcomeCompleted        = core.RunOutcomeCompleted
	RunOutcomeReachedStepLimit = core.RunOutcomeReachedStepLimit
	RunOutcomeCancelled        = core.RunOutcomeCancelled
	RunOutcomeFailed           = core.RunOutcomeFailed

	PermissionProtocolUnknown       = core.PermissionProtocolUnknown
	PermissionProtocolRich          = core.PermissionProtocolRich
	PermissionProtocolLegacyOneShot = core.PermissionProtocolLegacyOneShot

	PermissionReplyAllow           = core.PermissionReplyAllow
	PermissionReplyDeny            = core.PermissionReplyDeny
	PermissionReplyReviseArguments = core.PermissionReplyReviseArguments

	PermissionReplyAccepted           = core.PermissionReplyAccepted
	PermissionReplyValidationRejected = core.PermissionReplyValidationRejected
	PermissionReplyAlreadyResolved    = core.PermissionReplyAlreadyResolved
)

// Schema-v1 typed payloads.
type (
	RunStartedPayload                     = core.RunStartedPayload
	StatusChangedPayload                  = core.StatusChangedPayload
	AssistantStartedPayload               = core.AssistantStartedPayload
	AssistantTextDeltaPayload             = core.AssistantTextDeltaPayload
	AssistantReasoningSummaryDeltaPayload = core.AssistantReasoningSummaryDeltaPayload
	AssistantFinishedPayload              = core.AssistantFinishedPayload
	ToolProposedPayload                   = core.ToolProposedPayload
	ToolPreparedPayload                   = core.ToolPreparedPayload
	PermissionRequestedPayload            = core.PermissionRequestedPayload
	ToolExecutingPayload                  = core.ToolExecutingPayload
	ToolFinishedPayload                   = core.ToolFinishedPayload
	ContextUsageUpdatedPayload            = core.ContextUsageUpdatedPayload
	OmissionReportedPayload               = core.OmissionReportedPayload
	HookOutcomePayload                    = core.HookOutcomePayload
	SubagentStartedPayload                = core.SubagentStartedPayload
	SubagentFinishedPayload               = core.SubagentFinishedPayload
	WarningPayload                        = core.WarningPayload
	ErrorPayload                          = core.ErrorPayload
	RunFinishedPayload                    = core.RunFinishedPayload
)
