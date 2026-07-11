package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blkcor/coragent/pkg/agent"
)

func TestDefaultSessionDelegatesWithIsolatedContextAndResultOnly(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/input.txt"
	if err := os.WriteFile(path, []byte("config value"), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{taskCall("task-1", "inspect config", "read the config", nil)}},
		providerStep{text: []string{"child working"}, calls: []agent.ToolCall{{ID: "read-1", ToolName: "read_file", Arguments: map[string]interface{}{"path": path}}}},
		providerStep{text: []string{"child answer"}},
		providerStep{text: []string{"parent final"}},
	)
	session := agent.NewSession(agent.SessionConfig{
		Provider:         provider,
		SystemPrompt:     "root system",
		PermissionMode:   "bypass",
		WorkingDirectory: dir,
	})

	events := drain(t, mustRun(t, session, "root request"))
	records := provider.recordsSnapshot()
	if len(records) != 4 {
		t.Fatalf("provider calls = %d, want 4", len(records))
	}

	childFirst := records[1]
	if got := rolesOf(childFirst.conversation); !equalStrings(got, []string{"system", "user"}) {
		t.Fatalf("first child roles = %v, want [system user]", got)
	}
	if childFirst.conversation.Turns[1].Content != "read the config" {
		t.Fatalf("child instruction = %q", childFirst.conversation.Turns[1].Content)
	}
	for _, turn := range childFirst.conversation.Turns {
		if strings.Contains(turn.Content, "root request") || strings.Contains(turn.Content, "root system") {
			t.Fatalf("parent history leaked into child: %+v", childFirst.conversation.Turns)
		}
	}
	if got := namesOfTools(childFirst.tools); !equalStrings(got, []string{"read_file", "search_content", "find_files", "task"}) {
		t.Fatalf("child safe-default tools = %v", got)
	}

	result := findToolResult(events)
	if result == nil || result.IsError || result.Result != "child answer" {
		t.Fatalf("task result = %+v", result)
	}
	if got := lifecycleLabels(events); !equalStrings(got, []string{"subagent_started:inspect config", "subagent_finished:inspect config"}) {
		t.Fatalf("lifecycle = %v", got)
	}
	for _, ev := range events {
		if ev.Type == agent.TextDelta && (ev.TextDelta == "child working" || ev.TextDelta == "child answer") {
			t.Fatalf("child text leaked to parent stream: %+v", ev)
		}
	}

	conversation := session.Conversation()
	if got := rolesOf(conversation); !equalStrings(got, []string{"system", "user", "assistant", "tool", "assistant"}) {
		t.Fatalf("parent roles = %v", got)
	}
	toolTurn := conversation.Turns[3]
	if len(toolTurn.ToolResults) != 1 || toolTurn.ToolResults[0].Result != "child answer" {
		t.Fatalf("parent tool turn = %+v", toolTurn)
	}
	for _, turn := range conversation.Turns {
		if strings.Contains(turn.Content, "child working") || strings.Contains(turn.Content, "config value") {
			t.Fatalf("child intermediate work leaked into parent: %+v", conversation.Turns)
		}
	}
}

func TestChildPermissionRequestReachesParentFrontend(t *testing.T) {
	dir := t.TempDir()
	target := dir + "/child.txt"
	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{taskCall("task-1", "write child file", "write it", []string{"write_file"})}},
		providerStep{calls: []agent.ToolCall{{ID: "write-1", ToolName: "write_file", Arguments: map[string]interface{}{"path": target, "content": "from child"}}}},
		providerStep{text: []string{"write complete"}},
		providerStep{text: []string{"parent complete"}},
	)
	session := agent.NewSession(agent.SessionConfig{
		Provider:         provider,
		SystemPrompt:     "root",
		WorkingDirectory: dir,
	})

	ch, err := session.Run(context.Background(), "delegate write")
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	var events []agent.RunEvent
	permissionCount := 0
	childPermissionCount := 0
	for ev := range ch {
		events = append(events, ev)
		if ev.Type == agent.PermissionRequestedEvent {
			permissionCount++
			if ev.Permission == nil || ev.Permission.ReplyPath == nil {
				t.Fatalf("permission event missing reply path: %+v", ev)
			}
			if ev.Permission.ToolCall.ToolName == "write_file" {
				childPermissionCount++
			}
			ev.Permission.ReplyPath <- agent.PermissionDecision{Allow: true}
		}
	}
	if permissionCount != 2 || childPermissionCount != 1 {
		t.Fatalf("permission requests total=%d child-write=%d, want 2 and 1", permissionCount, childPermissionCount)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "from child" {
		t.Fatalf("child write data=%q err=%v", data, err)
	}
	if result := findToolResult(events); result == nil || result.IsError || result.Result != "write complete" {
		t.Fatalf("task result = %+v", result)
	}
}

func TestTaskRegistrationCompatibilityBoundaries(t *testing.T) {
	t.Run("caller-owned task handler wins", func(t *testing.T) {
		provider := newSessionScriptProvider(
			providerStep{calls: []agent.ToolCall{{ID: "custom-1", ToolName: "task", Arguments: map[string]interface{}{}}}},
			providerStep{text: []string{"done"}},
		)
		session := agent.NewSession(agent.SessionConfig{
			Provider:       provider,
			ToolHandlers:   []agent.ToolHandler{callerTaskHandler{}},
			PermissionMode: "bypass",
		})
		events := drain(t, mustRun(t, session, "use caller task"))
		if result := findToolResult(events); result == nil || result.IsError || result.Result != "caller task result" {
			t.Fatalf("caller task result = %+v", result)
		}
		records := provider.recordsSnapshot()
		if got := descriptorDescription(records[0].tools, "task"); got != "caller-owned task" {
			t.Fatalf("advertised task description = %q", got)
		}
	})

	t.Run("explicit empty tools stays empty", func(t *testing.T) {
		provider := newSessionScriptProvider(providerStep{text: []string{"done"}})
		session := agent.NewSession(agent.SessionConfig{Provider: provider, Tools: []agent.Tool{}})
		drain(t, mustRun(t, session, "no tools"))
		if got := len(provider.recordsSnapshot()[0].tools); got != 0 {
			t.Fatalf("advertised tools = %d, want 0", got)
		}
	})

	t.Run("custom dispatcher remains authoritative", func(t *testing.T) {
		provider := newSessionScriptProvider(
			providerStep{calls: []agent.ToolCall{{ID: "owned-1", ToolName: "task"}}},
			providerStep{text: []string{"done"}},
		)
		dispatcher := &ownedDispatcher{}
		session := agent.NewSession(agent.SessionConfig{
			Provider:   provider,
			Dispatcher: dispatcher,
			Tools:      []agent.Tool{{Name: "task", Description: "dispatcher-owned"}},
		})
		events := drain(t, mustRun(t, session, "use dispatcher"))
		if result := findToolResult(events); result == nil || result.Result != "dispatcher result" {
			t.Fatalf("dispatcher result = %+v", result)
		}
		if dispatcher.calls != 1 {
			t.Fatalf("dispatcher calls = %d", dispatcher.calls)
		}
		if got := len(provider.recordsSnapshot()[0].tools); got != 1 {
			t.Fatalf("custom dispatcher advertised %d tools, want 1", got)
		}
	})
}

func TestMultipleTaskCallsRemainSequential(t *testing.T) {
	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{
			taskCall("task-1", "one", "first child", nil),
			taskCall("task-2", "two", "second child", nil),
		}},
		providerStep{text: []string{"first result"}},
		providerStep{text: []string{"second result"}},
		providerStep{text: []string{"parent done"}},
	)
	session := agent.NewSession(agent.SessionConfig{Provider: provider, PermissionMode: "bypass"})
	events := drain(t, mustRun(t, session, "delegate twice"))

	if got := lifecycleLabels(events); !equalStrings(got, []string{
		"subagent_started:one",
		"subagent_finished:one",
		"subagent_started:two",
		"subagent_finished:two",
	}) {
		t.Fatalf("lifecycle order = %v", got)
	}
	records := provider.recordsSnapshot()
	if len(records) != 4 || lastUser(records[1].conversation) != "first child" || lastUser(records[2].conversation) != "second child" {
		t.Fatalf("provider order = %+v", records)
	}
	conversation := session.Conversation()
	toolResults := conversation.Turns[3].ToolResults
	if len(toolResults) != 2 || toolResults[0].Result != "first result" || toolResults[1].Result != "second result" {
		t.Fatalf("ordered task results = %+v", toolResults)
	}
}

func TestParentCancellationStopsChildProviderAndEndsCancelled(t *testing.T) {
	provider := newSessionScriptProvider(
		providerStep{calls: []agent.ToolCall{taskCall("task-1", "block", "wait forever", nil)}},
		providerStep{block: true},
	)
	session := agent.NewSession(agent.SessionConfig{Provider: provider, PermissionMode: "bypass"})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := session.Run(ctx, "delegate blocking child")
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}

	done := make(chan []agent.RunEvent, 1)
	go func() {
		var events []agent.RunEvent
		for ev := range ch {
			events = append(events, ev)
		}
		done <- events
	}()
	waitClosed(t, provider.blockStarted, "child provider start")
	cancel()

	var events []agent.RunEvent
	select {
	case events = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled parent run did not finish")
	}
	waitClosed(t, provider.blockStopped, "child provider cancellation")
	last := events[len(events)-1]
	if last.RunFinished == nil || last.RunFinished.Reason != agent.StopCancelled {
		t.Fatalf("terminal event = %+v", last)
	}
	for _, ev := range events {
		if ev.Type == agent.ToolFinishedEvent {
			t.Fatalf("cancelled task must not be recorded as recoverable tool result: %+v", ev)
		}
	}
}

type providerStep struct {
	text  []string
	calls []agent.ToolCall
	err   error
	block bool
}

type providerRecord struct {
	conversation agent.Conversation
	tools        []agent.Tool
}

type sessionScriptProvider struct {
	mu           sync.Mutex
	steps        []providerStep
	next         int
	records      []providerRecord
	blockStarted chan struct{}
	blockStopped chan struct{}
	startOnce    sync.Once
	stopOnce     sync.Once
}

func newSessionScriptProvider(steps ...providerStep) *sessionScriptProvider {
	return &sessionScriptProvider{
		steps:        steps,
		blockStarted: make(chan struct{}),
		blockStopped: make(chan struct{}),
	}
}

func (p *sessionScriptProvider) StreamReply(ctx context.Context, conversation agent.Conversation, tools []agent.Tool, _ agent.StreamOptions) <-chan agent.RunEvent {
	p.mu.Lock()
	index := p.next
	p.next++
	p.records = append(p.records, providerRecord{conversation: conversation, tools: cloneTools(tools)})
	var step providerStep
	if index < len(p.steps) {
		step = p.steps[index]
	} else {
		step.err = fmt.Errorf("no provider step %d", index)
	}
	p.mu.Unlock()

	ch := make(chan agent.RunEvent, len(step.text)+len(step.calls)+2)
	go func() {
		defer close(ch)
		if step.block {
			p.startOnce.Do(func() { close(p.blockStarted) })
			<-ctx.Done()
			p.stopOnce.Do(func() { close(p.blockStopped) })
			ch <- agent.RunEvent{Type: agent.ErrorEvent, Error: ctx.Err()}
			return
		}
		if step.err != nil {
			ch <- agent.RunEvent{Type: agent.ErrorEvent, Error: step.err}
			return
		}
		for _, text := range step.text {
			ch <- agent.RunEvent{Type: agent.TextDelta, TextDelta: text}
		}
		for _, call := range step.calls {
			copyCall := call
			ch <- agent.RunEvent{Type: agent.ToolCallEvent, ToolCall: &copyCall}
		}
		ch <- agent.RunEvent{Type: agent.ReplyEndedEvent, ReplyEnded: &agent.ReplyEnded{Reason: agent.Finished}}
	}()
	return ch
}

func (p *sessionScriptProvider) recordsSnapshot() []providerRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]providerRecord(nil), p.records...)
}

type callerTaskHandler struct{}

func (callerTaskHandler) Descriptor() agent.Tool {
	return agent.Tool{Name: "task", Description: "caller-owned task", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (callerTaskHandler) Execute(context.Context, map[string]interface{}) (string, error) {
	return "caller task result", nil
}
func (callerTaskHandler) RunsCommands() bool           { return false }
func (callerTaskHandler) ActionKind() agent.ActionKind { return agent.ActionRead }

type ownedDispatcher struct{ calls int }

func (d *ownedDispatcher) Dispatch(_ context.Context, call agent.ToolCall, _ func(agent.RunEvent) error) (agent.ToolResult, error) {
	d.calls++
	return agent.ToolResult{ToolCallID: call.ID, Result: "dispatcher result"}, nil
}

func taskCall(id, label, instruction string, tools []string) agent.ToolCall {
	arguments := map[string]interface{}{"label": label, "instruction": instruction}
	if tools != nil {
		items := make([]interface{}, len(tools))
		for i, name := range tools {
			items[i] = name
		}
		arguments["tools"] = items
	}
	return agent.ToolCall{ID: id, ToolName: "task", Arguments: arguments}
}

func lifecycleLabels(events []agent.RunEvent) []string {
	var out []string
	for _, ev := range events {
		if ev.Type != agent.StatusChange || (ev.Status != agent.StatusSubagentStarted && ev.Status != agent.StatusSubagentFinished) {
			continue
		}
		label := ""
		if ev.ToolCall != nil {
			label, _ = ev.ToolCall.Arguments["label"].(string)
		}
		out = append(out, ev.Status+":"+label)
	}
	return out
}

func namesOfTools(tools []agent.Tool) []string {
	out := make([]string, len(tools))
	for i, tool := range tools {
		out[i] = tool.Name
	}
	return out
}

func rolesOf(conversation agent.Conversation) []string {
	out := make([]string, len(conversation.Turns))
	for i, turn := range conversation.Turns {
		out[i] = turn.Role
	}
	return out
}

func lastUser(conversation agent.Conversation) string {
	for i := len(conversation.Turns) - 1; i >= 0; i-- {
		if conversation.Turns[i].Role == "user" {
			return conversation.Turns[i].Content
		}
	}
	return ""
}

func descriptorDescription(tools []agent.Tool, name string) string {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Description
		}
	}
	return ""
}

func cloneTools(tools []agent.Tool) []agent.Tool {
	out := make([]agent.Tool, len(tools))
	for i, tool := range tools {
		out[i] = tool
		out[i].Parameters = append(json.RawMessage(nil), tool.Parameters...)
	}
	return out
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

var _ agent.Provider = (*sessionScriptProvider)(nil)
var _ agent.ToolHandler = callerTaskHandler{}
var _ agent.ActionClassifier = callerTaskHandler{}
var _ agent.Dispatcher = (*ownedDispatcher)(nil)
