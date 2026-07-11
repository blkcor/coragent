package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
)

func TestNestedDelegationCannotRestoreExplicitlyRemovedCapabilities(t *testing.T) {
	tests := []struct {
		name         string
		capability   string
		runsCommands bool
	}{
		{name: "write", capability: "write_file"},
		{name: "command", capability: "run_command", runsCommands: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			read := newReviewCountedTool("read_file", false)
			removed := newReviewCountedTool(tc.capability, tc.runsCommands)
			catalog := catalogWith(t, read, removed)
			provider := newReviewRouteProvider(map[string][]scriptedReply{
				"narrow child": {
					toolReply("", reviewTaskCall("nested-task", "grandchild explicit", "grandchild", tc.capability)),
					finalReply("child handled grandchild"),
				},
				"grandchild explicit": {
					toolReply("", core.ToolCall{ID: "removed-call", ToolName: tc.capability, Arguments: map[string]interface{}{}}),
					finalReply("grandchild handled refusal"),
				},
			})
			handler := handlerFor(provider, catalog, catalog.Advertise(), 6, nil)

			got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
				"label":       "narrow",
				"instruction": "narrow child",
				"tools":       []interface{}{"read_file"},
			}, func(core.RunEvent) error { return nil })
			if err != nil {
				t.Fatalf("ExecuteWithEvents(): %v", err)
			}
			if got != "child handled grandchild" {
				t.Fatalf("result = %q", got)
			}
			if calls := removed.callCount(); calls != 0 {
				t.Fatalf("grandchild restored and executed removed %s capability %d times", tc.name, calls)
			}

			grandchildCalls := provider.snapshotCalls("grandchild explicit")
			if len(grandchildCalls) != 2 {
				t.Fatalf("grandchild provider calls = %d, want 2", len(grandchildCalls))
			}
			if reviewHasTool(grandchildCalls[0].tools, tc.capability) {
				t.Fatalf("grandchild re-advertised removed capability %q: %+v", tc.capability, grandchildCalls[0].tools)
			}
			result := reviewLastToolResult(t, grandchildCalls[1].conversation)
			if !result.IsError || !strings.Contains(result.Result, "unknown tool") {
				t.Fatalf("removed capability result = %+v, want recoverable unknown-tool error", result)
			}
			if active := provider.active.Load(); active != 0 {
				t.Fatalf("active provider streams after completion = %d", active)
			}
		})
	}
}

func TestNestedDelegationDefaultsCannotRestoreRemovedReadCapability(t *testing.T) {
	read := newReviewCountedTool("read_file", false)
	write := newReviewCountedTool("write_file", false)
	catalog := catalogWith(t, read, write)
	provider := newReviewRouteProvider(map[string][]scriptedReply{
		"write-only child": {
			toolReply("", reviewTaskCall("nested-default-task", "default grandchild", "grandchild")),
			finalReply("child handled grandchild"),
		},
		"default grandchild": {
			toolReply("", core.ToolCall{ID: "read-call", ToolName: "read_file", Arguments: map[string]interface{}{}}),
			finalReply("grandchild handled refusal"),
		},
	})
	handler := handlerFor(provider, catalog, catalog.Advertise(), 6, nil)

	got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "write only",
		"instruction": "write-only child",
		"tools":       []interface{}{"write_file"},
	}, func(core.RunEvent) error { return nil })
	if err != nil {
		t.Fatalf("ExecuteWithEvents(): %v", err)
	}
	if got != "child handled grandchild" {
		t.Fatalf("result = %q", got)
	}
	if calls := read.callCount(); calls != 0 {
		t.Fatalf("grandchild defaults restored removed read capability %d times", calls)
	}

	grandchildCalls := provider.snapshotCalls("default grandchild")
	if len(grandchildCalls) != 2 {
		t.Fatalf("grandchild provider calls = %d, want 2", len(grandchildCalls))
	}
	if reviewHasTool(grandchildCalls[0].tools, "read_file") {
		t.Fatalf("grandchild defaults re-advertised removed read capability: %+v", grandchildCalls[0].tools)
	}
	result := reviewLastToolResult(t, grandchildCalls[1].conversation)
	if !result.IsError || !strings.Contains(result.Result, "unknown tool") {
		t.Fatalf("removed default capability result = %+v, want recoverable unknown-tool error", result)
	}
}

func TestParentEmitterFailureCancelsChildTreeWithoutOrphans(t *testing.T) {
	sentinel := errors.New("review emitter closed")

	t.Run("permission request", func(t *testing.T) {
		guarded := newReviewCountedTool("guarded_read", false)
		catalog := catalogWith(t, guarded)
		provider := newReviewRouteProvider(map[string][]scriptedReply{
			"permission child": {
				toolReply("", core.ToolCall{ID: "guarded-call", ToolName: "guarded_read", Arguments: map[string]interface{}{}}),
			},
		})
		permission := newReviewBlockingPermission()
		stages := executor.InertStages()
		stages.Permission = permission
		handler := handlerForStages(provider, catalog, catalog.Advertise(), 4, nil, stages)
		events := &eventCollector{}
		done := make(chan taskCallOutcome, 1)

		go func() {
			result, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
				"label":       "permission",
				"instruction": "permission child",
				"tools":       []interface{}{"guarded_read"},
			}, func(ev core.RunEvent) error {
				_ = events.emit(ev)
				if ev.Type == core.PermissionRequestedEvent {
					return sentinel
				}
				return nil
			})
			done <- taskCallOutcome{result: result, err: err}
		}()

		waitFor(t, permission.started, "child permission wait to start")
		outcome := waitForTaskCall(t, done, "permission forwarding failure")
		if outcome.result != "" || !errors.Is(outcome.err, sentinel) {
			t.Fatalf("result=%q error=%v, want wrapped emitter sentinel", outcome.result, outcome.err)
		}
		waitFor(t, permission.stopped, "child permission wait to observe cancellation")
		if active := permission.active.Load(); active != 0 {
			t.Fatalf("active permission waits after emitter failure = %d", active)
		}
		if calls := guarded.callCount(); calls != 0 {
			t.Fatalf("guarded handler ran after failed permission forwarding %d times", calls)
		}
		if active := provider.active.Load(); active != 0 {
			t.Fatalf("active provider streams after permission forwarding failure = %d", active)
		}
		assertLifecycleEvents(t, events.snapshot(), []string{
			core.StatusSubagentStarted + ":permission",
			core.StatusSubagentFinished + ":permission",
		})
	})

	t.Run("nested lifecycle", func(t *testing.T) {
		catalog := catalogWith(t)
		provider := newReviewRouteProvider(map[string][]scriptedReply{
			"outer child": {
				toolReply("", reviewTaskCall("nested-task", "grandchild work", "grandchild")),
				finalReply("outer child must not continue"),
			},
			"grandchild work": {finalReply("grandchild done")},
		})
		handler := handlerFor(provider, catalog, nil, 5, nil)
		events := &eventCollector{}
		done := make(chan taskCallOutcome, 1)

		go func() {
			result, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
				"label":       "outer",
				"instruction": "outer child",
			}, func(ev core.RunEvent) error {
				_ = events.emit(ev)
				if ev.Type == core.StatusChange && ev.Status == core.StatusSubagentFinished && reviewEventLabel(ev) == "grandchild" {
					return sentinel
				}
				return nil
			})
			done <- taskCallOutcome{result: result, err: err}
		}()

		outcome := waitForTaskCall(t, done, "nested lifecycle forwarding failure")
		if outcome.result != "" || !errors.Is(outcome.err, sentinel) {
			t.Fatalf("result=%q error=%v, want wrapped emitter sentinel", outcome.result, outcome.err)
		}
		if calls := len(provider.snapshotCalls("outer child")); calls != 1 {
			t.Fatalf("outer child continued after derived-context cancellation: provider calls=%d", calls)
		}
		if calls := len(provider.snapshotCalls("grandchild work")); calls != 1 {
			t.Fatalf("grandchild provider calls = %d, want 1 completed call", calls)
		}
		if active := provider.active.Load(); active != 0 {
			t.Fatalf("active descendant provider streams after lifecycle forwarding failure = %d", active)
		}
		assertLifecycleEvents(t, events.snapshot(), []string{
			core.StatusSubagentStarted + ":outer",
			core.StatusSubagentStarted + ":grandchild",
			core.StatusSubagentFinished + ":grandchild",
			core.StatusSubagentFinished + ":outer",
		})
	})
}

func TestChildEventBoundaryIsStrictAndFailedChildFinishesBeforeReturn(t *testing.T) {
	hooks := reviewNoisyLifecycleHooks{}
	catalog := catalogWith(t)
	provider := newReviewRouteProvider(map[string][]scriptedReply{
		"fail noisily": {failureReply(errors.New("child backend failed"))},
	})
	handler := handlerFor(provider, catalog, nil, 3, hooks)
	events := &eventCollector{}

	result, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "noisy failure",
		"instruction": "fail noisily",
	}, events.emit)
	if result != "" || err == nil || !strings.Contains(err.Error(), "child backend failed") {
		t.Fatalf("result=%q error=%v, want recoverable child failure", result, err)
	}

	visible := events.snapshot()
	if len(visible) != 2 {
		t.Fatalf("visible events = %+v, want only matching lifecycle pair", visible)
	}
	assertLifecycleEvents(t, visible, []string{
		core.StatusSubagentStarted + ":noisy failure",
		core.StatusSubagentFinished + ":noisy failure",
	})
	if last := visible[len(visible)-1]; last.Type != core.StatusChange || last.Status != core.StatusSubagentFinished {
		t.Fatalf("last event before handler result = %+v, want matching finished status", last)
	}
	if active := provider.active.Load(); active != 0 {
		t.Fatalf("active provider streams after failed child = %d", active)
	}
}

func TestNestedDelegationDoesNotRequireTaskRequestAndDepthRefusalIsRecoverable(t *testing.T) {
	provider := newReviewRouteProvider(map[string][]scriptedReply{
		"depth one": {
			toolReply("", reviewTaskCall("task-two", "depth two", "two")),
			finalReply("depth one final"),
		},
		"depth two": {
			toolReply("", reviewTaskCall("task-three", "depth three", "three")),
			finalReply("depth two final"),
		},
		"depth three": {
			toolReply("", reviewTaskCall("task-four", "depth four", "four")),
			finalReply("depth three recovered"),
		},
	})
	catalog := catalogWith(t)
	handler := handlerFor(provider, catalog, nil, 8, nil)
	events := &eventCollector{}

	got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "one",
		"instruction": "depth one",
	}, events.emit)
	if err != nil {
		t.Fatalf("ExecuteWithEvents(): %v", err)
	}
	if got != "depth one final" {
		t.Fatalf("result = %q", got)
	}
	for _, instruction := range []string{"depth one", "depth two", "depth three"} {
		if calls := len(provider.snapshotCalls(instruction)); calls != 2 {
			t.Fatalf("provider calls for %q = %d, want 2", instruction, calls)
		}
	}
	if calls := len(provider.snapshotCalls("depth four")); calls != 0 {
		t.Fatalf("over-depth child started with %d provider calls", calls)
	}

	depthThreeCalls := provider.snapshotCalls("depth three")
	depthResult := reviewLastToolResult(t, depthThreeCalls[1].conversation)
	if !depthResult.IsError || !strings.Contains(depthResult.Result, "maximum delegation depth 3 exceeded") {
		t.Fatalf("depth refusal result = %+v", depthResult)
	}
	assertLifecycleEvents(t, events.snapshot(), []string{
		core.StatusSubagentStarted + ":one",
		core.StatusSubagentStarted + ":two",
		core.StatusSubagentStarted + ":three",
		core.StatusSubagentFinished + ":three",
		core.StatusSubagentFinished + ":two",
		core.StatusSubagentFinished + ":one",
	})
}

func reviewTaskCall(id, instruction, label string, requested ...string) core.ToolCall {
	arguments := map[string]interface{}{
		"label":       label,
		"instruction": instruction,
	}
	if len(requested) > 0 {
		selected := make([]interface{}, len(requested))
		for i, name := range requested {
			selected[i] = name
		}
		arguments["tools"] = selected
	}
	return core.ToolCall{ID: id, ToolName: ToolName, Arguments: arguments}
}

func reviewHasTool(advertised []core.Tool, name string) bool {
	for _, descriptor := range advertised {
		if descriptor.Name == name {
			return true
		}
	}
	return false
}

func reviewLastToolResult(t *testing.T, conv core.Conversation) core.ToolResult {
	t.Helper()
	if len(conv.Turns) == 0 {
		t.Fatal("conversation has no turns")
	}
	results := conv.Turns[len(conv.Turns)-1].ToolResults
	if len(results) != 1 {
		t.Fatalf("last turn tool results = %+v, want exactly one", results)
	}
	return results[0]
}

func reviewEventLabel(ev core.RunEvent) string {
	if ev.ToolCall == nil {
		return ""
	}
	label, _ := ev.ToolCall.Arguments["label"].(string)
	return label
}

type reviewCountedTool struct {
	descriptor   core.Tool
	commands     bool
	executeCalls atomic.Int32
}

func newReviewCountedTool(name string, commands bool) *reviewCountedTool {
	return &reviewCountedTool{
		descriptor: core.Tool{
			Name:        name,
			Description: "review counted tool " + name,
			Parameters:  []byte(`{"type":"object"}`),
		},
		commands: commands,
	}
}

func (h *reviewCountedTool) Descriptor() core.Tool { return h.descriptor }

func (h *reviewCountedTool) Execute(context.Context, map[string]interface{}) (string, error) {
	h.executeCalls.Add(1)
	return "unexpected execution", nil
}

func (h *reviewCountedTool) RunsCommands() bool { return h.commands }

func (h *reviewCountedTool) ActionKind() core.ActionKind {
	if h.commands {
		return core.ActionCommand
	}
	return core.ActionEdit
}

func (h *reviewCountedTool) callCount() int { return int(h.executeCalls.Load()) }

type reviewRouteProvider struct {
	mu      sync.Mutex
	routes  map[string][]scriptedReply
	indices map[string]int
	calls   map[string][]providerCall
	active  atomic.Int32
}

func newReviewRouteProvider(routes map[string][]scriptedReply) *reviewRouteProvider {
	return &reviewRouteProvider{
		routes:  routes,
		indices: make(map[string]int),
		calls:   make(map[string][]providerCall),
	}
}

func (p *reviewRouteProvider) StreamReply(ctx context.Context, conv core.Conversation, advertised []core.Tool, _ core.StreamOptions) <-chan core.RunEvent {
	key := lastUserInstruction(conv)
	p.mu.Lock()
	index := p.indices[key]
	p.indices[key] = index + 1
	p.calls[key] = append(p.calls[key], providerCall{
		conversation: conv,
		tools:        append([]core.Tool(nil), advertised...),
	})
	replies := p.routes[key]
	var events []core.RunEvent
	if index >= len(replies) {
		events = []core.RunEvent{{Type: core.ErrorEvent, Error: errors.New("review provider: missing scripted reply")}}
	} else {
		events = append([]core.RunEvent(nil), replies[index].events...)
	}
	p.mu.Unlock()

	ch := make(chan core.RunEvent)
	p.active.Add(1)
	go func() {
		defer close(ch)
		defer p.active.Add(-1)
		for _, ev := range events {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func (p *reviewRouteProvider) snapshotCalls(instruction string) []providerCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]providerCall(nil), p.calls[instruction]...)
}

type reviewBlockingPermission struct {
	started chan struct{}
	stopped chan struct{}
	active  atomic.Int32
	once    sync.Once
}

func newReviewBlockingPermission() *reviewBlockingPermission {
	return &reviewBlockingPermission{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (p *reviewBlockingPermission) Decide(ctx context.Context, call core.ToolCall, _ core.ActionKind, emit func(core.RunEvent) error) core.PermissionResult {
	p.active.Add(1)
	p.once.Do(func() { close(p.started) })
	defer func() {
		p.active.Add(-1)
		close(p.stopped)
	}()

	reply := make(chan core.PermissionDecision)
	_ = emit(core.RunEvent{
		Type: core.PermissionRequestedEvent,
		Permission: &core.PermissionRequest{
			ToolCall:  call,
			Reason:    "review permission",
			ReplyPath: reply,
		},
	})
	select {
	case decision := <-reply:
		return core.PermissionResult{Allow: decision.Allow}
	case <-ctx.Done():
		return core.PermissionResult{Allow: false, Reason: ctx.Err().Error()}
	}
}

type reviewNoisyLifecycleHooks struct{}

func (reviewNoisyLifecycleHooks) SessionStart(_ context.Context, _ core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	call := core.ToolCall{ID: "private", ToolName: "private_tool"}
	result := core.ToolResult{ToolCallID: call.ID, Result: "private result"}
	finished := core.RunFinished{Reason: core.StopFailed, Err: errors.New("private terminal")}
	privateEvents := []core.RunEvent{
		{Type: core.StatusChange, Status: core.StatusThinking},
		{Type: core.TextDelta, TextDelta: "private text"},
		{Type: core.ToolCallEvent, ToolCall: &call},
		{Type: core.ToolStartedEvent, ToolCall: &call},
		{Type: core.ToolFinishedEvent, ToolResult: &result},
		{Type: core.HookOutcomeEvent, HookOutcome: &core.HookOutcome{Moment: core.HookSessionStart, Action: core.HookAllowed}},
		{Type: core.ErrorEvent, Error: errors.New("private error")},
		{Type: core.OverBudgetWarningEvent, Warning: "private warning"},
		{Type: core.ReplyEndedEvent, ReplyEnded: &core.ReplyEnded{Reason: core.Finished}},
		{Type: core.RunFinishedEvent, RunFinished: &finished},
	}
	for _, ev := range privateEvents {
		_ = emit(ev)
	}
	return core.HookLifecycleResult{}
}

func (reviewNoisyLifecycleHooks) PromptSubmit(context.Context, string, core.Conversation, func(core.RunEvent) error) core.HookLifecycleResult {
	return core.HookLifecycleResult{}
}

func (reviewNoisyLifecycleHooks) RunFinished(context.Context, core.RunFinished, core.Conversation, func(core.RunEvent) error) core.HookLifecycleResult {
	return core.HookLifecycleResult{}
}

func (reviewNoisyLifecycleHooks) SessionStop(context.Context, core.Conversation, func(core.RunEvent) error) core.HookLifecycleResult {
	return core.HookLifecycleResult{}
}

var (
	_ core.ToolHandler      = (*reviewCountedTool)(nil)
	_ core.ActionClassifier = (*reviewCountedTool)(nil)
	_ core.Provider         = (*reviewRouteProvider)(nil)
	_ core.Permission       = (*reviewBlockingPermission)(nil)
	_ core.LifecycleHooks   = reviewNoisyLifecycleHooks{}
)
