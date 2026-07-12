package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestObservedSchemaV1ClosedKindsAndPayloads(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	tests := []struct {
		kind    ObservedEventKind
		payload ObservedEventPayload
	}{
		{ObservedKindRunStarted, &RunStartedPayload{}},
		{ObservedKindStatusChanged, &StatusChangedPayload{Status: ActivityThinking}},
		{ObservedKindAssistantStarted, &AssistantStartedPayload{Round: 1}},
		{ObservedKindAssistantTextDelta, &AssistantTextDeltaPayload{Round: 1, Delta: "hello"}},
		{ObservedKindAssistantReasoningSummaryDelta, &AssistantReasoningSummaryDeltaPayload{Round: 1, Delta: "summary"}},
		{ObservedKindAssistantFinished, &AssistantFinishedPayload{Round: 1, Reason: ProviderTerminationStop}},
		{ObservedKindToolProposed, &ToolProposedPayload{Round: 1, CallID: "call-1"}},
		{ObservedKindToolPrepared, &ToolPreparedPayload{CallID: "call-1", Revision: 1}},
		{ObservedKindPermissionRequested, &PermissionRequestedPayload{Request: ObservedPermissionRequest{RequestID: "request-1", CallID: "call-1", Revision: 1}}},
		{ObservedKindToolExecuting, &ToolExecutingPayload{CallID: "call-1", Revision: 1}},
		{ObservedKindToolFinished, &ToolFinishedPayload{CallID: "call-1", Revision: 1, Outcome: ToolOutcomeSucceeded}},
		{ObservedKindContextUsageUpdated, &ContextUsageUpdatedPayload{Usage: ContextUsage{Round: 1, Source: ContextUsageEstimated, MeasuredAt: now}}},
		{ObservedKindOmissionReported, &OmissionReportedPayload{Omission: Omission{Kind: OmissionOutputBudget}}},
		{ObservedKindHookOutcome, &HookOutcomePayload{}},
		{ObservedKindSubagentStarted, &SubagentStartedPayload{}},
		{ObservedKindSubagentFinished, &SubagentFinishedPayload{}},
		{ObservedKindWarning, &WarningPayload{Code: "budget"}},
		{ObservedKindError, &ErrorPayload{Error: ObservedError{Code: "provider"}}},
		{ObservedKindRunFinished, &RunFinishedPayload{Outcome: RunOutcomeCompleted}},
	}

	if len(tests) != 19 {
		t.Fatalf("schema-v1 kind count = %d, want 19", len(tests))
	}
	seen := make(map[ObservedEventKind]struct{}, len(tests))
	for i, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if !tt.kind.IsSchemaV1() {
				t.Fatalf("kind %q is not recognized as schema v1", tt.kind)
			}
			if _, duplicate := seen[tt.kind]; duplicate {
				t.Fatalf("duplicate kind %q", tt.kind)
			}
			seen[tt.kind] = struct{}{}
			event := validObservedEvent(now)
			event.Sequence = uint64(i + 1)
			event.Kind = tt.kind
			event.Payload = tt.payload
			if err := event.Validate(); err != nil {
				t.Fatalf("Validate(): %v", err)
			}
		})
	}
	if ObservedEventKind("future_kind").IsSchemaV1() {
		t.Fatal("schema v1 accepted an undeclared kind")
	}
}

func TestObservedEventValidateRejectsVersionKindAndPayload(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	t.Run("unsupported version takes precedence", func(t *testing.T) {
		event := validObservedEvent(now)
		event.SchemaVersion = 2
		event.Kind = "future_kind"
		event.Payload = nil
		err := event.Validate()
		var target *UnsupportedObservedSchemaError
		if !errors.As(err, &target) || target.Version != 2 {
			t.Fatalf("error = %T %v, want UnsupportedObservedSchemaError", err, err)
		}
	})

	t.Run("unknown kind", func(t *testing.T) {
		event := validObservedEvent(now)
		event.Kind = "future_kind"
		err := event.Validate()
		var target *UnknownObservedKindError
		if !errors.As(err, &target) || target.Kind != "future_kind" {
			t.Fatalf("error = %T %v, want UnknownObservedKindError", err, err)
		}
	})

	for _, tt := range []struct {
		name    string
		payload ObservedEventPayload
	}{
		{name: "nil interface"},
		{name: "typed nil", payload: (*RunStartedPayload)(nil)},
		{name: "mismatch", payload: &StatusChangedPayload{Status: ActivityThinking}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			event := validObservedEvent(now)
			event.Payload = tt.payload
			err := event.Validate()
			var target *ObservedPayloadMismatchError
			if !errors.As(err, &target) {
				t.Fatalf("error = %T %v, want ObservedPayloadMismatchError", err, err)
			}
			if target.Kind != ObservedKindRunStarted {
				t.Fatalf("mismatch kind = %q", target.Kind)
			}
		})
	}
}

func TestObservedEventCloneDeepCopiesNestedValues(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	original := validObservedEvent(now)
	original.Kind = ObservedKindToolPrepared
	original.Payload = &ToolPreparedPayload{
		CallID:   "call-1",
		Revision: 1,
		EffectiveCall: ToolCall{
			ID:       "provider-call",
			ToolName: "write_file",
			Arguments: map[string]interface{}{
				"nested": map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{"path": "before.txt"},
						[]string{"one", "two"},
					},
				},
			},
		},
		Preview: ActionPreview{
			Kind:      ActionPreviewFileDiff,
			Operation: ActionOperationModify,
			Targets:   []string{"before.txt"},
			Metadata:  map[string]string{"encoding": "utf-8"},
			FileDiff: &FileDiffPreview{
				Path:  "before.txt",
				Hunks: []DiffHunk{{Lines: []DiffLine{{Kind: DiffLineRemoved, Text: "old"}}}},
			},
			Omission: &Omission{Kind: OmissionPreviewBudget},
		},
	}

	cloned := original.Clone()
	clonePayload := cloned.Payload.(*ToolPreparedPayload)
	clonePayload.EffectiveCall.Arguments["nested"].(map[string]interface{})["items"].([]interface{})[0].(map[string]interface{})["path"] = "after.txt"
	clonePayload.EffectiveCall.Arguments["nested"].(map[string]interface{})["items"].([]interface{})[1].([]string)[0] = "changed"
	clonePayload.Preview.Targets[0] = "after.txt"
	clonePayload.Preview.Metadata["encoding"] = "binary"
	clonePayload.Preview.FileDiff.Path = "after.txt"
	clonePayload.Preview.FileDiff.Hunks[0].Lines[0].Text = "changed"
	clonePayload.Preview.Omission.Kind = OmissionRedacted

	originalPayload := original.Payload.(*ToolPreparedPayload)
	items := originalPayload.EffectiveCall.Arguments["nested"].(map[string]interface{})["items"].([]interface{})
	if got := items[0].(map[string]interface{})["path"]; got != "before.txt" {
		t.Fatalf("nested map was shared: %v", got)
	}
	if got := items[1].([]string)[0]; got != "one" {
		t.Fatalf("nested typed slice was shared: %v", got)
	}
	if originalPayload.Preview.Targets[0] != "before.txt" || originalPayload.Preview.Metadata["encoding"] != "utf-8" {
		t.Fatal("preview slices or maps were shared")
	}
	if originalPayload.Preview.FileDiff.Path != "before.txt" || originalPayload.Preview.FileDiff.Hunks[0].Lines[0].Text != "old" {
		t.Fatal("file diff was shared")
	}
	if originalPayload.Preview.Omission.Kind != OmissionPreviewBudget {
		t.Fatal("omission was shared")
	}
}

func TestCapabilityCategoryCloneAndUnknownStates(t *testing.T) {
	category := CapabilityCategory{
		Kind:    CapabilityKindSkill,
		Support: CapabilitySupportSupported,
		Source:  "custom",
		Items:   []Capability{{Kind: CapabilityKindSkill, Name: "review", Availability: CapabilityAvailabilityAvailable}},
	}
	clone := category.Clone()
	clone.Items[0].Name = "changed"
	if category.Items[0].Name != "review" {
		t.Fatal("capability items were shared")
	}
	if CapabilitySupportUnknown != "unknown" || CapabilityAvailabilityUnknown != "unknown" || CapabilityKindUnknown != "unknown" {
		t.Fatal("capability unknown states must be explicit")
	}
	if ContinuationUnknown != "unknown" || RecoverabilityUnknown != "unknown" || ContextUsageSourceUnknown != "unknown" {
		t.Fatal("usage and omission unknown states must be explicit")
	}
}

func TestLegacyObservedPermissionOnlyAdvertisesSafeLiveOperations(t *testing.T) {
	t.Run("exact remembered rule exposes safe scope only", func(t *testing.T) {
		replies := make(chan PermissionDecision, 1)
		request := NewLegacyObservedPermissionRequest("request-1", "call-1", PermissionRequest{
			ToolCall: ToolCall{ID: "provider-call", ToolName: "task", Arguments: map[string]interface{}{
				"instruction": "TOP-SECRET-INSTRUCTION",
			}},
			RememberedRule: "exact-v2:read:hmac-sha256:" + strings.Repeat("a", 64),
			ReplyPath:      replies,
		})
		if !request.Capabilities.Remember || request.RememberedScope == nil || request.RememberedScope.ScopeKind != RememberedRuleScopeExact {
			t.Fatalf("exact scope unavailable: %+v", request)
		}
		if request.RememberedScope.ToolName != "task" || strings.Contains(request.RememberedScope.Display, "TOP-SECRET") || strings.Contains(request.RememberedScope.Display, "aaaa") {
			t.Fatalf("exact scope leaked identity material: %+v", request.RememberedScope)
		}
	})

	t.Run("unsafe legacy exact rule does not offer remember", func(t *testing.T) {
		replies := make(chan PermissionDecision, 1)
		request := NewLegacyObservedPermissionRequest("request-v1", "call-v1", PermissionRequest{
			ToolCall:       ToolCall{ToolName: "task"},
			RememberedRule: "exact-v1:read:sha256:" + strings.Repeat("a", 64),
			ReplyPath:      replies,
		})
		if request.Capabilities.Remember || request.RememberedScope != nil {
			t.Fatalf("legacy unkeyed exact rule must fail safe: %+v", request)
		}
	})

	t.Run("malformed remembered rule stays unsupported and answerable", func(t *testing.T) {
		replies := make(chan PermissionDecision, 1)
		request := NewLegacyObservedPermissionRequest("request-1", "call-1", PermissionRequest{
			ToolCall:       ToolCall{ID: "provider-call", ToolName: "run_command"},
			RememberedRule: "command:echo ok; rm -rf target",
			ReplyPath:      replies,
		})
		if request.Capabilities.Remember || request.RememberedScope != nil {
			t.Fatalf("malformed rule was advertised as rememberable: %+v", request)
		}

		decision := ObservedPermissionDecision{
			RequestID: request.RequestID,
			CallID:    request.CallID,
			Revision:  request.Revision,
			Action:    PermissionReplyAllow,
			Remember:  true,
		}
		if got := request.Reply(context.Background(), decision).Status; got != PermissionReplyValidationRejected {
			t.Fatalf("remember reply: want validation_rejected, got %q", got)
		}
		select {
		case reply := <-replies:
			t.Fatalf("invalid remember reply reached legacy path: %+v", reply)
		default:
		}

		decision.Remember = false
		if got := request.Reply(context.Background(), decision).Status; got != PermissionReplyAccepted {
			t.Fatalf("corrected reply: want accepted, got %q", got)
		}
	})

	t.Run("missing reply path advertises no reply capability", func(t *testing.T) {
		request := NewLegacyObservedPermissionRequest("request-1", "call-1", PermissionRequest{})
		if request.Capabilities.Allow || request.Capabilities.Deny || request.Capabilities.Remember {
			t.Fatalf("missing path advertised live operations: %+v", request.Capabilities)
		}
		decision := ObservedPermissionDecision{
			RequestID: request.RequestID,
			CallID:    request.CallID,
			Revision:  request.Revision,
			Action:    PermissionReplyAllow,
		}
		result := request.Reply(context.Background(), decision)
		if result.Status != PermissionReplyValidationRejected || len(result.Feedback) != 1 || result.Feedback[0].Code != "reply_unavailable" {
			t.Fatalf("missing path reply result: %+v", result)
		}
	})
}

func TestRichObservedPermissionValidationRetryAndExactlyOnce(t *testing.T) {
	runContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, accepted := NewRichObservedPermissionRequest(RichPermissionRequestConfig{
		RunContext: runContext, RequestID: "request-1", CallID: "call-1", Revision: 2,
		EffectiveCall: ToolCall{ID: "provider", ToolName: "write_file", Arguments: map[string]interface{}{"path": "target"}},
		Capabilities:  PermissionCapabilities{Allow: true, Deny: true, ReviseArguments: true, SandboxGrants: true},
		GrantOptions:  SandboxGrantOptions{Support: CapabilitySupportSupported, ReadRoots: true},
		Validate: func(decision ObservedPermissionDecision) []PermissionReplyFeedback {
			if decision.Action == PermissionReplyReviseArguments {
				if _, ok := decision.RevisedArguments["path"].(string); !ok {
					return []PermissionReplyFeedback{{Field: "path", Code: "schema_invalid", Message: "path must be a string"}}
				}
			}
			return nil
		},
	})

	wrong := ObservedPermissionDecision{RequestID: "wrong", CallID: request.CallID, Revision: request.Revision, Action: PermissionReplyAllow}
	if result := request.Reply(context.Background(), wrong); result.Status != PermissionReplyValidationRejected {
		t.Fatalf("mismatched reply = %+v", result)
	}
	invalid := ObservedPermissionDecision{
		RequestID: request.RequestID, CallID: request.CallID, Revision: request.Revision,
		Action: PermissionReplyReviseArguments, RevisedArguments: map[string]interface{}{"path": 3},
	}
	if result := request.Reply(context.Background(), invalid); result.Status != PermissionReplyValidationRejected || len(result.Feedback) != 1 || result.Feedback[0].Field != "path" {
		t.Fatalf("schema-invalid reply = %+v", result)
	}
	select {
	case decision := <-accepted:
		t.Fatalf("invalid decision consumed request: %+v", decision)
	default:
	}

	valid := invalid
	valid.RevisedArguments = map[string]interface{}{"path": "revised"}
	const replies = 12
	statuses := make(chan PermissionReplyStatus, replies)
	var wait sync.WaitGroup
	for range replies {
		wait.Add(1)
		go func() {
			defer wait.Done()
			statuses <- request.Reply(context.Background(), valid).Status
		}()
	}
	wait.Wait()
	close(statuses)
	acceptedCount, resolvedCount := 0, 0
	for status := range statuses {
		switch status {
		case PermissionReplyAccepted:
			acceptedCount++
		case PermissionReplyAlreadyResolved:
			resolvedCount++
		default:
			t.Fatalf("valid reply status = %q", status)
		}
	}
	if acceptedCount != 1 || resolvedCount != replies-1 {
		t.Fatalf("accepted=%d resolved=%d", acceptedCount, resolvedCount)
	}
	decision := <-accepted
	if decision.RevisedArguments["path"] != "revised" {
		t.Fatalf("accepted decision = %+v", decision)
	}
	decision.RevisedArguments["path"] = "mutated"
	if request.EffectiveCall.Arguments["path"] != "target" {
		t.Fatal("accepted decision shared arguments with request")
	}
}

func TestRichObservedPermissionCancellationClosesLateReply(t *testing.T) {
	runContext, cancel := context.WithCancel(context.Background())
	request, _ := NewRichObservedPermissionRequest(RichPermissionRequestConfig{
		RunContext: runContext, RequestID: "request-1", CallID: "call-1", Revision: 1,
		Capabilities: PermissionCapabilities{Allow: true, Deny: true},
	})
	cancel()
	decision := ObservedPermissionDecision{RequestID: request.RequestID, CallID: request.CallID, Revision: request.Revision, Action: PermissionReplyAllow}
	if result := request.Reply(context.Background(), decision); result.Status != PermissionReplyAlreadyResolved {
		t.Fatalf("late reply = %+v", result)
	}
}

func validObservedEvent(now time.Time) ObservedEvent {
	return ObservedEvent{
		SchemaVersion: ObservedSchemaV1,
		RunID:         "run-1",
		Sequence:      1,
		Timestamp:     now,
		Origin:        Origin{AgentID: "agent-root"},
		Kind:          ObservedKindRunStarted,
		Payload:       &RunStartedPayload{},
	}
}
