package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/permission"
	"github.com/blkcor/coragent/internal/tools"
)

func TestRichPermissionRevisionRequiresFreshRequestAndFinalAllow(t *testing.T) {
	tool := &preparedTestTool{name: "modify", operation: core.ActionOperationModify, output: "ok"}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	engine := permission.New(permission.Config{Mode: permission.ModeDefault})
	executor := New(catalog, Stages{Pre: neverBlockCheck{}, Permission: engine, Sandbox: directSandbox{}, Post: neverBlockCheck{}}, 0)
	var requests []core.ObservedPermissionRequest
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{
		ID: "provider", ToolName: "modify", Arguments: map[string]interface{}{"path": "before"},
	}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind != core.ObservedKindPermissionRequested {
			return nil
		}
		request := event.Payload.(*core.PermissionRequestedPayload).Request
		requests = append(requests, request)
		if request.Protocol != core.PermissionProtocolRich || request.Preview.Kind == core.ActionPreviewUnavailable || !request.Capabilities.ReviseArguments || !request.Capabilities.SchemaAwareEdit || request.Mode != "default" {
			t.Fatalf("rich request shape = %+v", request)
		}
		if len(requests) == 1 {
			invalid := decisionFor(request, core.PermissionReplyReviseArguments)
			invalid.RevisedArguments = map[string]interface{}{"path": 3}
			if reply := request.Reply(context.Background(), invalid); reply.Status != core.PermissionReplyValidationRejected {
				t.Fatalf("invalid revision = %+v", reply)
			}
			valid := decisionFor(request, core.PermissionReplyReviseArguments)
			valid.RevisedArguments = map[string]interface{}{"path": "after"}
			if reply := request.Reply(context.Background(), valid); reply.Status != core.PermissionReplyAccepted {
				t.Fatalf("valid revision = %+v", reply)
			}
			if reply := request.Reply(context.Background(), decisionFor(request, core.PermissionReplyAllow)); reply.Status != core.PermissionReplyAlreadyResolved {
				t.Fatalf("stale allow = %+v", reply)
			}
		} else {
			if request.Revision != 2 || request.RequestID == requests[0].RequestID || request.EffectiveCall.Arguments["path"] != "after" {
				t.Fatalf("replacement request = %+v", request)
			}
			if reply := request.Reply(context.Background(), decisionFor(request, core.PermissionReplyAllow)); reply.Status != core.PermissionReplyAccepted {
				t.Fatalf("final allow = %+v", reply)
			}
		}
		return nil
	})
	if err != nil || result.Result.IsError || result.Revision != 2 || len(requests) != 2 || !tool.committed || tool.preparedArgs["path"] != "after" {
		t.Fatalf("result=%+v err=%v requests=%d committed=%v args=%v", result, err, len(requests), tool.committed, tool.preparedArgs)
	}
}

type captureGrantSandbox struct{ grants []core.SandboxGrants }

func (sandbox *captureGrantSandbox) Run(ctx context.Context, handler core.ToolHandler, arguments map[string]interface{}) (string, error) {
	return sandbox.RunWithGrants(ctx, handler, arguments, core.SandboxGrants{})
}
func (sandbox *captureGrantSandbox) RunWithGrants(ctx context.Context, handler core.ToolHandler, arguments map[string]interface{}, grants core.SandboxGrants) (string, error) {
	sandbox.grants = append(sandbox.grants, grants.Clone())
	return handler.Execute(ctx, arguments)
}

func TestRichSandboxGrantsAreValidatedOneCallAndNotRemembered(t *testing.T) {
	tool := &fakeTool{name: "shell", runsCmds: true, output: "ok"}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	engine := permission.New(permission.Config{Mode: permission.ModeDefault})
	sandbox := &captureGrantSandbox{}
	executor := New(catalog, Stages{Pre: neverBlockCheck{}, Permission: engine, Sandbox: sandbox, Post: neverBlockCheck{}}, 0)
	call := core.ToolCall{ID: "provider-1", ToolName: "shell", Arguments: map[string]interface{}{"command": "git status"}}
	requests := 0
	_, err := executor.DispatchRich(context.Background(), call, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind != core.ObservedKindPermissionRequested {
			return nil
		}
		requests++
		request := event.Payload.(*core.PermissionRequestedPayload).Request
		invalid := decisionFor(request, core.PermissionReplyAllow)
		invalid.SandboxGrants.ExtraReadRoots = []string{"relative"}
		if reply := request.Reply(context.Background(), invalid); reply.Status != core.PermissionReplyValidationRejected {
			t.Fatalf("invalid grant reply = %+v", reply)
		}
		valid := decisionFor(request, core.PermissionReplyAllow)
		valid.Remember = true
		valid.SandboxGrants = core.SandboxGrants{ExtraReadRoots: []string{"/tmp"}, Network: true}
		if reply := request.Reply(context.Background(), valid); reply.Status != core.PermissionReplyAccepted {
			t.Fatalf("valid grant reply = %+v", reply)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	call.ID = "provider-2"
	_, err = executor.DispatchRich(context.Background(), call, "call-2", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindPermissionRequested {
			t.Fatal("remembered action unexpectedly prompted again")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(sandbox.grants) != 2 || len(sandbox.grants[0].ExtraReadRoots) != 1 || sandbox.grants[0].ExtraReadRoots[0] != "/tmp" || !sandbox.grants[0].Network || len(sandbox.grants[1].ExtraReadRoots) != 0 || sandbox.grants[1].Network {
		t.Fatalf("requests=%d grants=%+v", requests, sandbox.grants)
	}
}

type blockOnRevisionPre struct{ calls int }

func (stage *blockOnRevisionPre) PreCheck(context.Context, core.ToolCall) core.StageDecision {
	stage.calls++
	if stage.calls == 2 {
		return core.StageDecision{Block: true, Reason: "revision blocked"}
	}
	return core.StageDecision{}
}

func TestRichRevisionHookBlockTerminatesWithoutReplacementRequest(t *testing.T) {
	tool := &preparedTestTool{name: "modify", operation: core.ActionOperationModify, output: "ok"}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	pre := &blockOnRevisionPre{}
	engine := permission.New(permission.Config{Mode: permission.ModeDefault})
	executor := New(catalog, Stages{Pre: pre, Permission: engine, Sandbox: directSandbox{}, Post: neverBlockCheck{}}, 0)
	requests := 0
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{ID: "provider", ToolName: "modify", Arguments: map[string]interface{}{"path": "before"}}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindPermissionRequested {
			requests++
			request := event.Payload.(*core.PermissionRequestedPayload).Request
			decision := decisionFor(request, core.PermissionReplyReviseArguments)
			decision.RevisedArguments = map[string]interface{}{"path": "after"}
			if reply := request.Reply(context.Background(), decision); reply.Status != core.PermissionReplyAccepted {
				t.Fatalf("revision reply = %+v", reply)
			}
		}
		return nil
	})
	if err != nil || !result.Result.IsError || result.Outcome != core.ToolOutcomeHookBlocked || requests != 1 || tool.committed {
		t.Fatalf("result=%+v err=%v requests=%d committed=%v", result, err, requests, tool.committed)
	}
}

type failReprepareTool struct{ preparedTestTool }

func (tool *failReprepareTool) Prepare(ctx context.Context, arguments map[string]interface{}) (core.PreparedAction, error) {
	if arguments["path"] == "after" {
		return core.PreparedAction{}, errors.New("cannot prepare revision")
	}
	return tool.preparedTestTool.Prepare(ctx, arguments)
}

func TestRichRevisionPreparationFailureTerminatesWithoutReplacementRequest(t *testing.T) {
	tool := &failReprepareTool{preparedTestTool: preparedTestTool{name: "modify", operation: core.ActionOperationModify, output: "ok"}}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	engine := permission.New(permission.Config{Mode: permission.ModeDefault})
	executor := New(catalog, Stages{Pre: neverBlockCheck{}, Permission: engine, Sandbox: directSandbox{}, Post: neverBlockCheck{}}, 0)
	requests := 0
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{ID: "provider", ToolName: "modify", Arguments: map[string]interface{}{"path": "before"}}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindPermissionRequested {
			requests++
			request := event.Payload.(*core.PermissionRequestedPayload).Request
			decision := decisionFor(request, core.PermissionReplyReviseArguments)
			decision.RevisedArguments = map[string]interface{}{"path": "after"}
			if reply := request.Reply(context.Background(), decision); reply.Status != core.PermissionReplyAccepted {
				t.Fatalf("revision reply = %+v", reply)
			}
		}
		return nil
	})
	if err != nil || !result.Result.IsError || !strings.Contains(result.Result.Result, "cannot prepare revision") || requests != 1 || tool.committed {
		t.Fatalf("result=%+v err=%v requests=%d committed=%v", result, err, requests, tool.committed)
	}
}

func decisionFor(request core.ObservedPermissionRequest, action core.PermissionReplyAction) core.ObservedPermissionDecision {
	return core.ObservedPermissionDecision{RequestID: request.RequestID, CallID: request.CallID, Revision: request.Revision, Action: action}
}
