package context

import (
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

func TestNewSeedsSystemTurn(t *testing.T) {
	m := New("you are a helpful agent")
	snap := m.Snapshot()
	if len(snap.Turns) != 1 {
		t.Fatalf("want 1 seeded turn, got %d", len(snap.Turns))
	}
	if snap.Turns[0].Role != "system" || snap.Turns[0].Content != "you are a helpful agent" {
		t.Fatalf("want system framing turn, got %+v", snap.Turns[0])
	}
}

func TestAppendOrder(t *testing.T) {
	m := New("sys")
	m.AppendUser("hello")
	m.AppendAssistant("on it", []core.ToolCall{{ID: "c1", ToolName: "read", Arguments: map[string]interface{}{"path": "a.txt"}}})
	m.AppendToolResults([]core.ToolResult{{ToolCallID: "c1", Result: "ok"}})
	m.AppendAssistant("done", nil)

	snap := m.Snapshot()
	wantRoles := []string{"system", "user", "assistant", "tool", "assistant"}
	if len(snap.Turns) != len(wantRoles) {
		t.Fatalf("want %d turns, got %d", len(wantRoles), len(snap.Turns))
	}
	for i, role := range wantRoles {
		if snap.Turns[i].Role != role {
			t.Errorf("turn %d: want role %q, got %q", i, role, snap.Turns[i].Role)
		}
	}
	if snap.Turns[2].ToolCalls[0].ToolName != "read" {
		t.Errorf("assistant turn should carry the tool call")
	}
	if snap.Turns[3].ToolResults[0].Result != "ok" {
		t.Errorf("tool turn should carry the result")
	}
}

func TestSnapshotIsUncorruptable(t *testing.T) {
	m := New("sys")
	m.AppendUser("hello")
	m.AppendAssistant("on it", []core.ToolCall{{ID: "c1", ToolName: "read", Arguments: map[string]interface{}{"path": "a.txt"}}})

	snap := m.Snapshot()
	// Mutate every layer of the snapshot.
	snap.Turns[0].Content = "HACKED"
	snap.Turns[1].Content = "HACKED"
	snap.Turns = append(snap.Turns, core.Turn{Role: "user", Content: "injected"})
	snap.Turns[2].ToolCalls[0].ToolName = "rm"
	snap.Turns[2].ToolCalls[0].Arguments["path"] = "/etc/passwd"

	again := m.Snapshot()
	if again.Turns[0].Content != "sys" || again.Turns[1].Content != "hello" {
		t.Errorf("mutating snapshot corrupted live content: %+v", again.Turns)
	}
	if len(again.Turns) != 3 {
		t.Errorf("mutating snapshot slice changed live length: got %d", len(again.Turns))
	}
	if again.Turns[2].ToolCalls[0].ToolName != "read" {
		t.Errorf("mutating snapshot tool call corrupted live: %q", again.Turns[2].ToolCalls[0].ToolName)
	}
	if again.Turns[2].ToolCalls[0].Arguments["path"] != "a.txt" {
		t.Errorf("mutating snapshot arguments corrupted live: %v", again.Turns[2].ToolCalls[0].Arguments["path"])
	}
}

func TestEstimateTokensGrows(t *testing.T) {
	m := New("sys")
	before := m.EstimateTokens()
	m.AppendUser("a much longer user message that should push the estimate upward")
	after := m.EstimateTokens()
	if after <= before {
		t.Errorf("estimate should grow with content: before=%d after=%d", before, after)
	}
}
