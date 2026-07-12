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
	"encoding/json"
	"sync"
	"time"

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

// AppendSystem appends harness-provided standing context. It uses the system
// role so injected policy is visible to the model without pretending the user
// typed it.
func (m *Manager) AppendSystem(content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conv.Turns = append(m.conv.Turns, core.Turn{Role: "system", Content: content})
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

// EstimateRequestTokens estimates the effective assembled provider request,
// including durable/transient conversation, role and tool-call framing,
// advertised tool descriptors and schemas, and request options visible to the
// harness. encoding/json provides a deterministic map-key order, so identical
// effective inputs produce identical estimates.
func EstimateRequestTokens(conversation core.Conversation, tools []core.Tool, options core.StreamOptions) uint64 {
	request := struct {
		Conversation core.Conversation  `json:"conversation"`
		Tools        []core.Tool        `json:"tools,omitempty"`
		Options      core.StreamOptions `json:"options"`
	}{
		Conversation: conversation,
		Tools:        tools,
		Options:      options,
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		// All current request fields are JSON-safe. Retain a deterministic,
		// conservative framing estimate if a future caller supplies an invalid
		// value through a tool-argument map.
		encoded = []byte(`{"conversation":"unavailable","tools":"unavailable"}`)
	}
	return uint64((len(encoded) + charsPerTokenEstimate - 1) / charsPerTokenEstimate)
}

// UsageSnapshot constructs one truthful typed snapshot. windowTokens <= 0
// leaves window and remaining capacity unknown instead of fabricating zero.
func UsageSnapshot(round uint64, source core.ContextUsageSource, measuredAt time.Time, usedTokens uint64, windowTokens int) core.ContextUsage {
	usage := core.ContextUsage{
		Round: round, Source: source, MeasuredAt: measuredAt, UsedTokens: usedTokens,
	}
	if windowTokens > 0 {
		window := uint64(windowTokens)
		usage.WindowTokens = core.OptionalUint64{Known: true, Value: window}
		usage.RemainingTokens.Known = true
		if usedTokens >= window {
			usage.OverBudget = usedTokens > window
			usage.RemainingTokens.Value = 0
		} else {
			usage.RemainingTokens.Value = window - usedTokens
		}
	}
	return usage
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
