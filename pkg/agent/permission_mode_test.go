package agent_test

import (
	"context"
	"errors"
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

func TestBothPermissionModeSettersApplyMidRun(t *testing.T) {
	provider := &neverClosingModeProvider{started: make(chan struct{})}
	s := agent.NewSession(agent.SessionConfig{Provider: provider})
	ctx, cancel := context.WithCancel(context.Background())
	events, err := s.Run(ctx, "hold")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-provider.started

	if err := s.SetPermissionModeTyped(agent.PermissionModePlan); err != nil {
		t.Fatalf("typed setter: %v", err)
	}
	mode, err := s.PermissionMode()
	if err != nil || mode != agent.PermissionModePlan {
		t.Fatalf("typed setter changed mode to %q, %v", mode, err)
	}
	if err := s.SetPermissionMode("bypass"); err != nil {
		t.Fatalf("string setter: %v", err)
	}
	mode, err = s.PermissionMode()
	if err != nil || mode != agent.PermissionModeBypass {
		t.Fatalf("string setter changed mode to %q, %v", mode, err)
	}

	cancel()
	for range events {
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
	terminal := 0
	for event := range events {
		if event.Type == agent.RunFinishedEvent {
			terminal++
			if event.RunFinished == nil || event.RunFinished.Reason != agent.StopCancelled {
				t.Fatalf("terminal = %+v, want cancelled", event)
			}
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal count = %d, want 1", terminal)
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
