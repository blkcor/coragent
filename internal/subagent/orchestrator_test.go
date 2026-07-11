package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
	"github.com/blkcor/coragent/internal/tools"
)

func TestParseRequestValidatesAndNormalizesArguments(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr string
	}{
		{name: "missing label", args: map[string]interface{}{"instruction": "work"}, wantErr: "label"},
		{name: "blank label", args: map[string]interface{}{"label": " \t ", "instruction": "work"}, wantErr: "label"},
		{name: "non-string label", args: map[string]interface{}{"label": 7, "instruction": "work"}, wantErr: "label"},
		{name: "missing instruction", args: map[string]interface{}{"label": "work"}, wantErr: "instruction"},
		{name: "blank instruction", args: map[string]interface{}{"label": "work", "instruction": "\n "}, wantErr: "instruction"},
		{name: "tools is not an array", args: map[string]interface{}{"label": "work", "instruction": "do it", "tools": "read_file"}, wantErr: "array of strings"},
		{name: "tools contains non-string", args: map[string]interface{}{"label": "work", "instruction": "do it", "tools": []interface{}{"read_file", 3}}, wantErr: "only strings"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRequest(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parseRequest() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}

	for _, toolsValue := range []interface{}{nil, []string{"read_file"}, []interface{}{"read_file", "find_files"}} {
		args := map[string]interface{}{
			"label":       "  inspect config  ",
			"instruction": "  locate defaults  ",
		}
		if toolsValue != nil {
			args["tools"] = toolsValue
		}
		got, err := parseRequest(args)
		if err != nil {
			t.Fatalf("parseRequest(valid args): %v", err)
		}
		if got.label != "inspect config" || got.instruction != "locate defaults" {
			t.Fatalf("normalized request = %+v", got)
		}
	}
}

func TestInvalidTaskArgumentsDoNotStartChildOrEmitLifecycle(t *testing.T) {
	invalid := []map[string]interface{}{
		{"instruction": "work"},
		{"label": "   ", "instruction": "work"},
		{"label": "work", "instruction": "\t"},
		{"label": "work", "instruction": "do it", "tools": []interface{}{"read_file", false}},
	}
	for i, args := range invalid {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			provider := newQueueProvider(finalReply("must not run"))
			catalog := catalogWith(t)
			handler := handlerFor(provider, catalog, nil, 3, nil)
			events := &eventCollector{}

			if _, err := handler.ExecuteWithEvents(context.Background(), args, events.emit); err == nil {
				t.Fatal("invalid task unexpectedly succeeded")
			}
			if calls := len(provider.snapshotCalls()); calls != 0 {
				t.Fatalf("invalid task started provider %d times", calls)
			}
			if got := lifecycleStrings(events.snapshot()); len(got) != 0 {
				t.Fatalf("invalid task emitted lifecycle status: %v", got)
			}
		})
	}
}

func TestTaskHandlerUsesFreshContextSafeDefaultsAndSuppressesRawChildEvents(t *testing.T) {
	read := newStubTool("read_file", "file contents")
	write := newStubTool("write_file", "wrote")
	search := newStubTool("search_content", "match")
	find := newStubTool("find_files", "file.go")
	catalog := catalogWith(t, read, write, search, find)
	provider := newQueueProvider(
		toolReply("private intermediate text", core.ToolCall{ID: "read-1", ToolName: "read_file", Arguments: map[string]interface{}{}}),
		finalReply("child final answer"),
	)
	handler := handlerFor(provider, catalog, catalog.Advertise(), 5, nil)
	events := &eventCollector{}

	got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "inspect config",
		"instruction": "find the defaults",
	}, events.emit)
	if err != nil {
		t.Fatalf("ExecuteWithEvents(): %v", err)
	}
	if got != "child final answer" {
		t.Fatalf("result = %q, want final answer only", got)
	}
	if read.callCount() != 1 || write.callCount() != 0 {
		t.Fatalf("tool calls: read=%d write=%d", read.callCount(), write.callCount())
	}

	calls := provider.snapshotCalls()
	if len(calls) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(calls))
	}
	assertConversation(t, calls[0].conversation, []core.Turn{
		{Role: "system", Content: childSystemPrompt},
		{Role: "user", Content: "find the defaults"},
	})
	assertToolNames(t, calls[0].tools, []string{"read_file", "search_content", "find_files", ToolName})

	visible := events.snapshot()
	assertLifecycleEvents(t, visible, []string{
		core.StatusSubagentStarted + ":inspect config",
		core.StatusSubagentFinished + ":inspect config",
	})
	for _, ev := range visible {
		if ev.Type == core.TextDelta || ev.Type == core.ToolStartedEvent || ev.Type == core.ToolFinishedEvent || ev.Type == core.RunFinishedEvent {
			t.Fatalf("raw child event leaked to parent: %+v", ev)
		}
	}
}

func TestTaskHandlerExplicitSubsetCannotExecuteOutOfSetTool(t *testing.T) {
	read := newStubTool("read_file", "read")
	write := newStubTool("write_file", "write")
	catalog := catalogWith(t, read, write)
	provider := newQueueProvider(
		toolReply("", core.ToolCall{ID: "write-1", ToolName: "write_file", Arguments: map[string]interface{}{}}),
		finalReply("recovered from refusal"),
	)
	handler := handlerFor(provider, catalog, catalog.Advertise(), 5, nil)

	got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "read only",
		"instruction": "inspect without writing",
		"tools":       []interface{}{"read_file", "missing_tool"},
	}, func(core.RunEvent) error { return nil })
	if err != nil {
		t.Fatalf("ExecuteWithEvents(): %v", err)
	}
	if got != "recovered from refusal" {
		t.Fatalf("result = %q", got)
	}
	if write.callCount() != 0 {
		t.Fatalf("out-of-set write handler ran %d times", write.callCount())
	}

	calls := provider.snapshotCalls()
	if len(calls) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(calls))
	}
	assertToolNames(t, calls[0].tools, []string{"read_file", ToolName})
	results := calls[1].conversation.Turns[len(calls[1].conversation.Turns)-1].ToolResults
	if len(results) != 1 || !results[0].IsError || !strings.Contains(results[0].Result, "unknown tool") {
		t.Fatalf("out-of-set result = %+v", results)
	}
}

func TestTaskHandlerTerminalOutcomes(t *testing.T) {
	t.Run("empty final answer succeeds", func(t *testing.T) {
		catalog := catalogWith(t)
		handler := handlerFor(newQueueProvider(finalReply("")), catalog, nil, 3, nil)
		got, err := handler.ExecuteWithEvents(context.Background(), validTaskArgs(), func(core.RunEvent) error { return nil })
		if err != nil || got != "" {
			t.Fatalf("result=%q error=%v, want successful empty answer", got, err)
		}
	})

	t.Run("provider failure is recoverable handler error", func(t *testing.T) {
		catalog := catalogWith(t)
		handler := handlerFor(newQueueProvider(failureReply(errors.New("backend unavailable"))), catalog, nil, 3, nil)
		_, err := handler.ExecuteWithEvents(context.Background(), validTaskArgs(), func(core.RunEvent) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "child failed") || !strings.Contains(err.Error(), "backend unavailable") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("step limit rejects partial answer", func(t *testing.T) {
		read := newStubTool("read_file", "result")
		catalog := catalogWith(t, read)
		provider := newQueueProvider(toolReply("partial answer", core.ToolCall{ID: "read-1", ToolName: "read_file"}))
		handler := handlerFor(provider, catalog, catalog.Advertise(), 1, nil)
		got, err := handler.ExecuteWithEvents(context.Background(), validTaskArgs(), func(core.RunEvent) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "step limit") {
			t.Fatalf("result=%q error=%v", got, err)
		}
		if strings.Contains(got, "partial answer") {
			t.Fatalf("partial child text escaped as final result: %q", got)
		}
	})

	t.Run("missing final assistant is distinguished from empty", func(t *testing.T) {
		if got, ok := finalAssistant(core.Conversation{Turns: []core.Turn{{Role: "tool"}}}); ok || got != "" {
			t.Fatalf("malformed conversation returned (%q, %v)", got, ok)
		}
		if got, ok := finalAssistant(core.Conversation{Turns: []core.Turn{{Role: "assistant", Content: ""}}}); !ok || got != "" {
			t.Fatalf("explicit empty assistant returned (%q, %v)", got, ok)
		}
	})
}

func TestTaskHandlerAllowsThreeDelegationEdgesAndForwardsNestedLifecycle(t *testing.T) {
	provider := newRoutingProvider(map[string][]scriptedReply{
		"depth one": {
			toolReply("", nestedTaskCall("task-2", "depth two", "two")),
			finalReply("depth one final"),
		},
		"depth two": {
			toolReply("", nestedTaskCall("task-3", "depth three", "three")),
			finalReply("depth two final"),
		},
		"depth three": {
			toolReply("", nestedTaskCall("task-4", "depth four", "four")),
			finalReply("depth three handled the refusal"),
		},
	})
	catalog := catalogWith(t)
	handler := handlerFor(provider, catalog, nil, 8, nil)
	events := &eventCollector{}

	got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "one",
		"instruction": "depth one",
		"tools":       []interface{}{ToolName},
	}, events.emit)
	if err != nil {
		t.Fatalf("ExecuteWithEvents(): %v", err)
	}
	if got != "depth one final" {
		t.Fatalf("result = %q", got)
	}
	for _, instruction := range []string{"depth one", "depth two", "depth three"} {
		if got := provider.callCount(instruction); got != 2 {
			t.Fatalf("provider calls for %q = %d, want 2", instruction, got)
		}
	}
	if got := provider.callCount("depth four"); got != 0 {
		t.Fatalf("over-depth child started with %d provider calls", got)
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

func TestTaskHandlerParentCancellationStopsChildWithoutOrphan(t *testing.T) {
	provider := newBlockingProvider()
	catalog := catalogWith(t)
	handler := handlerFor(provider, catalog, nil, 3, nil)
	events := &eventCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	go func() {
		_, err := handler.ExecuteWithEvents(ctx, validTaskArgs(), events.emit)
		result <- err
	}()

	waitFor(t, provider.started, "child provider to start")
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task handler did not return promptly after parent cancellation")
	}
	waitFor(t, provider.stopped, "child provider to stop")
	if active := provider.active.Load(); active != 0 {
		t.Fatalf("active child providers after cancellation = %d", active)
	}

	statuses := lifecycleStrings(events.snapshot())
	if len(statuses) != 1 || statuses[0] != core.StatusSubagentStarted+":test task" {
		t.Fatalf("cancellation lifecycle status = %v; finished must not delay cancellation", statuses)
	}
}

func TestTaskHandlerForwardsPermissionRequestAndReplyPath(t *testing.T) {
	guarded := newStubTool("guarded_read", "approved result")
	catalog := catalogWith(t, guarded)
	provider := newQueueProvider(
		toolReply("", core.ToolCall{ID: "guarded-1", ToolName: "guarded_read"}),
		finalReply("permission completed"),
	)
	stages := executor.InertStages()
	stages.Permission = promptingPermission{}
	handler := handlerForStages(provider, catalog, catalog.Advertise(), 5, nil, stages)
	events := &eventCollector{}

	emit := func(ev core.RunEvent) error {
		if err := events.emit(ev); err != nil {
			return err
		}
		if ev.Type == core.PermissionRequestedEvent {
			if ev.Permission == nil || ev.Permission.ReplyPath == nil {
				return errors.New("permission request missing reply path")
			}
			ev.Permission.ReplyPath <- core.PermissionDecision{Allow: true}
		}
		return nil
	}
	got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "permission",
		"instruction": "use the guarded reader",
		"tools":       []interface{}{"guarded_read"},
	}, emit)
	if err != nil {
		t.Fatalf("ExecuteWithEvents(): %v", err)
	}
	if got != "permission completed" || guarded.callCount() != 1 {
		t.Fatalf("result=%q guarded calls=%d", got, guarded.callCount())
	}

	visible := events.snapshot()
	permissionCount := 0
	for _, ev := range visible {
		if ev.Type == core.PermissionRequestedEvent {
			permissionCount++
		}
		if ev.Type == core.ToolStartedEvent || ev.Type == core.ToolFinishedEvent || ev.Type == core.TextDelta {
			t.Fatalf("raw child event leaked beside permission request: %+v", ev)
		}
	}
	if permissionCount != 1 {
		t.Fatalf("permission events = %d, want 1", permissionCount)
	}
}

func TestTaskHandlerRunsChildLifecycleWithChildScopedInjection(t *testing.T) {
	hooks := &recordingLifecycleHooks{
		startInjection:  "child standing context",
		promptInjection: "child transient context",
		blockFinished:   true,
	}
	provider := newQueueProvider(finalReply("answer survives run-finished block"))
	catalog := catalogWith(t)
	handler := handlerFor(provider, catalog, nil, 3, hooks)
	events := &eventCollector{}

	got, err := handler.ExecuteWithEvents(context.Background(), map[string]interface{}{
		"label":       "hooks",
		"instruction": "child instruction",
	}, events.emit)
	if err != nil {
		t.Fatalf("ExecuteWithEvents(): %v", err)
	}
	if got != "answer survives run-finished block" {
		t.Fatalf("result = %q", got)
	}
	if hooks.counts() != [4]int{1, 1, 1, 1} {
		t.Fatalf("lifecycle counts = %v", hooks.counts())
	}

	calls := provider.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("provider calls = %d", len(calls))
	}
	assertConversation(t, calls[0].conversation, []core.Turn{
		{Role: "system", Content: childSystemPrompt},
		{Role: "system", Content: "child transient context"},
		{Role: "system", Content: "child standing context"},
		{Role: "user", Content: "child instruction"},
	})
	for _, ev := range events.snapshot() {
		if ev.Type == core.HookOutcomeEvent {
			t.Fatalf("child hook outcome leaked to parent: %+v", ev)
		}
	}

	t.Run("startup block prevents provider work", func(t *testing.T) {
		blockedHooks := &recordingLifecycleHooks{blockStart: true}
		blockedProvider := newQueueProvider(finalReply("must not run"))
		blockedHandler := handlerFor(blockedProvider, catalog, nil, 3, blockedHooks)
		_, err := blockedHandler.ExecuteWithEvents(context.Background(), validTaskArgs(), func(core.RunEvent) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "startup failed") {
			t.Fatalf("startup error = %v", err)
		}
		if calls := len(blockedProvider.snapshotCalls()); calls != 0 {
			t.Fatalf("provider ran %d times after startup block", calls)
		}
	})

	t.Run("cleanup block replaces successful answer with error", func(t *testing.T) {
		cleanupHooks := &recordingLifecycleHooks{blockStop: true}
		cleanupProvider := newQueueProvider(finalReply("hidden by cleanup failure"))
		cleanupHandler := handlerFor(cleanupProvider, catalog, nil, 3, cleanupHooks)
		got, err := cleanupHandler.ExecuteWithEvents(context.Background(), validTaskArgs(), func(core.RunEvent) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "cleanup failed") || got != "" {
			t.Fatalf("result=%q cleanup error=%v", got, err)
		}
	})
}

func validTaskArgs() map[string]interface{} {
	return map[string]interface{}{
		"label":       "test task",
		"instruction": "do the child work",
	}
}

func nestedTaskCall(id, instruction, label string) core.ToolCall {
	return core.ToolCall{
		ID:       id,
		ToolName: ToolName,
		Arguments: map[string]interface{}{
			"label":       label,
			"instruction": instruction,
			"tools":       []interface{}{ToolName},
		},
	}
}

func handlerFor(provider core.Provider, catalog *tools.Catalog, advertised []core.Tool, maxRounds int, hooks core.LifecycleHooks) *TaskHandler {
	return handlerForStages(provider, catalog, advertised, maxRounds, hooks, executor.InertStages())
}

func handlerForStages(provider core.Provider, catalog *tools.Catalog, advertised []core.Tool, maxRounds int, hooks core.LifecycleHooks, stages executor.Stages) *TaskHandler {
	blueprint := NewBlueprint(BlueprintConfig{
		Provider:   provider,
		Catalog:    catalog,
		Advertised: advertised,
		Stages:     stages,
		Hooks:      hooks,
		MaxRounds:  maxRounds,
	})
	return NewTaskHandler(blueprint)
}

func catalogWith(t *testing.T, handlers ...core.ToolHandler) *tools.Catalog {
	t.Helper()
	catalog := tools.NewCatalog()
	for _, handler := range handlers {
		if err := catalog.Register(handler); err != nil {
			t.Fatalf("register %q: %v", handler.Descriptor().Name, err)
		}
	}
	return catalog
}

type stubTool struct {
	descriptor core.Tool
	output     string
	calls      atomic.Int32
}

func newStubTool(name, output string) *stubTool {
	return &stubTool{
		descriptor: core.Tool{
			Name:        name,
			Description: "test tool " + name,
			Parameters:  []byte(`{"type":"object"}`),
		},
		output: output,
	}
}

func (h *stubTool) Descriptor() core.Tool { return h.descriptor }

func (h *stubTool) Execute(context.Context, map[string]interface{}) (string, error) {
	h.calls.Add(1)
	return h.output, nil
}

func (*stubTool) RunsCommands() bool          { return false }
func (*stubTool) ActionKind() core.ActionKind { return core.ActionRead }
func (h *stubTool) callCount() int            { return int(h.calls.Load()) }

type scriptedReply struct {
	events []core.RunEvent
}

func finalReply(text string) scriptedReply {
	events := make([]core.RunEvent, 0, 2)
	if text != "" {
		events = append(events, core.RunEvent{Type: core.TextDelta, TextDelta: text})
	}
	return scriptedReply{events: append(events, core.RunEvent{
		Type:       core.ReplyEndedEvent,
		ReplyEnded: &core.ReplyEnded{Reason: core.Finished},
	})}
}

func toolReply(text string, call core.ToolCall) scriptedReply {
	events := make([]core.RunEvent, 0, 3)
	if text != "" {
		events = append(events, core.RunEvent{Type: core.TextDelta, TextDelta: text})
	}
	events = append(events,
		core.RunEvent{Type: core.ToolCallEvent, ToolCall: &call},
		core.RunEvent{Type: core.ReplyEndedEvent, ReplyEnded: &core.ReplyEnded{Reason: core.StoppedToCallTools}},
	)
	return scriptedReply{events: events}
}

func failureReply(err error) scriptedReply {
	return scriptedReply{events: []core.RunEvent{{Type: core.ErrorEvent, Error: err}}}
}

type providerCall struct {
	conversation core.Conversation
	tools        []core.Tool
}

type queueProvider struct {
	mu      sync.Mutex
	replies []scriptedReply
	calls   []providerCall
	next    int
}

func newQueueProvider(replies ...scriptedReply) *queueProvider {
	return &queueProvider{replies: replies}
}

func (p *queueProvider) StreamReply(_ context.Context, conv core.Conversation, advertised []core.Tool, _ core.StreamOptions) <-chan core.RunEvent {
	p.mu.Lock()
	p.calls = append(p.calls, providerCall{conversation: conv, tools: append([]core.Tool(nil), advertised...)})
	if p.next >= len(p.replies) {
		p.mu.Unlock()
		return eventsChannel([]core.RunEvent{{Type: core.ErrorEvent, Error: errors.New("test provider: no scripted reply")}})
	}
	reply := p.replies[p.next]
	p.next++
	p.mu.Unlock()
	return eventsChannel(reply.events)
}

func (p *queueProvider) snapshotCalls() []providerCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]providerCall(nil), p.calls...)
}

type routingProvider struct {
	mu      sync.Mutex
	routes  map[string][]scriptedReply
	indices map[string]int
	calls   map[string]int
}

func newRoutingProvider(routes map[string][]scriptedReply) *routingProvider {
	return &routingProvider{routes: routes, indices: make(map[string]int), calls: make(map[string]int)}
}

func (p *routingProvider) StreamReply(_ context.Context, conv core.Conversation, _ []core.Tool, _ core.StreamOptions) <-chan core.RunEvent {
	key := lastUserInstruction(conv)
	p.mu.Lock()
	index := p.indices[key]
	p.indices[key] = index + 1
	p.calls[key]++
	replies := p.routes[key]
	if index >= len(replies) {
		p.mu.Unlock()
		return eventsChannel([]core.RunEvent{{Type: core.ErrorEvent, Error: fmt.Errorf("test provider: no route for %q call %d", key, index+1)}})
	}
	reply := replies[index]
	p.mu.Unlock()
	return eventsChannel(reply.events)
}

func (p *routingProvider) callCount(instruction string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[instruction]
}

func lastUserInstruction(conv core.Conversation) string {
	for i := len(conv.Turns) - 1; i >= 0; i-- {
		if conv.Turns[i].Role == "user" {
			return conv.Turns[i].Content
		}
	}
	return ""
}

func eventsChannel(events []core.RunEvent) <-chan core.RunEvent {
	ch := make(chan core.RunEvent, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch
}

type blockingProvider struct {
	started chan struct{}
	stopped chan struct{}
	active  atomic.Int32
	once    sync.Once
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (p *blockingProvider) StreamReply(ctx context.Context, _ core.Conversation, _ []core.Tool, _ core.StreamOptions) <-chan core.RunEvent {
	ch := make(chan core.RunEvent)
	p.active.Add(1)
	p.once.Do(func() { close(p.started) })
	go func() {
		defer close(ch)
		defer close(p.stopped)
		defer p.active.Add(-1)
		<-ctx.Done()
	}()
	return ch
}

type eventCollector struct {
	mu     sync.Mutex
	events []core.RunEvent
}

func (c *eventCollector) emit(ev core.RunEvent) error {
	c.mu.Lock()
	c.events = append(c.events, ev)
	c.mu.Unlock()
	return nil
}

func (c *eventCollector) snapshot() []core.RunEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]core.RunEvent(nil), c.events...)
}

type promptingPermission struct{}

func (promptingPermission) Decide(ctx context.Context, call core.ToolCall, _ core.ActionKind, emit func(core.RunEvent) error) core.PermissionResult {
	reply := make(chan core.PermissionDecision, 1)
	request := &core.PermissionRequest{ToolCall: call, Reason: "test approval", ReplyPath: reply}
	if emit == nil {
		return core.PermissionResult{Allow: false, Reason: "missing event stream"}
	}
	if err := emit(core.RunEvent{Type: core.PermissionRequestedEvent, Permission: request}); err != nil {
		return core.PermissionResult{Allow: false, Reason: err.Error()}
	}
	select {
	case decision := <-reply:
		return core.PermissionResult{Allow: decision.Allow, Reason: "test decision"}
	case <-ctx.Done():
		return core.PermissionResult{Allow: false, Reason: ctx.Err().Error()}
	}
}

type recordingLifecycleHooks struct {
	mu              sync.Mutex
	startCount      int
	promptCount     int
	finishedCount   int
	stopCount       int
	startInjection  string
	promptInjection string
	blockStart      bool
	blockPrompt     bool
	blockFinished   bool
	blockStop       bool
}

func (h *recordingLifecycleHooks) SessionStart(_ context.Context, _ core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	h.mu.Lock()
	h.startCount++
	h.mu.Unlock()
	emitHookOutcome(emit, core.HookSessionStart)
	return core.HookLifecycleResult{Block: h.blockStart, Reason: "start blocked", InjectedContext: nonEmpty(h.startInjection)}
}

func (h *recordingLifecycleHooks) PromptSubmit(_ context.Context, _ string, _ core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	h.mu.Lock()
	h.promptCount++
	h.mu.Unlock()
	emitHookOutcome(emit, core.HookPromptSubmit)
	return core.HookLifecycleResult{Block: h.blockPrompt, Reason: "prompt blocked", InjectedContext: nonEmpty(h.promptInjection)}
}

func (h *recordingLifecycleHooks) RunFinished(_ context.Context, _ core.RunFinished, _ core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	h.mu.Lock()
	h.finishedCount++
	h.mu.Unlock()
	emitHookOutcome(emit, core.HookRunFinished)
	return core.HookLifecycleResult{Block: h.blockFinished, Reason: "finish blocked"}
}

func (h *recordingLifecycleHooks) SessionStop(_ context.Context, _ core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	h.mu.Lock()
	h.stopCount++
	h.mu.Unlock()
	emitHookOutcome(emit, core.HookSessionStop)
	return core.HookLifecycleResult{Block: h.blockStop, Reason: "cleanup blocked"}
}

func (h *recordingLifecycleHooks) counts() [4]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return [4]int{h.startCount, h.promptCount, h.finishedCount, h.stopCount}
}

func emitHookOutcome(emit func(core.RunEvent) error, moment core.HookMoment) {
	if emit == nil {
		return
	}
	_ = emit(core.RunEvent{Type: core.HookOutcomeEvent, HookOutcome: &core.HookOutcome{Moment: moment, Action: core.HookAllowed}})
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func assertConversation(t *testing.T, got core.Conversation, want []core.Turn) {
	t.Helper()
	if len(got.Turns) != len(want) {
		t.Fatalf("conversation turns = %+v, want %+v", got.Turns, want)
	}
	for i := range want {
		if got.Turns[i].Role != want[i].Role || got.Turns[i].Content != want[i].Content {
			t.Fatalf("conversation turn %d = %+v, want %+v", i, got.Turns[i], want[i])
		}
	}
}

func assertToolNames(t *testing.T, got []core.Tool, want []string) {
	t.Helper()
	gotNames := make([]string, len(got))
	for i := range got {
		gotNames[i] = got[i].Name
	}
	if strings.Join(gotNames, ",") != strings.Join(want, ",") {
		t.Fatalf("advertised tools = %v, want %v", gotNames, want)
	}
}

func assertLifecycleEvents(t *testing.T, events []core.RunEvent, want []string) {
	t.Helper()
	got := lifecycleStrings(events)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle events = %v, want %v", got, want)
	}
}

func lifecycleStrings(events []core.RunEvent) []string {
	var got []string
	for _, ev := range events {
		if ev.Type != core.StatusChange || (ev.Status != core.StatusSubagentStarted && ev.Status != core.StatusSubagentFinished) {
			continue
		}
		label := ""
		if ev.ToolCall != nil {
			label, _ = ev.ToolCall.Arguments["label"].(string)
		}
		got = append(got, ev.Status+":"+label)
	}
	return got
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
