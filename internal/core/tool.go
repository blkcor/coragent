package core

import (
	"context"
	"time"
)

// ToolHandler is an executable capability. It bundles the model-facing descriptor
// the catalog advertises, the execution behavior the chain invokes, and a marker
// declaring whether the tool runs shell commands — which decides whether the
// executor routes it through the sandbox stage.
//
// Built-in and custom tools implement this identically; the executor treats them
// the same on the one path.
type ToolHandler interface {
	// Descriptor returns the model-facing Tool (name, description, parameter
	// schema) advertised to the model and registered in the catalog.
	Descriptor() Tool

	// Execute performs the tool's work on validated arguments and returns its
	// textual output. A returned error makes the call an error result (the loop
	// keeps running); the captured output, if any, is preserved in that result.
	Execute(ctx context.Context, args map[string]interface{}) (string, error)

	// RunsCommands reports whether this tool executes shell commands and so must
	// pass through the sandbox stage. Pure file operations return false.
	RunsCommands() bool
}

// PreparedAction is a side-effect-free candidate derived from validated
// effective arguments. CommitToken is opaque to the executor and frontend; only
// the handler that prepared it may interpret and commit it.
type PreparedAction struct {
	EffectiveArguments map[string]interface{}
	Operation          ActionOperation
	Preview            ActionPreview
	CommitToken        interface{}
}

// Clone returns a frontend-safe independent copy. The opaque commit token is
// intentionally retained only for the in-process executor path.
func (a PreparedAction) Clone() PreparedAction {
	out := a
	out.EffectiveArguments = clonePreparedArguments(a.EffectiveArguments)
	out.Preview = a.Preview.Clone()
	return out
}

// PreparedActionHandler is an optional additive tool contract. Prepare must be
// cancellable and side-effect-free. ExecutePrepared must commit only the exact
// identity-bound candidate represented by the supplied token and fail closed if
// its preconditions are stale or unsupported.
type PreparedActionHandler interface {
	ToolHandler
	Prepare(ctx context.Context, args map[string]interface{}) (PreparedAction, error)
	ExecutePrepared(ctx context.Context, prepared PreparedAction) (string, error)
}

func clonePreparedArguments(arguments map[string]interface{}) map[string]interface{} {
	if arguments == nil {
		return nil
	}
	out := make(map[string]interface{}, len(arguments))
	for key, value := range arguments {
		switch typed := value.(type) {
		case map[string]interface{}:
			out[key] = clonePreparedArguments(typed)
		case []interface{}:
			items := make([]interface{}, len(typed))
			for index, item := range typed {
				if nested, ok := item.(map[string]interface{}); ok {
					items[index] = clonePreparedArguments(nested)
				} else {
					items[index] = item
				}
			}
			out[key] = items
		case []string:
			out[key] = append([]string(nil), typed...)
		default:
			out[key] = value
		}
	}
	return out
}

// CommandSpec describes one shell process a command-running tool wants to
// launch. The sandbox owns process creation; the handler retains argument
// validation and result post-processing around the runner call.
type CommandSpec struct {
	Command string
	Timeout time.Duration
}

// CommandRunner is the only process-launch path available to a sandbox-aware
// command tool. Its implementation applies the active confinement policy.
type CommandRunner interface {
	Run(ctx context.Context, spec CommandSpec) (string, error)
}

// CommandToolHandler is the companion contract for handlers whose RunsCommands
// method returns true. ExecuteCommand keeps handler semantics intact while
// requiring the actual process to be launched by the supplied runner.
// Command-running handlers that do not implement this interface fail closed.
type CommandToolHandler interface {
	ToolHandler
	ExecuteCommand(ctx context.Context, args map[string]interface{}, runner CommandRunner) (string, error)
}

// ActionKind classifies what a tool call does to the machine's state, so the
// soft permission gate can apply mode-aware decisions: plan mode blocks every
// mutating kind and lets reads through; auto-accept-edits allows only edits.
type ActionKind int

const (
	// ActionUnknown means the action's effect on state could not be determined.
	// Plan mode treats it as state-changing and blocks it, erring on the safe side.
	ActionUnknown ActionKind = iota

	// ActionRead is a read-only action with no state change.
	ActionRead

	// ActionEdit changes files on disk (write, edit).
	ActionEdit

	// ActionCommand runs a shell command and may change arbitrary state.
	ActionCommand
)

// ActionClassifier is the optional interface a ToolHandler implements to declare
// its ActionKind. It is queried by type assertion, so existing handlers that do
// not implement it keep working — the executor falls back to RunsCommands and
// otherwise ActionUnknown.
type ActionClassifier interface {
	ActionKind() ActionKind
}

// StageDecision is the verdict of a hard gate (PreToolCheck / PostToolCheck).
// A hard block is unconditional: the model has no way to override it.
type StageDecision struct {
	// Block, when true, aborts the call (pre-check) or vetoes the result
	// (post-check). The decision carries Reason for the resulting error result.
	Block bool

	// Reason explains a block; empty when Block is false.
	Reason string

	// EditedArguments, when non-nil, replace a before-tool call's arguments.
	EditedArguments map[string]interface{}

	// ReplacementResult, when non-nil, replaces the after-tool result.
	ReplacementResult *ToolResult

	// Outcome is emitted on the run stream when the hard gate took an observable
	// hook action.
	Outcome *HookOutcome
}

// PermissionResult is the verdict of the human-permission stage. It may allow,
// deny, or hand back corrected arguments to run instead of the originals.
type PermissionResult struct {
	// Allow reports whether the call may proceed.
	Allow bool

	// Reason explains a denial; empty when Allow is true.
	Reason string

	// EditedArguments, when non-nil, replace the call's arguments. The executor
	// re-validates them against the tool's declared shape before running.
	EditedArguments map[string]interface{}

	// SandboxGrants are additive grants for this approved call's sandbox policy.
	SandboxGrants SandboxGrants
}

// SandboxGrants are per-call additive policy grants for command sandboxing.
type SandboxGrants struct {
	ExtraReadRoots  []string
	ExtraWriteRoots []string
	Network         bool
}

// PreToolCheck is the hard pre-execution gate (Phase 4 arms it). Phase 2 ships an
// inert never-block placeholder. A block stops permission, sandbox, and the tool.
type PreToolCheck interface {
	PreCheck(ctx context.Context, call ToolCall) StageDecision
}

// Permission is the soft human-in-the-loop gate (Phase 3 arms it). Phase 2 ships
// an inert allow-everything placeholder. It is handed the call's ActionKind so it
// can apply mode-aware decisions, plus the same live emit stream the frontend
// drains, so a real prompt can reach the human and block on a reply.
type Permission interface {
	Decide(ctx context.Context, call ToolCall, kind ActionKind, emit func(RunEvent) error) PermissionResult
}

// RichPermissionInput binds one request to the currently prepared effective
// call and preview. ValidateReply performs schema and grant validation without
// side effects; validation feedback leaves the request open.
type RichPermissionInput struct {
	RunContext      context.Context
	RequestID       RequestID
	CallID          CallID
	Revision        PreviewRevision
	Origin          Origin
	EffectiveCall   ToolCall
	Explanation     string
	Action          ActionKind
	Preview         ActionPreview
	RememberedScope *RememberedRuleScope
	GrantOptions    SandboxGrantOptions
	Mode            string
	ValidateReply   func(ObservedPermissionDecision) []PermissionReplyFeedback
}

type RichPermissionResult struct {
	Action               PermissionReplyAction
	Reason               string
	Remember             bool
	RevisedArguments     map[string]interface{}
	SandboxGrants        SandboxGrants
	LegacyEditedApproval bool
}

// RichPermission is the optional full permission protocol used by observed
// runs. Permission remains the required legacy compatibility seam.
type RichPermission interface {
	DecideRich(ctx context.Context, input RichPermissionInput, emit func(RichEvent) error) RichPermissionResult
}

// Sandbox is the OS-confinement stage for command execution (Phase 5 arms it).
// Phase 2 ships an inert run-directly placeholder. Only command-running tools are
// routed here; pure file operations skip the stage entirely.
type Sandbox interface {
	Run(ctx context.Context, handler ToolHandler, args map[string]interface{}) (string, error)
}

// PostToolCheck is the hard post-execution gate (Phase 4 arms it). Phase 2 ships
// an inert never-block placeholder. A block turns an otherwise-successful result
// into an error carrying the block's reason.
type PostToolCheck interface {
	PostCheck(ctx context.Context, call ToolCall, result ToolResult) StageDecision
}
