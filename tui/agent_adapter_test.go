package tui

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blkcor/coragent/pkg/agent"
)

func TestAgentSessionAdapterDescribeModeAndClose(t *testing.T) {
	workingDirectory := t.TempDir()
	session := agent.NewSession(agent.SessionConfig{
		Provider:         finalTextProvider{},
		WorkingDirectory: workingDirectory,
		PermissionMode:   string(agent.PermissionModePlan),
		StreamOptions:    agent.StreamOptions{Model: "gpt-adapter-test"},
	})
	adapter := NewAgentSessionAdapter(session)

	info, err := adapter.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if info.Project != workingDirectory || info.Model != "gpt-adapter-test" {
		t.Fatalf("descriptor projection = %+v", info)
	}
	if info.Mode != ModePlan || !info.ModeChangeable {
		t.Fatalf("mode projection = %q changeable=%v", info.Mode, info.ModeChangeable)
	}
	if info.Sandbox == "" {
		t.Fatal("sandbox posture was omitted")
	}

	if err := adapter.SetMode(context.Background(), ModeDefault); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	info, err = adapter.Describe(context.Background())
	if err != nil || info.Mode != ModeDefault {
		t.Fatalf("Describe after SetMode = %+v, %v", info, err)
	}
	if err := adapter.SetMode(context.Background(), ModeExternal); err == nil {
		t.Fatal("external ownership state was accepted as a selectable mode")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Describe(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Describe cancelled error = %v", err)
	}
	if err := adapter.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestAgentSessionAdapterProjectsRunAndBindsLivePermissionReply(t *testing.T) {
	provider := &permissionScriptProvider{}
	tool := &adapterActionTool{}
	session := agent.NewSession(agent.SessionConfig{
		Provider:         provider,
		WorkingDirectory: t.TempDir(),
		ToolHandlers:     []agent.ToolHandler{tool},
	})
	adapter := NewAgentSessionAdapter(session)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := adapter.Run(ctx, "request permission")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var (
		permissionCount int
		toolStarted     bool
		toolFinished    bool
		terminal        RunOutcome
	)
	for event := range stream {
		switch event.Kind {
		case EventToolStarted:
			toolStarted = true
			if event.ToolName != "adapter_action" || event.CallID == "" {
				t.Fatalf("tool projection = %+v", event)
			}
		case EventPermissionRequested:
			permissionCount++
			if event.Permission == nil || event.Permission.Reply == nil {
				t.Fatal("permission projection lost the live reply path")
			}
			if event.Permission.Tool != "adapter_action" || event.Permission.Origin != "root agent" {
				t.Fatalf("permission prompt = %+v", event.Permission)
			}
			first, err := event.Permission.Reply(ctx, DecisionDenyOnce)
			if err != nil || first.Status != ReplyAccepted {
				t.Fatalf("first reply = %+v, %v", first, err)
			}
			second, err := event.Permission.Reply(ctx, DecisionDenyOnce)
			if err != nil || second.Status != ReplyAlreadyResolved {
				t.Fatalf("duplicate reply = %+v, %v", second, err)
			}
		case EventToolFinished:
			toolFinished = true
			if event.Tool != ToolDenied {
				t.Fatalf("denied tool outcome = %q", event.Tool)
			}
		case EventRunFinished:
			terminal = event.Terminal
		}
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("run did not finish before timeout: %v", err)
	}
	if permissionCount != 1 || !toolStarted || !toolFinished || terminal != RunCompleted {
		t.Fatalf("projection summary: permissions=%d started=%v finished=%v terminal=%q", permissionCount, toolStarted, toolFinished, terminal)
	}
	if tool.executions.Load() != 0 {
		t.Fatalf("denied action executed %d times", tool.executions.Load())
	}
}

func TestProjectObservedRichFactsRemainTyped(t *testing.T) {
	runID := agent.RunID("run-rich-projection")
	tests := []struct {
		name  string
		event agent.ObservedEvent
		check func(t *testing.T, event UIEvent)
	}{
		{
			name: "reasoning summary",
			event: agent.ObservedEvent{RunID: runID, Kind: agent.ObservedKindAssistantReasoningSummaryDelta,
				Payload: &agent.AssistantReasoningSummaryDeltaPayload{Round: 2, Delta: "safe summary"}},
			check: func(t *testing.T, event UIEvent) {
				if event.Kind != EventAssistantReasoningSummaryDelta || event.AssistantID == "" || event.Text != "safe summary" {
					t.Fatalf("reasoning projection = %+v", event)
				}
			},
		},
		{
			name: "prepared diff",
			event: agent.ObservedEvent{RunID: runID, Kind: agent.ObservedKindToolPrepared,
				Payload: &agent.ToolPreparedPayload{CallID: "call", Revision: 3,
					EffectiveCall: agent.ToolCall{ToolName: "edit_file", Arguments: map[string]interface{}{"path": "a.txt"}},
					Preview: agent.ActionPreview{Kind: agent.ActionPreviewFileDiff, Operation: agent.ActionOperationModify,
						FileDiff: &agent.FileDiffPreview{Path: "a.txt", AddedLines: agent.OptionalUint64{Known: true, Value: 1}}}}},
			check: func(t *testing.T, event UIEvent) {
				if event.Kind != EventToolPrepared || event.Revision != 3 || event.Preview == nil || event.Preview.FileDiff == nil || event.Preview.FileDiff.Path != "a.txt" {
					t.Fatalf("prepared projection = %+v", event)
				}
			},
		},
		{
			name: "usage",
			event: agent.ObservedEvent{RunID: runID, Kind: agent.ObservedKindContextUsageUpdated,
				Payload: &agent.ContextUsageUpdatedPayload{Usage: agent.ContextUsage{Round: 1, Source: agent.ContextUsageProvider, UsedTokens: 9, WindowTokens: agent.OptionalUint64{Known: true, Value: 10}}}},
			check: func(t *testing.T, event UIEvent) {
				if event.Kind != EventContextUsage || event.Usage == nil || event.Usage.Source != "provider" || event.Usage.Window.Value != 10 {
					t.Fatalf("usage projection = %+v", event)
				}
			},
		},
		{
			name: "omission",
			event: agent.ObservedEvent{RunID: runID, Kind: agent.ObservedKindOmissionReported,
				Payload: &agent.OmissionReportedPayload{Omission: agent.Omission{Kind: agent.OmissionPreviewBudget, Scope: agent.OmissionScopeActionPreview, CallID: "call"}}},
			check: func(t *testing.T, event UIEvent) {
				if event.Kind != EventOmission || event.Omission == nil || event.Omission.Kind != "preview_budget" || event.CallID != "call" {
					t.Fatalf("omission projection = %+v", event)
				}
			},
		},
		{
			name: "hook",
			event: agent.ObservedEvent{RunID: runID, Kind: agent.ObservedKindHookOutcome,
				Payload: &agent.HookOutcomePayload{CallID: "call", Outcome: agent.HookOutcome{HookName: "guard", Moment: agent.HookBeforeTool, Action: agent.HookBlocked, Reason: "no"}}},
			check: func(t *testing.T, event UIEvent) {
				if event.Kind != EventHookOutcome || event.Hook == nil || event.Hook.Action != "blocked" {
					t.Fatalf("hook projection = %+v", event)
				}
			},
		},
		{
			name: "subagent",
			event: agent.ObservedEvent{RunID: runID, Kind: agent.ObservedKindSubagentStarted,
				Payload: &agent.SubagentStartedPayload{Agent: agent.SubagentProvenance{AgentID: "child", ParentAgentID: "root", DelegationCallID: "delegate", Label: "review", Depth: 1}}},
			check: func(t *testing.T, event UIEvent) {
				if event.Kind != EventSubagentStarted || event.Subagent == nil || event.Subagent.AgentID != "child" || event.Subagent.ParentAgentID != "root" {
					t.Fatalf("subagent projection = %+v", event)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projected, present, err := projectObservedEvent(test.event)
			if err != nil || !present {
				t.Fatalf("projectObservedEvent = %+v, present=%v, err=%v", projected, present, err)
			}
			test.check(t, projected)
		})
	}
}

func TestAgentSessionAdapterCancellationStillDeliversTerminal(t *testing.T) {
	session := agent.NewSession(agent.SessionConfig{Provider: neverClosingAdapterProvider{}})
	adapter := NewAgentSessionAdapter(session)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := adapter.Run(ctx, "cancel me")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-stream: // run_started
	case <-time.After(time.Second):
		t.Fatal("run did not start")
	}
	cancel()

	deadline := time.After(2 * time.Second)
	var terminal RunOutcome
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				if terminal != RunCancelled {
					t.Fatalf("terminal = %q, want cancelled", terminal)
				}
				return
			}
			if event.Kind == EventRunFinished {
				terminal = event.Terminal
			}
		case <-deadline:
			t.Fatal("cancelled observed stream did not close")
		}
	}
}

func TestAgentSessionAdapterImmediateCancellationRetainsRunBoundaries(t *testing.T) {
	session := agent.NewSession(agent.SessionConfig{Provider: neverClosingAdapterProvider{}})
	adapter := NewAgentSessionAdapter(session)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := adapter.Run(ctx, "cancel immediately")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	cancel()

	var events []UIEvent
	for event := range stream {
		events = append(events, event)
	}
	if len(events) < 2 || events[0].Kind != EventRunStarted || events[len(events)-1].Kind != EventRunFinished {
		t.Fatalf("immediate cancellation events = %+v", events)
	}
	if events[len(events)-1].Terminal != RunCancelled {
		t.Fatalf("terminal = %q, want cancelled", events[len(events)-1].Terminal)
	}
	for _, event := range events {
		if event.Kind == eventObservedProtocolError {
			t.Fatalf("normal immediate cancellation became a protocol error: %+v", events)
		}
	}
}

func TestBridgeValidatesEveryEnvelopeAndSafeDrainsToTerminal(t *testing.T) {
	source := make(chan agent.ObservedEvent, 2)
	source <- agent.ObservedEvent{SchemaVersion: 99}
	source <- observedFixture(2, agent.ObservedKindRunFinished, &agent.RunFinishedPayload{Outcome: agent.RunOutcomeFailed})
	close(source)

	projected := bridgeObservedEvents(context.Background(), source)
	first, ok := <-projected
	if !ok || first.Kind != eventObservedProtocolError {
		t.Fatalf("first projected event = %+v, %v", first, ok)
	}
	terminal, ok := <-projected
	if !ok || terminal.Kind != EventRunFinished || terminal.Terminal != RunFailed {
		t.Fatalf("terminal after rejection = %+v, %v", terminal, ok)
	}
	if _, ok := <-projected; ok {
		t.Fatal("bridge did not close after safe drain")
	}
}

func TestBridgePreservesBackpressureUntilCancellation(t *testing.T) {
	source := make(chan agent.ObservedEvent)
	projected := bridgeObservedEvents(context.Background(), source)

	sentFirst := sendObserved(source, observedFixture(1, agent.ObservedKindRunStarted, &agent.RunStartedPayload{}))
	waitClosed(t, sentFirst, "first source send")
	sentSecond := sendObserved(source, observedFixture(2, agent.ObservedKindStatusChanged, &agent.StatusChangedPayload{Status: agent.ActivityThinking}))
	waitClosed(t, sentSecond, "second source send")
	sentThird := sendObserved(source, observedFixture(3, agent.ObservedKindStatusChanged, &agent.StatusChangedPayload{Status: agent.ActivityIdle}))
	waitClosed(t, sentThird, "third source send")
	sentFourth := sendObserved(source, observedFixture(4, agent.ObservedKindStatusChanged, &agent.StatusChangedPayload{Status: agent.ActivityThinking}))
	select {
	case <-sentFourth:
		t.Fatal("bridge consumed a fourth source item while its two-item output was backpressured")
	case <-time.After(50 * time.Millisecond):
	}

	if event := <-projected; event.Kind != EventRunStarted {
		t.Fatalf("first event = %+v", event)
	}
	waitClosed(t, sentFourth, "fourth source send after output drain")
	if event := <-projected; event.Activity != ActivityThinking {
		t.Fatalf("second event = %+v", event)
	}
	if event := <-projected; event.Activity != ActivityIdle {
		t.Fatalf("third event = %+v", event)
	}
	if event := <-projected; event.Activity != ActivityThinking {
		t.Fatalf("fourth event = %+v", event)
	}

	sentTerminal := sendObserved(source, observedFixture(5, agent.ObservedKindRunFinished, &agent.RunFinishedPayload{Outcome: agent.RunOutcomeCompleted}))
	waitClosed(t, sentTerminal, "terminal source send")
	close(source)
	if event := <-projected; event.Kind != EventRunFinished {
		t.Fatalf("terminal event = %+v", event)
	}
	if _, ok := <-projected; ok {
		t.Fatal("projected stream did not close")
	}
}

func observedFixture(sequence uint64, kind agent.ObservedEventKind, payload agent.ObservedEventPayload) agent.ObservedEvent {
	return agent.ObservedEvent{
		SchemaVersion: agent.ObservedSchemaV1,
		RunID:         "run-adapter",
		Sequence:      sequence,
		Timestamp:     time.Unix(int64(sequence), 0).UTC(),
		Origin:        agent.Origin{AgentID: "agent-root"},
		Kind:          kind,
		Payload:       payload,
	}
}

func sendObserved(target chan<- agent.ObservedEvent, event agent.ObservedEvent) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		target <- event
		close(done)
	}()
	return done
}

func waitClosed(t *testing.T, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

type finalTextProvider struct{}

func (finalTextProvider) StreamReply(ctx context.Context, _ agent.Conversation, _ []agent.Tool, _ agent.StreamOptions) <-chan agent.RunEvent {
	return providerEvents(ctx,
		agent.RunEvent{Type: agent.TextDelta, TextDelta: "done"},
		agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}},
	)
}

type permissionScriptProvider struct {
	mu    sync.Mutex
	round int
}

func (provider *permissionScriptProvider) StreamReply(ctx context.Context, _ agent.Conversation, _ []agent.Tool, _ agent.StreamOptions) <-chan agent.RunEvent {
	provider.mu.Lock()
	provider.round++
	round := provider.round
	provider.mu.Unlock()
	if round == 1 {
		return providerEvents(ctx,
			agent.RunEvent{Type: agent.ToolCallEvent, ToolCall: &agent.ToolCall{
				ID:        "provider-call",
				ToolName:  "adapter_action",
				Arguments: map[string]interface{}{"value": "test"},
			}},
			agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.StoppedToCallTools}},
		)
	}
	return providerEvents(ctx,
		agent.RunEvent{Type: agent.TextDelta, TextDelta: "denial recorded"},
		agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}},
	)
}

func providerEvents(ctx context.Context, events ...agent.RunEvent) <-chan agent.RunEvent {
	stream := make(chan agent.RunEvent)
	go func() {
		defer close(stream)
		for _, event := range events {
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return stream
}

type neverClosingAdapterProvider struct{}

func (neverClosingAdapterProvider) StreamReply(context.Context, agent.Conversation, []agent.Tool, agent.StreamOptions) <-chan agent.RunEvent {
	return make(chan agent.RunEvent)
}

type adapterActionTool struct{ executions atomic.Int32 }

func (*adapterActionTool) Descriptor() agent.Tool {
	return agent.Tool{
		Name:        "adapter_action",
		Description: "Exercise the production TUI permission adapter.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`),
	}
}

func (tool *adapterActionTool) Execute(context.Context, map[string]interface{}) (string, error) {
	tool.executions.Add(1)
	return "executed", nil
}

func (*adapterActionTool) RunsCommands() bool           { return false }
func (*adapterActionTool) ActionKind() agent.ActionKind { return agent.ActionCommand }
