package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/core"
)

func TestScopeMatching(t *testing.T) {
	var fired int
	engine, err := New([]core.HookRegistration{{
		Name:   "shell-rm",
		Moment: core.HookBeforeTool,
		Scope:  core.HookScope{ToolName: "shell", Pattern: "rm -rf"},
		Handler: func(context.Context, core.HookEvent) core.HookVerdict {
			fired++
			return core.HookVerdict{Block: true, Reason: "nope"}
		},
	}}, nil, Options{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	allowed := engine.PreCheck(context.Background(), core.ToolCall{ID: "1", ToolName: "read", Arguments: map[string]interface{}{"path": "rm -rf"}})
	if allowed.Block {
		t.Fatalf("read should not match shell-scoped hook")
	}
	blocked := engine.PreCheck(context.Background(), core.ToolCall{ID: "2", ToolName: "shell", Arguments: map[string]interface{}{"command": "rm -rf tmp"}})
	if !blocked.Block || !strings.Contains(blocked.Reason, "nope") {
		t.Fatalf("shell rm should block, got %+v", blocked)
	}
	if fired != 1 {
		t.Fatalf("hook should fire once, fired %d", fired)
	}
}

func TestOrderCompositionAndFirstBlockWins(t *testing.T) {
	var order []string
	engine, err := New([]core.HookRegistration{
		{
			Name:   "edit-1",
			Moment: core.HookBeforeTool,
			Handler: func(context.Context, core.HookEvent) core.HookVerdict {
				order = append(order, "edit-1")
				return core.HookVerdict{Arguments: map[string]interface{}{"path": "first"}}
			},
		},
		{
			Name:   "edit-2",
			Moment: core.HookBeforeTool,
			Handler: func(context.Context, core.HookEvent) core.HookVerdict {
				order = append(order, "edit-2")
				return core.HookVerdict{Arguments: map[string]interface{}{"path": "second"}}
			},
		},
		{
			Name:   "block",
			Moment: core.HookBeforeTool,
			Handler: func(context.Context, core.HookEvent) core.HookVerdict {
				order = append(order, "block")
				return core.HookVerdict{Block: true, Reason: "stop"}
			},
		},
		{
			Name:   "never",
			Moment: core.HookBeforeTool,
			Handler: func(context.Context, core.HookEvent) core.HookVerdict {
				order = append(order, "never")
				return core.HookVerdict{}
			},
		},
	}, nil, Options{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	decision := engine.PreCheck(context.Background(), core.ToolCall{ID: "1", ToolName: "read", Arguments: map[string]interface{}{"path": "original"}})
	if !decision.Block || decision.Reason != "stop" {
		t.Fatalf("expected first block to win, got %+v", decision)
	}
	if strings.Join(order, ",") != "edit-1,edit-2,block" {
		t.Fatalf("order = %v", order)
	}
}

func TestInProcessPanicFailsClosed(t *testing.T) {
	engine, err := New([]core.HookRegistration{{
		Name:   "boom",
		Moment: core.HookBeforeTool,
		Handler: func(context.Context, core.HookEvent) core.HookVerdict {
			panic("bad hook")
		},
	}}, nil, Options{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	decision := engine.PreCheck(context.Background(), core.ToolCall{ID: "1", ToolName: "read"})
	if !decision.Block || !strings.Contains(decision.Reason, "panicked") {
		t.Fatalf("panic should fail closed, got %+v", decision)
	}
}

func TestExternalHookExitAndStructuredVerdict(t *testing.T) {
	dir := t.TempDir()
	blockScript := writeScript(t, dir, "block.sh", "echo blocked >&2\nexit 7\n")
	replaceScript := writeScript(t, dir, "replace.sh", "printf '%s' '{\"result\":\"redacted\"}'\n")

	engine, err := New(nil, []core.ExternalHook{
		{Name: "blocker", Moment: core.HookBeforeTool, Command: []string{"/bin/sh", blockScript}},
		{Name: "redactor", Moment: core.HookAfterTool, Command: []string{"/bin/sh", replaceScript}},
	}, Options{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	pre := engine.PreCheck(context.Background(), core.ToolCall{ID: "1", ToolName: "read"})
	if !pre.Block || !strings.Contains(pre.Reason, "blocked") {
		t.Fatalf("non-zero exit should block with stderr reason, got %+v", pre)
	}

	post := engine.PostCheck(context.Background(), core.ToolCall{ID: "1", ToolName: "read"}, core.ToolResult{ToolCallID: "1", Result: "secret"})
	if post.ReplacementResult == nil || post.ReplacementResult.Result != "redacted" {
		t.Fatalf("structured verdict should replace result, got %+v", post)
	}
}

func TestExternalHookMalformedAndOversizedOutputFailClosed(t *testing.T) {
	dir := t.TempDir()
	bad := writeScript(t, dir, "bad.sh", "printf 'not-json'\n")
	flood := writeScript(t, dir, "flood.sh", "printf 'abcdefghij'\n")

	for _, tc := range []struct {
		name  string
		cmd   string
		limit int
		want  string
	}{
		{name: "bad", cmd: bad, limit: 1024, want: "malformed"},
		{name: "flood", cmd: flood, limit: 4, want: "exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := New(nil, []core.ExternalHook{{Name: tc.name, Moment: core.HookBeforeTool, Command: []string{"/bin/sh", tc.cmd}}}, Options{ExternalOutputLimit: tc.limit})
			if err != nil {
				t.Fatalf("new engine: %v", err)
			}
			decision := engine.PreCheck(context.Background(), core.ToolCall{ID: "1", ToolName: "read"})
			if !decision.Block || !strings.Contains(decision.Reason, tc.want) {
				t.Fatalf("want fail-closed reason containing %q, got %+v", tc.want, decision)
			}
		})
	}
}

func TestValidationCatchesBadDefinitions(t *testing.T) {
	if _, err := New([]core.HookRegistration{{Name: "bad", Moment: "later", Handler: func(context.Context, core.HookEvent) core.HookVerdict { return core.HookVerdict{} }}}, nil, Options{}); err == nil {
		t.Fatalf("invalid moment should fail")
	}
	if _, err := New([]core.HookRegistration{{Name: "bad", Moment: core.HookBeforeTool, Scope: core.HookScope{Pattern: "["}, Handler: func(context.Context, core.HookEvent) core.HookVerdict { return core.HookVerdict{} }}}, nil, Options{}); err == nil {
		t.Fatalf("invalid pattern should fail")
	}
	if _, err := New(nil, []core.ExternalHook{{Name: "missing", Moment: core.HookBeforeTool, Command: []string{filepath.Join(t.TempDir(), "missing")}}}, Options{}); err == nil {
		t.Fatalf("missing command should fail")
	}
}

func TestPromptLifecycleInjectionAndEvents(t *testing.T) {
	engine, err := New([]core.HookRegistration{{
		Name:   "policy",
		Moment: core.HookPromptSubmit,
		Handler: func(context.Context, core.HookEvent) core.HookVerdict {
			return core.HookVerdict{InjectedContext: []string{"stay safe"}}
		},
	}}, nil, Options{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	var events []core.RunEvent
	result := engine.PromptSubmit(context.Background(), "hello", core.Conversation{}, func(ev core.RunEvent) error {
		events = append(events, ev)
		return nil
	})
	if len(result.InjectedContext) != 1 || result.InjectedContext[0] != "stay safe" {
		t.Fatalf("expected injected context, got %+v", result)
	}
	if len(events) != 1 || events[0].Type != core.HookOutcomeEvent || events[0].HookOutcome.Action != core.HookInjected {
		t.Fatalf("expected hook outcome event, got %+v", events)
	}
}

func TestExternalHookTimeoutFailsClosed(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "slow.sh", "sleep 2\n")
	engine, err := New(nil, []core.ExternalHook{{Name: "slow", Moment: core.HookBeforeTool, Command: []string{"/bin/sh", script}, Timeout: time.Millisecond}}, Options{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	decision := engine.PreCheck(context.Background(), core.ToolCall{ID: "1", ToolName: "read"})
	if !decision.Block || !strings.Contains(decision.Reason, "cancelled") {
		t.Fatalf("timeout should fail closed, got %+v", decision)
	}
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
