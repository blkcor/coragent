package core

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// ObservedSchemaVersion identifies an observed-event wire contract.
type ObservedSchemaVersion uint32

// ObservedSchemaV1 is the first closed observed-event schema.
const ObservedSchemaV1 ObservedSchemaVersion = 1

// Stable opaque identifiers used to correlate one observed run.
type (
	RunID           string
	AgentID         string
	CallID          string
	RequestID       string
	PreviewRevision uint64
)

// Origin attributes an observed fact to the root agent or a delegated agent.
// ParentAgentID and DelegationCallID are empty for the root agent.
type Origin struct {
	AgentID          AgentID
	ParentAgentID    AgentID
	Depth            int
	DelegationCallID CallID
}

// ObservedEventKind is a member of a schema's closed event vocabulary.
type ObservedEventKind string

// Schema-v1 observed event kinds. Adding a kind requires a new schema version.
const (
	ObservedKindRunStarted                     ObservedEventKind = "run_started"
	ObservedKindStatusChanged                  ObservedEventKind = "status_changed"
	ObservedKindAssistantStarted               ObservedEventKind = "assistant_started"
	ObservedKindAssistantTextDelta             ObservedEventKind = "assistant_text_delta"
	ObservedKindAssistantReasoningSummaryDelta ObservedEventKind = "assistant_reasoning_summary_delta"
	ObservedKindAssistantFinished              ObservedEventKind = "assistant_finished"
	ObservedKindToolProposed                   ObservedEventKind = "tool_proposed"
	ObservedKindToolPrepared                   ObservedEventKind = "tool_prepared"
	ObservedKindPermissionRequested            ObservedEventKind = "permission_requested"
	ObservedKindToolExecuting                  ObservedEventKind = "tool_executing"
	ObservedKindToolFinished                   ObservedEventKind = "tool_finished"
	ObservedKindContextUsageUpdated            ObservedEventKind = "context_usage_updated"
	ObservedKindOmissionReported               ObservedEventKind = "omission_reported"
	ObservedKindHookOutcome                    ObservedEventKind = "hook_outcome"
	ObservedKindSubagentStarted                ObservedEventKind = "subagent_started"
	ObservedKindSubagentFinished               ObservedEventKind = "subagent_finished"
	ObservedKindWarning                        ObservedEventKind = "warning"
	ObservedKindError                          ObservedEventKind = "error"
	ObservedKindRunFinished                    ObservedEventKind = "run_finished"
)

// IsSchemaV1 reports whether k is one of schema v1's exact closed kinds.
func (k ObservedEventKind) IsSchemaV1() bool {
	switch k {
	case ObservedKindRunStarted,
		ObservedKindStatusChanged,
		ObservedKindAssistantStarted,
		ObservedKindAssistantTextDelta,
		ObservedKindAssistantReasoningSummaryDelta,
		ObservedKindAssistantFinished,
		ObservedKindToolProposed,
		ObservedKindToolPrepared,
		ObservedKindPermissionRequested,
		ObservedKindToolExecuting,
		ObservedKindToolFinished,
		ObservedKindContextUsageUpdated,
		ObservedKindOmissionReported,
		ObservedKindHookOutcome,
		ObservedKindSubagentStarted,
		ObservedKindSubagentFinished,
		ObservedKindWarning,
		ObservedKindError,
		ObservedKindRunFinished:
		return true
	default:
		return false
	}
}

// ObservedEvent is one immutable-by-convention fact delivered by RunObserved.
// Payload is a closed interface; Validate proves that it matches Kind.
type ObservedEvent struct {
	SchemaVersion ObservedSchemaVersion
	RunID         RunID
	Sequence      uint64
	Timestamp     time.Time
	Origin        Origin
	Kind          ObservedEventKind
	Payload       ObservedEventPayload
}

// ObservedEventPayload is implemented only by the schema payloads in this
// package. Consumers can construct those payloads but cannot extend schema v1.
type ObservedEventPayload interface {
	observedEventPayload()
}

// UnsupportedObservedSchemaError reports a version that must not be guessed at.
type UnsupportedObservedSchemaError struct {
	Version ObservedSchemaVersion
}

func (e *UnsupportedObservedSchemaError) Error() string {
	return fmt.Sprintf("observed event: unsupported schema version %d", e.Version)
}

// UnknownObservedKindError reports a kind outside a declared schema's closed set.
type UnknownObservedKindError struct {
	SchemaVersion ObservedSchemaVersion
	Kind          ObservedEventKind
}

func (e *UnknownObservedKindError) Error() string {
	return fmt.Sprintf("observed event: unknown kind %q for schema version %d", e.Kind, e.SchemaVersion)
}

// ObservedPayloadMismatchError reports a nil payload or a kind/payload mismatch.
type ObservedPayloadMismatchError struct {
	Kind     ObservedEventKind
	Expected string
	Actual   string
}

func (e *ObservedPayloadMismatchError) Error() string {
	return fmt.Sprintf("observed event: kind %q requires %s, got %s", e.Kind, e.Expected, e.Actual)
}

// InvalidObservedEventError reports a malformed required envelope or payload field.
type InvalidObservedEventError struct {
	Field  string
	Reason string
}

func (e *InvalidObservedEventError) Error() string {
	return fmt.Sprintf("observed event: invalid %s: %s", e.Field, e.Reason)
}

// Validate rejects unsupported schemas before inspecting their kind or payload,
// then checks schema v1's closed kind/payload correspondence and required
// correlation fields.
func (e ObservedEvent) Validate() error {
	if e.SchemaVersion != ObservedSchemaV1 {
		return &UnsupportedObservedSchemaError{Version: e.SchemaVersion}
	}
	if !e.Kind.IsSchemaV1() {
		return &UnknownObservedKindError{SchemaVersion: e.SchemaVersion, Kind: e.Kind}
	}
	if e.RunID == "" {
		return &InvalidObservedEventError{Field: "run ID", Reason: "must be non-empty"}
	}
	if e.Sequence == 0 {
		return &InvalidObservedEventError{Field: "sequence", Reason: "must start at one"}
	}
	if e.Timestamp.IsZero() {
		return &InvalidObservedEventError{Field: "timestamp", Reason: "must be non-zero"}
	}
	if err := validateOrigin(e.Origin); err != nil {
		return err
	}
	actualKind, actualName, nilPayload := observedPayloadIdentity(e.Payload)
	expectedName := observedPayloadTypeName(e.Kind)
	if nilPayload || actualKind != e.Kind {
		if actualName == "" {
			actualName = "nil"
		}
		return &ObservedPayloadMismatchError{Kind: e.Kind, Expected: expectedName, Actual: actualName}
	}
	return validateObservedPayload(e.Payload)
}

func validateOrigin(origin Origin) error {
	if origin.AgentID == "" {
		return &InvalidObservedEventError{Field: "origin.agent ID", Reason: "must be non-empty"}
	}
	if origin.Depth < 0 {
		return &InvalidObservedEventError{Field: "origin.depth", Reason: "must be non-negative"}
	}
	if origin.Depth == 0 {
		if origin.ParentAgentID != "" || origin.DelegationCallID != "" {
			return &InvalidObservedEventError{Field: "origin", Reason: "root origin cannot declare parent provenance"}
		}
		return nil
	}
	if origin.ParentAgentID == "" || origin.DelegationCallID == "" {
		return &InvalidObservedEventError{Field: "origin", Reason: "delegated origin requires parent agent and delegation call IDs"}
	}
	return nil
}

// Clone returns a recursively independent value snapshot. The only intentionally
// shared values are immutable strings, time values, and future reply operations.
func (e ObservedEvent) Clone() ObservedEvent {
	out := e
	out.Payload = cloneObservedPayload(e.Payload)
	return out
}

// OptionalUint64 distinguishes an unavailable measurement from a measured zero.
type OptionalUint64 struct {
	Known bool
	Value uint64
}

// CapabilitySupport distinguishes unsupported, supported, and unknown ownership.
type CapabilitySupport string

const (
	CapabilitySupportUnknown     CapabilitySupport = "unknown"
	CapabilitySupportUnsupported CapabilitySupport = "unsupported"
	CapabilitySupportSupported   CapabilitySupport = "supported"
)

// CapabilityAvailability describes whether one reported item can be used.
type CapabilityAvailability string

const (
	CapabilityAvailabilityUnknown     CapabilityAvailability = "unknown"
	CapabilityAvailabilityUnavailable CapabilityAvailability = "unavailable"
	CapabilityAvailabilityAvailable   CapabilityAvailability = "available"
)

// CapabilityKind classifies frontend-inspectable capability inventory.
type CapabilityKind string

const (
	CapabilityKindUnknown  CapabilityKind = "unknown"
	CapabilityKindTool     CapabilityKind = "tool"
	CapabilityKindHook     CapabilityKind = "hook"
	CapabilityKindSandbox  CapabilityKind = "sandbox"
	CapabilityKindSubagent CapabilityKind = "subagent"
	CapabilityKindSkill    CapabilityKind = "skill"
	CapabilityKindMCP      CapabilityKind = "mcp"
)

// Capability is one safe, descriptive inventory entry. It grants no authority.
type Capability struct {
	Kind         CapabilityKind
	Name         string
	Source       string
	Availability CapabilityAvailability
	Detail       string
}

// CapabilityCategory distinguishes unsupported from supported-but-empty inventory.
type CapabilityCategory struct {
	Kind    CapabilityKind
	Support CapabilitySupport
	Source  string
	Items   []Capability
}

// Clone returns an independent category inventory.
func (c CapabilityCategory) Clone() CapabilityCategory {
	out := c
	out.Items = append([]Capability(nil), c.Items...)
	return out
}

// ContextUsageSource identifies who measured a context snapshot.
type ContextUsageSource string

const (
	ContextUsageSourceUnknown ContextUsageSource = "unknown"
	ContextUsageEstimated     ContextUsageSource = "estimated"
	ContextUsageProvider      ContextUsageSource = "provider"
)

// ContextUsage reports prompt-side usage for one model round. A percentage is
// deliberately absent: consumers derive it only when WindowTokens is known.
type ContextUsage struct {
	Round           uint64
	Source          ContextUsageSource
	MeasuredAt      time.Time
	UsedTokens      uint64
	WindowTokens    OptionalUint64
	RemainingTokens OptionalUint64
	OverBudget      bool
}

// ProviderUsage preserves optional provider token counts without turning
// missing values into measured zero.
type ProviderUsage struct {
	Round                     uint64
	PromptTokens              OptionalUint64
	CompletionTokens          OptionalUint64
	TotalTokens               OptionalUint64
	CachedPromptTokens        OptionalUint64
	ReasoningCompletionTokens OptionalUint64
	ContextWindowTokens       OptionalUint64
}

// OmissionKind is the schema-v1 irreversible-loss taxonomy.
type OmissionKind string

const (
	OmissionKindUnknown       OmissionKind = "unknown"
	OmissionOutputBudget      OmissionKind = "output_budget"
	OmissionPreviewBudget     OmissionKind = "preview_budget"
	OmissionProviderLength    OmissionKind = "provider_length"
	OmissionContentFilter     OmissionKind = "content_filter"
	OmissionRedacted          OmissionKind = "redacted"
	OmissionContextCompaction OmissionKind = "context_compaction"
)

// OmissionScope identifies the affected public content.
type OmissionScope string

const (
	OmissionScopeUnknown        OmissionScope = "unknown"
	OmissionScopeAssistantReply OmissionScope = "assistant_reply"
	OmissionScopeToolOutput     OmissionScope = "tool_output"
	OmissionScopeActionPreview  OmissionScope = "action_preview"
	OmissionScopeConversation   OmissionScope = "conversation"
	OmissionScopePublicPayload  OmissionScope = "public_payload"
)

// Recoverability states whether omitted content remains obtainable.
type Recoverability string

const (
	RecoverabilityUnknown       Recoverability = "unknown"
	RecoverabilityRecoverable   Recoverability = "recoverable"
	RecoverabilityUnrecoverable Recoverability = "unrecoverable"
)

// ContinuationMode describes the only truthful follow-up affordance.
type ContinuationMode string

const (
	ContinuationUnknown     ContinuationMode = "unknown"
	ContinuationUnavailable ContinuationMode = "unavailable"
	ContinuationNewUserTurn ContinuationMode = "new_user_turn"
)

// Omission describes irreversible loss known to the harness.
type Omission struct {
	Kind           OmissionKind
	Scope          OmissionScope
	CorrelationID  string
	CallID         CallID
	Revision       PreviewRevision
	Recoverability Recoverability
	Continuation   ContinuationMode
	OriginalBytes  OptionalUint64
	RetainedBytes  OptionalUint64
	OriginalLines  OptionalUint64
	RetainedLines  OptionalUint64
}

// ActionOperation classifies the candidate effect represented by a preview.
type ActionOperation string

const (
	ActionOperationUnknown ActionOperation = "unknown"
	ActionOperationCreate  ActionOperation = "create"
	ActionOperationModify  ActionOperation = "modify"
	ActionOperationDelete  ActionOperation = "delete"
	ActionOperationCommand ActionOperation = "command"
	ActionOperationCustom  ActionOperation = "custom"
)

// ActionPreviewKind describes the structured form retained in a preview.
type ActionPreviewKind string

const (
	ActionPreviewKindUnknown ActionPreviewKind = "unknown"
	ActionPreviewUnavailable ActionPreviewKind = "unavailable"
	ActionPreviewText        ActionPreviewKind = "text"
	ActionPreviewFileDiff    ActionPreviewKind = "file_diff"
	ActionPreviewMetadata    ActionPreviewKind = "metadata"
)

// DiffLineKind classifies one retained line in a structured file diff.
type DiffLineKind string

const (
	DiffLineContext DiffLineKind = "context"
	DiffLineAdded   DiffLineKind = "added"
	DiffLineRemoved DiffLineKind = "removed"
)

type DiffLine struct {
	Kind DiffLineKind
	Text string
}

type DiffHunk struct {
	OldStart uint64
	OldLines uint64
	NewStart uint64
	NewLines uint64
	Lines    []DiffLine
}

// FileDiffPreview contains retained hunks plus authoritative aggregate counts.
type FileDiffPreview struct {
	Path                     string
	BeforeBytes              OptionalUint64
	CandidateBytes           OptionalUint64
	AddedLines               OptionalUint64
	RemovedLines             OptionalUint64
	ChangedRegions           OptionalUint64
	BeforeHasTrailingNewline bool
	AfterHasTrailingNewline  bool
	NonText                  bool
	Hunks                    []DiffHunk
}

// ActionPreview is the bounded, frontend-neutral description of a candidate.
type ActionPreview struct {
	Kind              ActionPreviewKind
	Operation         ActionOperation
	Summary           string
	Targets           []string
	Text              string
	UnavailableReason string
	FileDiff          *FileDiffPreview
	Omission          *Omission
	Metadata          map[string]string
}

// Clone returns an independent preview, including all nested hunks and metadata.
func (p ActionPreview) Clone() ActionPreview {
	out := p
	out.Targets = append([]string(nil), p.Targets...)
	if p.FileDiff != nil {
		diff := *p.FileDiff
		diff.Hunks = make([]DiffHunk, len(p.FileDiff.Hunks))
		for i, h := range p.FileDiff.Hunks {
			diff.Hunks[i] = h
			diff.Hunks[i].Lines = append([]DiffLine(nil), h.Lines...)
		}
		out.FileDiff = &diff
	}
	if p.Omission != nil {
		omission := *p.Omission
		out.Omission = &omission
	}
	if p.Metadata != nil {
		out.Metadata = make(map[string]string, len(p.Metadata))
		for k, v := range p.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

// SandboxGrantOptions reports whether rich per-call grants are supported and
// which dimensions a request may accept.
type SandboxGrantOptions struct {
	Support         CapabilitySupport
	ReadRoots       bool
	WriteRoots      bool
	Network         bool
	SuggestedReads  []string
	SuggestedWrites []string
}

func (o SandboxGrantOptions) Clone() SandboxGrantOptions {
	out := o
	out.SuggestedReads = append([]string(nil), o.SuggestedReads...)
	out.SuggestedWrites = append([]string(nil), o.SuggestedWrites...)
	return out
}

// Clone returns independent grant root slices.
func (g SandboxGrants) Clone() SandboxGrants {
	out := g
	out.ExtraReadRoots = append([]string(nil), g.ExtraReadRoots...)
	out.ExtraWriteRoots = append([]string(nil), g.ExtraWriteRoots...)
	return out
}

// SubagentOutcome is the terminal lifecycle state of a child that actually started.
type SubagentOutcome string

const (
	SubagentOutcomeUnknown          SubagentOutcome = "unknown"
	SubagentOutcomeCompleted        SubagentOutcome = "completed"
	SubagentOutcomeFailed           SubagentOutcome = "failed"
	SubagentOutcomeCancelled        SubagentOutcome = "cancelled"
	SubagentOutcomeReachedStepLimit SubagentOutcome = "reached_step_limit"
)

// SubagentProvenance is stable for a delegated agent's full lifetime.
type SubagentProvenance struct {
	AgentID          AgentID
	ParentAgentID    AgentID
	Depth            int
	DelegationCallID CallID
	Label            string
}

// ActivityStatus is advisory observed run activity.
type ActivityStatus string

const (
	ActivityUnknown           ActivityStatus = "unknown"
	ActivityThinking          ActivityStatus = "thinking"
	ActivityPreparingTool     ActivityStatus = "preparing_tool"
	ActivityWaitingPermission ActivityStatus = "waiting_permission"
	ActivityCallingTool       ActivityStatus = "calling_tool"
	ActivityCancelling        ActivityStatus = "cancelling"
	ActivityIdle              ActivityStatus = "idle"
)

// ProviderTerminationReason preserves rich reply endings without changing the
// legacy ReplyEndReason vocabulary.
type ProviderTerminationReason string

const (
	ProviderTerminationUnknown          ProviderTerminationReason = "unknown"
	ProviderTerminationStop             ProviderTerminationReason = "stop"
	ProviderTerminationToolCalls        ProviderTerminationReason = "tool_calls"
	ProviderTerminationLength           ProviderTerminationReason = "length"
	ProviderTerminationContentFilter    ProviderTerminationReason = "content_filter"
	ProviderTerminationFailure          ProviderTerminationReason = "failure"
	ProviderTerminationProviderSpecific ProviderTerminationReason = "provider_specific"
)

// ToolOutcome is the observed terminal state of one correlated call.
type ToolOutcome string

const (
	ToolOutcomeUnknown     ToolOutcome = "unknown"
	ToolOutcomeSucceeded   ToolOutcome = "succeeded"
	ToolOutcomeFailed      ToolOutcome = "failed"
	ToolOutcomeDenied      ToolOutcome = "denied"
	ToolOutcomeCancelled   ToolOutcome = "cancelled"
	ToolOutcomeHookBlocked ToolOutcome = "hook_blocked"
)

// RunOutcome is the public terminal state of an observed run.
type RunOutcome string

const (
	RunOutcomeUnknown          RunOutcome = "unknown"
	RunOutcomeCompleted        RunOutcome = "completed"
	RunOutcomeReachedStepLimit RunOutcome = "reached_step_limit"
	RunOutcomeCancelled        RunOutcome = "cancelled"
	RunOutcomeFailed           RunOutcome = "failed"
)

// ObservedError is a secret-safe error classification, not a raw backend error.
type ObservedError struct {
	Code        string
	Message     string
	Recoverable bool
}

// PermissionProtocol distinguishes full rich requests from caller-owned legacy prompts.
type PermissionProtocol string

const (
	PermissionProtocolUnknown       PermissionProtocol = "unknown"
	PermissionProtocolRich          PermissionProtocol = "rich"
	PermissionProtocolLegacyOneShot PermissionProtocol = "legacy_one_shot"
)

// PermissionCapabilities advertises only operations the current request supports.
type PermissionCapabilities struct {
	Allow           bool
	Deny            bool
	Remember        bool
	ReviseArguments bool
	SchemaAwareEdit bool
	Preview         bool
	SandboxGrants   bool
}

// RememberedRuleScopeKind distinguishes a human-readable family rule from an
// exact-call fingerprint. Exact scopes never expose the persisted digest or the
// call's argument contents.
type RememberedRuleScopeKind string

const (
	RememberedRuleScopeFamily RememberedRuleScopeKind = "family"
	RememberedRuleScopeExact  RememberedRuleScopeKind = "exact"
)

// RememberedRuleScope is the safe scope shown before remembering. Match is kept
// for compatibility with family-rule frontends; Display is the preferred safe
// label and never contains an exact-call fingerprint.
type RememberedRuleScope struct {
	Kind      ActionKind
	Match     string
	ScopeKind RememberedRuleScopeKind
	ToolName  string
	Display   string
}

// ObservedPermissionRequest contains only the current effective revision. The
// live reply operation is added by the permission implementation, not inferred
// from the legacy PermissionRequest shape.
type ObservedPermissionRequest struct {
	RequestID       RequestID
	CallID          CallID
	Revision        PreviewRevision
	Protocol        PermissionProtocol
	EffectiveCall   ToolCall
	Explanation     string
	Action          ActionKind
	Preview         ActionPreview
	RememberedScope *RememberedRuleScope
	GrantOptions    SandboxGrantOptions
	Capabilities    PermissionCapabilities
	Mode            string
	reply           permissionReplier
}

func (r ObservedPermissionRequest) Clone() ObservedPermissionRequest {
	out := r
	out.EffectiveCall = cloneToolCall(r.EffectiveCall)
	out.Preview = r.Preview.Clone()
	if r.RememberedScope != nil {
		scope := *r.RememberedScope
		out.RememberedScope = &scope
	}
	out.GrantOptions = r.GrantOptions.Clone()
	return out
}

// PermissionReplyAction is one rich permission reply operation.
type PermissionReplyAction string

const (
	PermissionReplyAllow           PermissionReplyAction = "allow"
	PermissionReplyDeny            PermissionReplyAction = "deny"
	PermissionReplyReviseArguments PermissionReplyAction = "revise_arguments"
)

// PermissionReplyStatus states whether a reply consumed the open request.
type PermissionReplyStatus string

const (
	PermissionReplyAccepted           PermissionReplyStatus = "accepted"
	PermissionReplyValidationRejected PermissionReplyStatus = "validation_rejected"
	PermissionReplyAlreadyResolved    PermissionReplyStatus = "already_resolved"
)

// PermissionReplyFeedback is safe, field-oriented correction guidance.
type PermissionReplyFeedback struct {
	Field   string
	Code    string
	Message string
}

// ObservedPermissionDecision names the request and revision it answers. Revised
// arguments and grants are accepted only when the request capabilities permit
// them.
type ObservedPermissionDecision struct {
	RequestID        RequestID
	CallID           CallID
	Revision         PreviewRevision
	Action           PermissionReplyAction
	Remember         bool
	RevisedArguments map[string]interface{}
	SandboxGrants    SandboxGrants
}

// PermissionReplyResult is returned synchronously by the request's bound reply
// operation. ValidationRejected leaves the same request answerable.
type PermissionReplyResult struct {
	Status   PermissionReplyStatus
	Feedback []PermissionReplyFeedback
}

type permissionReplier interface {
	reply(context.Context, ObservedPermissionDecision) PermissionReplyResult
}

type permissionReplyOperation struct {
	mu        sync.Mutex
	resolved  bool
	runCtx    context.Context
	request   PermissionRequest
	requestID RequestID
	callID    CallID
	revision  PreviewRevision
	remember  bool
}

// RichPermissionRequestConfig contains the already-prepared effective revision
// and a side-effect-free validator for reply/schema/grant applicability.
type RichPermissionRequestConfig struct {
	RunContext      context.Context
	RequestID       RequestID
	CallID          CallID
	Revision        PreviewRevision
	EffectiveCall   ToolCall
	Explanation     string
	Action          ActionKind
	Preview         ActionPreview
	RememberedScope *RememberedRuleScope
	GrantOptions    SandboxGrantOptions
	Capabilities    PermissionCapabilities
	Mode            string
	Validate        func(ObservedPermissionDecision) []PermissionReplyFeedback
}

type richPermissionReplyOperation struct {
	mu        sync.Mutex
	resolved  bool
	runCtx    context.Context
	requestID RequestID
	callID    CallID
	revision  PreviewRevision
	config    RichPermissionRequestConfig
	accepted  chan ObservedPermissionDecision
}

// NewRichObservedPermissionRequest constructs one exactly-once rich request and
// returns the accepted-decision channel consumed by the permission engine.
// Validation-rejected replies leave the operation open and never reach the
// channel.
func NewRichObservedPermissionRequest(config RichPermissionRequestConfig) (ObservedPermissionRequest, <-chan ObservedPermissionDecision) {
	if config.RunContext == nil {
		config.RunContext = context.Background()
	}
	accepted := make(chan ObservedPermissionDecision, 1)
	operation := &richPermissionReplyOperation{
		runCtx: config.RunContext, requestID: config.RequestID, callID: config.CallID,
		revision: config.Revision, config: config, accepted: accepted,
	}
	request := ObservedPermissionRequest{
		RequestID: config.RequestID, CallID: config.CallID, Revision: config.Revision,
		Protocol: PermissionProtocolRich, EffectiveCall: cloneToolCall(config.EffectiveCall),
		Explanation: config.Explanation, Action: config.Action, Preview: config.Preview.Clone(),
		RememberedScope: config.RememberedScope, GrantOptions: config.GrantOptions.Clone(),
		Capabilities: config.Capabilities, Mode: config.Mode, reply: operation,
	}
	if config.RememberedScope != nil {
		scope := *config.RememberedScope
		request.RememberedScope = &scope
	}
	return request, accepted
}

func (operation *richPermissionReplyOperation) reply(ctx context.Context, decision ObservedPermissionDecision) PermissionReplyResult {
	operation.mu.Lock()
	defer operation.mu.Unlock()
	if operation.resolved {
		return PermissionReplyResult{Status: PermissionReplyAlreadyResolved}
	}
	if operation.runCtx.Err() != nil {
		operation.resolved = true
		return PermissionReplyResult{Status: PermissionReplyAlreadyResolved}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return rejectedPermissionReply("context", "cancelled", "permission reply was cancelled before validation")
	}
	feedback := validateRichPermissionDecision(operation.config, decision)
	if len(feedback) == 0 && operation.config.Validate != nil {
		feedback = operation.config.Validate(decision)
	}
	if len(feedback) > 0 {
		return PermissionReplyResult{Status: PermissionReplyValidationRejected, Feedback: append([]PermissionReplyFeedback(nil), feedback...)}
	}
	operation.accepted <- cloneObservedPermissionDecision(decision)
	operation.resolved = true
	return PermissionReplyResult{Status: PermissionReplyAccepted}
}

func validateRichPermissionDecision(config RichPermissionRequestConfig, decision ObservedPermissionDecision) []PermissionReplyFeedback {
	reject := func(field, code, message string) []PermissionReplyFeedback {
		return []PermissionReplyFeedback{{Field: field, Code: code, Message: message}}
	}
	switch {
	case decision.RequestID != config.RequestID:
		return reject("request_id", "mismatch", "reply does not target the open request")
	case decision.CallID != config.CallID:
		return reject("call_id", "mismatch", "reply does not target the open tool call")
	case decision.Revision != config.Revision:
		return reject("revision", "stale", "reply does not target the open preview revision")
	}
	switch decision.Action {
	case PermissionReplyAllow:
		if !config.Capabilities.Allow {
			return reject("action", "unsupported", "allow is unavailable for this request")
		}
	case PermissionReplyDeny:
		if !config.Capabilities.Deny {
			return reject("action", "unsupported", "deny is unavailable for this request")
		}
	case PermissionReplyReviseArguments:
		if !config.Capabilities.ReviseArguments {
			return reject("action", "unsupported", "argument revision is unavailable for this request")
		}
	default:
		return reject("action", "invalid", "reply action is not recognized")
	}
	if decision.Remember && (!config.Capabilities.Remember || config.RememberedScope == nil) {
		return reject("remember", "unsupported", "this request has no safe remembered-rule scope")
	}
	if decision.Action == PermissionReplyReviseArguments {
		if decision.RevisedArguments == nil {
			return reject("revised_arguments", "required", "argument revision requires a JSON object")
		}
		if decision.Remember {
			return reject("remember", "inapplicable", "a non-approving revision cannot be remembered")
		}
		if hasSandboxGrants(decision.SandboxGrants) {
			return reject("sandbox_grants", "inapplicable", "grants must be selected on the replacement permission request")
		}
	} else if decision.RevisedArguments != nil {
		return reject("revised_arguments", "inapplicable", "allow or deny cannot carry revised arguments")
	}
	if hasSandboxGrants(decision.SandboxGrants) && (!config.Capabilities.SandboxGrants || config.GrantOptions.Support != CapabilitySupportSupported) {
		return reject("sandbox_grants", "unsupported", "per-call sandbox grants are unavailable for this request")
	}
	return nil
}

func cloneObservedPermissionDecision(decision ObservedPermissionDecision) ObservedPermissionDecision {
	out := decision
	out.RevisedArguments = cloneArguments(decision.RevisedArguments)
	out.SandboxGrants = decision.SandboxGrants.Clone()
	return out
}

// Reply answers this exact request. A request without a live operation (for
// example a serialized fixture) rejects the reply without pretending it was
// delivered.
func (r ObservedPermissionRequest) Reply(ctx context.Context, decision ObservedPermissionDecision) PermissionReplyResult {
	if r.reply == nil {
		return rejectedPermissionReply("request", "reply_unavailable", "this permission request has no live reply operation")
	}
	return r.reply.reply(ctx, decision)
}

// NewLegacyObservedPermissionRequest adapts one existing caller-owned reply
// path into the deliberately limited legacy_one_shot observed protocol.
func NewLegacyObservedPermissionRequest(requestID RequestID, callID CallID, request PermissionRequest) ObservedPermissionRequest {
	return NewLegacyObservedPermissionRequestWithContext(context.Background(), requestID, callID, request)
}

// NewLegacyObservedPermissionRequestWithContext adapts a legacy request and
// ties its one-shot reply operation to the governing run lifetime. Once that
// run ends, a retained request cannot claim that a late reply was accepted.
func NewLegacyObservedPermissionRequestWithContext(runCtx context.Context, requestID RequestID, callID CallID, request PermissionRequest) ObservedPermissionRequest {
	available := request.ReplyPath != nil
	rememberedScope := parseLegacyRememberedScope(request.RememberedRule, request.ToolCall)
	capabilities := PermissionCapabilities{Allow: available, Deny: available}
	capabilities.Remember = available && rememberedScope != nil
	if runCtx == nil {
		runCtx = context.Background()
	}
	return ObservedPermissionRequest{
		RequestID:       requestID,
		CallID:          callID,
		Revision:        1,
		Protocol:        PermissionProtocolLegacyOneShot,
		EffectiveCall:   cloneToolCall(request.ToolCall),
		Explanation:     request.Reason,
		RememberedScope: rememberedScope,
		Preview: ActionPreview{
			Kind:              ActionPreviewUnavailable,
			UnavailableReason: "the caller-owned legacy permission protocol provides no authoritative prepared preview",
		},
		Capabilities: capabilities,
		reply: &permissionReplyOperation{
			runCtx:    runCtx,
			request:   request,
			requestID: requestID,
			callID:    callID,
			revision:  1,
			remember:  capabilities.Remember,
		},
	}
}

func (operation *permissionReplyOperation) reply(ctx context.Context, decision ObservedPermissionDecision) PermissionReplyResult {
	operation.mu.Lock()
	defer operation.mu.Unlock()

	if operation.resolved {
		return PermissionReplyResult{Status: PermissionReplyAlreadyResolved}
	}
	if operation.request.ReplyPath == nil {
		operation.resolved = true
		return rejectedPermissionReply("request", "reply_unavailable", "this legacy permission request has no reply path")
	}
	if operation.runCtx.Err() != nil {
		operation.resolved = true
		return PermissionReplyResult{Status: PermissionReplyAlreadyResolved}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return rejectedPermissionReply("context", "cancelled", "permission reply was cancelled before delivery")
	}
	if decision.RequestID != operation.requestID {
		return rejectedPermissionReply("request_id", "mismatch", "reply does not target the open request")
	}
	if decision.CallID != operation.callID {
		return rejectedPermissionReply("call_id", "mismatch", "reply does not target the open tool call")
	}
	if decision.Revision != operation.revision {
		return rejectedPermissionReply("revision", "stale", "reply does not target the open preview revision")
	}
	if decision.Action != PermissionReplyAllow && decision.Action != PermissionReplyDeny {
		return rejectedPermissionReply("action", "unsupported", "legacy_one_shot supports only allow or deny")
	}
	if decision.Remember && !operation.remember {
		return rejectedPermissionReply("remember", "unsupported", "this request has no safe remembered-rule scope")
	}
	if decision.RevisedArguments != nil {
		return rejectedPermissionReply("revised_arguments", "unsupported", "legacy_one_shot does not support argument revision")
	}
	if hasSandboxGrants(decision.SandboxGrants) {
		return rejectedPermissionReply("sandbox_grants", "unsupported", "legacy_one_shot does not support per-call sandbox grants")
	}

	legacy := PermissionDecision{
		Allow:    decision.Action == PermissionReplyAllow,
		Remember: decision.Remember,
	}
	select {
	case operation.request.ReplyPath <- legacy:
		operation.resolved = true
		return PermissionReplyResult{Status: PermissionReplyAccepted}
	case <-ctx.Done():
		return rejectedPermissionReply("context", "cancelled", "permission reply was cancelled before delivery")
	case <-operation.runCtx.Done():
		operation.resolved = true
		return PermissionReplyResult{Status: PermissionReplyAlreadyResolved}
	}
}

func parseLegacyRememberedScope(rule string, call ToolCall) *RememberedRuleScope {
	if strings.HasPrefix(rule, "exact-v2:") {
		parts := strings.Split(rule, ":")
		if len(parts) != 4 || parts[0] != "exact-v2" || parts[2] != "hmac-sha256" || len(parts[3]) != 64 {
			return nil
		}
		for _, char := range parts[3] {
			if !strings.ContainsRune("0123456789abcdef", char) {
				return nil
			}
		}
		var action ActionKind
		switch parts[1] {
		case "read":
			action = ActionRead
		case "edit":
			action = ActionEdit
		case "command":
			action = ActionCommand
		case "unknown":
			action = ActionUnknown
		default:
			return nil
		}
		display := "exact " + call.ToolName + " call"
		return &RememberedRuleScope{
			Kind: action, Match: display, ScopeKind: RememberedRuleScopeExact,
			ToolName: call.ToolName, Display: display,
		}
	}
	kind, match, ok := strings.Cut(rule, ":")
	match = strings.TrimSpace(match)
	if !ok || match == "" {
		return nil
	}
	var action ActionKind
	switch strings.TrimSpace(kind) {
	case "read":
		action = ActionRead
	case "edit":
		action = ActionEdit
	case "command":
		if strings.ContainsAny(match, "&|;<>()$`\n") {
			return nil
		}
		action = ActionCommand
	default:
		return nil
	}
	return &RememberedRuleScope{Kind: action, Match: match, ScopeKind: RememberedRuleScopeFamily, Display: match}
}

func rejectedPermissionReply(field, code, message string) PermissionReplyResult {
	return PermissionReplyResult{
		Status: PermissionReplyValidationRejected,
		Feedback: []PermissionReplyFeedback{{
			Field: field, Code: code, Message: message,
		}},
	}
}

func hasSandboxGrants(grants SandboxGrants) bool {
	return len(grants.ExtraReadRoots) > 0 || len(grants.ExtraWriteRoots) > 0 || grants.Network
}

// Schema-v1 payloads. Correlation fields remain in payloads so consumers can
// update stable transcript items without parsing names or prose.
type RunStartedPayload struct{}
type StatusChangedPayload struct {
	Status ActivityStatus
	Detail string
}
type AssistantStartedPayload struct{ Round uint64 }
type AssistantTextDeltaPayload struct {
	Round uint64
	Delta string
}
type AssistantReasoningSummaryDeltaPayload struct {
	Round uint64
	Delta string
}
type AssistantFinishedPayload struct {
	Round              uint64
	Reason             ProviderTerminationReason
	ProviderReasonCode string
}
type ToolProposedPayload struct {
	Round  uint64
	CallID CallID
	Call   ToolCall
}
type ToolPreparedPayload struct {
	CallID        CallID
	Revision      PreviewRevision
	EffectiveCall ToolCall
	Preview       ActionPreview
}
type PermissionRequestedPayload struct{ Request ObservedPermissionRequest }
type ToolExecutingPayload struct {
	CallID   CallID
	Revision PreviewRevision
}
type ToolFinishedPayload struct {
	CallID   CallID
	Revision PreviewRevision
	Outcome  ToolOutcome
	Result   ToolResult
	Duration time.Duration
}
type ContextUsageUpdatedPayload struct{ Usage ContextUsage }
type OmissionReportedPayload struct{ Omission Omission }
type HookOutcomePayload struct {
	CallID  CallID
	Outcome HookOutcome
}
type SubagentStartedPayload struct {
	Agent     SubagentProvenance
	StartedAt time.Time
}
type SubagentFinishedPayload struct {
	Agent      SubagentProvenance
	Outcome    SubagentOutcome
	FinishedAt time.Time
	Error      *ObservedError
}
type WarningPayload struct {
	Code    string
	Message string
	CallID  CallID
}
type ErrorPayload struct {
	Error  ObservedError
	CallID CallID
}
type RunFinishedPayload struct {
	Outcome RunOutcome
	Error   *ObservedError
}

func (*RunStartedPayload) observedEventPayload()                     {}
func (*StatusChangedPayload) observedEventPayload()                  {}
func (*AssistantStartedPayload) observedEventPayload()               {}
func (*AssistantTextDeltaPayload) observedEventPayload()             {}
func (*AssistantReasoningSummaryDeltaPayload) observedEventPayload() {}
func (*AssistantFinishedPayload) observedEventPayload()              {}
func (*ToolProposedPayload) observedEventPayload()                   {}
func (*ToolPreparedPayload) observedEventPayload()                   {}
func (*PermissionRequestedPayload) observedEventPayload()            {}
func (*ToolExecutingPayload) observedEventPayload()                  {}
func (*ToolFinishedPayload) observedEventPayload()                   {}
func (*ContextUsageUpdatedPayload) observedEventPayload()            {}
func (*OmissionReportedPayload) observedEventPayload()               {}
func (*HookOutcomePayload) observedEventPayload()                    {}
func (*SubagentStartedPayload) observedEventPayload()                {}
func (*SubagentFinishedPayload) observedEventPayload()               {}
func (*WarningPayload) observedEventPayload()                        {}
func (*ErrorPayload) observedEventPayload()                          {}
func (*RunFinishedPayload) observedEventPayload()                    {}

func observedPayloadIdentity(payload ObservedEventPayload) (ObservedEventKind, string, bool) {
	switch p := payload.(type) {
	case *RunStartedPayload:
		return ObservedKindRunStarted, "*RunStartedPayload", p == nil
	case *StatusChangedPayload:
		return ObservedKindStatusChanged, "*StatusChangedPayload", p == nil
	case *AssistantStartedPayload:
		return ObservedKindAssistantStarted, "*AssistantStartedPayload", p == nil
	case *AssistantTextDeltaPayload:
		return ObservedKindAssistantTextDelta, "*AssistantTextDeltaPayload", p == nil
	case *AssistantReasoningSummaryDeltaPayload:
		return ObservedKindAssistantReasoningSummaryDelta, "*AssistantReasoningSummaryDeltaPayload", p == nil
	case *AssistantFinishedPayload:
		return ObservedKindAssistantFinished, "*AssistantFinishedPayload", p == nil
	case *ToolProposedPayload:
		return ObservedKindToolProposed, "*ToolProposedPayload", p == nil
	case *ToolPreparedPayload:
		return ObservedKindToolPrepared, "*ToolPreparedPayload", p == nil
	case *PermissionRequestedPayload:
		return ObservedKindPermissionRequested, "*PermissionRequestedPayload", p == nil
	case *ToolExecutingPayload:
		return ObservedKindToolExecuting, "*ToolExecutingPayload", p == nil
	case *ToolFinishedPayload:
		return ObservedKindToolFinished, "*ToolFinishedPayload", p == nil
	case *ContextUsageUpdatedPayload:
		return ObservedKindContextUsageUpdated, "*ContextUsageUpdatedPayload", p == nil
	case *OmissionReportedPayload:
		return ObservedKindOmissionReported, "*OmissionReportedPayload", p == nil
	case *HookOutcomePayload:
		return ObservedKindHookOutcome, "*HookOutcomePayload", p == nil
	case *SubagentStartedPayload:
		return ObservedKindSubagentStarted, "*SubagentStartedPayload", p == nil
	case *SubagentFinishedPayload:
		return ObservedKindSubagentFinished, "*SubagentFinishedPayload", p == nil
	case *WarningPayload:
		return ObservedKindWarning, "*WarningPayload", p == nil
	case *ErrorPayload:
		return ObservedKindError, "*ErrorPayload", p == nil
	case *RunFinishedPayload:
		return ObservedKindRunFinished, "*RunFinishedPayload", p == nil
	case nil:
		return "", "nil", true
	default:
		return "", fmt.Sprintf("%T", payload), false
	}
}

func observedPayloadTypeName(kind ObservedEventKind) string {
	names := map[ObservedEventKind]string{
		ObservedKindRunStarted:                     "*RunStartedPayload",
		ObservedKindStatusChanged:                  "*StatusChangedPayload",
		ObservedKindAssistantStarted:               "*AssistantStartedPayload",
		ObservedKindAssistantTextDelta:             "*AssistantTextDeltaPayload",
		ObservedKindAssistantReasoningSummaryDelta: "*AssistantReasoningSummaryDeltaPayload",
		ObservedKindAssistantFinished:              "*AssistantFinishedPayload",
		ObservedKindToolProposed:                   "*ToolProposedPayload",
		ObservedKindToolPrepared:                   "*ToolPreparedPayload",
		ObservedKindPermissionRequested:            "*PermissionRequestedPayload",
		ObservedKindToolExecuting:                  "*ToolExecutingPayload",
		ObservedKindToolFinished:                   "*ToolFinishedPayload",
		ObservedKindContextUsageUpdated:            "*ContextUsageUpdatedPayload",
		ObservedKindOmissionReported:               "*OmissionReportedPayload",
		ObservedKindHookOutcome:                    "*HookOutcomePayload",
		ObservedKindSubagentStarted:                "*SubagentStartedPayload",
		ObservedKindSubagentFinished:               "*SubagentFinishedPayload",
		ObservedKindWarning:                        "*WarningPayload",
		ObservedKindError:                          "*ErrorPayload",
		ObservedKindRunFinished:                    "*RunFinishedPayload",
	}
	return names[kind]
}

func validateObservedPayload(payload ObservedEventPayload) error {
	requireCall := func(callID CallID) error {
		if callID == "" {
			return &InvalidObservedEventError{Field: "payload.call ID", Reason: "must be non-empty"}
		}
		return nil
	}
	switch p := payload.(type) {
	case *ToolProposedPayload:
		return requireCall(p.CallID)
	case *ToolPreparedPayload:
		if err := requireCall(p.CallID); err != nil {
			return err
		}
		if p.Revision == 0 {
			return &InvalidObservedEventError{Field: "payload.preview revision", Reason: "must start at one"}
		}
	case *PermissionRequestedPayload:
		if p.Request.RequestID == "" {
			return &InvalidObservedEventError{Field: "payload.request ID", Reason: "must be non-empty"}
		}
		if err := requireCall(p.Request.CallID); err != nil {
			return err
		}
		if p.Request.Revision == 0 {
			return &InvalidObservedEventError{Field: "payload.preview revision", Reason: "must start at one"}
		}
	case *ToolExecutingPayload:
		return requireCall(p.CallID)
	case *ToolFinishedPayload:
		if p.Duration < 0 {
			return &InvalidObservedEventError{Field: "payload.duration", Reason: "must be non-negative"}
		}
		return requireCall(p.CallID)
	case *ContextUsageUpdatedPayload:
		if p.Usage.Round == 0 {
			return &InvalidObservedEventError{Field: "payload.context round", Reason: "must start at one"}
		}
		if p.Usage.MeasuredAt.IsZero() {
			return &InvalidObservedEventError{Field: "payload.context measured time", Reason: "must be non-zero"}
		}
	}
	return nil
}

func cloneObservedPayload(payload ObservedEventPayload) ObservedEventPayload {
	switch p := payload.(type) {
	case *RunStartedPayload:
		return clonePointer(p)
	case *StatusChangedPayload:
		return clonePointer(p)
	case *AssistantStartedPayload:
		return clonePointer(p)
	case *AssistantTextDeltaPayload:
		return clonePointer(p)
	case *AssistantReasoningSummaryDeltaPayload:
		return clonePointer(p)
	case *AssistantFinishedPayload:
		return clonePointer(p)
	case *ToolProposedPayload:
		if p == nil {
			return (*ToolProposedPayload)(nil)
		}
		out := *p
		out.Call = cloneToolCall(p.Call)
		return &out
	case *ToolPreparedPayload:
		if p == nil {
			return (*ToolPreparedPayload)(nil)
		}
		out := *p
		out.EffectiveCall = cloneToolCall(p.EffectiveCall)
		out.Preview = p.Preview.Clone()
		return &out
	case *PermissionRequestedPayload:
		if p == nil {
			return (*PermissionRequestedPayload)(nil)
		}
		out := *p
		out.Request = p.Request.Clone()
		return &out
	case *ToolExecutingPayload:
		return clonePointer(p)
	case *ToolFinishedPayload:
		return clonePointer(p)
	case *ContextUsageUpdatedPayload:
		return clonePointer(p)
	case *OmissionReportedPayload:
		return clonePointer(p)
	case *HookOutcomePayload:
		return clonePointer(p)
	case *SubagentStartedPayload:
		return clonePointer(p)
	case *SubagentFinishedPayload:
		if p == nil {
			return (*SubagentFinishedPayload)(nil)
		}
		out := *p
		if p.Error != nil {
			value := *p.Error
			out.Error = &value
		}
		return &out
	case *WarningPayload:
		return clonePointer(p)
	case *ErrorPayload:
		return clonePointer(p)
	case *RunFinishedPayload:
		if p == nil {
			return (*RunFinishedPayload)(nil)
		}
		out := *p
		if p.Error != nil {
			value := *p.Error
			out.Error = &value
		}
		return &out
	default:
		return payload
	}
}

func clonePointer[T any](in *T) *T {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneToolCall(call ToolCall) ToolCall {
	out := call
	out.Arguments = cloneArguments(call.Arguments)
	return out
}

func cloneArguments(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = cloneDynamicValue(v)
	}
	return out
}

// cloneDynamicValue recursively copies JSON-shaped maps and slices while
// preserving their concrete types. Scalars, functions, and channels are
// immutable or intentionally identity-bearing and are returned as-is.
func cloneDynamicValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	return cloneReflectValue(reflect.ValueOf(value)).Interface()
}

func cloneReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneReflectValue(value.Elem())
		out := reflect.New(value.Type()).Elem()
		out.Set(cloned)
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneReflectValue(iter.Value()))
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return out
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloneReflectValue(value.Elem()))
		return out
	default:
		return value
	}
}
