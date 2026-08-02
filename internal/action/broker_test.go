package action

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
