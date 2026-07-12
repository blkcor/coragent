package loop

import (
	"context"
	"testing"
	"time"

	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
)

type scriptedRichProvider struct {
	richCalls   int
	legacyCalls int
	events      []core.RichProviderEvent
}

func (p *scriptedRichProvider) StreamReply(context.Context, core.Conversation, []core.Tool, core.StreamOptions) <-chan core.RunEvent {
	p.legacyCalls++
	channel := make(chan core.RunEvent)
	close(channel)
	return channel
}

func (p *scriptedRichProvider) StreamRichReply(ctx context.Context, _ core.Conversation, _ []core.Tool, _ core.StreamOptions) <-chan core.RichProviderEvent {
	p.richCalls++
	channel := make(chan core.RichProviderEvent)
	go func() {
		defer close(channel)
		for _, event := range p.events {
			select {
			case channel <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return channel
}

func TestRunRichSelectsOptionalProviderOnceAndKeepsSummaryOutOfConversation(t *testing.T) {
	provider := &scriptedRichProvider{events: []core.RichProviderEvent{
		{Type: core.RichProviderReasoningSummaryDelta, ReasoningSummaryDelta: "private display-safe summary"},
		{Type: core.RichProviderTextDelta, TextDelta: "answer"},
		{Type: core.RichProviderUsage, Usage: &core.ProviderUsage{PromptTokens: core.OptionalUint64{Known: true, Value: 7}}},
		{Type: core.RichProviderReplyEnded, ReplyEnded: &core.RichReplyEnded{Reason: core.ProviderTerminationStop}},
	}}
	manager := convo.New("sys")
	manager.AppendUser("hello")
	var events []core.RichEvent
	finished := RunRich(context.Background(), Deps{
		Provider: provider, Context: manager, Dispatcher: executor.StandIn{}, MaxRounds: 2, UseRichProvider: true,
	}, core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		events = append(events, event.Clone())
		return nil
	})
	if finished.Reason != core.StopCompleted || provider.richCalls != 1 || provider.legacyCalls != 0 {
		t.Fatalf("finished=%+v rich=%d legacy=%d", finished, provider.richCalls, provider.legacyCalls)
	}
	wantOrder := []core.ObservedEventKind{
		core.ObservedKindContextUsageUpdated, core.ObservedKindStatusChanged, core.ObservedKindAssistantStarted,
		core.ObservedKindAssistantReasoningSummaryDelta, core.ObservedKindAssistantTextDelta,
		core.ObservedKindContextUsageUpdated, core.ObservedKindAssistantFinished, core.ObservedKindStatusChanged,
	}
	if len(events) != len(wantOrder) {
		t.Fatalf("event count=%d want=%d: %+v", len(events), len(wantOrder), events)
	}
	for index, want := range wantOrder {
		if events[index].Kind != want {
			t.Fatalf("event %d kind=%q want=%q", index, events[index].Kind, want)
		}
	}
	snapshot := manager.Snapshot()
	last := snapshot.Turns[len(snapshot.Turns)-1]
	if last.Content != "answer" {
		t.Fatalf("assistant conversation content=%q", last.Content)
	}
	for _, turn := range snapshot.Turns {
		if turn.Content == "private display-safe summary" {
			t.Fatal("reasoning summary was persisted in Conversation")
		}
	}
}

type neverClosingRichProvider struct{ started chan struct{} }

func (p *neverClosingRichProvider) StreamReply(context.Context, core.Conversation, []core.Tool, core.StreamOptions) <-chan core.RunEvent {
	panic("legacy provider path must not be selected")
}

func (p *neverClosingRichProvider) StreamRichReply(context.Context, core.Conversation, []core.Tool, core.StreamOptions) <-chan core.RichProviderEvent {
	channel := make(chan core.RichProviderEvent)
	close(p.started)
	return channel
}

func TestRunRichCancellationDoesNotWaitForProviderClosure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &neverClosingRichProvider{started: make(chan struct{})}
	manager := convo.New("sys")
	manager.AppendUser("hello")
	done := make(chan core.RunFinished, 1)
	go func() {
		done <- RunRich(ctx, Deps{Provider: provider, Context: manager, Dispatcher: executor.StandIn{}, MaxRounds: 1, UseRichProvider: true}, core.Origin{AgentID: "root"}, func(core.RichEvent) error { return nil })
	}()
	<-provider.started
	cancel()
	select {
	case finished := <-done:
		if finished.Reason != core.StopCancelled {
			t.Fatalf("reason=%v want cancelled", finished.Reason)
		}
	case <-time.After(time.Second):
		t.Fatal("rich provider cancellation waited for channel closure")
	}
}

type timedRichDispatcher struct {
	result  core.ToolResult
	outcome core.ToolOutcome
}

func (dispatcher timedRichDispatcher) Dispatch(context.Context, core.ToolCall, func(core.RunEvent) error) (core.ToolResult, error) {
	panic("legacy dispatcher path must not be selected")
}

func (dispatcher timedRichDispatcher) DispatchRich(context.Context, core.ToolCall, core.CallID, core.Origin, func(core.RichEvent) error) (core.RichDispatchResult, error) {
	time.Sleep(2 * time.Millisecond)
	return core.RichDispatchResult{Result: dispatcher.result, Revision: 1, Outcome: dispatcher.outcome}, nil
}

func TestRunRichMeasuresToolDurationForEveryOutcome(t *testing.T) {
	tests := []struct {
		name    string
		result  core.ToolResult
		outcome core.ToolOutcome
	}{
		{"success", core.ToolResult{ToolCallID: "provider-call", Result: "ok"}, core.ToolOutcomeSucceeded},
		{"failure", core.ToolResult{ToolCallID: "provider-call", Result: "failed", IsError: true}, core.ToolOutcomeFailed},
		{"denied", core.ToolResult{ToolCallID: "provider-call", Result: "denied", IsError: true}, core.ToolOutcomeDenied},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &scriptedRichProvider{events: []core.RichProviderEvent{
				{Type: core.RichProviderToolCall, ToolCall: &core.ToolCall{ID: "provider-call", ToolName: "fixture"}},
				{Type: core.RichProviderReplyEnded, ReplyEnded: &core.RichReplyEnded{Reason: core.ProviderTerminationToolCalls}},
			}}
			manager := convo.New("sys")
			manager.AppendUser("run")
			var finished *core.ToolFinishedPayload
			RunRich(context.Background(), Deps{
				Provider: provider, Context: manager,
				Dispatcher: timedRichDispatcher{result: test.result, outcome: test.outcome}, MaxRounds: 1, UseRichProvider: true,
			}, core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
				if event.Kind == core.ObservedKindToolFinished {
					finished = event.Payload.(*core.ToolFinishedPayload)
				}
				return nil
			})
			if finished == nil || finished.Outcome != test.outcome || finished.Duration < time.Millisecond {
				t.Fatalf("tool finish = %+v", finished)
			}
		})
	}
}

var _ core.RichDispatcher = timedRichDispatcher{}
