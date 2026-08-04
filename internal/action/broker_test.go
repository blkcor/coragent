package action

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/transcript"
)

type projectingTool struct{ secret string }

func (t projectingTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}
}

type largeTool struct{ projectingTool }

func (largeTool) Execute(context.Context, Prepared) Execution {
	return Execution{Outcome: transcript.ToolResultSuccess, Content: strings.Repeat("界", 30000)}
}
func (t projectingTool) Prepare(context.Context, json.RawMessage) (Prepared, error) {
	return Prepared{Tool: "read", Effects: []Effect{EffectRead}}, nil
}

func TestBrokerEnforcesFinalUTF8ByteBound(t *testing.T) {
	broker, err := NewBroker(largeTool{})
	if err != nil {
		t.Fatal(err)
	}
	result := broker.Execute(context.Background(), provider.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{}`)})
	if len(result.Content) > 64*1024 || !strings.Contains(result.Content, "truncated=true") || !utf8.ValidString(result.Content) {
		t.Fatalf("bounded result bytes=%d", len(result.Content))
	}
}
func (t projectingTool) Execute(context.Context, Prepared) Execution {
	return Execution{Outcome: transcript.ToolResultSuccess, Content: "result " + t.secret}
}

func TestBrokerProjectsEveryToolResult(t *testing.T) {
	secret := strings.Join([]string{"sk", "0123456789abcdefghij012345"}, "-")
	broker, err := NewBrokerWithProjector(dataproj.New(), projectingTool{secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	result := broker.Execute(context.Background(), provider.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{}`)})
	if result.Outcome != transcript.ToolResultSuccess || strings.Contains(result.Content, secret) || !strings.Contains(result.Content, "REDACTED") {
		t.Fatalf("result = %+v", result)
	}
}

// blockingTool blocks inside Execute until its context is cancelled, so a test
// can prove cancellation interrupts an already-running tool rather than only a
// tool that has not started.
type blockingTool struct {
	projectingTool
	entered chan struct{}
}

func (t blockingTool) Execute(ctx context.Context, _ Prepared) Execution {
	close(t.entered)
	<-ctx.Done()
	return Execution{Outcome: transcript.ToolResultCancelled, Content: "stopped"}
}

func TestBrokerCancelsBlockingToolPromptly(t *testing.T) {
	entered := make(chan struct{})
	broker, err := NewBroker(blockingTool{entered: entered})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	call := provider.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{}`)}
	done := make(chan transcript.ToolResultPayload, 1)
	go func() { done <- broker.Execute(ctx, call) }()
	<-entered // the tool is now blocked inside Execute; the run is mid-flight
	cancel()
	select {
	case result := <-done:
		if result.Outcome != transcript.ToolResultCancelled {
			t.Fatalf("outcome = %v, want cancelled", result.Outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return promptly after cancellation")
	}
}

func TestBrokerBatchPairsCancelledThenSkipped(t *testing.T) {
	broker, err := NewBroker(largeTool{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := broker.ExecuteBatch(ctx, []provider.ToolCall{
		{ID: "c1", Name: "read", Arguments: json.RawMessage(`{}`)},
		{ID: "c2", Name: "read", Arguments: json.RawMessage(`{}`)},
	})
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Outcome != transcript.ToolResultCancelled {
		t.Errorf("first = %v, want cancelled", results[0].Outcome)
	}
	if results[1].Outcome != transcript.ToolResultSkipped {
		t.Errorf("second = %v, want skipped", results[1].Outcome)
	}
}
