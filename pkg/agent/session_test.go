package agent_test

import (
	"context"
	"strings"
	"sync"
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

func TestPromptSubmitHookBlocksBeforeProviderCall(t *testing.T) {
	p := &recordingProvider{}
	s := agent.NewSession(agent.SessionConfig{
		Provider: p,
		Hooks: []agent.HookRegistration{{
			Name:   "block",
			Moment: agent.HookPromptSubmit,
			Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
				return agent.HookVerdict{Block: true, Reason: "forbidden prompt"}
			},
		}},
	})

	ch, err := s.Run(context.Background(), "do it")
	if err != nil {
		t.Fatalf("run should start: %v", err)
	}
	events := drain(t, ch)
	if p.calls() != 0 {
		t.Fatalf("provider must not be called after prompt block")
	}
	var sawHook, sawFailed bool
	for _, ev := range events {
		if ev.Type == agent.HookOutcomeEvent && ev.HookOutcome.Action == agent.HookBlocked {
			sawHook = true
		}
		if ev.Type == agent.RunFinishedEvent && ev.RunFinished.Reason == agent.StopFailed {
			sawFailed = true
		}
	}
	if !sawHook || !sawFailed {
		t.Fatalf("expected hook outcome and failed terminal, got %+v", events)
	}
}

func TestPromptSubmitHookInjectionVisibleToProvider(t *testing.T) {
	p := &recordingProvider{reply: []agent.RunEvent{{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}}}}
	s := agent.NewSession(agent.SessionConfig{
		Provider:     p,
		SystemPrompt: "sys",
		Hooks: []agent.HookRegistration{{
			Name:   "inject",
			Moment: agent.HookPromptSubmit,
			Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
				return agent.HookVerdict{InjectedContext: []string{"repo policy"}}
			},
		}},
	})

	ch, err := s.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	drain(t, ch)
	conv := p.lastConversation()
	if len(conv.Turns) < 3 || conv.Turns[1].Role != "system" || conv.Turns[1].Content != "repo policy" {
		t.Fatalf("injected context should precede provider call, got %+v", conv.Turns)
	}
	for _, turn := range s.Conversation().Turns {
		if turn.Content == "repo policy" {
			t.Fatalf("prompt-submit injection must not persist in session history: %+v", s.Conversation().Turns)
		}
	}
}

func TestSessionStartHookBlockAndInjection(t *testing.T) {
	_, err := agent.NewSessionWithError(agent.SessionConfig{
		Provider: &recordingProvider{},
		Hooks: []agent.HookRegistration{{
			Name:   "block-start",
			Moment: agent.HookSessionStart,
			Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
				return agent.HookVerdict{Block: true, Reason: "bad dir"}
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "bad dir") {
		t.Fatalf("session-start block should surface, got %v", err)
	}

	p := &recordingProvider{reply: []agent.RunEvent{{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}}}}
	s, err := agent.NewSessionWithError(agent.SessionConfig{
		Provider: p,
		Hooks: []agent.HookRegistration{{
			Name:   "inject-start",
			Moment: agent.HookSessionStart,
			Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
				return agent.HookVerdict{InjectedContext: []string{"standing context"}}
			},
		}},
	})
	if err != nil {
		t.Fatalf("session should start: %v", err)
	}
	ch, _ := s.Run(context.Background(), "hello")
	drain(t, ch)
	conv := p.lastConversation()
	if len(conv.Turns) < 2 || conv.Turns[1].Content != "standing context" {
		t.Fatalf("session-start injection missing from provider conversation: %+v", conv.Turns)
	}
}

func TestSessionStopHookSurfacesButDoesNotChangeCompletion(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{TextDeltas: []string{"ok"}, EndReason: agent.Finished}})
	s := agent.NewSession(agent.SessionConfig{
		Provider: p,
		Hooks: []agent.HookRegistration{{
			Name:   "stop",
			Moment: agent.HookSessionStop,
			Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
				return agent.HookVerdict{Block: true, Reason: "cleanup failed"}
			},
		}},
	})
	ch, _ := s.Run(context.Background(), "hello")
	events := drain(t, ch)
	last := events[len(events)-1]
	for _, ev := range events {
		if ev.Type == agent.HookOutcomeEvent && ev.HookOutcome.Moment == agent.HookSessionStop {
			t.Fatalf("session-stop hook must not run at normal run completion: %+v", events)
		}
	}
	if last.RunFinished == nil || last.RunFinished.Reason != agent.StopCompleted {
		t.Fatalf("session-stop block must not un-complete the run, got %+v", last)
	}
	if err := s.Close(context.Background()); err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("close should return session-stop failure, got %v", err)
	}
	if _, err := s.Run(context.Background(), "again"); err != agent.ErrSessionClosed {
		t.Fatalf("closed session should refuse runs, got %v", err)
	}
}

func TestRunFinishedHookRunsBeforeTerminalAndDoesNotChangeCompletion(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{TextDeltas: []string{"ok"}, EndReason: agent.Finished}})
	s := agent.NewSession(agent.SessionConfig{
		Provider: p,
		Hooks: []agent.HookRegistration{{
			Name:   "notify",
			Moment: agent.HookRunFinished,
			Handler: func(_ context.Context, ev agent.HookEvent) agent.HookVerdict {
				if ev.RunFinished == nil || ev.RunFinished.Reason != agent.StopCompleted {
					return agent.HookVerdict{Block: true, Reason: "missing completed result"}
				}
				return agent.HookVerdict{Block: true, Reason: "notification failed"}
			},
		}},
	})

	ch, err := s.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run should start: %v", err)
	}
	events := drain(t, ch)

	if len(events) < 2 {
		t.Fatalf("expected hook outcome and terminal events, got %+v", events)
	}
	hook := events[len(events)-2]
	if hook.Type != agent.HookOutcomeEvent || hook.HookOutcome.Moment != agent.HookRunFinished || hook.HookOutcome.Action != agent.HookBlocked {
		t.Fatalf("run-finished hook outcome should precede terminal event, got %+v", events)
	}
	last := events[len(events)-1]
	if last.RunFinished == nil || last.RunFinished.Reason != agent.StopCompleted {
		t.Fatalf("run-finished hook must not un-complete the run, got %+v", last)
	}
}

func TestRunFinishedHookRunsAfterPromptSubmitBlock(t *testing.T) {
	p := &recordingProvider{}
	var sawFailed bool
	s := agent.NewSession(agent.SessionConfig{
		Provider: p,
		Hooks: []agent.HookRegistration{
			{
				Name:   "block-prompt",
				Moment: agent.HookPromptSubmit,
				Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
					return agent.HookVerdict{Block: true, Reason: "forbidden prompt"}
				},
			},
			{
				Name:   "notify",
				Moment: agent.HookRunFinished,
				Handler: func(_ context.Context, ev agent.HookEvent) agent.HookVerdict {
					sawFailed = ev.RunFinished != nil && ev.RunFinished.Reason == agent.StopFailed
					return agent.HookVerdict{}
				},
			},
		},
	})

	ch, err := s.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run should start: %v", err)
	}
	events := drain(t, ch)

	if p.calls() != 0 {
		t.Fatalf("provider must not be called after prompt block")
	}
	if !sawFailed {
		t.Fatalf("run-finished hook should observe prompt-block failure, got %+v", events)
	}
	last := events[len(events)-1]
	if last.RunFinished == nil || last.RunFinished.Reason != agent.StopFailed {
		t.Fatalf("terminal event should preserve prompt-block failure, got %+v", last)
	}
}

// --- permission wiring ------------------------------------------------------

// findToolResult returns the first ToolFinishedEvent's result.
func findToolResult(events []agent.RunEvent) *agent.ToolResult {
	for _, ev := range events {
		if ev.Type == agent.ToolFinishedEvent {
			return ev.ToolResult
		}
	}
	return nil
}

func TestPermissionStartingModeFromConfig(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"x.txt","content":"hi"}`}}, EndReason: agent.StoppedToCallTools},
		{TextDeltas: []string{"ok"}, EndReason: agent.Finished},
	})
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys", PermissionMode: "plan"})

	events := drain(t, mustRun(t, s, "go"))
	res := findToolResult(events)
	if res == nil || !res.IsError || !strings.Contains(res.Result, "plan mode") {
		t.Fatalf("plan mode from config must block the write, got %+v", res)
	}
}

func TestSetPermissionModeSwitchesBetweenTurns(t *testing.T) {
	dir := t.TempDir()
	target := dir + "/out.txt"
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"` + target + `","content":"hi"}`}}, EndReason: agent.StoppedToCallTools},
		{TextDeltas: []string{"ok"}, EndReason: agent.Finished},
	})
	// Start in plan mode, then loosen to bypass before the turn.
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys", PermissionMode: "plan"})
	if err := s.SetPermissionMode("bypass"); err != nil {
		t.Fatalf("set mode: %v", err)
	}

	events := drain(t, mustRun(t, s, "go"))
	res := findToolResult(events)
	if res == nil || res.IsError {
		t.Fatalf("after switching to bypass, the write must succeed, got %+v", res)
	}
}

func TestSetPermissionModeRejectsUnknown(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{TextDeltas: []string{"hi"}, EndReason: agent.Finished}})
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys"})
	if err := s.SetPermissionMode("nonsense"); err == nil {
		t.Error("an unknown mode must be rejected")
	}
}

func mustRun(t *testing.T, s *agent.Session, input string) <-chan agent.RunEvent {
	t.Helper()
	ch, err := s.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	return ch
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

type recordingProvider struct {
	mu    sync.Mutex
	n     int
	last  agent.Conversation
	reply []agent.RunEvent
}

func (p *recordingProvider) StreamReply(_ context.Context, conv agent.Conversation, _ []agent.Tool, _ agent.StreamOptions) <-chan agent.RunEvent {
	p.mu.Lock()
	p.n++
	p.last = conv
	reply := append([]agent.RunEvent(nil), p.reply...)
	p.mu.Unlock()

	ch := make(chan agent.RunEvent, len(reply)+1)
	go func() {
		defer close(ch)
		for _, ev := range reply {
			ch <- ev
		}
		if len(reply) == 0 {
			ch <- agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}}
		}
	}()
	return ch
}

func (p *recordingProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func (p *recordingProvider) lastConversation() agent.Conversation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}
