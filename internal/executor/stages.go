package executor

import (
	"context"

	"github.com/blkcor/coragent/internal/core"
)

// This file holds the Phase 2 inert stage placeholders. Each is a one-method
// drop-in a later phase replaces without altering the chain: Phase 3 swaps in a
// real Permission, Phase 4 real PreToolCheck/PostToolCheck, Phase 5 a real
// Sandbox. The chain assembly does not change — only which implementation it is
// constructed with.

// allowAllPermission is the inert human-permission stage: it allows every call
// and never edits arguments. Phase 3 replaces it.
type allowAllPermission struct{}

func (allowAllPermission) Decide(context.Context, core.ToolCall, func(core.RunEvent) error) core.PermissionResult {
	return core.PermissionResult{Allow: true}
}

// neverBlockCheck is the inert hard-gate stage used for both pre- and post-checks:
// it never blocks. Phase 4 replaces it.
type neverBlockCheck struct{}

func (neverBlockCheck) PreCheck(context.Context, core.ToolCall) core.StageDecision {
	return core.StageDecision{}
}

func (neverBlockCheck) PostCheck(context.Context, core.ToolCall, core.ToolResult) core.StageDecision {
	return core.StageDecision{}
}

// directSandbox is the inert sandbox stage: it runs the tool's command path
// directly, with no OS confinement. Phase 5 replaces it.
type directSandbox struct{}

func (directSandbox) Run(ctx context.Context, handler core.ToolHandler, args map[string]interface{}) (string, error) {
	return handler.Execute(ctx, args)
}

// InertStages returns the Phase 2 pass-through stage set: allow-everything
// permission, never-block hard checks, run-directly sandbox.
func InertStages() Stages {
	return Stages{
		Pre:        neverBlockCheck{},
		Permission: allowAllPermission{},
		Sandbox:    directSandbox{},
		Post:       neverBlockCheck{},
	}
}
