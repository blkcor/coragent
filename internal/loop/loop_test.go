package loop

import (
	"context"
	"errors"
	"testing"

	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
	"github.com/blkcor/coragent/internal/provider/testutil"
)

// collector records emitted events and supports a per-event hook (used to drive
// cancellation and to answer permission requests).
type collector struct {
	events []core.RunEvent
	hook   func(core.RunEvent) error
}

func (c *collector) emit(ev core.RunEvent) error {
	c.events = append(c.events, ev)
	if c.hook != nil {
		return c.hook(ev)
	}
	return nil
}

func (c *collector) types() []core.RunEventType {
	out := make([]core.RunEventType, len(c.events))
	for i, ev := range c.events {
		out[i] = ev.Type
	}
	return out
}

func deps(p core.Provider, m *convo.Manager, d core.Dispatcher, maxRounds int) Deps {
	return Deps{
		Provider:   p,
		Context:    m,
		Dispatcher: d,
		MaxRounds:  maxRounds,
	}
}

// --- fake dispatchers -------------------------------------------------------

type funcDispatcher func(ctx context.Context, call core.ToolCall, emit func(core.RunEvent) error) (core.ToolResult, error)

func (f funcDispatcher) Dispatch(ctx context.Context, call core.ToolCall, emit func(core.RunEvent) error) (core.ToolResult, error) {
	return f(ctx, call, emit)
}

// --- fake providers ---------------------------------------------------------

// alwaysCallsTool emits one tool call every round, forever.
type alwaysCallsTool struct{}

func (alwaysCallsTool) StreamReply(ctx context.Context, _ core.Conversation, _ []core.Tool, _ core.StreamOptions) <-chan core.RunEvent {
	ch := make(chan core.RunEvent, 4)
	go func() {
		defer close(ch)
		ch <- core.RunEvent{Type: core.ToolCallEvent, ToolCall: &core.ToolCall{ID: "c", ToolName: "noop"}}
		ch <- core.RunEvent{Type: core.ReplyEndedEvent, ReplyEnded: &core.ReplyEnded{Reason: core.StoppedToCallTools}}
	}()
	return ch
}

// blockingProvider emits one text delta, then blocks until the context is
// cancelled, modelling a long think.
type blockingProvider struct{}

func (blockingProvider) StreamReply(ctx context.Context, _ core.Conversation, _ []core.Tool, _ core.StreamOptions) <-chan core.RunEvent {
	ch := make(chan core.RunEvent)
	go func() {
		defer close(ch)
		select {
		case ch <- core.RunEvent{Type: core.TextDelta, TextDelta: "thinking..."}:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
		ch <- core.RunEvent{Type: core.ErrorEvent, Error: ctx.Err()}
	}()
	return ch
}

// --- tests ------------------------------------------------------------------

func TestHeadlineOrder(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{TextDeltas: []string{"Let me check."}, ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "read", Arguments: `{"path":"a.txt"}`}}, EndReason: core.StoppedToCallTools},
		{TextDeltas: []string{"All done."}, EndReason: core.Finished},
	})
	m := convo.New("sys")
	m.AppendUser("do it")
	c := &collector{}

	fin := Run(context.Background(), deps(p, m, executor.StandIn{}, 8), c.emit)

	if fin.Reason != core.StopCompleted {
		t.Fatalf("want StopCompleted, got %v (err=%v)", fin.Reason, fin.Err)
	}
	want := []core.RunEventType{
		core.StatusChange,      // thinking
		core.TextDelta,         // first text
		core.StatusChange,      // calling_tool
		core.ToolStartedEvent,  // tool start
		core.ToolFinishedEvent, // tool finish
		core.StatusChange,      // thinking
		core.TextDelta,         // second text
		core.StatusChange,      // idle
	}
	got := c.types()
	if len(got) != len(want) {
		t.Fatalf("event count: want %d got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d: want %v got %v", i, want[i], got[i])
		}
	}
	// Status values at the four StatusChange positions.
	if c.events[0].Status != core.StatusThinking || c.events[2].Status != core.StatusCallingTool || c.events[5].Status != core.StatusThinking || c.events[7].Status != core.StatusIdle {
		t.Errorf("status sequence wrong: %q %q %q %q", c.events[0].Status, c.events[2].Status, c.events[5].Status, c.events[7].Status)
	}
	// Tool start carries name+args; finish carries the stand-in result.
	if c.events[3].ToolCall == nil || c.events[3].ToolCall.ToolName != "read" {
		t.Errorf("tool start must carry the request")
	}
	if c.events[4].ToolResult == nil || c.events[4].ToolResult.IsError {
		t.Errorf("tool finish must carry a non-error result")
	}
	// Conversation reads system, user, assistant(+call), tool, assistant.
	snap := m.Snapshot()
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

func TestRecoverableToolFailureContinues(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "boom"}}, EndReason: core.StoppedToCallTools},
		{TextDeltas: []string{"recovered"}, EndReason: core.Finished},
	})
	m := convo.New("sys")
	d := funcDispatcher(func(_ context.Context, call core.ToolCall, _ func(core.RunEvent) error) (core.ToolResult, error) {
		return core.ToolResult{ToolCallID: call.ID, Result: "it failed", IsError: true}, nil
	})
	c := &collector{}

	fin := Run(context.Background(), deps(p, m, d, 8), c.emit)
	if fin.Reason != core.StopCompleted {
		t.Fatalf("recoverable failure must let the run complete, got %v", fin.Reason)
	}
	var sawFailedStep bool
	for _, ev := range c.events {
		if ev.Type == core.ToolFinishedEvent && ev.ToolResult != nil && ev.ToolResult.IsError {
			sawFailedStep = true
		}
	}
	if !sawFailedStep {
		t.Errorf("a finished-but-failed step should appear on the stream")
	}
}

func TestUnrecoverableDispatchEndsFailed(t *testing.T) {
	boom := errors.New("dispatch exploded")
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "boom"}}, EndReason: core.StoppedToCallTools},
		{TextDeltas: []string{"should never run"}, EndReason: core.Finished},
	})
	m := convo.New("sys")
	d := funcDispatcher(func(_ context.Context, _ core.ToolCall, _ func(core.RunEvent) error) (core.ToolResult, error) {
		return core.ToolResult{}, boom
	})
	c := &collector{}

	fin := Run(context.Background(), deps(p, m, d, 8), c.emit)
	if fin.Reason != core.StopFailed || !errors.Is(fin.Err, boom) {
		t.Fatalf("want StopFailed naming the cause, got %v err=%v", fin.Reason, fin.Err)
	}
	for _, ev := range c.events {
		if ev.Type == core.ToolFinishedEvent {
			t.Errorf("an unrecoverable dispatch must not emit a tool finish")
		}
	}
}

func TestProviderErrorEndsFailedNoPartialTurn(t *testing.T) {
	boom := errors.New("backend down")
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{TextDeltas: []string{"partial"}, Error: boom},
	})
	m := convo.New("sys")
	m.AppendUser("hi")
	c := &collector{}

	fin := Run(context.Background(), deps(p, m, executor.StandIn{}, 8), c.emit)
	if fin.Reason != core.StopFailed || !errors.Is(fin.Err, boom) {
		t.Fatalf("want StopFailed naming the cause, got %v err=%v", fin.Reason, fin.Err)
	}
	// No assistant turn recorded: still just system + user.
	snap := m.Snapshot()
	if len(snap.Turns) != 2 {
		t.Errorf("partial reply must not be recorded as a turn, got %d turns", len(snap.Turns))
	}
}

func TestStepLimitNormalStop(t *testing.T) {
	m := convo.New("sys")
	c := &collector{}
	fin := Run(context.Background(), deps(alwaysCallsTool{}, m, executor.StandIn{}, 3), c.emit)
	if fin.Reason != core.StopReachedStepLimit {
		t.Fatalf("want StopReachedStepLimit, got %v", fin.Reason)
	}
	if fin.Reason.IsError() {
		t.Errorf("step limit must be a normal stop, not an error")
	}
}

func TestCancelMidThink(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := convo.New("sys")
	c := &collector{hook: func(ev core.RunEvent) error {
		if ev.Type == core.TextDelta {
			cancel() // pull the plug as the model streams
		}
		return nil
	}}
	fin := Run(ctx, deps(blockingProvider{}, m, executor.StandIn{}, 8), c.emit)
	if fin.Reason != core.StopCancelled {
		t.Fatalf("want StopCancelled, got %v", fin.Reason)
	}
}

func TestCancelMidTool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "slow"}}, EndReason: core.StoppedToCallTools},
	})
	m := convo.New("sys")
	d := funcDispatcher(func(ctx context.Context, _ core.ToolCall, _ func(core.RunEvent) error) (core.ToolResult, error) {
		cancel()     // user interrupts mid-tool
		<-ctx.Done() // a well-behaved tool observes cancellation
		return core.ToolResult{}, ctx.Err()
	})
	c := &collector{}
	fin := Run(ctx, deps(p, m, d, 8), c.emit)
	if fin.Reason != core.StopCancelled {
		t.Fatalf("want StopCancelled, got %v err=%v", fin.Reason, fin.Err)
	}
}

func TestPermissionPauseOrdering(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "write"}}, EndReason: core.StoppedToCallTools},
		{TextDeltas: []string{"approved and done"}, EndReason: core.Finished},
	})
	m := convo.New("sys")
	d := funcDispatcher(func(_ context.Context, call core.ToolCall, emit func(core.RunEvent) error) (core.ToolResult, error) {
		reply := make(chan core.PermissionDecision, 1)
		if err := emit(core.RunEvent{Type: core.PermissionRequestedEvent, Permission: &core.PermissionRequest{ToolCall: call, ReplyPath: reply}}); err != nil {
			return core.ToolResult{}, err
		}
		decision := <-reply
		return core.ToolResult{ToolCallID: call.ID, Result: "written", IsError: !decision.Allow}, nil
	})
	c := &collector{hook: func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			ev.Permission.ReplyPath <- core.PermissionDecision{Allow: true}
		}
		return nil
	}}

	fin := Run(context.Background(), deps(p, m, d, 8), c.emit)
	if fin.Reason != core.StopCompleted {
		t.Fatalf("want StopCompleted after approval, got %v", fin.Reason)
	}
	// The permission question must sit between the tool start and finish.
	var iStart, iPerm, iFinish = -1, -1, -1
	for i, ev := range c.events {
		switch ev.Type {
		case core.ToolStartedEvent:
			iStart = i
		case core.PermissionRequestedEvent:
			iPerm = i
		case core.ToolFinishedEvent:
			iFinish = i
		}
	}
	if !(iStart >= 0 && iPerm > iStart && iFinish > iPerm) {
		t.Errorf("want start < permission < finish, got %d %d %d", iStart, iPerm, iFinish)
	}
}

// _ ensures the public Dispatcher is satisfied by the test helper.
var _ core.Dispatcher = funcDispatcher(nil)
