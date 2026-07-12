package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/tools"
)

type preparedTestTool struct {
	name         string
	operation    core.ActionOperation
	preview      core.ActionPreview
	output       string
	visits       *[]string
	preparedArgs map[string]interface{}
	committed    bool
}

func (tool *preparedTestTool) Descriptor() core.Tool {
	return core.Tool{Name: tool.name, Parameters: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}
}
func (tool *preparedTestTool) RunsCommands() bool          { return false }
func (tool *preparedTestTool) ActionKind() core.ActionKind { return core.ActionEdit }
func (tool *preparedTestTool) Execute(context.Context, map[string]interface{}) (string, error) {
	return "", errors.New("legacy Execute must not run for a prepared handler")
}
func (tool *preparedTestTool) Prepare(_ context.Context, args map[string]interface{}) (core.PreparedAction, error) {
	if tool.visits != nil {
		*tool.visits = append(*tool.visits, "prepare")
	}
	tool.preparedArgs = cloneArgumentMap(args)
	preview := tool.preview
	if preview.Kind == "" {
		preview = core.ActionPreview{Kind: core.ActionPreviewText, Operation: tool.operation, Text: "candidate"}
	}
	return core.PreparedAction{EffectiveArguments: cloneArgumentMap(args), Operation: tool.operation, Preview: preview, CommitToken: "token"}, nil
}
func (tool *preparedTestTool) ExecutePrepared(_ context.Context, prepared core.PreparedAction) (string, error) {
	if tool.visits != nil {
		*tool.visits = append(*tool.visits, "commit")
	}
	if prepared.CommitToken != "token" {
		return "", errors.New("wrong commit token")
	}
	tool.committed = true
	return tool.output, nil
}

func TestRichDispatchPreparesAfterHookAndBeforePermission(t *testing.T) {
	var visits []string
	tool := &preparedTestTool{name: "modify", operation: core.ActionOperationModify, output: "ok", visits: &visits}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	executor := New(catalog, recordingStages(&visits, recordCfg{preEditArgs: map[string]interface{}{"path": "hook.txt"}}), 0)
	var events []core.RichEvent
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{ID: "provider", ToolName: "modify", Arguments: map[string]interface{}{"path": "provider.txt"}}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		events = append(events, event.Clone())
		return nil
	})
	if err != nil || result.Result.IsError || !tool.committed {
		t.Fatalf("result=%+v err=%v committed=%v", result, err, tool.committed)
	}
	assertOrder(t, visits, []string{"pre", "prepare", "permission", "commit", "post"})
	if tool.preparedArgs["path"] != "hook.txt" {
		t.Fatalf("prepared args = %v", tool.preparedArgs)
	}
	wantKinds := []core.ObservedEventKind{core.ObservedKindToolPrepared, core.ObservedKindToolExecuting}
	if len(events) != len(wantKinds) {
		t.Fatalf("events = %+v", events)
	}
	for index, kind := range wantKinds {
		if events[index].Kind != kind {
			t.Fatalf("event %d = %q, want %q", index, events[index].Kind, kind)
		}
	}
	prepared := events[0].Payload.(*core.ToolPreparedPayload)
	if prepared.CallID != "call-1" || prepared.Revision != 1 || prepared.EffectiveCall.Arguments["path"] != "hook.txt" {
		t.Fatalf("prepared payload = %+v", prepared)
	}
}

func TestLegacyHandlerReportsPreviewUnavailableWithoutChangingExecution(t *testing.T) {
	tool := &fakeTool{name: "legacy", output: "ok"}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	executor := New(catalog, InertStages(), 0)
	var prepared *core.ToolPreparedPayload
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{ID: "provider", ToolName: "legacy"}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindToolPrepared {
			prepared = event.Payload.(*core.ToolPreparedPayload)
		}
		return nil
	})
	if err != nil || result.Result.IsError || !tool.executed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if prepared == nil || prepared.Preview.Kind != core.ActionPreviewUnavailable || prepared.Preview.UnavailableReason == "" {
		t.Fatalf("fallback preview = %+v", prepared)
	}
}

func TestGenericPreparedDeletePreviewDoesNotRegisterDeleteBuiltin(t *testing.T) {
	tool := &preparedTestTool{name: "custom_delete", operation: core.ActionOperationDelete, output: "deleted"}
	catalog := tools.NewDefaultCatalog()
	if _, exists := catalog.Lookup("delete_file"); exists {
		t.Fatal("Phase 7 unexpectedly registered a built-in delete capability")
	}
	catalog.MustRegister(tool)
	executor := New(catalog, InertStages(), 0)
	var operation core.ActionOperation
	_, err := executor.DispatchRich(context.Background(), core.ToolCall{ID: "provider", ToolName: "custom_delete", Arguments: map[string]interface{}{"path": "target"}}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindToolPrepared {
			operation = event.Payload.(*core.ToolPreparedPayload).Preview.Operation
		}
		return nil
	})
	if err != nil || operation != core.ActionOperationDelete || !tool.committed {
		t.Fatalf("operation=%q committed=%v err=%v", operation, tool.committed, err)
	}
}

func TestOutputBudgetEmitsStructuredOmissionAndLegacyMarker(t *testing.T) {
	tool := &fakeTool{name: "verbose", output: strings.Repeat("界", 20)}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	executor := New(catalog, InertStages(), 7)
	var omission *core.Omission
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{ID: "provider", ToolName: "verbose"}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindOmissionReported {
			value := event.Payload.(*core.OmissionReportedPayload).Omission
			omission = &value
		}
		return nil
	})
	if err != nil || !strings.Contains(result.Result.Result, "[output truncated:") || !strings.HasPrefix(result.Result.Result, "界界") {
		t.Fatalf("bounded result=%+v err=%v", result, err)
	}
	if omission == nil || omission.Kind != core.OmissionOutputBudget || omission.CallID != "call-1" || omission.Recoverability != core.RecoverabilityUnrecoverable || !omission.OriginalBytes.Known || !omission.RetainedBytes.Known {
		t.Fatalf("omission=%+v", omission)
	}
}

func TestPreparedPreviewDeliveryFailurePreventsCommit(t *testing.T) {
	streamErr := errors.New("stream closed")
	tool := &preparedTestTool{name: "modify", operation: core.ActionOperationModify, output: "ok"}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	executor := New(catalog, InertStages(), 0)
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{ID: "provider", ToolName: "modify", Arguments: map[string]interface{}{"path": "target"}}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindToolPrepared {
			return streamErr
		}
		return nil
	})
	if err != nil || !result.Result.IsError || !strings.Contains(result.Result.Result, streamErr.Error()) || tool.committed {
		t.Fatalf("result=%+v err=%v committed=%v", result, err, tool.committed)
	}
}

type revisionPre struct{ calls int }

func (stage *revisionPre) PreCheck(_ context.Context, call core.ToolCall) core.StageDecision {
	stage.calls++
	if stage.calls == 2 {
		return core.StageDecision{EditedArguments: map[string]interface{}{"path": "hook-replacement"}}
	}
	return core.StageDecision{}
}

type revisionPermission struct{ calls int }

func (stage *revisionPermission) Decide(_ context.Context, _ core.ToolCall, _ core.ActionKind, _ func(core.RunEvent) error) core.PermissionResult {
	stage.calls++
	if stage.calls == 1 {
		return core.PermissionResult{Allow: true, EditedArguments: map[string]interface{}{"path": "human-edit"}}
	}
	return core.PermissionResult{Allow: true}
}

func TestLegacyEditedApprovalRequiresFreshPermissionAfterHookReplacement(t *testing.T) {
	pre := &revisionPre{}
	permission := &revisionPermission{}
	tool := &preparedTestTool{name: "modify", operation: core.ActionOperationModify, output: "ok"}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	executor := New(catalog, Stages{Pre: pre, Permission: permission, Sandbox: directSandbox{}, Post: neverBlockCheck{}}, 0)
	result, err := executor.Dispatch(context.Background(), core.ToolCall{ID: "provider", ToolName: "modify", Arguments: map[string]interface{}{"path": "provider"}}, nil)
	if err != nil || result.IsError || pre.calls != 2 || permission.calls != 2 || tool.preparedArgs["path"] != "hook-replacement" || !tool.committed {
		t.Fatalf("result=%+v err=%v pre=%d permission=%d args=%v committed=%v", result, err, pre.calls, permission.calls, tool.preparedArgs, tool.committed)
	}
}
