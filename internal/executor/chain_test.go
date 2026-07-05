package executor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/hooks"
	"github.com/blkcor/coragent/internal/tools"
)

// --- truncation -------------------------------------------------------------

func TestTruncateUnderBudgetUnchanged(t *testing.T) {
	s := "hello world"
	if got := truncate(s, 100); got != s {
		t.Errorf("under-budget output must be unchanged, got %q", got)
	}
}

func TestTruncateOverBudgetClipsWithMarker(t *testing.T) {
	s := strings.Repeat("a", 50)
	got := truncate(s, 10)
	if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
		t.Errorf("clipped output must keep the first budget bytes, got %q", got)
	}
	if !strings.Contains(got, "truncated") || !strings.Contains(got, "40 bytes") {
		t.Errorf("marker must state how much was elided, got %q", got)
	}
}

func TestTruncateCutsOnRuneBoundary(t *testing.T) {
	// Each "世" is 3 bytes; a budget of 7 would split the third rune.
	s := "世界和平" // 12 bytes
	got := truncate(s, 7)
	kept := strings.TrimSuffix(got, got[strings.Index(got, "\n["):])
	for _, r := range kept {
		if r == '�' {
			t.Fatalf("truncation split a rune (produced U+FFFD): %q", got)
		}
	}
	// 7 backs off to 6 bytes = two full runes.
	if kept != "世界" {
		t.Errorf("expected clean two-rune prefix %q, got %q", "世界", kept)
	}
}

// --- inert stage placeholders ----------------------------------------------

func TestInertPermissionAllowsAndNeverEdits(t *testing.T) {
	r := allowAllPermission{}.Decide(context.Background(), core.ToolCall{}, func(core.RunEvent) error { return nil })
	if !r.Allow {
		t.Errorf("inert permission must allow")
	}
	if r.EditedArguments != nil {
		t.Errorf("inert permission must not edit arguments")
	}
}

func TestInertHardChecksNeverBlock(t *testing.T) {
	c := neverBlockCheck{}
	if c.PreCheck(context.Background(), core.ToolCall{}).Block {
		t.Errorf("inert pre-check must not block")
	}
	if c.PostCheck(context.Background(), core.ToolCall{}, core.ToolResult{}).Block {
		t.Errorf("inert post-check must not block")
	}
}

func TestInertSandboxRunsToolDirectly(t *testing.T) {
	h := &fakeTool{name: "x", output: "ran"}
	out, err := directSandbox{}.Run(context.Background(), h, nil)
	if err != nil {
		t.Fatalf("direct sandbox: %v", err)
	}
	if out != "ran" {
		t.Errorf("direct sandbox must return the tool's output, got %q", out)
	}
	if !h.executed {
		t.Errorf("direct sandbox must invoke the tool's command path")
	}
}

// --- chain ordering ---------------------------------------------------------

func TestChainOrderForShellRoutesThroughSandbox(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{})
	tool := &fakeTool{name: "shell", runsCmds: true, output: "ok", visits: &visits}

	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, err := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "shell"}, noEmit)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", res.Result)
	}
	want := []string{"pre", "permission", "sandbox", "execute", "post"}
	assertOrder(t, visits, want)
}

func TestChainOrderForFileOpSkipsSandbox(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{})
	tool := &fakeTool{name: "read", runsCmds: false, output: "ok", visits: &visits}

	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, err := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "read"}, noEmit)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", res.Result)
	}
	want := []string{"pre", "permission", "execute", "post"}
	assertOrder(t, visits, want)
}

// --- short-circuits ---------------------------------------------------------

func TestUnknownToolShortCircuitsBeforeAnyStage(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{})
	ex := New(tools.NewCatalog(), st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "nope"}, noEmit)
	if !res.IsError || !strings.Contains(res.Result, "unknown tool") {
		t.Errorf("want unknown-tool error, got %+v", res)
	}
	if res.ToolCallID != "c1" {
		t.Errorf("error result must be tied to the call, got %q", res.ToolCallID)
	}
	if len(visits) != 0 {
		t.Errorf("no stage may run for an unknown tool, ran %v", visits)
	}
}

func TestBadArgumentsRejectedBeforeExecute(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{})
	tool := &fakeTool{
		name:   "read",
		schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		visits: &visits,
	}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "read", Arguments: map[string]interface{}{}}, noEmit)
	if !res.IsError || !strings.Contains(res.Result, "invalid arguments") {
		t.Errorf("want validation error, got %+v", res)
	}
	if tool.executed {
		t.Errorf("tool work must never run on bad input")
	}
	if len(visits) != 0 {
		t.Errorf("no stage may run before validation, ran %v", visits)
	}
}

func TestHardPreCheckBlockStopsDownstream(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{preBlock: "denied by policy"})
	tool := &fakeTool{name: "shell", runsCmds: true, visits: &visits}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "shell"}, noEmit)
	if !res.IsError || !strings.Contains(res.Result, "denied by policy") {
		t.Errorf("want pre-check block error carrying the reason, got %+v", res)
	}
	if tool.executed {
		t.Errorf("permission, sandbox, and tool must not run after a hard block")
	}
	assertOrder(t, visits, []string{"pre"})
}

func TestPermissionDenialStopsSandboxAndTool(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{denyPermission: "user said no"})
	tool := &fakeTool{name: "shell", runsCmds: true, visits: &visits}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "shell"}, noEmit)
	if !res.IsError || !strings.Contains(res.Result, "permission denied") || !strings.Contains(res.Result, "user said no") {
		t.Errorf("want permission-denied error carrying the reason, got %+v", res)
	}
	if tool.executed {
		t.Errorf("sandbox and tool must not run after denial")
	}
	assertOrder(t, visits, []string{"pre", "permission"})
}

func TestEditedArgumentsAreRevalidatedAndUsed(t *testing.T) {
	var visits []string
	edited := map[string]interface{}{"path": "corrected.txt"}
	st := recordingStages(&visits, recordCfg{editArgs: edited})
	tool := &fakeTool{
		name:   "read",
		schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		output: "ok",
		visits: &visits,
	}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{
		ID: "c1", ToolName: "read", Arguments: map[string]interface{}{"path": "original.txt"},
	}, noEmit)
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Result)
	}
	if tool.gotArgs["path"] != "corrected.txt" {
		t.Errorf("tool must run on corrected arguments, got %v", tool.gotArgs)
	}
}

func TestEditedArgumentsFailingValidationAreRejected(t *testing.T) {
	var visits []string
	// Edited args drop the required field.
	st := recordingStages(&visits, recordCfg{editArgs: map[string]interface{}{"wrong": 1}})
	tool := &fakeTool{
		name:   "read",
		schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		visits: &visits,
	}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{
		ID: "c1", ToolName: "read", Arguments: map[string]interface{}{"path": "ok.txt"},
	}, noEmit)
	if !res.IsError || !strings.Contains(res.Result, "invalid edited arguments") {
		t.Errorf("want edited-args validation error, got %+v", res)
	}
	if tool.executed {
		t.Errorf("tool must not run on invalid edited arguments")
	}
}

func TestHookEditedArgumentsAreRevalidatedBeforePermission(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{preEditArgs: map[string]interface{}{"wrong": 1}})
	tool := &fakeTool{
		name:   "read",
		schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		visits: &visits,
	}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{
		ID: "c1", ToolName: "read", Arguments: map[string]interface{}{"path": "ok.txt"},
	}, noEmit)
	if !res.IsError || !strings.Contains(res.Result, "invalid hook-edited arguments") {
		t.Errorf("want hook edit validation error, got %+v", res)
	}
	if tool.executed {
		t.Errorf("tool must not run on invalid hook-edited arguments")
	}
	assertOrder(t, visits, []string{"pre"})
}

func TestToolHookReplacementOutcomesEmitPerHook(t *testing.T) {
	engine, err := hooks.New([]core.HookRegistration{
		{
			Name:   "first",
			Moment: core.HookBeforeTool,
			Handler: func(context.Context, core.HookEvent) core.HookVerdict {
				return core.HookVerdict{Arguments: map[string]interface{}{"path": "first.txt"}}
			},
		},
		{
			Name:   "second",
			Moment: core.HookBeforeTool,
			Handler: func(context.Context, core.HookEvent) core.HookVerdict {
				return core.HookVerdict{Arguments: map[string]interface{}{"path": "second.txt"}}
			},
		},
	}, nil, hooks.Options{})
	if err != nil {
		t.Fatalf("hook engine: %v", err)
	}

	st := InertStages()
	st.Pre = engine
	tool := &fakeTool{
		name:   "read",
		schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		output: "ok",
	}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	var events []core.RunEvent
	res, _ := ex.Dispatch(context.Background(), core.ToolCall{
		ID: "c1", ToolName: "read", Arguments: map[string]interface{}{"path": "original.txt"},
	}, func(ev core.RunEvent) error {
		events = append(events, ev)
		return nil
	})
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if tool.gotArgs["path"] != "second.txt" {
		t.Fatalf("last replacement should feed the tool, got %v", tool.gotArgs)
	}
	var names []string
	for _, ev := range events {
		if ev.Type == core.HookOutcomeEvent {
			names = append(names, ev.HookOutcome.HookName)
		}
	}
	if strings.Join(names, ",") != "first,second" {
		t.Fatalf("expected one outcome per replacing hook, got %v", names)
	}
}

func TestHardPostCheckVetoTurnsSuccessIntoError(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{postBlock: "secret leaked"})
	tool := &fakeTool{name: "read", output: "sensitive output", visits: &visits}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "read"}, noEmit)
	if !res.IsError || !strings.Contains(res.Result, "secret leaked") {
		t.Errorf("want post-check veto error carrying the reason, got %+v", res)
	}
	if !tool.executed {
		t.Errorf("the tool runs before the post-check vetoes its result")
	}
	assertOrder(t, visits, []string{"pre", "permission", "execute", "post"})
}

func TestHardPostCheckReplacementReachesModel(t *testing.T) {
	var visits []string
	st := recordingStages(&visits, recordCfg{postReplacement: "redacted"})
	tool := &fakeTool{name: "read", output: "secret", visits: &visits}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, st, 0)

	res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "read"}, noEmit)
	if res.IsError || res.Result != "redacted" {
		t.Errorf("want replaced result, got %+v", res)
	}
}

func TestCancellationReturnsErrorResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tool := &fakeTool{name: "read", failWith: context.Canceled}
	cat := tools.NewCatalog()
	cat.MustRegister(tool)
	ex := New(cat, InertStages(), 0)

	res, err := ex.Dispatch(ctx, core.ToolCall{ID: "c1", ToolName: "read"}, noEmit)
	if err != nil {
		t.Fatalf("dispatch must not return a Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Result, "context canceled") {
		t.Errorf("want cancellation error result, got %+v", res)
	}
}

// --- helpers ----------------------------------------------------------------

// --- custom-tool parity -----------------------------------------------------

// A custom (SDK-registered) tool must travel the identical path as a built-in:
// gated, sandboxed only when it declares command execution, and truncated by the
// same output budget — with no special-casing.
func TestCustomToolParity(t *testing.T) {
	t.Run("command tool routes through sandbox", func(t *testing.T) {
		var visits []string
		st := recordingStages(&visits, recordCfg{})
		custom := &fakeTool{name: "deploy", runsCmds: true, output: "shipped", visits: &visits}
		cat := tools.NewCatalog()
		cat.MustRegister(custom)
		ex := New(cat, st, 0)

		res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "deploy"}, noEmit)
		if res.IsError {
			t.Fatalf("unexpected error: %q", res.Result)
		}
		assertOrder(t, visits, []string{"pre", "permission", "sandbox", "execute", "post"})
	})

	t.Run("non-command tool skips sandbox", func(t *testing.T) {
		var visits []string
		st := recordingStages(&visits, recordCfg{})
		custom := &fakeTool{name: "lint", runsCmds: false, output: "clean", visits: &visits}
		cat := tools.NewCatalog()
		cat.MustRegister(custom)
		ex := New(cat, st, 0)

		_, _ = ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "lint"}, noEmit)
		assertOrder(t, visits, []string{"pre", "permission", "execute", "post"})
	})

	t.Run("output truncated by the same budget", func(t *testing.T) {
		custom := &fakeTool{name: "dump", output: strings.Repeat("z", 100)}
		cat := tools.NewCatalog()
		cat.MustRegister(custom)
		ex := New(cat, InertStages(), 10)

		res, _ := ex.Dispatch(context.Background(), core.ToolCall{ID: "c1", ToolName: "dump"}, noEmit)
		if !strings.Contains(res.Result, "truncated") {
			t.Errorf("custom tool output must be truncated by the shared budget, got %q", res.Result)
		}
	})
}

func noEmit(core.RunEvent) error { return nil }

// fakeTool is a configurable ToolHandler that records when its work runs and on
// what arguments. It appends "execute" to a shared visit log so chain ordering is
// observable.
type fakeTool struct {
	name     string
	schema   string
	output   string
	runsCmds bool
	failWith error

	visits   *[]string
	executed bool
	gotArgs  map[string]interface{}
}

func (f *fakeTool) Descriptor() core.Tool {
	schema := f.schema
	if schema == "" {
		schema = `{"type":"object"}`
	}
	return core.Tool{Name: f.name, Description: "fake " + f.name, Parameters: json.RawMessage(schema)}
}

func (f *fakeTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	f.executed = true
	f.gotArgs = args
	if f.visits != nil {
		*f.visits = append(*f.visits, "execute")
	}
	if f.failWith != nil {
		return "", f.failWith
	}
	return f.output, nil
}

func (f *fakeTool) RunsCommands() bool { return f.runsCmds }

// recordCfg configures the recording stages' verdicts.
type recordCfg struct {
	preBlock        string
	preEditArgs     map[string]interface{}
	denyPermission  string
	editArgs        map[string]interface{}
	postBlock       string
	postReplacement string
}

func recordingStages(visits *[]string, cfg recordCfg) Stages {
	return Stages{
		Pre:        recPre{visits, cfg.preBlock, cfg.preEditArgs},
		Permission: recPerm{visits, cfg.denyPermission, cfg.editArgs},
		Sandbox:    recSandbox{visits},
		Post:       recPost{visits, cfg.postBlock, cfg.postReplacement},
	}
}

type recPre struct {
	visits *[]string
	block  string
	edit   map[string]interface{}
}

func (r recPre) PreCheck(context.Context, core.ToolCall) core.StageDecision {
	*r.visits = append(*r.visits, "pre")
	return core.StageDecision{Block: r.block != "", Reason: r.block, EditedArguments: r.edit}
}

type recPerm struct {
	visits *[]string
	deny   string
	edit   map[string]interface{}
}

func (r recPerm) Decide(context.Context, core.ToolCall, func(core.RunEvent) error) core.PermissionResult {
	*r.visits = append(*r.visits, "permission")
	if r.deny != "" {
		return core.PermissionResult{Allow: false, Reason: r.deny}
	}
	return core.PermissionResult{Allow: true, EditedArguments: r.edit}
}

type recSandbox struct{ visits *[]string }

func (r recSandbox) Run(ctx context.Context, h core.ToolHandler, args map[string]interface{}) (string, error) {
	*r.visits = append(*r.visits, "sandbox")
	return h.Execute(ctx, args)
}

type recPost struct {
	visits      *[]string
	block       string
	replacement string
}

func (r recPost) PostCheck(_ context.Context, _ core.ToolCall, result core.ToolResult) core.StageDecision {
	*r.visits = append(*r.visits, "post")
	if r.replacement != "" {
		result.Result = r.replacement
		return core.StageDecision{ReplacementResult: &result}
	}
	return core.StageDecision{Block: r.block != "", Reason: r.block}
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("stage order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage order = %v, want %v", got, want)
		}
	}
}
