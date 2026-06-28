package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/provider/testutil"
	"github.com/blkcor/coragent/pkg/agent"
)

func drain(t *testing.T, ch <-chan agent.RunEvent) []agent.RunEvent {
	t.Helper()
	var out []agent.RunEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestHeadlineScenario(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{TextDeltas: []string{"Let me check."}, ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "read", Arguments: `{"path":"a.txt"}`}}, EndReason: agent.StoppedToCallTools},
		{TextDeltas: []string{"All done."}, EndReason: agent.Finished},
	})
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys"})

	ch, err := s.Run(context.Background(), "do it")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	events := drain(t, ch)

	want := []agent.RunEventType{
		agent.StatusChange, agent.TextDelta, agent.StatusChange,
		agent.ToolStartedEvent, agent.ToolFinishedEvent,
		agent.StatusChange, agent.TextDelta, agent.StatusChange,
		agent.RunFinishedEvent,
	}
	if len(events) != len(want) {
		t.Fatalf("event count: want %d got %d (%v)", len(want), len(events), typesOf(events))
	}
	for i := range want {
		if events[i].Type != want[i] {
			t.Errorf("event %d: want %v got %v", i, want[i], events[i].Type)
		}
	}
	last := events[len(events)-1]
	if last.RunFinished == nil || last.RunFinished.Reason != agent.StopCompleted {
		t.Errorf("run must finish completed, got %+v", last.RunFinished)
	}

	snap := s.Conversation()
	roles := []string{"system", "user", "assistant", "tool", "assistant"}
	if len(snap.Turns) != len(roles) {
		t.Fatalf("conversation: want %d turns got %d", len(roles), len(snap.Turns))
	}
	for i, r := range roles {
		if snap.Turns[i].Role != r {
			t.Errorf("turn %d: want %q got %q", i, r, snap.Turns[i].Role)
		}
	}
}

func TestExactlyOneTerminalThenClose(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{TextDeltas: []string{"hi"}, EndReason: agent.Finished}})
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys"})
	ch, err := s.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	events := drain(t, ch)

	var terminals int
	for i, ev := range events {
		if ev.Type == agent.RunFinishedEvent {
			terminals++
			if i != len(events)-1 {
				t.Errorf("RunFinishedEvent must be the last event")
			}
		}
	}
	if terminals != 1 {
		t.Errorf("want exactly one terminal event, got %d", terminals)
	}
}

func TestConcurrentRunRefused(t *testing.T) {
	// A provider that blocks until cancelled keeps the first run in flight.
	p := blockingForeverProvider{}
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1, err := s.Run(ctx, "first")
	if err != nil {
		t.Fatalf("first run should start: %v", err)
	}
	// Let the first run get in flight.
	time.Sleep(20 * time.Millisecond)

	_, err2 := s.Run(context.Background(), "second")
	if err2 == nil {
		t.Errorf("second concurrent run must be refused")
	}
	// History unchanged by the refusal: still system + first user turn.
	if got := len(s.Conversation().Turns); got != 2 {
		t.Errorf("refused run must not change history, got %d turns", got)
	}
	cancel()
	drain(t, ch1)
}

func TestHistoryAccumulatesAcrossRuns(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{TextDeltas: []string{"first reply"}, EndReason: agent.Finished},
		{TextDeltas: []string{"second reply"}, EndReason: agent.Finished},
	})
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys"})

	ch1, _ := s.Run(context.Background(), "one")
	drain(t, ch1)
	ch2, _ := s.Run(context.Background(), "two")
	drain(t, ch2)

	snap := s.Conversation()
	roles := []string{"system", "user", "assistant", "user", "assistant"}
	if len(snap.Turns) != len(roles) {
		t.Fatalf("want %d turns got %d", len(roles), len(snap.Turns))
	}
	for i, r := range roles {
		if snap.Turns[i].Role != r {
			t.Errorf("turn %d: want %q got %q", i, r, snap.Turns[i].Role)
		}
	}
}

func TestOverBudgetWarning(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{TextDeltas: []string{"ok"}, EndReason: agent.Finished}})
	// A tiny budget guarantees the seeded system + user turns exceed it.
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: strings.Repeat("x", 400), ContextBudgetTokens: 1})
	ch, _ := s.Run(context.Background(), "go")
	events := drain(t, ch)

	var warned bool
	var finished bool
	for _, ev := range events {
		if ev.Type == agent.OverBudgetWarningEvent {
			warned = true
		}
		if ev.Type == agent.RunFinishedEvent && ev.RunFinished.Reason == agent.StopCompleted {
			finished = true
		}
	}
	if !warned {
		t.Errorf("an over-budget conversation must emit an advisory warning")
	}
	if !finished {
		t.Errorf("the run must proceed and complete despite the warning")
	}
}

func TestBackpressureNoLossThenCancelUnblocks(t *testing.T) {
	// Many text deltas; a slow reader consumes them one at a time.
	deltas := make([]string, 50)
	for i := range deltas {
		deltas[i] = "x"
	}
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{TextDeltas: deltas, EndReason: agent.Finished}})
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys"})

	ch, _ := s.Run(context.Background(), "go")
	var textCount int
	for ev := range ch {
		if ev.Type == agent.TextDelta {
			textCount++
			time.Sleep(time.Millisecond) // slow reader
		}
	}
	if textCount != len(deltas) {
		t.Errorf("slow reader lost events: want %d got %d", len(deltas), textCount)
	}

	// Abandoned-and-cancelled reader: the run goroutine must not wedge.
	p2 := blockingForeverProvider{}
	s2 := agent.NewSession(agent.SessionConfig{Provider: p2, SystemPrompt: "sys"})
	ctx, cancel := context.WithCancel(context.Background())
	ch2, _ := s2.Run(ctx, "go")
	cancel() // abandon without draining
	// The channel must eventually close even though we never read events.
	select {
	case <-closeWaiter(ch2):
	case <-time.After(2 * time.Second):
		t.Errorf("cancelled run with abandoned reader wedged the agent")
	}
}

// --- helpers ---------------------------------------------------------------

func typesOf(events []agent.RunEvent) []agent.RunEventType {
	out := make([]agent.RunEventType, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}

// closeWaiter drains ch in the background and signals when it closes.
func closeWaiter(ch <-chan agent.RunEvent) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	return done
}

// blockingForeverProvider blocks until the context is cancelled.
type blockingForeverProvider struct{}

func (blockingForeverProvider) StreamReply(ctx context.Context, _ agent.Conversation, _ []agent.Tool, _ agent.StreamOptions) <-chan agent.RunEvent {
	ch := make(chan agent.RunEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
		ch <- agent.RunEvent{Type: agent.ErrorEvent, Error: ctx.Err()}
	}()
	return ch
}
