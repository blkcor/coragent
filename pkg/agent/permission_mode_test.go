package agent_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/blkcor/coragent/pkg/agent"
)

func TestTypedPermissionModeBetweenRuns(t *testing.T) {
	s := agent.NewSession(agent.SessionConfig{Provider: immediateProvider{}})

	mode, err := s.PermissionMode()
	if err != nil || mode != agent.PermissionModeDefault {
		t.Fatalf("initial mode = %q, %v", mode, err)
	}
	if err := s.SetPermissionModeTyped(agent.PermissionModePlan); err != nil {
		t.Fatalf("SetPermissionModeTyped: %v", err)
	}
	mode, err = s.PermissionMode()
	if err != nil || mode != agent.PermissionModePlan {
		t.Fatalf("updated mode = %q, %v", mode, err)
	}
	if err := s.SetPermissionModeTyped(agent.PermissionMode("surprise")); err == nil {
		t.Fatal("unknown typed mode was accepted")
	}
}

func TestBothPermissionModeSettersRejectMidRun(t *testing.T) {
	provider := &neverClosingModeProvider{started: make(chan struct{})}
	s := agent.NewSession(agent.SessionConfig{Provider: provider})
	ctx, cancel := context.WithCancel(context.Background())
	events, err := s.Run(ctx, "hold")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-provider.started

	if err := s.SetPermissionModeTyped(agent.PermissionModePlan); !errors.Is(err, agent.ErrPermissionModeChangeInFlight) {
		t.Fatalf("typed setter error = %v", err)
	}
	if err := s.SetPermissionMode("plan"); !errors.Is(err, agent.ErrPermissionModeChangeInFlight) {
		t.Fatalf("string setter error = %v", err)
	}
	mode, err := s.PermissionMode()
	if err != nil || mode != agent.PermissionModeDefault {
		t.Fatalf("mid-run setters changed mode to %q, %v", mode, err)
	}

	cancel()
	for range events {
	}
	if err := s.SetPermissionModeTyped(agent.PermissionModePlan); err != nil {
		t.Fatalf("setter after run: %v", err)
	}
}

func TestCancellationReservesBufferedSlotForTerminal(t *testing.T) {
	provider := &neverClosingModeProvider{started: make(chan struct{})}
	s := agent.NewSession(agent.SessionConfig{Provider: provider})
	ctx, cancel := context.WithCancel(context.Background())
	events, err := s.Run(ctx, "hold")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-provider.started

	// Do not drain the stream before cancellation. StatusThinking occupies the
	// one-slot buffer, exercising terminal delivery under backpressure.
	cancel()
	settled := false
	for i := 0; i < 100_000; i++ {
		err := s.SetPermissionModeTyped(agent.PermissionModePlan)
		if err == nil {
			settled = true
			break
		}
		if !errors.Is(err, agent.ErrPermissionModeChangeInFlight) {
			t.Fatalf("wait for cancelled run: %v", err)
		}
		runtime.Gosched()
	}
	if !settled {
		t.Fatal("cancelled run did not settle")
	}
	first, ok := <-events
	if !ok {
		t.Fatal("stream closed without a terminal event")
	}
	if first.Type != agent.RunFinishedEvent || first.RunFinished == nil || first.RunFinished.Reason != agent.StopCancelled {
		t.Fatalf("first event after cancellation = %+v, want cancelled terminal", first)
	}
	if _, ok := <-events; ok {
		t.Fatal("event arrived after the terminal")
	}
}

func TestPermissionModeReportsExternalOwnership(t *testing.T) {
	s := agent.NewSession(agent.SessionConfig{
		Provider:   immediateProvider{},
		Dispatcher: externalDispatcher{},
	})
	if _, err := s.PermissionMode(); !errors.Is(err, agent.ErrPermissionModeExternallyOwned) {
		t.Fatalf("PermissionMode error = %v", err)
	}
	if err := s.SetPermissionModeTyped(agent.PermissionModePlan); !errors.Is(err, agent.ErrPermissionModeExternallyOwned) {
		t.Fatalf("SetPermissionModeTyped error = %v", err)
	}
}

type immediateProvider struct{}

func (immediateProvider) StreamReply(context.Context, agent.Conversation, []agent.Tool, agent.StreamOptions) <-chan agent.RunEvent {
	ch := make(chan agent.RunEvent, 1)
	ch <- agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}}
	close(ch)
	return ch
}

type neverClosingModeProvider struct {
	started chan struct{}
}

func (p *neverClosingModeProvider) StreamReply(context.Context, agent.Conversation, []agent.Tool, agent.StreamOptions) <-chan agent.RunEvent {
	close(p.started)
	return make(chan agent.RunEvent)
}

type externalDispatcher struct{}

func (externalDispatcher) Dispatch(context.Context, agent.ToolCall, func(agent.RunEvent) error) (agent.ToolResult, error) {
	return agent.ToolResult{}, nil
}
