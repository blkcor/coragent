// This file holds the Phase 1 inert stand-in dispatcher. The real ordered chain
// lives in chain.go (Phase 2); StandIn remains as a trivial Dispatcher for tests
// and as documentation of the seam's original shape.
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
