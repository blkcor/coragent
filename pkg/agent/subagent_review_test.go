package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/blkcor/coragent/pkg/agent"
)

func TestReviewDefaultSessionAdvertisesOneStandardTaskSchema(t *testing.T) {
	provider := newSessionScriptProvider(providerStep{text: []string{"done"}})
	session := agent.NewSession(agent.SessionConfig{
		Provider:         provider,
		PermissionMode:   "bypass",
		WorkingDirectory: t.TempDir(),
	})
	drain(t, mustRun(t, session, "inspect tools"))

	records := provider.recordsSnapshot()
	if len(records) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(records))
	}
	var tasks []agent.Tool
	for _, descriptor := range records[0].tools {
		if descriptor.Name == "task" {
			tasks = append(tasks, descriptor)
		}
	}
	if len(tasks) != 1 {
		t.Fatalf("standard task descriptors = %d, want exactly 1; tools=%v", len(tasks), namesOfTools(records[0].tools))
	}

	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(tasks[0].Parameters, &schema); err != nil {
		t.Fatalf("decode task schema: %v", err)
	}
	if !sameRequiredNames(schema.Required, "label", "instruction") {
		t.Fatalf("task required fields = %v, want label and instruction", schema.Required)
	}

	for _, name := range []string{"label", "instruction"} {
		var property struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(schema.Properties[name], &property); err != nil {
			t.Fatalf("decode task %s property: %v", name, err)
		}
		if property.Type != "string" {
			t.Fatalf("task %s type = %q, want string", name, property.Type)
		}
	}

	var toolsProperty struct {
		Type  string `json:"type"`
		Items struct {
			Type string `json:"type"`
		} `json:"items"`
	}
	if err := json.Unmarshal(schema.Properties["tools"], &toolsProperty); err != nil {
		t.Fatalf("decode task tools property: %v", err)
	}
	if toolsProperty.Type != "array" || toolsProperty.Items.Type != "string" {
		t.Fatalf("task tools schema = type:%q items:%q, want array of string", toolsProperty.Type, toolsProperty.Items.Type)
	}
}

func TestReviewPlanModeStartsTaskButRejectsChildEdits(t *testing.T) {
	dir := t.TempDir()
	createdPath := dir + "/created.txt"
	editedPath := dir + "/existing.txt"
	if err := os.WriteFile(editedPath, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{taskCall(
			"task-plan",
			"attempt child edits",
			"try both edits",
			[]string{"write_file", "edit_file"},
		)}},
		providerStep{calls: []agent.ToolCall{
			{
				ID:        "write-child",
				ToolName:  "write_file",
				Arguments: map[string]interface{}{"path": createdPath, "content": "created"},
			},
			{
				ID:        "edit-child",
				ToolName:  "edit_file",
				Arguments: map[string]interface{}{"path": editedPath, "old_string": "before", "new_string": "after"},
			},
		}},
		providerStep{text: []string{"child observed plan denials"}},
		providerStep{text: []string{"parent complete"}},
	)
	session := agent.NewSession(agent.SessionConfig{
		Provider:         provider,
		PermissionMode:   "plan",
		WorkingDirectory: dir,
	})

	events := drain(t, mustRun(t, session, "delegate edits"))
	if got := lifecycleLabels(events); !equalStrings(got, []string{
		"subagent_started:attempt child edits",
		"subagent_finished:attempt child edits",
	}) {
		t.Fatalf("task did not start and finish in plan mode: %v", got)
	}
	for _, ev := range events {
		if ev.Type == agent.PermissionRequestedEvent {
			t.Fatalf("plan mode should decide without a human prompt: %+v", ev)
		}
	}

	if _, err := os.Stat(createdPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child write handler ran in plan mode: stat err=%v", err)
	}
	if data, err := os.ReadFile(editedPath); err != nil || string(data) != "before" {
		t.Fatalf("child edit handler changed file in plan mode: data=%q err=%v", data, err)
	}

	records := provider.recordsSnapshot()
	if len(records) != 4 {
		t.Fatalf("provider calls = %d, want 4", len(records))
	}
	results := latestToolResults(t, records[2].conversation)
	if len(results) != 2 {
		t.Fatalf("child mutation results = %+v, want 2", results)
	}
	for _, result := range results {
		if !result.IsError || !strings.Contains(result.Result, "plan mode") {
			t.Fatalf("child mutation result = %+v, want plan-mode denial", result)
		}
	}
}

func TestReviewChildToolHooksUseInheritedHardGates(t *testing.T) {
	tests := []struct {
		name          string
		moment        agent.HookMoment
		verdict       func() agent.HookVerdict
		wantCalls     int32
		wantError     bool
		wantResult    string
		wantSubstring string
	}{
		{
			name:          "before block prevents handler",
			moment:        agent.HookBeforeTool,
			verdict:       func() agent.HookVerdict { return agent.HookVerdict{Block: true, Reason: "review pre-block"} },
			wantCalls:     0,
			wantError:     true,
			wantSubstring: "blocked by hard pre-check: review pre-block",
		},
		{
			name:   "after replacement reaches child conversation",
			moment: agent.HookAfterTool,
			verdict: func() agent.HookVerdict {
				replacement := "hook-replaced child output"
				return agent.HookVerdict{Result: &replacement}
			},
			wantCalls:  1,
			wantResult: "hook-replaced child output",
		},
		{
			name:          "after block turns child result into error",
			moment:        agent.HookAfterTool,
			verdict:       func() agent.HookVerdict { return agent.HookVerdict{Block: true, Reason: "review post-block"} },
			wantCalls:     1,
			wantError:     true,
			wantSubstring: "blocked by hard post-check: review post-block",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := &reviewReadTool{name: "review_child_tool"}
			provider := newSessionScriptProvider(
				providerStep{calls: []agent.ToolCall{taskCall("task-hook", "hook review", "invoke review tool", []string{handler.name})}},
				providerStep{calls: []agent.ToolCall{{ID: "child-tool", ToolName: handler.name, Arguments: map[string]interface{}{}}}},
				providerStep{text: []string{"child hook review complete"}},
				providerStep{text: []string{"parent complete"}},
			)
			session := agent.NewSession(agent.SessionConfig{
				Provider:         provider,
				ToolHandlers:     []agent.ToolHandler{handler},
				PermissionMode:   "bypass",
				WorkingDirectory: t.TempDir(),
				Hooks: []agent.HookRegistration{{
					Name:   "review-child-hook",
					Moment: tc.moment,
					Scope:  agent.HookScope{ToolName: handler.name},
					Handler: func(context.Context, agent.HookEvent) agent.HookVerdict {
						return tc.verdict()
					},
				}},
			})

			drain(t, mustRun(t, session, "delegate hook review"))
			if got := handler.calls.Load(); got != tc.wantCalls {
				t.Fatalf("child handler calls = %d, want %d", got, tc.wantCalls)
			}
			records := provider.recordsSnapshot()
			if len(records) != 4 {
				t.Fatalf("provider calls = %d, want 4", len(records))
			}
			results := latestToolResults(t, records[2].conversation)
			if len(results) != 1 {
				t.Fatalf("child tool results = %+v, want 1", results)
			}
			result := results[0]
			if result.IsError != tc.wantError {
				t.Fatalf("child result error = %v, want %v: %+v", result.IsError, tc.wantError, result)
			}
			if tc.wantResult != "" && result.Result != tc.wantResult {
				t.Fatalf("child result = %q, want %q", result.Result, tc.wantResult)
			}
			if tc.wantSubstring != "" && !strings.Contains(result.Result, tc.wantSubstring) {
				t.Fatalf("child result = %q, want substring %q", result.Result, tc.wantSubstring)
			}
		})
	}
}

func TestReviewChildCommandUsesInheritedSandboxStageWithoutLaunchingProcess(t *testing.T) {
	handler := &reviewCommandTool{name: "review_child_command"}
	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{taskCall("task-command", "command safety", "invoke command probe", []string{handler.name})}},
		providerStep{calls: []agent.ToolCall{{ID: "child-command", ToolName: handler.name, Arguments: map[string]interface{}{}}}},
		providerStep{text: []string{"child command review complete"}},
		providerStep{text: []string{"parent complete"}},
	)
	session := agent.NewSession(agent.SessionConfig{
		Provider:         provider,
		ToolHandlers:     []agent.ToolHandler{handler},
		PermissionMode:   "bypass",
		WorkingDirectory: t.TempDir(),
	})

	drain(t, mustRun(t, session, "delegate command review"))
	if got := handler.directCalls.Load(); got != 0 {
		t.Fatalf("child command used unrestricted Execute %d times", got)
	}
	if got := handler.sandboxCalls.Load(); got != 1 {
		t.Fatalf("child command entered inherited sandbox handler %d times, want 1", got)
	}

	records := provider.recordsSnapshot()
	if len(records) != 4 {
		t.Fatalf("provider calls = %d, want 4", len(records))
	}
	results := latestToolResults(t, records[2].conversation)
	if len(results) != 1 || results[0].IsError || results[0].Result != "inherited sandbox stage" {
		t.Fatalf("child command result = %+v", results)
	}
}

func sameRequiredNames(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, name := range got {
		seen[name] = true
	}
	for _, name := range want {
		if !seen[name] {
			return false
		}
	}
	return true
}

func latestToolResults(t *testing.T, conversation agent.Conversation) []agent.ToolResult {
	t.Helper()
	for i := len(conversation.Turns) - 1; i >= 0; i-- {
		if conversation.Turns[i].Role == "tool" {
			return conversation.Turns[i].ToolResults
		}
	}
	t.Fatalf("conversation has no tool-result turn: %+v", conversation.Turns)
	return nil
}

type reviewReadTool struct {
	name  string
	calls atomic.Int32
}

func (h *reviewReadTool) Descriptor() agent.Tool {
	return agent.Tool{Name: h.name, Description: "review child hook inheritance", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (h *reviewReadTool) Execute(context.Context, map[string]interface{}) (string, error) {
	h.calls.Add(1)
	return "original child output", nil
}

func (*reviewReadTool) RunsCommands() bool           { return false }
func (*reviewReadTool) ActionKind() agent.ActionKind { return agent.ActionRead }

type reviewCommandTool struct {
	name         string
	directCalls  atomic.Int32
	sandboxCalls atomic.Int32
}

func (h *reviewCommandTool) Descriptor() agent.Tool {
	return agent.Tool{Name: h.name, Description: "review child sandbox inheritance", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (h *reviewCommandTool) Execute(context.Context, map[string]interface{}) (string, error) {
	h.directCalls.Add(1)
	return "", errors.New("review command bypassed sandbox")
}

func (h *reviewCommandTool) ExecuteCommand(_ context.Context, _ map[string]interface{}, runner agent.CommandRunner) (string, error) {
	if runner == nil {
		return "", errors.New("review command received no sandbox runner")
	}
	h.sandboxCalls.Add(1)
	// Deliberately do not launch a process. Reaching this command-handler method
	// proves the inherited sandbox stage adapted the child tool; an unrestricted
	// execution path would have called Execute above instead.
	return "inherited sandbox stage", nil
}

func (*reviewCommandTool) RunsCommands() bool           { return true }
func (*reviewCommandTool) ActionKind() agent.ActionKind { return agent.ActionCommand }

var _ agent.ToolHandler = (*reviewReadTool)(nil)
var _ agent.ActionClassifier = (*reviewReadTool)(nil)
var _ agent.CommandToolHandler = (*reviewCommandTool)(nil)
var _ agent.ActionClassifier = (*reviewCommandTool)(nil)
