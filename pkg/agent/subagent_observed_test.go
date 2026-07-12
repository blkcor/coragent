package agent_test

import (
	"context"
	"testing"

	"github.com/blkcor/coragent/pkg/agent"
)

func TestObservedSubagentDuplicateLabelsHaveStableDistinctProvenanceAndRawIsolation(t *testing.T) {
	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{
			taskCall("task-one", "same label", "first child", nil),
			taskCall("task-two", "same label", "second child", nil),
		}},
		providerStep{text: []string{"first private child answer"}},
		providerStep{text: []string{"second private child answer"}},
		providerStep{text: []string{"root visible answer"}},
	)
	session := agent.NewSession(agent.SessionConfig{Provider: provider, PermissionMode: "bypass"})
	events := drainObserved(t, mustRunObserved(t, session, context.Background(), "delegate twice"))
	assertObservedEnvelopesAllowingOrigins(t, events)

	callIDs := make(map[string]agent.CallID)
	starts := make(map[agent.AgentID]agent.SubagentProvenance)
	finishes := make(map[agent.AgentID]agent.SubagentFinishedPayload)
	var rootText string
	for _, event := range events {
		switch event.Kind {
		case agent.ObservedKindToolProposed:
			payload := event.Payload.(*agent.ToolProposedPayload)
			callIDs[payload.Call.ID] = payload.CallID
		case agent.ObservedKindSubagentStarted:
			payload := event.Payload.(*agent.SubagentStartedPayload)
			starts[payload.Agent.AgentID] = payload.Agent
		case agent.ObservedKindSubagentFinished:
			payload := event.Payload.(*agent.SubagentFinishedPayload)
			finishes[payload.Agent.AgentID] = *payload
		case agent.ObservedKindAssistantTextDelta:
			if event.Origin.Depth == 0 {
				rootText += event.Payload.(*agent.AssistantTextDeltaPayload).Delta
			}
		}
		if event.Origin.Depth > 0 {
			switch event.Kind {
			case agent.ObservedKindSubagentStarted, agent.ObservedKindSubagentFinished, agent.ObservedKindPermissionRequested:
			default:
				t.Fatalf("raw child event escaped isolation: origin=%+v kind=%q", event.Origin, event.Kind)
			}
		}
	}
	if len(starts) != 2 || len(finishes) != 2 {
		t.Fatalf("starts=%+v finishes=%+v", starts, finishes)
	}
	if rootText != "root visible answer" {
		t.Fatalf("root assistant text = %q", rootText)
	}
	for agentID, provenance := range starts {
		if provenance.Label != "same label" || provenance.Depth != 1 || provenance.ParentAgentID == "" || provenance.AgentID != agentID {
			t.Fatalf("provenance = %+v", provenance)
		}
		if provenance.DelegationCallID != callIDs["task-one"] && provenance.DelegationCallID != callIDs["task-two"] {
			t.Fatalf("delegation call not correlated: %+v calls=%+v", provenance, callIDs)
		}
		finish, ok := finishes[agentID]
		if !ok || finish.Agent != provenance || finish.Outcome != agent.SubagentOutcomeCompleted || finish.FinishedAt.IsZero() {
			t.Fatalf("finish for %q = %+v", agentID, finish)
		}
	}
}

func TestObservedNestedSubagentUsesImmediateParentAndDepth(t *testing.T) {
	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{taskCall("outer", "outer", "delegate nested", []string{"read_file"})}},
		providerStep{calls: []agent.ToolCall{taskCall("inner", "inner", "finish nested", nil)}},
		providerStep{text: []string{"grandchild answer"}},
		providerStep{text: []string{"child answer"}},
		providerStep{text: []string{"root answer"}},
	)
	session := agent.NewSession(agent.SessionConfig{Provider: provider, PermissionMode: "bypass"})
	events := drainObserved(t, mustRunObserved(t, session, context.Background(), "nested"))
	var outer, inner agent.SubagentProvenance
	for _, event := range events {
		if event.Kind != agent.ObservedKindSubagentStarted {
			continue
		}
		provenance := event.Payload.(*agent.SubagentStartedPayload).Agent
		switch provenance.Label {
		case "outer":
			outer = provenance
		case "inner":
			inner = provenance
		}
	}
	if outer.AgentID == "" || inner.AgentID == "" || outer.Depth != 1 || inner.Depth != 2 || inner.ParentAgentID != outer.AgentID || inner.AgentID == outer.AgentID {
		t.Fatalf("outer=%+v inner=%+v", outer, inner)
	}
}

func TestObservedChildPermissionCarriesLifecycleOriginAndLiveReply(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/child.txt"
	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{taskCall("task", "child", "write a file", []string{"write_file"})}},
		providerStep{calls: []agent.ToolCall{{
			ID: "child-write", ToolName: "write_file", Arguments: map[string]interface{}{"path": path, "content": "value"},
		}}},
		providerStep{text: []string{"child handled denial"}},
		providerStep{text: []string{"root done"}},
	)
	session := agent.NewSession(agent.SessionConfig{
		Provider: provider, PermissionAllow: []string{"read:*"}, WorkingDirectory: directory,
	})
	stream := mustRunObserved(t, session, context.Background(), "delegate")
	var child agent.SubagentProvenance
	permissionCount := 0
	for event := range stream {
		switch event.Kind {
		case agent.ObservedKindSubagentStarted:
			child = event.Payload.(*agent.SubagentStartedPayload).Agent
		case agent.ObservedKindPermissionRequested:
			request := event.Payload.(*agent.PermissionRequestedPayload).Request
			if request.EffectiveCall.ID != "child-write" {
				continue
			}
			permissionCount++
			if child.AgentID == "" || event.Origin.AgentID != child.AgentID || event.Origin.ParentAgentID != child.ParentAgentID || event.Origin.Depth != child.Depth {
				t.Fatalf("permission origin=%+v child=%+v", event.Origin, child)
			}
			if reply := request.Reply(context.Background(), decisionForPublic(request, agent.PermissionReplyDeny)); reply.Status != agent.PermissionReplyAccepted {
				t.Fatalf("deny reply = %+v", reply)
			}
		}
	}
	if permissionCount != 1 {
		t.Fatalf("child permission count = %d", permissionCount)
	}
}

func TestObservedSubagentOutcomesDistinguishFailureAndStepLimit(t *testing.T) {
	tests := []struct {
		name      string
		steps     []providerStep
		maxRounds int
		want      agent.SubagentOutcome
	}{
		{
			name: "failed",
			steps: []providerStep{
				{calls: []agent.ToolCall{taskCall("task", "child", "fail", nil)}},
				{err: context.DeadlineExceeded},
			},
			maxRounds: 3,
			want:      agent.SubagentOutcomeFailed,
		},
		{
			name: "step limit",
			steps: []providerStep{
				{calls: []agent.ToolCall{taskCall("task", "child", "loop", []string{"read_file"})}},
				{calls: []agent.ToolCall{{ID: "child-read", ToolName: "read_file", Arguments: map[string]interface{}{"path": "missing"}}}},
			},
			maxRounds: 1,
			want:      agent.SubagentOutcomeReachedStepLimit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newSessionScriptProvider(test.steps...)
			session := agent.NewSession(agent.SessionConfig{Provider: provider, PermissionMode: "bypass", MaxRounds: test.maxRounds})
			events := drainObserved(t, mustRunObserved(t, session, context.Background(), "delegate"))
			found := false
			for _, event := range events {
				if event.Kind != agent.ObservedKindSubagentFinished {
					continue
				}
				payload := event.Payload.(*agent.SubagentFinishedPayload)
				if payload.Agent.Label == "child" {
					found = true
					if payload.Outcome != test.want {
						t.Fatalf("outcome=%q want=%q payload=%+v", payload.Outcome, test.want, payload)
					}
				}
			}
			if !found {
				t.Fatal("missing child terminal lifecycle")
			}
		})
	}
}

func decisionForPublic(request agent.ObservedPermissionRequest, action agent.PermissionReplyAction) agent.ObservedPermissionDecision {
	return agent.ObservedPermissionDecision{RequestID: request.RequestID, CallID: request.CallID, Revision: request.Revision, Action: action}
}

func assertObservedEnvelopesAllowingOrigins(t *testing.T, events []agent.ObservedEvent) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("empty observed stream")
	}
	runID := events[0].RunID
	terminals := 0
	for index, event := range events {
		if err := event.Validate(); err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
		if event.RunID != runID || event.Sequence != uint64(index+1) {
			t.Fatalf("event %d envelope = %+v", index, event)
		}
		if event.Kind == agent.ObservedKindRunFinished {
			terminals++
			if index != len(events)-1 {
				t.Fatal("terminal was not last")
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("terminal count = %d", terminals)
	}
}
