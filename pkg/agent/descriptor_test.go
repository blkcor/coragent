package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blkcor/coragent/pkg/agent"
)

func TestSessionDescribeReturnsIndependentSafeFacts(t *testing.T) {
	provider := agent.NewOpenAIProvider("https://user:secret@example.test/v1?token=hidden", "api-secret", "gpt-test")
	session := agent.NewSession(agent.SessionConfig{
		Provider:         provider,
		WorkingDirectory: t.TempDir(),
		PermissionMode:   "plan",
		Hooks: []agent.HookRegistration{{
			Name:   "before-write",
			Moment: agent.HookBeforeTool,
			Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
				return agent.HookVerdict{}
			},
		}},
	})

	first := session.Describe()
	if first.SessionID == "" || first.RootAgentID == "" {
		t.Fatalf("descriptor IDs are empty: %+v", first)
	}
	if first.Model != "gpt-test" || first.Provider != "openai-compatible" {
		t.Fatalf("provider identity = %q/%q", first.Provider, first.Model)
	}
	if first.Permission.Ownership != agent.PermissionOwnershipEngine || first.Permission.Mode != agent.PermissionModePlan {
		t.Fatalf("permission descriptor = %+v", first.Permission)
	}
	if len(first.Capabilities) == 0 {
		t.Fatal("capability inventory is empty")
	}

	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for _, secret := range []string{"api-secret", "user:secret", "token=hidden"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("descriptor leaked %q: %s", secret, encoded)
		}
	}

	first.Capabilities[0].Items[0].Name = "mutated"
	if err := session.SetPermissionModeTyped(agent.PermissionModeDefault); err != nil {
		t.Fatalf("SetPermissionModeTyped: %v", err)
	}
	second := session.Describe()
	if second.Capabilities[0].Items[0].Name == "mutated" {
		t.Fatal("mutating a returned descriptor changed live inventory")
	}
	if second.Permission.Mode != agent.PermissionModeDefault {
		t.Fatalf("fresh descriptor mode = %q", second.Permission.Mode)
	}
	if second.SessionID != first.SessionID || second.RootAgentID != first.RootAgentID {
		t.Fatal("stable session identity changed between descriptors")
	}
}

func TestSessionDescribeMarksCustomDispatcherOwnership(t *testing.T) {
	session := agent.NewSession(agent.SessionConfig{
		Provider:   immediateProvider{},
		Dispatcher: externalDispatcher{},
		Tools: []agent.Tool{{
			Name: "caller_tool",
		}},
	})
	description := session.Describe()
	if description.Permission.Ownership != agent.PermissionOwnershipExternal {
		t.Fatalf("permission ownership = %q", description.Permission.Ownership)
	}
	if description.Sandbox.Posture != agent.SandboxPostureExternal {
		t.Fatalf("sandbox posture = %q", description.Sandbox.Posture)
	}
	tools := capabilityCategory(t, description, agent.CapabilityKindTool)
	if tools.Support != agent.CapabilitySupportUnknown || len(tools.Items) != 1 || tools.Items[0].Availability != agent.CapabilityAvailabilityUnknown {
		t.Fatalf("custom dispatcher inventory = %+v", tools)
	}
}

func TestSessionDescribeDoesNotTreatAdvisoryBudgetAsContextWindow(t *testing.T) {
	session := agent.NewSession(agent.SessionConfig{
		Provider:            immediateProvider{},
		ContextBudgetTokens: 128_000,
	})
	if window := session.Describe().ContextWindow; window.Known || window.Tokens != 0 {
		t.Fatalf("advisory budget became provider context window: %+v", window)
	}
}

func TestOptionalCapabilityReporterDistinguishesSupportedEmpty(t *testing.T) {
	provider := reportingProvider{categories: []agent.CapabilityCategory{{
		Kind:    agent.CapabilityKindSkill,
		Support: agent.CapabilitySupportSupported,
		Source:  "test-runtime",
	}}}
	session := agent.NewSession(agent.SessionConfig{
		Provider:        provider,
		SkillRootUser:   "/nonexistent/skill-test-user",
		SkillRootProject: "/nonexistent/skill-test-project",
	})
	description := session.Describe()

	skills := capabilityCategory(t, description, agent.CapabilityKindSkill)
	if skills.Support != agent.CapabilitySupportSupported || skills.Source != "test-runtime" || len(skills.Items) != 0 {
		t.Fatalf("skills category = %+v", skills)
	}
	mcp := capabilityCategory(t, description, agent.CapabilityKindMCP)
	if mcp.Support != agent.CapabilitySupportUnsupported {
		t.Fatalf("MCP category = %+v", mcp)
	}
}

func capabilityCategory(t *testing.T, description agent.SessionDescription, kind agent.CapabilityKind) agent.CapabilityCategory {
	t.Helper()
	for _, category := range description.Capabilities {
		if category.Kind == kind {
			return category
		}
	}
	t.Fatalf("capability category %q is missing", kind)
	return agent.CapabilityCategory{}
}

type reportingProvider struct {
	categories []agent.CapabilityCategory
}

func (p reportingProvider) StreamReply(context.Context, agent.Conversation, []agent.Tool, agent.StreamOptions) <-chan agent.RunEvent {
	ch := make(chan agent.RunEvent, 1)
	ch <- agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}}
	close(ch)
	return ch
}

func (p reportingProvider) CapabilityCategories() []agent.CapabilityCategory {
	return p.categories
}
