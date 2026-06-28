// Package context manages conversation history for a run.
//
// A Manager accumulates the conversation across requests, presents it to the
// model in order, and exposes it only as a deep-copied, uncorruptable snapshot.
// It estimates context usage with a cheap heuristic so the loop can warn before
// the model's window overflows.
//
// Compaction (actually shrinking history) is out of v1. The plug-in point is
// EstimateTokens together with the loop's over-budget warning; the shrinking
// step behind it is intentionally absent.
package context

import (
	"sync"

	"github.com/blkcor/coragent/internal/core"
)

// charsPerTokenEstimate is the cheap divisor used to approximate token count
// from character length. A precise tokenizer is future work; this estimate is
// sufficient to fire the over-budget warning.
const charsPerTokenEstimate = 4

// Manager guards a live conversation. One run is in flight per conversation, but
// callers may read a snapshot concurrently, so access is mutex-guarded.
type Manager struct {
	mu   sync.Mutex
	conv core.Conversation
}

// New returns a Manager seeded with the system framing turn.
func New(systemPrompt string) *Manager {
	return &Manager{
		conv: core.Conversation{
			Turns: []core.Turn{{Role: "system", Content: systemPrompt}},
		},
	}
}

// AppendUser appends a user turn.
func (m *Manager) AppendUser(content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conv.Turns = append(m.conv.Turns, core.Turn{Role: "user", Content: content})
}

// AppendAssistant appends an assistant turn carrying any requested tool calls.
func (m *Manager) AppendAssistant(content string, calls []core.ToolCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conv.Turns = append(m.conv.Turns, core.Turn{
		Role:      "assistant",
		Content:   content,
		ToolCalls: calls,
	})
}

// AppendToolResults appends one tool turn carrying all results from a round.
func (m *Manager) AppendToolResults(results []core.ToolResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conv.Turns = append(m.conv.Turns, core.Turn{
		Role:        "tool",
		ToolResults: results,
	})
}

// Snapshot returns a deep copy of the conversation. Mutating the returned value
// cannot corrupt the live conversation.
func (m *Manager) Snapshot() core.Conversation {
	m.mu.Lock()
	defer m.mu.Unlock()

	turns := make([]core.Turn, len(m.conv.Turns))
	for i, t := range m.conv.Turns {
		turns[i] = cloneTurn(t)
	}
	return core.Conversation{Turns: turns}
}

// EstimateTokens returns a cheap estimate of the conversation's token footprint.
func (m *Manager) EstimateTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	chars := 0
	for _, t := range m.conv.Turns {
		chars += len(t.Content)
		for _, c := range t.ToolCalls {
			chars += len(c.ToolName)
			for k, v := range c.Arguments {
				chars += len(k)
				if s, ok := v.(string); ok {
					chars += len(s)
				}
			}
		}
		for _, r := range t.ToolResults {
			chars += len(r.Result)
		}
	}
	return chars / charsPerTokenEstimate
}

// cloneTurn deep-copies a turn, including nested tool calls, tool results, and
// argument maps, so a snapshot shares no mutable state with the live turn.
func cloneTurn(t core.Turn) core.Turn {
	out := core.Turn{Role: t.Role, Content: t.Content}

	if t.ToolCalls != nil {
		out.ToolCalls = make([]core.ToolCall, len(t.ToolCalls))
		for i, c := range t.ToolCalls {
			cc := core.ToolCall{ID: c.ID, ToolName: c.ToolName}
			if c.Arguments != nil {
				cc.Arguments = make(map[string]interface{}, len(c.Arguments))
				for k, v := range c.Arguments {
					cc.Arguments[k] = v
				}
			}
			out.ToolCalls[i] = cc
		}
	}

	if t.ToolResults != nil {
		out.ToolResults = make([]core.ToolResult, len(t.ToolResults))
		copy(out.ToolResults, t.ToolResults)
	}

	return out
}
