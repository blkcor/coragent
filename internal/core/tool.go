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

// StageDecision is the verdict of a hard gate (PreToolCheck / PostToolCheck).
// A hard block is unconditional: the model has no way to override it.
type StageDecision struct {
	// Block, when true, aborts the call (pre-check) or vetoes the result
	// (post-check). The decision carries Reason for the resulting error result.
	Block bool

	// Reason explains a block; empty when Block is false.
	Reason string
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
// an inert allow-everything placeholder. It is handed the same live emit stream
// the frontend drains, so a real prompt can reach the human and block on a reply.
type Permission interface {
	Decide(ctx context.Context, call ToolCall, emit func(RunEvent) error) PermissionResult
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
