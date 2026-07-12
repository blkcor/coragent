package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/blkcor/coragent/pkg/agent"
)

var (
	_ agent.Provider = (*legacyCompatibilityProvider)(nil)

	_ func(agent.SessionConfig) *agent.Session                                     = agent.NewSession
	_ func(agent.SessionConfig) (*agent.Session, error)                            = agent.NewSessionWithError
	_ func(*agent.Session, context.Context, string) (<-chan agent.RunEvent, error) = (*agent.Session).Run
)

func TestLegacySDKClientReceivesUnchangedEventContract(t *testing.T) {
	tests := []struct {
		name       string
		provider   []agent.RunEvent
		wantTypes  []agent.RunEventType
		wantReason agent.StopReason
	}{
		{
			name: "completed text",
			provider: []agent.RunEvent{
				{Type: agent.TextDelta, TextDelta: "legacy reply"},
				{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}},
			},
			wantTypes:  []agent.RunEventType{agent.StatusChange, agent.TextDelta, agent.StatusChange, agent.RunFinishedEvent},
			wantReason: agent.StopCompleted,
		},
		{
			name: "provider failure",
			provider: []agent.RunEvent{
				{Type: agent.ErrorEvent, Error: errors.New("scripted provider failure")},
			},
			wantTypes:  []agent.RunEventType{agent.StatusChange, agent.StatusChange, agent.RunFinishedEvent},
			wantReason: agent.StopFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &legacyCompatibilityProvider{events: test.provider}
			session := agent.NewSession(agent.SessionConfig{Provider: provider, SystemPrompt: "legacy system"})
			stream, err := session.Run(context.Background(), "legacy user input")
			if err != nil {
				t.Fatalf("legacy Run: %v", err)
			}
			events := drain(t, stream)
			if len(events) != len(test.wantTypes) {
				t.Fatalf("event count = %d, want %d: %v", len(events), len(test.wantTypes), typesOf(events))
			}
			for index, want := range test.wantTypes {
				if events[index].Type != want {
					t.Errorf("event %d type = %v, want %v", index, events[index].Type, want)
				}
			}
			terminal := events[len(events)-1]
			if terminal.RunFinished == nil || terminal.RunFinished.Reason != test.wantReason {
				t.Fatalf("terminal = %+v, want reason %v", terminal.RunFinished, test.wantReason)
			}
		})
	}
}

func TestLegacyRunDoesNotSelectOptionalRichProvider(t *testing.T) {
	provider := &dualProtocolProvider{}
	legacySession := agent.NewSession(agent.SessionConfig{Provider: provider})
	legacy, err := legacySession.Run(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("legacy Run: %v", err)
	}
	drain(t, legacy)
	if provider.legacyCalls != 1 || provider.richCalls != 0 {
		t.Fatalf("legacy Run selected provider paths legacy=%d rich=%d", provider.legacyCalls, provider.richCalls)
	}

	observedSession := agent.NewSession(agent.SessionConfig{Provider: provider})
	observed, err := observedSession.RunObserved(context.Background(), "observed")
	if err != nil {
		t.Fatalf("RunObserved: %v", err)
	}
	for range observed {
	}
	if provider.legacyCalls != 1 || provider.richCalls != 1 {
		t.Fatalf("RunObserved selected provider paths legacy=%d rich=%d", provider.legacyCalls, provider.richCalls)
	}
}

// legacyCompatibilityProvider intentionally implements only the original
// required Provider method. RunObserved remains additive and does not force an
// existing provider to grow a rich-stream method.
type legacyCompatibilityProvider struct {
	events []agent.RunEvent
}

type dualProtocolProvider struct {
	legacyCalls int
	richCalls   int
}

func (provider *dualProtocolProvider) StreamReply(context.Context, agent.Conversation, []agent.Tool, agent.StreamOptions) <-chan agent.RunEvent {
	provider.legacyCalls++
	stream := make(chan agent.RunEvent, 1)
	stream <- agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}}
	close(stream)
	return stream
}

func (provider *dualProtocolProvider) StreamRichReply(context.Context, agent.Conversation, []agent.Tool, agent.StreamOptions) <-chan agent.RichProviderEvent {
	provider.richCalls++
	stream := make(chan agent.RichProviderEvent, 1)
	stream <- agent.RichProviderEvent{Type: agent.RichProviderReplyEnded, ReplyEnded: &agent.RichReplyEnded{Reason: agent.ProviderTerminationStop}}
	close(stream)
	return stream
}

func (provider *legacyCompatibilityProvider) StreamReply(ctx context.Context, _ agent.Conversation, _ []agent.Tool, _ agent.StreamOptions) <-chan agent.RunEvent {
	stream := make(chan agent.RunEvent)
	go func() {
		defer close(stream)
		for _, event := range provider.events {
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return stream
}
