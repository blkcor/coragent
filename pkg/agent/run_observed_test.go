package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/provider/testutil"
	"github.com/blkcor/coragent/pkg/agent"
)

var (
	_ func(*agent.Session, context.Context, string) (<-chan agent.RunEvent, error)      = (*agent.Session).Run
	_ func(*agent.Session, context.Context, string) (<-chan agent.ObservedEvent, error) = (*agent.Session).RunObserved
)

func TestRunObservedTextEnvelopeAndOrder(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{
		TextDeltas: []string{"hel", "lo"},
		EndReason:  agent.Finished,
	}})
	s := agent.NewSession(agent.SessionConfig{Provider: p, SystemPrompt: "sys"})

	events := drainObserved(t, mustRunObserved(t, s, context.Background(), "hello"))
	assertObservedEnvelopes(t, events)

	wantKinds := []agent.ObservedEventKind{
		agent.ObservedKindRunStarted,
		agent.ObservedKindContextUsageUpdated,
		agent.ObservedKindStatusChanged,
		agent.ObservedKindAssistantStarted,
		agent.ObservedKindAssistantTextDelta,
		agent.ObservedKindAssistantTextDelta,
		agent.ObservedKindAssistantFinished,
		agent.ObservedKindStatusChanged,
		agent.ObservedKindRunFinished,
	}
	if got := observedKinds(events); !equalObservedKinds(got, wantKinds) {
		t.Fatalf("observed order:\nwant %v\n got %v", wantKinds, got)
	}

	if got := events[1].Payload.(*agent.ContextUsageUpdatedPayload).Usage.Source; got != agent.ContextUsageEstimated {
		t.Fatalf("initial context source: want estimated, got %q", got)
	}
	if got := events[2].Payload.(*agent.StatusChangedPayload).Status; got != agent.ActivityThinking {
		t.Fatalf("first activity: want thinking, got %q", got)
	}
	if got := events[3].Payload.(*agent.AssistantStartedPayload).Round; got != 1 {
		t.Fatalf("assistant round: want 1, got %d", got)
	}
	if got := events[6].Payload.(*agent.AssistantFinishedPayload).Reason; got != agent.ProviderTerminationStop {
		t.Fatalf("assistant termination: want stop, got %q", got)
	}
	if got := events[7].Payload.(*agent.StatusChangedPayload).Status; got != agent.ActivityIdle {
		t.Fatalf("last activity: want idle, got %q", got)
	}
	if got := events[8].Payload.(*agent.RunFinishedPayload).Outcome; got != agent.RunOutcomeCompleted {
		t.Fatalf("run outcome: want completed, got %q", got)
	}
}

func TestRunObservedLegacyToolProjectionDoesNotInventRichLifecycle(t *testing.T) {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{
			TextDeltas: []string{"checking"},
			ToolCalls: []testutil.ScriptedToolCall{{
				ID: "provider-call-1", Name: "run_command", Arguments: `{"command":"echo ok"}`,
			}},
			EndReason: agent.StoppedToCallTools,
		},
		{TextDeltas: []string{"done"}, EndReason: agent.Finished},
	})
	s := agent.NewSession(agent.SessionConfig{
		Provider:   p,
		Dispatcher: observedImmediateDispatcher{},
		Tools:      []agent.Tool{{Name: "run_command"}},
	})

	events := drainObserved(t, mustRunObserved(t, s, context.Background(), "run it"))
	assertObservedEnvelopes(t, events)

	var proposed *agent.ToolProposedPayload
	var finished *agent.ToolFinishedPayload
	for _, event := range events {
		switch event.Kind {
		case agent.ObservedKindToolProposed:
			proposed = event.Payload.(*agent.ToolProposedPayload)
		case agent.ObservedKindToolFinished:
			finished = event.Payload.(*agent.ToolFinishedPayload)
		case agent.ObservedKindToolPrepared, agent.ObservedKindToolExecuting:
			t.Fatalf("legacy ToolStartedEvent cannot prove rich fact %q", event.Kind)
		}
	}
	if proposed == nil || finished == nil {
		t.Fatalf("expected correlated proposed and finished facts, got %v", observedKinds(events))
	}
	if proposed.CallID == "" || finished.CallID != proposed.CallID {
		t.Fatalf("tool correlation mismatch: proposed=%q finished=%q", proposed.CallID, finished.CallID)
	}
	if proposed.Call.ID != "provider-call-1" || proposed.Call.ToolName != "run_command" {
		t.Fatalf("proposed call lost provider facts: %+v", proposed.Call)
	}
	if finished.Outcome != agent.ToolOutcomeSucceeded {
		t.Fatalf("tool outcome: want succeeded, got %q", finished.Outcome)
	}
}

func TestRunObservedLegacyPermissionReplyValidationAndExactlyOnce(t *testing.T) {
	dispatcher := newObservedPermissionDispatcher("command:echo ok")
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{
			ToolCalls: []testutil.ScriptedToolCall{{
				ID: "provider-call-1", Name: "run_command", Arguments: `{"command":"echo ok"}`,
			}},
			EndReason: agent.StoppedToCallTools,
		},
		{TextDeltas: []string{"done"}, EndReason: agent.Finished},
	})
	s := agent.NewSession(agent.SessionConfig{
		Provider:   p,
		Dispatcher: dispatcher,
		Tools:      []agent.Tool{{Name: "run_command"}},
	})

	stream := mustRunObserved(t, s, context.Background(), "run it")
	var events []agent.ObservedEvent
	var answered bool
	for event := range stream {
		events = append(events, event)
		if event.Kind != agent.ObservedKindPermissionRequested {
			continue
		}
		if answered {
			t.Fatal("legacy dispatcher emitted more than one permission request")
		}
		answered = true
		request := event.Payload.(*agent.PermissionRequestedPayload).Request
		assertLegacyPermissionShape(t, request)

		wrongID := permissionDecision(request, agent.PermissionReplyAllow)
		wrongID.RequestID = "wrong-request"
		assertReplyStatus(t, request.Reply(context.Background(), wrongID), agent.PermissionReplyValidationRejected)
		assertNoLegacyDecision(t, dispatcher.decisions)

		unsupported := permissionDecision(request, agent.PermissionReplyReviseArguments)
		unsupported.RevisedArguments = map[string]interface{}{"command": "echo changed"}
		assertReplyStatus(t, request.Reply(context.Background(), unsupported), agent.PermissionReplyValidationRejected)
		assertNoLegacyDecision(t, dispatcher.decisions)

		const replies = 16
		results := make(chan agent.PermissionReplyStatus, replies)
		var wg sync.WaitGroup
		for range replies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				decision := permissionDecision(request, agent.PermissionReplyAllow)
				decision.Remember = true
				results <- request.Reply(context.Background(), decision).Status
			}()
		}
		wg.Wait()
		close(results)

		accepted := 0
		alreadyResolved := 0
		for status := range results {
			switch status {
			case agent.PermissionReplyAccepted:
				accepted++
			case agent.PermissionReplyAlreadyResolved:
				alreadyResolved++
			default:
				t.Fatalf("concurrent valid reply returned %q", status)
			}
		}
		if accepted != 1 || alreadyResolved != replies-1 {
			t.Fatalf("exactly-once statuses: accepted=%d already_resolved=%d", accepted, alreadyResolved)
		}

		select {
		case legacy := <-dispatcher.decisions:
			if !legacy.Allow || !legacy.Remember {
				t.Fatalf("legacy decision was not forwarded intact: %+v", legacy)
			}
		case <-time.After(time.Second):
			t.Fatal("accepted observed reply did not reach legacy reply path")
		}
		assertReplyStatus(t, request.Reply(context.Background(), permissionDecision(request, agent.PermissionReplyDeny)), agent.PermissionReplyAlreadyResolved)
		assertNoLegacyDecision(t, dispatcher.decisions)
	}

	if !answered {
		t.Fatal("expected a permission request")
	}
	assertObservedEnvelopes(t, events)
	for _, event := range events {
		if event.Kind == agent.ObservedKindToolExecuting {
			t.Fatal("legacy permission flow must not fabricate tool_executing")
		}
	}
}

func TestRunObservedRetainedPermissionCannotReplyAfterCancellation(t *testing.T) {
	dispatcher := newObservedPermissionDispatcher("command:echo ok")
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{{
		ToolCalls: []testutil.ScriptedToolCall{{
			ID: "provider-call-1", Name: "run_command", Arguments: `{"command":"echo ok"}`,
		}},
		EndReason: agent.StoppedToCallTools,
	}})
	s := agent.NewSession(agent.SessionConfig{
		Provider:   p,
		Dispatcher: dispatcher,
		Tools:      []agent.Tool{{Name: "run_command"}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := mustRunObserved(t, s, ctx, "run it")

	var request agent.ObservedPermissionRequest
	var events []agent.ObservedEvent
	for event := range stream {
		events = append(events, event)
		if event.Kind == agent.ObservedKindPermissionRequested {
			request = event.Payload.(*agent.PermissionRequestedPayload).Request
			cancel()
		}
	}
	if request.RequestID == "" {
		t.Fatal("expected a permission request before cancellation")
	}
	assertObservedEnvelopes(t, events)
	terminal := events[len(events)-1].Payload.(*agent.RunFinishedPayload)
	if terminal.Outcome != agent.RunOutcomeCancelled {
		t.Fatalf("terminal outcome: want cancelled, got %q", terminal.Outcome)
	}
	assertReplyStatus(t, request.Reply(context.Background(), permissionDecision(request, agent.PermissionReplyAllow)), agent.PermissionReplyAlreadyResolved)
	assertNoLegacyDecision(t, dispatcher.decisions)
}

func TestRunObservedUnreadCancellationReservesGapFreeTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := agent.NewSession(agent.SessionConfig{Provider: observedBlockingProvider{}})
	stream := mustRunObserved(t, s, ctx, "cancel")
	fullDeadline := time.Now().Add(time.Second)
	for len(stream) != cap(stream) {
		if time.Now().After(fullDeadline) {
			t.Fatalf("observed stream did not reach boundary capacity: len=%d cap=%d", len(stream), cap(stream))
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if _, err := s.RunObserved(context.Background(), "must remain serialized"); !errors.Is(err, agent.ErrRunInFlight) {
		t.Fatalf("late-draining cancelled run released its guard early: %v", err)
	}

	events := drainObserved(t, stream)
	assertObservedEnvelopes(t, events)
	if len(events) < 2 || events[0].Kind != agent.ObservedKindRunStarted || events[len(events)-1].Kind != agent.ObservedKindRunFinished {
		t.Fatalf("abandoned cancellation should retain run boundaries, got %v", observedKinds(events))
	}
	if got := events[len(events)-1].Payload.(*agent.RunFinishedPayload).Outcome; got != agent.RunOutcomeCancelled {
		t.Fatalf("terminal outcome: want cancelled, got %q", got)
	}
	if err := s.SetPermissionModeTyped(agent.PermissionModeDefault); err != nil {
		t.Fatalf("drained cancelled run retained its guard: %v", err)
	}
}

func TestRunObservedAdvisoryBudgetDoesNotFabricateWindow(t *testing.T) {
	provider := &observedRichProvider{events: []agent.RichProviderEvent{
		{Type: agent.RichProviderUsage, Usage: &agent.ProviderUsage{PromptTokens: agent.OptionalUint64{Known: true, Value: 9}}},
		{Type: agent.RichProviderReplyEnded, ReplyEnded: &agent.RichReplyEnded{Reason: agent.ProviderTerminationStop}},
	}}
	session := agent.NewSession(agent.SessionConfig{Provider: provider, ContextBudgetTokens: 10})
	events := drainObserved(t, mustRunObserved(t, session, context.Background(), "hello"))
	usageCount := 0
	for _, event := range events {
		if event.Kind != agent.ObservedKindContextUsageUpdated {
			continue
		}
		usageCount++
		usage := event.Payload.(*agent.ContextUsageUpdatedPayload).Usage
		if usage.WindowTokens.Known || usage.RemainingTokens.Known {
			t.Fatalf("advisory budget fabricated a context window: %+v", usage)
		}
	}
	if usageCount != 2 {
		t.Fatalf("usage snapshots = %d, want estimate and provider", usageCount)
	}
}

func TestRunAndRunObservedShareOneInFlightGuard(t *testing.T) {
	t.Run("observed blocks legacy", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := agent.NewSession(agent.SessionConfig{Provider: observedBlockingProvider{}})
		observed := mustRunObserved(t, s, ctx, "first")
		if _, err := s.Run(context.Background(), "second"); !errors.Is(err, agent.ErrRunInFlight) {
			t.Fatalf("legacy Run during RunObserved: want ErrRunInFlight, got %v", err)
		}
		cancel()
		drainObserved(t, observed)
	})

	t.Run("legacy blocks observed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := agent.NewSession(agent.SessionConfig{Provider: observedBlockingProvider{}})
		legacy, err := s.Run(ctx, "first")
		if err != nil {
			t.Fatalf("legacy Run: %v", err)
		}
		if _, err := s.RunObserved(context.Background(), "second"); !errors.Is(err, agent.ErrRunInFlight) {
			t.Fatalf("RunObserved during legacy Run: want ErrRunInFlight, got %v", err)
		}
		cancel()
		for range legacy {
		}
	})
}

func TestRunObservedRichUsagePrecedenceAndOmissionOrdering(t *testing.T) {
	tests := []struct {
		name         string
		ending       agent.ProviderTerminationReason
		wantOmission agent.OmissionKind
		continuation agent.ContinuationMode
	}{
		{"length", agent.ProviderTerminationLength, agent.OmissionProviderLength, agent.ContinuationNewUserTurn},
		{"content filter", agent.ProviderTerminationContentFilter, agent.OmissionContentFilter, agent.ContinuationUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &observedRichProvider{events: []agent.RichProviderEvent{
				{Type: agent.RichProviderTextDelta, TextDelta: "partial"},
				{Type: agent.RichProviderUsage, Usage: &agent.ProviderUsage{
					PromptTokens:        agent.OptionalUint64{Known: true, Value: 9},
					CompletionTokens:    agent.OptionalUint64{Known: true, Value: 2},
					ContextWindowTokens: agent.OptionalUint64{Known: true, Value: 20},
				}},
				{Type: agent.RichProviderReplyEnded, ReplyEnded: &agent.RichReplyEnded{Reason: test.ending}},
			}}
			session := agent.NewSession(agent.SessionConfig{Provider: provider, ContextBudgetTokens: 10})
			events := drainObserved(t, mustRunObserved(t, session, context.Background(), "hello"))
			assertObservedEnvelopes(t, events)
			if provider.richCalls != 1 || provider.legacyCalls != 0 {
				t.Fatalf("provider selection rich=%d legacy=%d", provider.richCalls, provider.legacyCalls)
			}

			var usages []agent.ContextUsage
			omissionIndex, terminalIndex := -1, -1
			for index, event := range events {
				switch event.Kind {
				case agent.ObservedKindContextUsageUpdated:
					usages = append(usages, event.Payload.(*agent.ContextUsageUpdatedPayload).Usage)
				case agent.ObservedKindOmissionReported:
					omission := event.Payload.(*agent.OmissionReportedPayload).Omission
					if omission.Kind == test.wantOmission {
						omissionIndex = index
						if omission.Continuation != test.continuation || omission.Recoverability != agent.RecoverabilityUnrecoverable {
							t.Fatalf("omission = %+v", omission)
						}
					}
				case agent.ObservedKindRunFinished:
					terminalIndex = index
				}
			}
			if len(usages) != 2 || usages[0].Source != agent.ContextUsageEstimated || usages[1].Source != agent.ContextUsageProvider || usages[1].UsedTokens != 9 || !usages[1].WindowTokens.Known || usages[1].WindowTokens.Value != 20 {
				t.Fatalf("usage precedence = %+v", usages)
			}
			if omissionIndex < 0 || terminalIndex < 0 || omissionIndex >= terminalIndex {
				t.Fatalf("omission/terminal order = %d/%d", omissionIndex, terminalIndex)
			}
			for _, event := range events {
				if event.Kind == agent.ObservedKindOmissionReported && event.Payload.(*agent.OmissionReportedPayload).Omission.Kind == agent.OmissionContextCompaction {
					t.Fatal("Phase 7 fabricated context compaction")
				}
			}
		})
	}
}

func TestRunObservedMalformedProviderUsageKeepsEstimate(t *testing.T) {
	provider := &observedRichProvider{events: []agent.RichProviderEvent{
		{Type: agent.RichProviderUsage, Usage: &agent.ProviderUsage{CompletionTokens: agent.OptionalUint64{Known: true, Value: 3}}},
		{Type: agent.RichProviderReplyEnded, ReplyEnded: &agent.RichReplyEnded{Reason: agent.ProviderTerminationStop}},
	}}
	session := agent.NewSession(agent.SessionConfig{Provider: provider})
	events := drainObserved(t, mustRunObserved(t, session, context.Background(), "hello"))
	usageCount, warningCount := 0, 0
	for _, event := range events {
		switch event.Kind {
		case agent.ObservedKindContextUsageUpdated:
			usageCount++
			usage := event.Payload.(*agent.ContextUsageUpdatedPayload).Usage
			if usage.Source != agent.ContextUsageEstimated || usage.WindowTokens.Known || usage.RemainingTokens.Known {
				t.Fatalf("malformed provider usage replaced estimate: %+v", usage)
			}
		case agent.ObservedKindWarning:
			if event.Payload.(*agent.WarningPayload).Code == "invalid_provider_usage" {
				warningCount++
			}
		}
	}
	if usageCount != 1 || warningCount != 1 {
		t.Fatalf("usage=%d warning=%d, want 1/1", usageCount, warningCount)
	}
}

func TestRunObservedStandardPermissionRevisionRepreparesBeforeAllow(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "created.txt")
	provider := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{
			ToolCalls: []testutil.ScriptedToolCall{{
				ID: "provider-call-1", Name: "write_file",
				Arguments: `{"path":` + quotedJSON(path) + `,"content":"before"}`,
			}},
			EndReason: agent.StoppedToCallTools,
		},
		{TextDeltas: []string{"done"}, EndReason: agent.Finished},
	})
	session := agent.NewSession(agent.SessionConfig{Provider: provider, WorkingDirectory: directory})
	stream := mustRunObserved(t, session, context.Background(), "create it")
	var requests []agent.ObservedPermissionRequest
	var preparedRevisions []agent.PreviewRevision
	var events []agent.ObservedEvent
	for event := range stream {
		events = append(events, event)
		switch event.Kind {
		case agent.ObservedKindToolPrepared:
			prepared := event.Payload.(*agent.ToolPreparedPayload)
			preparedRevisions = append(preparedRevisions, prepared.Revision)
		case agent.ObservedKindPermissionRequested:
			request := event.Payload.(*agent.PermissionRequestedPayload).Request
			requests = append(requests, request)
			if request.Protocol != agent.PermissionProtocolRich || request.Preview.Kind != agent.ActionPreviewFileDiff || !request.Capabilities.Preview || !request.Capabilities.ReviseArguments {
				t.Fatalf("request shape = %+v", request)
			}
			if len(requests) == 1 {
				invalid := permissionDecision(request, agent.PermissionReplyReviseArguments)
				invalid.RevisedArguments = map[string]interface{}{"path": path}
				assertReplyStatus(t, request.Reply(context.Background(), invalid), agent.PermissionReplyValidationRejected)
				revision := permissionDecision(request, agent.PermissionReplyReviseArguments)
				revision.RevisedArguments = map[string]interface{}{"path": path, "content": "after"}
				assertReplyStatus(t, request.Reply(context.Background(), revision), agent.PermissionReplyAccepted)
			} else {
				if request.Revision != 2 || request.RequestID == requests[0].RequestID || request.EffectiveCall.Arguments["content"] != "after" {
					t.Fatalf("replacement request = %+v", request)
				}
				assertReplyStatus(t, request.Reply(context.Background(), permissionDecision(request, agent.PermissionReplyAllow)), agent.PermissionReplyAccepted)
			}
		}
	}
	assertObservedEnvelopes(t, events)
	if len(requests) != 2 || len(preparedRevisions) != 2 || preparedRevisions[0] != 1 || preparedRevisions[1] != 2 {
		t.Fatalf("requests=%d prepared=%v", len(requests), preparedRevisions)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "after" {
		t.Fatalf("committed content=%q err=%v", content, err)
	}
}

type observedImmediateDispatcher struct{}

func (observedImmediateDispatcher) Dispatch(_ context.Context, call agent.ToolCall, _ func(agent.RunEvent) error) (agent.ToolResult, error) {
	return agent.ToolResult{ToolCallID: call.ID, Result: "ok"}, nil
}

type observedPermissionDispatcher struct {
	rememberedRule string
	decisions      chan agent.PermissionDecision
}

func newObservedPermissionDispatcher(rememberedRule string) *observedPermissionDispatcher {
	return &observedPermissionDispatcher{
		rememberedRule: rememberedRule,
		decisions:      make(chan agent.PermissionDecision, 4),
	}
}

func (d *observedPermissionDispatcher) Dispatch(ctx context.Context, call agent.ToolCall, emit func(agent.RunEvent) error) (agent.ToolResult, error) {
	reply := make(chan agent.PermissionDecision, 1)
	if err := emit(agent.RunEvent{
		Type: agent.PermissionRequestedEvent,
		Permission: &agent.PermissionRequest{
			ToolCall:       call,
			Reason:         "approval required",
			RememberedRule: d.rememberedRule,
			ReplyPath:      reply,
		},
	}); err != nil {
		return agent.ToolResult{ToolCallID: call.ID, IsError: true, Result: err.Error()}, err
	}
	select {
	case decision := <-reply:
		d.decisions <- decision
		if !decision.Allow {
			return agent.ToolResult{ToolCallID: call.ID, IsError: true, Result: "permission denied"}, nil
		}
		return agent.ToolResult{ToolCallID: call.ID, Result: "ok"}, nil
	case <-ctx.Done():
		return agent.ToolResult{ToolCallID: call.ID, IsError: true, Result: ctx.Err().Error()}, ctx.Err()
	}
}

type observedBlockingProvider struct{}

func (observedBlockingProvider) StreamReply(ctx context.Context, _ agent.Conversation, _ []agent.Tool, _ agent.StreamOptions) <-chan agent.RunEvent {
	stream := make(chan agent.RunEvent)
	go func() {
		defer close(stream)
		<-ctx.Done()
	}()
	return stream
}

type observedRichProvider struct {
	events      []agent.RichProviderEvent
	richCalls   int
	legacyCalls int
}

func (p *observedRichProvider) StreamReply(context.Context, agent.Conversation, []agent.Tool, agent.StreamOptions) <-chan agent.RunEvent {
	p.legacyCalls++
	stream := make(chan agent.RunEvent)
	close(stream)
	return stream
}

func (p *observedRichProvider) StreamRichReply(ctx context.Context, _ agent.Conversation, _ []agent.Tool, _ agent.StreamOptions) <-chan agent.RichProviderEvent {
	p.richCalls++
	stream := make(chan agent.RichProviderEvent)
	go func() {
		defer close(stream)
		for _, event := range p.events {
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return stream
}

func mustRunObserved(t *testing.T, s *agent.Session, ctx context.Context, input string) <-chan agent.ObservedEvent {
	t.Helper()
	stream, err := s.RunObserved(ctx, input)
	if err != nil {
		t.Fatalf("RunObserved: %v", err)
	}
	return stream
}

func drainObserved(t *testing.T, stream <-chan agent.ObservedEvent) []agent.ObservedEvent {
	t.Helper()
	done := make(chan []agent.ObservedEvent, 1)
	go func() {
		var events []agent.ObservedEvent
		for event := range stream {
			events = append(events, event)
		}
		done <- events
	}()
	select {
	case events := <-done:
		return events
	case <-time.After(2 * time.Second):
		t.Fatal("observed stream did not close")
		return nil
	}
}

func assertObservedEnvelopes(t *testing.T, events []agent.ObservedEvent) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("observed stream was empty")
	}
	runID := events[0].RunID
	origin := events[0].Origin
	terminals := 0
	for i, event := range events {
		if err := event.Validate(); err != nil {
			t.Fatalf("event %d (%s) failed validation: %v", i, event.Kind, err)
		}
		if event.RunID != runID {
			t.Fatalf("event %d changed run ID: %q != %q", i, event.RunID, runID)
		}
		if event.Origin != origin {
			t.Fatalf("event %d changed origin: %+v != %+v", i, event.Origin, origin)
		}
		if want := uint64(i + 1); event.Sequence != want {
			t.Fatalf("event %d sequence: want %d, got %d", i, want, event.Sequence)
		}
		if event.Kind == agent.ObservedKindRunFinished {
			terminals++
			if i != len(events)-1 {
				t.Fatalf("run_finished at index %d was not last", i)
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("want exactly one terminal, got %d", terminals)
	}
}

func assertLegacyPermissionShape(t *testing.T, request agent.ObservedPermissionRequest) {
	t.Helper()
	if request.RequestID == "" || request.CallID == "" || request.Revision != 1 {
		t.Fatalf("missing legacy correlation: %+v", request)
	}
	if request.Protocol != agent.PermissionProtocolLegacyOneShot {
		t.Fatalf("protocol: want legacy_one_shot, got %q", request.Protocol)
	}
	if request.Action != agent.ActionCommand {
		t.Fatalf("run_command action: want command, got %v", request.Action)
	}
	if !request.Capabilities.Allow || !request.Capabilities.Deny || !request.Capabilities.Remember {
		t.Fatalf("legacy capabilities lost supported operations: %+v", request.Capabilities)
	}
	if request.Capabilities.ReviseArguments || request.Capabilities.SchemaAwareEdit || request.Capabilities.Preview || request.Capabilities.SandboxGrants {
		t.Fatalf("legacy request advertised unsupported rich operations: %+v", request.Capabilities)
	}
	if request.Preview.Kind != agent.ActionPreviewUnavailable || request.RememberedScope == nil {
		t.Fatalf("legacy preview/scope should be explicit: preview=%+v scope=%+v", request.Preview, request.RememberedScope)
	}
}

func permissionDecision(request agent.ObservedPermissionRequest, action agent.PermissionReplyAction) agent.ObservedPermissionDecision {
	return agent.ObservedPermissionDecision{
		RequestID: request.RequestID,
		CallID:    request.CallID,
		Revision:  request.Revision,
		Action:    action,
	}
}

func assertReplyStatus(t *testing.T, result agent.PermissionReplyResult, want agent.PermissionReplyStatus) {
	t.Helper()
	if result.Status != want {
		t.Fatalf("reply status: want %q, got %q (%+v)", want, result.Status, result.Feedback)
	}
}

func assertNoLegacyDecision(t *testing.T, decisions <-chan agent.PermissionDecision) {
	t.Helper()
	select {
	case decision := <-decisions:
		t.Fatalf("unexpected legacy decision: %+v", decision)
	default:
	}
}

func observedKinds(events []agent.ObservedEvent) []agent.ObservedEventKind {
	out := make([]agent.ObservedEventKind, len(events))
	for i, event := range events {
		out[i] = event.Kind
	}
	return out
}

func equalObservedKinds(a, b []agent.ObservedEventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func quotedJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
