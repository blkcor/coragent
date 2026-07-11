package subagent

import (
	"context"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
	"github.com/blkcor/coragent/internal/tools"
)

func TestSilentChildProviderCloseBecomesRecoverableTaskResult(t *testing.T) {
	childCatalog := tools.NewCatalog()
	blueprint := NewBlueprint(BlueprintConfig{
		Provider:  silentChildProvider{},
		Catalog:   childCatalog,
		Stages:    executor.InertStages(),
		MaxRounds: 3,
	})

	parentCatalog := tools.NewCatalog()
	parentCatalog.MustRegister(NewTaskHandler(blueprint))
	dispatcher := executor.NewDefault(parentCatalog)
	result, err := dispatcher.Dispatch(context.Background(), core.ToolCall{
		ID:       "task-1",
		ToolName: ToolName,
		Arguments: map[string]interface{}{
			"label":       "silent child",
			"instruction": "finish the delegated work",
		},
	}, func(core.RunEvent) error { return nil })

	if err != nil {
		t.Fatalf("recoverable task failure returned dispatcher error: %v", err)
	}
	if !result.IsError || result.ToolCallID != "task-1" {
		t.Fatalf("task result = %+v, want call-scoped recoverable error", result)
	}
	if !strings.Contains(result.Result, "child failed") || !strings.Contains(result.Result, "ReplyEndedEvent") {
		t.Fatalf("task error = %q, want child protocol failure", result.Result)
	}
}

type silentChildProvider struct{}

func (silentChildProvider) StreamReply(context.Context, core.Conversation, []core.Tool, core.StreamOptions) <-chan core.RunEvent {
	ch := make(chan core.RunEvent)
	close(ch)
	return ch
}
