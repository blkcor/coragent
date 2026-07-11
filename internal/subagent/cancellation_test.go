package subagent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
)

func TestTaskHandlerParentCancellationStopsBlockingChildToolWithoutOrphan(t *testing.T) {
	blockingTool := newBlockingChildTool("blocking_child_tool")
	catalog := catalogWith(t, blockingTool)
	provider := newQueueProvider(toolReply("", core.ToolCall{
		ID:       "blocking-tool-1",
		ToolName: blockingTool.Descriptor().Name,
	}))
	handler := handlerFor(provider, catalog, catalog.Advertise(), 3, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan taskCallOutcome, 1)
	go func() {
		result, err := handler.ExecuteWithEvents(ctx, map[string]interface{}{
			"label":       "blocking child tool",
			"instruction": "run the blocking child tool",
			"tools":       []interface{}{blockingTool.Descriptor().Name},
		}, func(core.RunEvent) error { return nil })
		done <- taskCallOutcome{result: result, err: err}
	}()

	waitFor(t, blockingTool.started, "blocking child tool to start")
	cancel()

	outcome := waitForTaskCall(t, done, "child tool cancellation")
	if outcome.result != "" {
		t.Fatalf("cancelled child tool returned recoverable task content %q", outcome.result)
	}
	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("child tool cancellation error = %v, want context.Canceled", outcome.err)
	}
	waitFor(t, blockingTool.stopped, "blocking child tool to stop")
	if active := blockingTool.active.Load(); active != 0 {
		t.Fatalf("active child tools after root cancellation = %d", active)
	}
	if calls := len(provider.snapshotCalls()); calls != 1 {
		t.Fatalf("provider calls after child tool cancellation = %d, want 1", calls)
	}
}

func TestTaskHandlerParentCancellationStopsGrandchildCommandWithoutOrphan(t *testing.T) {
	runner := newBlockingCommandRunner()
	command := newBlockingCommandTool("blocking_command")
	catalog := catalogWith(t, command)
	provider := newRoutingProvider(map[string][]scriptedReply{
		"delegate to grandchild": {
			toolReply("", core.ToolCall{
				ID:       "grandchild-task-1",
				ToolName: ToolName,
				Arguments: map[string]interface{}{
					"label":       "grandchild command",
					"instruction": "run the grandchild command",
					"tools":       []interface{}{command.Descriptor().Name},
				},
			}),
		},
		"run the grandchild command": {
			toolReply("", core.ToolCall{
				ID:       "grandchild-command-1",
				ToolName: command.Descriptor().Name,
			}),
		},
	})
	stages := executor.InertStages()
	stages.Sandbox = commandRunnerSandbox{runner: runner}
	handler := handlerForStages(provider, catalog, catalog.Advertise(), 5, nil, stages)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan taskCallOutcome, 1)
	go func() {
		result, err := handler.ExecuteWithEvents(ctx, map[string]interface{}{
			"label":       "root delegation",
			"instruction": "delegate to grandchild",
			"tools":       []interface{}{command.Descriptor().Name},
		}, func(core.RunEvent) error { return nil })
		done <- taskCallOutcome{result: result, err: err}
	}()

	waitFor(t, command.started, "grandchild command handler to start")
	waitFor(t, runner.started, "grandchild command runner to start")
	cancel()

	outcome := waitForTaskCall(t, done, "grandchild command cancellation")
	if outcome.result != "" {
		t.Fatalf("cancelled grandchild command returned recoverable task content %q", outcome.result)
	}
	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("grandchild command cancellation error = %v, want context.Canceled", outcome.err)
	}
	waitFor(t, runner.stopped, "grandchild command runner to stop")
	waitFor(t, command.stopped, "grandchild command handler to stop")
	if active := runner.active.Load(); active != 0 {
		t.Fatalf("active grandchild command runners after root cancellation = %d", active)
	}
	if active := command.active.Load(); active != 0 {
		t.Fatalf("active grandchild command handlers after root cancellation = %d", active)
	}
	if calls := command.unconfinedCalls.Load(); calls != 0 {
		t.Fatalf("grandchild command bypassed sandbox through Execute %d times", calls)
	}
	if calls := provider.callCount("delegate to grandchild"); calls != 1 {
		t.Fatalf("root child provider calls = %d, want 1", calls)
	}
	if calls := provider.callCount("run the grandchild command"); calls != 1 {
		t.Fatalf("grandchild provider calls = %d, want 1", calls)
	}
}

type taskCallOutcome struct {
	result string
	err    error
}

func waitForTaskCall(t *testing.T, done <-chan taskCallOutcome, what string) taskCallOutcome {
	t.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(2 * time.Second):
		t.Fatalf("task handler did not return promptly after %s", what)
		return taskCallOutcome{}
	}
}

type blockingChildTool struct {
	descriptor core.Tool
	started    chan struct{}
	stopped    chan struct{}
	startOnce  sync.Once
	stopOnce   sync.Once
	active     atomic.Int32
}

func newBlockingChildTool(name string) *blockingChildTool {
	return &blockingChildTool{
		descriptor: core.Tool{
			Name:        name,
			Description: "test tool that blocks until cancellation",
			Parameters:  []byte(`{"type":"object"}`),
		},
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (h *blockingChildTool) Descriptor() core.Tool { return h.descriptor }

func (h *blockingChildTool) Execute(ctx context.Context, _ map[string]interface{}) (string, error) {
	h.active.Add(1)
	h.startOnce.Do(func() { close(h.started) })
	defer func() {
		h.active.Add(-1)
		h.stopOnce.Do(func() { close(h.stopped) })
	}()
	<-ctx.Done()
	return "", ctx.Err()
}

func (*blockingChildTool) RunsCommands() bool          { return false }
func (*blockingChildTool) ActionKind() core.ActionKind { return core.ActionRead }

type blockingCommandTool struct {
	descriptor      core.Tool
	started         chan struct{}
	stopped         chan struct{}
	startOnce       sync.Once
	stopOnce        sync.Once
	active          atomic.Int32
	unconfinedCalls atomic.Int32
}

func newBlockingCommandTool(name string) *blockingCommandTool {
	return &blockingCommandTool{
		descriptor: core.Tool{
			Name:        name,
			Description: "test command that blocks until cancellation",
			Parameters:  []byte(`{"type":"object"}`),
		},
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (h *blockingCommandTool) Descriptor() core.Tool { return h.descriptor }

func (h *blockingCommandTool) Execute(context.Context, map[string]interface{}) (string, error) {
	h.unconfinedCalls.Add(1)
	return "", errors.New("blocking command executed outside sandbox")
}

func (h *blockingCommandTool) ExecuteCommand(ctx context.Context, _ map[string]interface{}, runner core.CommandRunner) (string, error) {
	h.active.Add(1)
	h.startOnce.Do(func() { close(h.started) })
	defer func() {
		h.active.Add(-1)
		h.stopOnce.Do(func() { close(h.stopped) })
	}()
	return runner.Run(ctx, core.CommandSpec{Command: "block until cancelled"})
}

func (*blockingCommandTool) RunsCommands() bool          { return true }
func (*blockingCommandTool) ActionKind() core.ActionKind { return core.ActionCommand }

type commandRunnerSandbox struct {
	runner core.CommandRunner
}

func (s commandRunnerSandbox) Run(ctx context.Context, handler core.ToolHandler, args map[string]interface{}) (string, error) {
	commandHandler, ok := handler.(core.CommandToolHandler)
	if !ok {
		return "", errors.New("test sandbox: command handler contract missing")
	}
	return commandHandler.ExecuteCommand(ctx, args, s.runner)
}

type blockingCommandRunner struct {
	started   chan struct{}
	stopped   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	active    atomic.Int32
}

func newBlockingCommandRunner() *blockingCommandRunner {
	return &blockingCommandRunner{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (r *blockingCommandRunner) Run(ctx context.Context, _ core.CommandSpec) (string, error) {
	r.active.Add(1)
	r.startOnce.Do(func() { close(r.started) })
	defer func() {
		r.active.Add(-1)
		r.stopOnce.Do(func() { close(r.stopped) })
	}()
	<-ctx.Done()
	return "", ctx.Err()
}

var (
	_ core.ToolHandler        = (*blockingChildTool)(nil)
	_ core.ActionClassifier   = (*blockingChildTool)(nil)
	_ core.CommandToolHandler = (*blockingCommandTool)(nil)
	_ core.ActionClassifier   = (*blockingCommandTool)(nil)
	_ core.Sandbox            = commandRunnerSandbox{}
	_ core.CommandRunner      = (*blockingCommandRunner)(nil)
)
