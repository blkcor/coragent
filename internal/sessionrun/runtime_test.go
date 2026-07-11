package sessionrun

import (
	"context"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

func TestRuntimeOwnsLifecycleAndConversationWithoutEmittingTerminal(t *testing.T) {
	provider := &captureProvider{}
	hooks := &recordingLifecycle{}
	runtime := New(Config{
		Provider:     provider,
		Dispatcher:   inertDispatcher{},
		SystemPrompt: "child framing",
		MaxRounds:    3,
		Hooks:        hooks,
	})

	if err := runtime.Start(context.Background(), nil); err != nil {
		t.Fatalf("start runtime: %v", err)
	}

	var events []core.RunEvent
	fin := runtime.Run(context.Background(), "delegated instruction", func(ev core.RunEvent) error {
		events = append(events, ev)
		return nil
	})
	if fin.Reason != core.StopCompleted {
		t.Fatalf("run outcome = %v, want completed", fin.Reason)
	}
	if err := runtime.Stop(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("stop should surface cleanup failure, got %v", err)
	}

	if hooks.starts != 1 || hooks.prompts != 1 || hooks.finishes != 1 || hooks.stops != 1 {
		t.Fatalf("lifecycle counts = start:%d prompt:%d finish:%d stop:%d", hooks.starts, hooks.prompts, hooks.finishes, hooks.stops)
	}
	if hooks.finished.Reason != core.StopCompleted {
		t.Fatalf("run-finished hook saw %v, want completed", hooks.finished.Reason)
	}

	wantProvider := []core.Turn{
		{Role: "system", Content: "child framing"},
		{Role: "system", Content: "prompt-only context"},
		{Role: "system", Content: "standing context"},
		{Role: "user", Content: "delegated instruction"},
	}
	if len(provider.conv.Turns) != len(wantProvider) {
		t.Fatalf("provider conversation = %+v", provider.conv.Turns)
	}
	for i, want := range wantProvider {
		if got := provider.conv.Turns[i]; got.Role != want.Role || got.Content != want.Content {
			t.Errorf("provider turn %d = %+v, want %+v", i, got, want)
		}
	}

	snapshot := runtime.Conversation()
	if len(snapshot.Turns) != 4 {
		t.Fatalf("durable conversation = %+v", snapshot.Turns)
	}
	for _, turn := range snapshot.Turns {
		if turn.Content == "prompt-only context" {
			t.Fatalf("prompt-submit injection persisted in conversation: %+v", snapshot.Turns)
		}
	}
	if snapshot.Turns[1].Content != "standing context" || snapshot.Turns[3].Role != "assistant" || snapshot.Turns[3].Content != "final answer" {
		t.Fatalf("durable conversation = %+v", snapshot.Turns)
	}

	for _, ev := range events {
		if ev.Type == core.RunFinishedEvent {
			t.Fatalf("runtime owner, not Runtime.Run, must emit terminal event: %+v", events)
		}
	}
}

type captureProvider struct {
	conv core.Conversation
}

func (p *captureProvider) StreamReply(_ context.Context, conv core.Conversation, _ []core.Tool, _ core.StreamOptions) <-chan core.RunEvent {
	p.conv = conv
	ch := make(chan core.RunEvent, 2)
	ch <- core.RunEvent{Type: core.TextDelta, TextDelta: "final answer"}
	ch <- core.RunEvent{Type: core.ReplyEndedEvent, ReplyEnded: &core.ReplyEnded{Reason: core.Finished}}
	close(ch)
	return ch
}

type inertDispatcher struct{}

func (inertDispatcher) Dispatch(_ context.Context, call core.ToolCall, _ func(core.RunEvent) error) (core.ToolResult, error) {
	return core.ToolResult{ToolCallID: call.ID}, nil
}

type recordingLifecycle struct {
	starts   int
	prompts  int
	finishes int
	stops    int
	finished core.RunFinished
}

func (h *recordingLifecycle) SessionStart(context.Context, core.Conversation, func(core.RunEvent) error) core.HookLifecycleResult {
	h.starts++
	return core.HookLifecycleResult{InjectedContext: []string{"standing context"}}
}

func (h *recordingLifecycle) PromptSubmit(context.Context, string, core.Conversation, func(core.RunEvent) error) core.HookLifecycleResult {
	h.prompts++
	return core.HookLifecycleResult{InjectedContext: []string{"prompt-only context"}}
}

func (h *recordingLifecycle) RunFinished(_ context.Context, fin core.RunFinished, _ core.Conversation, _ func(core.RunEvent) error) core.HookLifecycleResult {
	h.finishes++
	h.finished = fin
	return core.HookLifecycleResult{Block: true, Reason: "notification failed"}
}

func (h *recordingLifecycle) SessionStop(context.Context, core.Conversation, func(core.RunEvent) error) core.HookLifecycleResult {
	h.stops++
	return core.HookLifecycleResult{Block: true, Reason: "cleanup failed"}
}
