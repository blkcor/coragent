package agent_test

import (
	"errors"
	"testing"
	"time"

	"github.com/blkcor/coragent/pkg/agent"
)

func TestPublicObservedEnvelopeIsConstructibleAndValid(t *testing.T) {
	event := agent.ObservedEvent{
		SchemaVersion: agent.ObservedSchemaV1,
		RunID:         "run-1",
		Sequence:      1,
		Timestamp:     time.Unix(1_700_000_000, 0).UTC(),
		Origin:        agent.Origin{AgentID: "agent-root"},
		Kind:          agent.ObservedKindAssistantTextDelta,
		Payload:       &agent.AssistantTextDeltaPayload{Round: 1, Delta: "hello"},
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
	if !event.Kind.IsSchemaV1() {
		t.Fatalf("public kind %q not recognized", event.Kind)
	}
}

func TestPublicObservedErrorsRemainTyped(t *testing.T) {
	event := agent.ObservedEvent{SchemaVersion: 99}
	err := event.Validate()
	var unsupported *agent.UnsupportedObservedSchemaError
	if !errors.As(err, &unsupported) || unsupported.Version != 99 {
		t.Fatalf("error = %T %v, want public typed schema error", err, err)
	}
}

func TestPublicObservedCloneDoesNotShareArguments(t *testing.T) {
	event := agent.ObservedEvent{
		SchemaVersion: agent.ObservedSchemaV1,
		RunID:         "run-1",
		Sequence:      1,
		Timestamp:     time.Unix(1_700_000_000, 0).UTC(),
		Origin:        agent.Origin{AgentID: "agent-root"},
		Kind:          agent.ObservedKindPermissionRequested,
		Payload: &agent.PermissionRequestedPayload{Request: agent.ObservedPermissionRequest{
			RequestID: "request-1",
			CallID:    "call-1",
			Revision:  1,
			EffectiveCall: agent.ToolCall{Arguments: map[string]interface{}{
				"paths": []interface{}{map[string]interface{}{"value": "before"}},
			}},
			Preview: agent.ActionPreview{
				Targets:  []string{"before"},
				Metadata: map[string]string{"state": "before"},
			},
			GrantOptions: agent.SandboxGrantOptions{
				Support:         agent.CapabilitySupportSupported,
				SuggestedReads:  []string{"/before/read"},
				SuggestedWrites: []string{"/before/write"},
			},
		}},
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}

	clone := event.Clone()
	request := clone.Payload.(*agent.PermissionRequestedPayload).Request
	request.EffectiveCall.Arguments["paths"].([]interface{})[0].(map[string]interface{})["value"] = "after"
	request.Preview.Targets[0] = "after"
	request.Preview.Metadata["state"] = "after"
	request.GrantOptions.SuggestedReads[0] = "/after/read"
	request.GrantOptions.SuggestedWrites[0] = "/after/write"

	original := event.Payload.(*agent.PermissionRequestedPayload).Request
	if got := original.EffectiveCall.Arguments["paths"].([]interface{})[0].(map[string]interface{})["value"]; got != "before" {
		t.Fatalf("nested arguments were shared: %v", got)
	}
	if original.Preview.Targets[0] != "before" || original.Preview.Metadata["state"] != "before" {
		t.Fatal("preview values were shared")
	}
	if original.GrantOptions.SuggestedReads[0] != "/before/read" || original.GrantOptions.SuggestedWrites[0] != "/before/write" {
		t.Fatal("grant option slices were shared")
	}
}

func TestPublicCapabilityCategoryDistinguishesUnsupportedFromEmpty(t *testing.T) {
	unsupported := agent.CapabilityCategory{Kind: agent.CapabilityKindSkill, Support: agent.CapabilitySupportUnsupported}
	empty := agent.CapabilityCategory{Kind: agent.CapabilityKindSkill, Support: agent.CapabilitySupportSupported, Source: "custom"}
	if unsupported.Support == empty.Support {
		t.Fatal("unsupported and supported-empty capability categories collapsed")
	}
}
