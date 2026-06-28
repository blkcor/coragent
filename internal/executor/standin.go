// Package executor holds the single tool-dispatch path.
//
// In Phase 1 the path is an inert stand-in: it produces a placeholder result and
// runs no real tool. It exists so the loop has exactly one dispatch seam to call.
// Phase 2 replaces it with the real registry and middleware chain (hooks →
// permission → sandbox → tool → post-hooks) behind the same core.Dispatcher
// interface, so the loop is untouched.
package executor

import (
	"context"

	"github.com/blkcor/coragent/internal/core"
)

// standInResult is the placeholder payload returned for every dispatched call.
const standInResult = "<stand-in: no tool executed>"

// StandIn is the Phase 1 inert dispatcher. It is the sole producer of tool
// results until the real executor arrives, proving that every tool request
// flows through one path.
type StandIn struct{}

// Dispatch returns a non-error placeholder result keyed to the call. It never
// raises an unrecoverable condition and does not consult the emit callback.
func (StandIn) Dispatch(_ context.Context, call core.ToolCall, _ func(core.RunEvent) error) (core.ToolResult, error) {
	return core.ToolResult{
		ToolCallID: call.ID,
		Result:     standInResult,
		IsError:    false,
	}, nil
}
