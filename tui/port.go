package tui

import (
	"context"
	"time"
)

// SessionPort is the deliberately narrow boundary between the Bubble Tea
// frontend and the harness. The production adapter translates these local DTOs
// to pkg/agent; reducer tests can provide a deterministic fake.
type SessionPort interface {
	Describe(context.Context) (SessionInfo, error)
	Run(context.Context, string) (<-chan UIEvent, error)
	// SetMode may be called while a run is active. Once it returns successfully,
	// later permission decisions observe the new mode; an already-open prompt still
	// requires an explicit reply.
	SetMode(context.Context, SessionMode) error
	Close(context.Context) error
}

// SessionMode is the frontend's display and selection vocabulary. External and
// unsupported are ownership states, not selectable permission modes.
type SessionMode string

const (
	ModeDefault         SessionMode = "default"
	ModeAutoAcceptEdits SessionMode = "auto-accept-edits"
	ModePlan            SessionMode = "plan"
	ModeBypass          SessionMode = "bypass"
	ModeExternal        SessionMode = "external"
	ModeUnsupported     SessionMode = "unsupported"
)

// SessionInfo contains only secret-free facts needed by the fixed shell.
type SessionInfo struct {
	Project                 string
	Branch                  string
	Model                   string
	Provider                string
	Mode                    SessionMode
	ModeChangeable          bool
	PermissionOwner         string
	Sandbox                 string
	SandboxReason           string
	Context                 string
	ContextWindow           OptionalCount
	ReasoningSummarySupport SupportState
	UsageSupport            SupportState
	Capabilities            []CapabilityCategory
}

// SupportState keeps unknown, unsupported, and supported distinct in the
// frontend. In particular, unsupported is not rendered as a fabricated empty
// inventory.
type SupportState string

const (
	SupportUnknown     SupportState = "unknown"
	SupportUnsupported SupportState = "unsupported"
	SupportSupported   SupportState = "supported"
)

type Availability string

const (
	AvailabilityUnknown     Availability = "unknown"
	AvailabilityUnavailable Availability = "unavailable"
	AvailabilityAvailable   Availability = "available"
)

// CapabilityCategory and CapabilityItem are secret-free descriptor facts for
// the on-demand inspector.
type CapabilityCategory struct {
	Kind    string
	Support SupportState
	Source  string
	Items   []CapabilityItem
}

type CapabilityItem struct {
	Name         string
	Source       string
	Availability Availability
	Detail       string
}

type OptionalCount struct {
	Known bool
	Value uint64
}

// UIEventKind is a closed frontend-local projection of the observed stream.
// Unknown values are protocol errors rather than silently ignored events.
type UIEventKind string

const (
	EventRunStarted                     UIEventKind = "run_started"
	EventStatusChanged                  UIEventKind = "status_changed"
	EventAssistantStarted               UIEventKind = "assistant_started"
	EventAssistantTextDelta             UIEventKind = "assistant_text_delta"
	EventAssistantReasoningSummaryDelta UIEventKind = "assistant_reasoning_summary_delta"
	EventAssistantFinished              UIEventKind = "assistant_finished"
	EventToolStarted                    UIEventKind = "tool_started"
	EventToolPrepared                   UIEventKind = "tool_prepared"
	EventPermissionRequested            UIEventKind = "permission_requested"
	EventToolExecuting                  UIEventKind = "tool_executing"
	EventToolFinished                   UIEventKind = "tool_finished"
	EventContextUsage                   UIEventKind = "context_usage"
	EventOmission                       UIEventKind = "omission"
	EventHookOutcome                    UIEventKind = "hook_outcome"
	EventSubagentStarted                UIEventKind = "subagent_started"
	EventSubagentFinished               UIEventKind = "subagent_finished"
	EventWarning                        UIEventKind = "warning"
	EventError                          UIEventKind = "error"
	EventNotice                         UIEventKind = "notice"
	EventRunFinished                    UIEventKind = "run_finished"
)

// RunActivity is advisory chrome state. RunFinished remains the only
// authoritative run terminal signal.
type RunActivity string

const (
	ActivityIdle        RunActivity = "idle"
	ActivityThinking    RunActivity = "thinking"
	ActivityCallingTool RunActivity = "calling tool"
	ActivityPermission  RunActivity = "waiting for approval"
	ActivityCancelling  RunActivity = "cancelling"
)

// ToolOutcome is the terminal result reported for one correlated tool call.
type ToolOutcome string

const (
	ToolSucceeded   ToolOutcome = "succeeded"
	ToolFailed      ToolOutcome = "failed"
	ToolDenied      ToolOutcome = "denied"
	ToolCancelled   ToolOutcome = "cancelled"
	ToolHookBlocked ToolOutcome = "hook-blocked"
)

// RunOutcome is the authoritative terminal result for one root run.
type RunOutcome string

const (
	RunCompleted        RunOutcome = "completed"
	RunFailed           RunOutcome = "failed"
	RunCancelled        RunOutcome = "cancelled"
	RunReachedStepLimit RunOutcome = "reached-step-limit"
)

// UIEvent is intentionally flat: Kind selects the fields that are meaningful.
// Text is a stream chunk for assistant deltas and safe display content for
// notices/results after the adapter has projected the public payload.
type UIEvent struct {
	Kind        UIEventKind
	RunID       string
	AssistantID string
	CallID      string
	Timestamp   time.Time

	Activity    RunActivity
	Text        string
	ToolName    string
	Arguments   string
	Result      string
	Tool        ToolOutcome
	Terminal    RunOutcome
	Err         string
	Recoverable bool
	Revision    uint64
	Duration    time.Duration
	Termination string

	Permission *PermissionPrompt
	Preview    *ActionPreview
	Usage      *ContextUsage
	Omission   *Omission
	Hook       *HookOutcome
	Subagent   *SubagentLifecycle
}

type ContextUsage struct {
	Round      uint64
	Source     string
	MeasuredAt time.Time
	Used       uint64
	Window     OptionalCount
	Remaining  OptionalCount
	OverBudget bool
}

type Omission struct {
	Kind           string
	Scope          string
	CorrelationID  string
	CallID         string
	Revision       uint64
	Recoverability string
	Continuation   string
	OriginalBytes  OptionalCount
	RetainedBytes  OptionalCount
	OriginalLines  OptionalCount
	RetainedLines  OptionalCount
}

type DiffLine struct {
	Kind string
	Text string
}

type DiffHunk struct {
	OldStart uint64
	OldLines uint64
	NewStart uint64
	NewLines uint64
	Lines    []DiffLine
}

type FileDiff struct {
	Path           string
	BeforeBytes    OptionalCount
	CandidateBytes OptionalCount
	AddedLines     OptionalCount
	RemovedLines   OptionalCount
	ChangedRegions OptionalCount
	NonText        bool
	Hunks          []DiffHunk
}

type ActionPreview struct {
	Kind              string
	Operation         string
	Summary           string
	Targets           []string
	Text              string
	UnavailableReason string
	FileDiff          *FileDiff
	Omission          *Omission
	Metadata          map[string]string
}

type HookOutcome struct {
	CallID string
	Name   string
	Moment string
	Action string
	Reason string
}

type SubagentLifecycle struct {
	AgentID          string
	ParentAgentID    string
	DelegationCallID string
	Label            string
	Depth            int
	Outcome          string
	Error            string
}

// PermissionDecision is deliberately limited in the first runnable slice.
// Remembered decisions, argument revision, and sandbox grants are not exposed
// until their complete interaction and validation paths are implemented.
type PermissionDecision string

const (
	DecisionAllowOnce       PermissionDecision = "allow-once"
	DecisionDenyOnce        PermissionDecision = "deny-once"
	DecisionAllowRemember   PermissionDecision = "allow-remember"
	DecisionDenyRemember    PermissionDecision = "deny-remember"
	DecisionReviseArguments PermissionDecision = "revise-arguments"
)

// PermissionReplyStatus controls the modal submission guard.
type PermissionReplyStatus string

const (
	ReplyAccepted           PermissionReplyStatus = "accepted"
	ReplyValidationRejected PermissionReplyStatus = "validation-rejected"
	ReplyAlreadyResolved    PermissionReplyStatus = "already-resolved"
)

type PermissionReplyResult struct {
	Status   PermissionReplyStatus
	Feedback string
}

// PermissionReply is the originating public reply path captured by the
// production adapter. Keeping it on the prompt prevents a second permission
// dispatch channel from appearing in the frontend.
type PermissionReply func(context.Context, PermissionDecision) (PermissionReplyResult, error)

// PermissionResponse is the complete rich reply. Editing arguments or grants
// alone never approves execution; a revision is submitted as its own action.
type PermissionResponse struct {
	Decision         PermissionDecision
	Remember         bool
	RevisedArguments map[string]interface{}
	Grants           SandboxGrants
}

type SandboxGrants struct {
	ReadRoots  []string
	WriteRoots []string
	Network    bool
}

type PermissionRichReply func(context.Context, PermissionResponse) (PermissionReplyResult, error)

type PermissionCapabilities struct {
	Allow           bool
	Deny            bool
	Remember        bool
	ReviseArguments bool
	SchemaAwareEdit bool
	Preview         bool
	SandboxGrants   bool
}

type GrantOptions struct {
	Support         SupportState
	ReadRoots       bool
	WriteRoots      bool
	Network         bool
	SuggestedReads  []string
	SuggestedWrites []string
}

// PermissionPrompt contains the reviewable, sanitized-by-renderer facts needed
// for one focused decision. Reply must resolve the originating request.
type PermissionPrompt struct {
	RequestID         string
	CallID            string
	Revision          uint64
	Tool              string
	Action            string
	Reason            string
	Origin            string
	Preview           string
	Protocol          string
	Arguments         string
	RememberScope     string
	Capabilities      PermissionCapabilities
	GrantOptions      GrantOptions
	StructuredPreview *ActionPreview
	Reply             PermissionReply
	RichReply         PermissionRichReply
}
