// Package executor holds the single tool-dispatch path: the one ordered chain
// every tool call flows through. There is no second door — built-in and custom
// tools alike travel the same stages in one fixed order:
//
//	resolve + validate → hard pre-checks → permission → sandbox → execute →
//	hard post-checks → truncate
//
// The sandbox stage applies only to tools that run commands; pure file
// operations skip it. The permission, hard-check, and sandbox stages ship in
// Phase 2 as inert placeholders (see stages.go) that later phases replace
// without altering this chain.
//
// Every failure mode — unknown tool, bad arguments, a hard block, a permission
// denial, a post-check veto, an I/O error, a cancellation — is returned as a
// ToolResult with IsError set and the originating call's ID, never a crash. The
// Go error return is reserved for genuine harness breakage and is unused in
// Phase 2.
package executor

import (
	"context"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/tools"
)

// Stages bundles the four replaceable gate implementations the chain runs around
// tool execution. Phase 2 constructs it via InertStages; later phases inject real
// implementations without changing the chain.
type Stages struct {
	Pre        core.PreToolCheck
	Permission core.Permission
	Sandbox    core.Sandbox
	Post       core.PostToolCheck
}

// Executor is the real Dispatcher: it resolves a call against the catalog and
// runs it through the one ordered chain. It replaces the Phase 1 StandIn behind
// the unchanged core.Dispatcher seam, so the loop is untouched.
type Executor struct {
	catalog *tools.Catalog
	stages  Stages
	budget  int
}

// New builds an Executor over a catalog with the given stages and per-output byte
// budget. A non-positive budget uses DefaultOutputBudget.
func New(catalog *tools.Catalog, stages Stages, budget int) *Executor {
	if budget <= 0 {
		budget = DefaultOutputBudget
	}
	return &Executor{catalog: catalog, stages: stages, budget: budget}
}

// NewDefault builds an Executor with the Phase 2 inert stages and the default
// output budget — the wiring the Session uses when no Dispatcher is supplied.
func NewDefault(catalog *tools.Catalog) *Executor {
	return New(catalog, InertStages(), DefaultOutputBudget)
}

// Dispatch runs one tool call through the chain and returns its result. It never
// returns a non-nil error in Phase 2: every failure is an error ToolResult tied
// to the call so the loop surfaces it and keeps going.
func (e *Executor) Dispatch(ctx context.Context, call core.ToolCall, emit func(core.RunEvent) error) (core.ToolResult, error) {
	// Resolve: an unknown tool short-circuits before any stage runs.
	handler, ok := e.catalog.Lookup(call.ToolName)
	if !ok {
		return e.errorResult(call.ID, "unknown tool: "+call.ToolName), nil
	}

	// Validate the call's arguments against the tool's declared shape before any
	// gate or work runs.
	args := call.Arguments
	if err := validateArgs(handler.Descriptor().Parameters, args); err != nil {
		return e.errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}

	// Hard pre-checks: an unconditional block stops permission, sandbox, and the
	// tool. The model cannot override it.
	if d := e.stages.Pre.PreCheck(ctx, call); d.Block {
		return e.errorResult(call.ID, "blocked by hard pre-check: "+d.Reason), nil
	}

	// Human permission: a denial stops the sandbox and the tool; edited arguments
	// are re-validated and replace the originals.
	perm := e.stages.Permission.Decide(ctx, call, emit)
	if !perm.Allow {
		return e.errorResult(call.ID, "permission denied: "+perm.Reason), nil
	}
	if perm.EditedArguments != nil {
		if err := validateArgs(handler.Descriptor().Parameters, perm.EditedArguments); err != nil {
			return e.errorResult(call.ID, "invalid edited arguments: "+err.Error()), nil
		}
		args = perm.EditedArguments
	}

	// Execute: command-running tools route through the sandbox stage; pure file
	// operations skip it and run directly.
	var (
		out string
		err error
	)
	if handler.RunsCommands() {
		out, err = e.stages.Sandbox.Run(ctx, handler, args)
	} else {
		out, err = handler.Execute(ctx, args)
	}

	if err != nil {
		// Cancellation is surfaced as an error result; the loop's own cancellation
		// precedence governs the run outcome.
		msg := out
		if msg == "" {
			msg = err.Error()
		}
		return e.errorResult(call.ID, msg), nil
	}

	// Hard post-checks: a veto turns the successful result into an error carrying
	// the block's reason.
	result := core.ToolResult{ToolCallID: call.ID, Result: truncate(out, e.budget), IsError: false}
	if d := e.stages.Post.PostCheck(ctx, call, result); d.Block {
		return e.errorResult(call.ID, "blocked by hard post-check: "+d.Reason), nil
	}
	return result, nil
}

// errorResult builds an error ToolResult tied to a call, with its message bounded
// by the same output budget.
func (e *Executor) errorResult(callID, msg string) core.ToolResult {
	return core.ToolResult{ToolCallID: callID, Result: truncate(msg, e.budget), IsError: true}
}

// Executor satisfies the public Dispatcher seam.
var _ core.Dispatcher = (*Executor)(nil)
