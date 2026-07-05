package core

import "context"

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
