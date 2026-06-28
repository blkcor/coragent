package executor

import (
	"context"
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

func TestStandInReturnsPlaceholderKeyedToCall(t *testing.T) {
	d := StandIn{}
	call := core.ToolCall{ID: "call-42", ToolName: "read", Arguments: map[string]interface{}{"path": "a.txt"}}

	res, err := d.Dispatch(context.Background(), call, func(core.RunEvent) error { return nil })
	if err != nil {
		t.Fatalf("stand-in must not return an unrecoverable error: %v", err)
	}
	if res.IsError {
		t.Errorf("stand-in result must not be an error result")
	}
	if res.ToolCallID != "call-42" {
		t.Errorf("result must be keyed to the call ID, got %q", res.ToolCallID)
	}
	if res.Result == "" {
		t.Errorf("result must carry a placeholder payload")
	}
}

// StandIn must satisfy the public Dispatcher seam.
var _ core.Dispatcher = StandIn{}
