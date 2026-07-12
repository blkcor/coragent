package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/tools"
)

func TestEventAwareHandlerReceivesLiveEmitterAtExistingExecutionSlot(t *testing.T) {
	var visits []string
	base := &fakeTool{
		name:   "task",
		schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		output: "child result",
		visits: &visits,
	}
	handler := &eventAwareTestTool{fakeTool: base}
	handler.execute = func(_ context.Context, _ map[string]interface{}, emit func(core.RunEvent) error) (string, error) {
		if err := emit(core.RunEvent{Type: core.StatusChange, Status: "subagent_started"}); err != nil {
			return "", err
		}
		return base.output, nil
	}

	stages := recordingStages(&visits, recordCfg{editArgs: map[string]interface{}{"path": "edited.txt"}})
	catalog := tools.NewCatalog()
	catalog.MustRegister(handler)
	executor := New(catalog, stages, 0)

	var emitted []core.RunEvent
	result, err := executor.Dispatch(context.Background(), core.ToolCall{
		ID:        "c1",
		ToolName:  "task",
		Arguments: map[string]interface{}{"path": "original.txt"},
	}, func(event core.RunEvent) error {
		visits = append(visits, "emit")
		emitted = append(emitted, event)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.IsError || result.Result != "child result" {
		t.Fatalf("unexpected task result: %+v", result)
	}
	assertOrder(t, visits, []string{"pre", "permission", "pre", "execute_with_events", "emit", "post"})
	if base.executed {
		t.Fatalf("ordinary Execute ran instead of ExecuteWithEvents")
	}
	if !handler.awareExecuted {
		t.Fatalf("ExecuteWithEvents did not run")
	}
	if handler.gotArgs["path"] != "edited.txt" {
		t.Fatalf("event-aware handler got args %v, want permission-edited args", handler.gotArgs)
	}
	if len(emitted) != 1 || emitted[0].Status != "subagent_started" {
		t.Fatalf("live emitter received events %+v", emitted)
	}
}

func TestEventAwareHandlerStillObservesUpstreamShortCircuits(t *testing.T) {
	tests := []struct {
		name      string
		stages    func(*[]string) Stages
		arguments map[string]interface{}
		wantOrder []string
	}{
		{
			name: "argument validation",
			stages: func(visits *[]string) Stages {
				return recordingStages(visits, recordCfg{})
			},
			arguments: map[string]interface{}{},
			wantOrder: nil,
		},
		{
			name: "pre-hook block",
			stages: func(visits *[]string) Stages {
				return recordingStages(visits, recordCfg{preBlock: "blocked"})
			},
			arguments: map[string]interface{}{"path": "ok.txt"},
			wantOrder: []string{"pre"},
		},
		{
			name: "permission denial",
			stages: func(visits *[]string) Stages {
				return recordingStages(visits, recordCfg{denyPermission: "denied"})
			},
			arguments: map[string]interface{}{"path": "ok.txt"},
			wantOrder: []string{"pre", "permission"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var visits []string
			handler := &eventAwareTestTool{fakeTool: &fakeTool{
				name:   "task",
				schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
				visits: &visits,
			}}
			catalog := tools.NewCatalog()
			catalog.MustRegister(handler)
			executor := New(catalog, test.stages(&visits), 0)

			result, err := executor.Dispatch(context.Background(), core.ToolCall{
				ID: "c1", ToolName: "task", Arguments: test.arguments,
			}, noEmit)
			if err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			if !result.IsError {
				t.Fatalf("short-circuit returned success: %+v", result)
			}
			if handler.awareExecuted {
				t.Fatalf("ExecuteWithEvents ran after %s", test.name)
			}
			assertOrder(t, visits, test.wantOrder)
		})
	}
}

func TestEventAwareCommandDeclarationStillRoutesThroughSandbox(t *testing.T) {
	var visits []string
	base := &fakeTool{name: "command_task", runsCmds: true, output: "ok", visits: &visits}
	handler := &eventAwareTestTool{fakeTool: base}
	catalog := tools.NewCatalog()
	catalog.MustRegister(handler)
	executor := New(catalog, recordingStages(&visits, recordCfg{}), 0)

	result, err := executor.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "command_task"}, noEmit)
	if err != nil || result.IsError {
		t.Fatalf("dispatch result=%+v err=%v", result, err)
	}
	assertOrder(t, visits, []string{"pre", "permission", "sandbox", "execute", "post"})
	if handler.awareExecuted {
		t.Fatalf("command-running handler bypassed the sandbox through ExecuteWithEvents")
	}
	if !base.executed {
		t.Fatalf("sandbox did not invoke the command handler")
	}
}

func TestEventAwareHandlerOutputStillRunsPostCheckAndTruncation(t *testing.T) {
	var visits []string
	handler := &eventAwareTestTool{fakeTool: &fakeTool{
		name: "task", output: strings.Repeat("x", 100), visits: &visits,
	}}
	catalog := tools.NewCatalog()
	catalog.MustRegister(handler)
	executor := New(catalog, recordingStages(&visits, recordCfg{}), 10)

	result, err := executor.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "task"}, noEmit)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.IsError || !strings.Contains(result.Result, "output truncated") {
		t.Fatalf("event-aware result was not centrally truncated: %+v", result)
	}
	assertOrder(t, visits, []string{"pre", "permission", "execute_with_events", "post"})
}

func TestEventAwareHandlerReceivesEmitterFailure(t *testing.T) {
	streamErr := errors.New("parent stream closed")
	var visits []string
	handler := &eventAwareTestTool{fakeTool: &fakeTool{name: "task", visits: &visits}}
	handler.execute = func(_ context.Context, _ map[string]interface{}, emit func(core.RunEvent) error) (string, error) {
		return "", emit(core.RunEvent{Type: core.StatusChange, Status: "subagent_started"})
	}
	catalog := tools.NewCatalog()
	catalog.MustRegister(handler)
	executor := New(catalog, recordingStages(&visits, recordCfg{}), 0)

	result, err := executor.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "task"}, func(core.RunEvent) error {
		visits = append(visits, "emit")
		return streamErr
	})
	if err != nil {
		t.Fatalf("emitter failure must remain a recoverable tool error: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Result, streamErr.Error()) {
		t.Fatalf("emitter failure result = %+v", result)
	}
	assertOrder(t, visits, []string{"pre", "permission", "execute_with_events", "emit"})
}

func TestEventAwareHandlerCancellationRemainsRecoverable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler := &eventAwareTestTool{fakeTool: &fakeTool{name: "task"}}
	handler.execute = func(ctx context.Context, _ map[string]interface{}, _ func(core.RunEvent) error) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	catalog := tools.NewCatalog()
	catalog.MustRegister(handler)
	executor := New(catalog, InertStages(), 0)

	result, err := executor.Dispatch(ctx, core.ToolCall{ID: "c1", ToolName: "task"}, noEmit)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Result, context.Canceled.Error()) {
		t.Fatalf("cancellation result = %+v", result)
	}
}

type eventAwareTestTool struct {
	*fakeTool
	execute       func(context.Context, map[string]interface{}, func(core.RunEvent) error) (string, error)
	awareExecuted bool
}

func (h *eventAwareTestTool) ExecuteWithEvents(ctx context.Context, args map[string]interface{}, emit func(core.RunEvent) error) (string, error) {
	h.awareExecuted = true
	h.gotArgs = args
	if h.visits != nil {
		*h.visits = append(*h.visits, "execute_with_events")
	}
	if h.execute != nil {
		return h.execute(ctx, args, emit)
	}
	if h.failWith != nil {
		return "", h.failWith
	}
	return h.output, nil
}

var _ eventAwareHandler = (*eventAwareTestTool)(nil)
