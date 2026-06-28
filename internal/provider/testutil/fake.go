package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/blkcor/coragent/internal/core"
)

// FakeProvider implements the Provider interface for testing.
// It scripts replies (text deltas and tool calls) without requiring a real endpoint.
type FakeProvider struct {
	scripts []ScriptedReply
	mu      sync.Mutex
	callIdx int
}

// ScriptedReply defines a scripted response
type ScriptedReply struct {
	TextDeltas []string
	ToolCalls  []ScriptedToolCall
	Error      error
	EndReason  core.ReplyEndReason
}

// ScriptedToolCall defines a scripted tool call
type ScriptedToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON string
}

// NewFakeProvider creates a fake provider with scripted replies
func NewFakeProvider(scripts []ScriptedReply) *FakeProvider {
	return &FakeProvider{scripts: scripts}
}

// StreamReply implements the Provider interface
func (p *FakeProvider) StreamReply(ctx context.Context, conv core.Conversation, tools []core.Tool, opts core.StreamOptions) <-chan core.RunEvent {
	events := make(chan core.RunEvent, 10)

	go func() {
		defer close(events)

		// Advance to the next script on each call
		p.mu.Lock()
		if p.callIdx >= len(p.scripts) {
			p.mu.Unlock()
			events <- core.RunEvent{
				Type:  core.ErrorEvent,
				Error: fmt.Errorf("no more scripted replies (call %d, have %d scripts)", p.callIdx+1, len(p.scripts)),
			}
			return
		}
		script := p.scripts[p.callIdx]
		p.callIdx++
		p.mu.Unlock()

		// Check for scripted error
		if script.Error != nil {
			events <- core.RunEvent{
				Type:  core.ErrorEvent,
				Error: script.Error,
			}
			return
		}

		// Emit text deltas
		for _, delta := range script.TextDeltas {
			select {
			case <-ctx.Done():
				events <- core.RunEvent{
					Type:  core.ErrorEvent,
					Error: ctx.Err(),
				}
				return
			default:
				events <- core.RunEvent{
					Type:      core.TextDelta,
					TextDelta: delta,
				}
			}
		}

		// Emit tool calls
		for _, tc := range script.ToolCalls {
			select {
			case <-ctx.Done():
				events <- core.RunEvent{
					Type:  core.ErrorEvent,
					Error: ctx.Err(),
				}
				return
			default:
				args, err := parseArguments(tc.Arguments)
				if err != nil {
					events <- core.RunEvent{
						Type:  core.ErrorEvent,
						Error: fmt.Errorf("fake provider: parse arguments for %s: %w", tc.Name, err),
					}
					return
				}

				events <- core.RunEvent{
					Type: core.ToolCallEvent,
					ToolCall: &core.ToolCall{
						ID:        tc.ID,
						ToolName:  tc.Name,
						Arguments: args,
					},
				}
			}
		}

		// Emit reply ended
		events <- core.RunEvent{
			Type: core.ReplyEndedEvent,
			ReplyEnded: &core.ReplyEnded{
				Reason: script.EndReason,
			},
		}
	}()

	return events
}

// parseArguments parses a JSON argument string into a map.
func parseArguments(jsonStr string) (map[string]interface{}, error) {
	if jsonStr == "" {
		return nil, nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &args); err != nil {
		return nil, err
	}
	return args, nil
}
