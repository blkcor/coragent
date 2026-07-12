package executor

import (
	"context"
	"errors"
	"runtime"
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

type previewOnlyTestTool struct {
	previewed []string
	executed  string
}

type oversizedPreviewTool struct{}

type heldAdversarialPreviewTool struct {
	preview core.ActionPreview
}

func (oversizedPreviewTool) Descriptor() core.Tool {
	return core.Tool{Name: "oversized_preview", Parameters: []byte(`{"type":"object"}`)}
}
func (oversizedPreviewTool) RunsCommands() bool          { return false }
func (oversizedPreviewTool) ActionKind() core.ActionKind { return core.ActionRead }
func (oversizedPreviewTool) PreviewAction(context.Context, map[string]interface{}) (core.ActionPreview, error) {
	return core.ActionPreview{
		Kind: core.ActionPreviewText, Operation: core.ActionOperationCustom,
		Summary: "authoritative summary", Targets: []string{"stable-target"},
		Text: strings.Repeat("oversized command body\n", 5_000),
		Metadata: map[string]string{
			"aggregate_count":  "5000",
			"oversized_detail": strings.Repeat("metadata ", 10_000),
		},
	}, nil
}
func (oversizedPreviewTool) Execute(context.Context, map[string]interface{}) (string, error) {
	return "ok", nil
}

func (*heldAdversarialPreviewTool) Descriptor() core.Tool {
	return core.Tool{Name: "held_adversarial_preview", Parameters: []byte(`{"type":"object"}`)}
}
func (*heldAdversarialPreviewTool) RunsCommands() bool          { return false }
func (*heldAdversarialPreviewTool) ActionKind() core.ActionKind { return core.ActionRead }
func (tool *heldAdversarialPreviewTool) PreviewAction(context.Context, map[string]interface{}) (core.ActionPreview, error) {
	return tool.preview, nil
}
func (*heldAdversarialPreviewTool) Execute(context.Context, map[string]interface{}) (string, error) {
	return "ok", nil
}

func (*previewOnlyTestTool) Descriptor() core.Tool {
	return core.Tool{Name: "preview_only", Parameters: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}
}
func (*previewOnlyTestTool) RunsCommands() bool          { return false }
func (*previewOnlyTestTool) ActionKind() core.ActionKind { return core.ActionRead }
func (tool *previewOnlyTestTool) PreviewAction(_ context.Context, args map[string]interface{}) (core.ActionPreview, error) {
	path := args["path"].(string)
	tool.previewed = append(tool.previewed, path)
	return core.ActionPreview{
		Kind: core.ActionPreviewMetadata, Operation: core.ActionOperationCustom,
		Metadata: map[string]string{"path": path},
	}, nil
}
func (tool *previewOnlyTestTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	tool.executed = args["path"].(string)
	return "ok", nil
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

func TestActionPreviewerRecomputesEveryEffectiveRevisionWithoutCommitPath(t *testing.T) {
	tool := &previewOnlyTestTool{}
	catalog := tools.NewCatalog()
	catalog.MustRegister(tool)
	permission := &revisionPermission{}
	executor := New(catalog, Stages{Pre: neverBlockCheck{}, Permission: permission, Sandbox: directSandbox{}, Post: neverBlockCheck{}}, 0)
	var revisions []core.PreviewRevision
	var paths []string
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{
		ID: "provider", ToolName: "preview_only", Arguments: map[string]interface{}{"path": "provider"},
	}, "call-1", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindToolPrepared {
			payload := event.Payload.(*core.ToolPreparedPayload)
			revisions = append(revisions, payload.Revision)
			paths = append(paths, payload.EffectiveCall.Arguments["path"].(string))
			if payload.Preview.Kind == core.ActionPreviewUnavailable {
				t.Fatal("ActionPreviewer produced unavailable preview")
			}
		}
		return nil
	})
	if err != nil || result.Result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got, want := strings.Join(tool.previewed, ","), "provider,human-edit"; got != want {
		t.Fatalf("previewed revisions = %q, want %q", got, want)
	}
	if len(revisions) != 2 || revisions[0] != 1 || revisions[1] != 2 || strings.Join(paths, ",") != "provider,human-edit" {
		t.Fatalf("revisions=%v paths=%v", revisions, paths)
	}
	if tool.executed != "human-edit" {
		t.Fatalf("executed args = %q", tool.executed)
	}
}

func TestActionPreviewerPayloadUsesSharedPreviewBudget(t *testing.T) {
	catalog := tools.NewCatalog()
	catalog.MustRegister(oversizedPreviewTool{})
	executor := New(catalog, InertStages(), 0)
	var prepared *core.ToolPreparedPayload
	var omission *core.Omission
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{
		ID: "provider", ToolName: "oversized_preview", Arguments: map[string]interface{}{},
	}, "call-budget", core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		switch event.Kind {
		case core.ObservedKindToolPrepared:
			prepared = event.Payload.(*core.ToolPreparedPayload)
		case core.ObservedKindOmissionReported:
			value := event.Payload.(*core.OmissionReportedPayload).Omission
			omission = &value
		}
		return nil
	})
	if err != nil || result.Result.IsError || prepared == nil {
		t.Fatalf("result=%+v prepared=%+v err=%v", result, prepared, err)
	}
	preview := prepared.Preview
	bytes, lines, _ := projectedPreviewUsage(preview)
	if bytes > actionPreviewByteLimit || lines > actionPreviewLineLimit || !strings.Contains(preview.Metadata["aggregate_count"], "5000") {
		t.Fatalf("bounded preview bytes=%d lines=%d metadata=%v", bytes, lines, preview.Metadata)
	}
	if preview.Operation != core.ActionOperationCustom || len(preview.Targets) != 1 || preview.Targets[0] != "stable-target" {
		t.Fatalf("preview identity facts were lost: %+v", preview)
	}
	if omission == nil || omission.Kind != core.OmissionPreviewBudget || omission.CallID != "call-budget" || omission.Revision != 1 || !omission.OriginalBytes.Known || !omission.RetainedBytes.Known || omission.OriginalBytes.Value <= omission.RetainedBytes.Value {
		t.Fatalf("omission = %+v", omission)
	}
}

func TestDispatchBoundsPreviewBeforeCloningForBothHandlerContracts(t *testing.T) {
	const (
		sourceRecords        = 1_500_000
		maxDispatchHeapBytes = 12 << 20
	)
	tests := []struct {
		name    string
		build   func(core.ActionPreview) (core.ToolHandler, core.ToolCall)
		commits func(core.ToolHandler) bool
	}{
		{
			name: "action previewer",
			build: func(preview core.ActionPreview) (core.ToolHandler, core.ToolCall) {
				tool := &heldAdversarialPreviewTool{preview: preview}
				return tool, core.ToolCall{ID: "provider", ToolName: tool.Descriptor().Name, Arguments: map[string]interface{}{}}
			},
		},
		{
			name: "prepared action",
			build: func(preview core.ActionPreview) (core.ToolHandler, core.ToolCall) {
				tool := &preparedTestTool{name: "adversarial_prepared", operation: core.ActionOperationModify, preview: preview, output: "ok"}
				return tool, core.ToolCall{ID: "provider", ToolName: tool.name, Arguments: map[string]interface{}{"path": "target"}}
			},
			commits: func(handler core.ToolHandler) bool { return handler.(*preparedTestTool).committed },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targets := make([]string, sourceRecords)
			preview := core.ActionPreview{
				Kind: core.ActionPreviewMetadata, Operation: core.ActionOperationCustom,
				Summary: "bounded dispatch", Targets: targets,
				Metadata: map[string]string{"aggregate_count": "1500000"},
			}
			handler, call := test.build(preview)
			catalog := tools.NewCatalog()
			catalog.MustRegister(handler)
			executor := New(catalog, InertStages(), 0)

			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			result, err := executor.DispatchRich(context.Background(), call, "call-prebound", core.Origin{AgentID: "root"}, func(core.RichEvent) error { return nil })
			runtime.ReadMemStats(&after)
			if err != nil || result.Result.IsError {
				t.Fatalf("dispatch result=%+v err=%v", result, err)
			}
			if test.commits != nil && !test.commits(handler) {
				t.Fatal("prepared commit token path was not preserved")
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			if allocated > maxDispatchHeapBytes {
				t.Fatalf("dispatch allocated %d bytes for an already-held %d-record preview; raw preview was likely cloned before bounding", allocated, sourceRecords)
			}
		})
	}
}

func TestDispatchBudgetsGeneratedPreviewOmissionIdentity(t *testing.T) {
	catalog := tools.NewCatalog()
	catalog.MustRegister(oversizedPreviewTool{})
	executor := New(catalog, InertStages(), 0)
	hugeCallID := core.CallID(strings.Repeat("call-id-", actionPreviewByteLimit))
	var prepared *core.ToolPreparedPayload
	result, err := executor.DispatchRich(context.Background(), core.ToolCall{
		ID: "provider", ToolName: "oversized_preview", Arguments: map[string]interface{}{},
	}, hugeCallID, core.Origin{AgentID: "root"}, func(event core.RichEvent) error {
		if event.Kind == core.ObservedKindToolPrepared {
			prepared = event.Payload.(*core.ToolPreparedPayload)
		}
		return nil
	})
	if err != nil || result.Result.IsError || prepared == nil {
		t.Fatalf("dispatch result=%+v prepared=%+v err=%v", result, prepared, err)
	}
	bytes, lines, _ := projectedPreviewUsage(prepared.Preview)
	if bytes > actionPreviewByteLimit || lines > actionPreviewLineLimit {
		t.Fatalf("preview with omission identity exceeded budget: bytes=%d lines=%d", bytes, lines)
	}
	if prepared.Preview.Omission == nil || prepared.Preview.Omission.Kind != core.OmissionPreviewBudget {
		t.Fatalf("preview omission = %+v", prepared.Preview.Omission)
	}
	if len(prepared.Preview.Omission.CallID) >= len(hugeCallID) || len(prepared.Preview.Omission.CorrelationID) >= len(hugeCallID) {
		t.Fatal("oversized omission identity was not bounded")
	}
}

func TestBoundActionPreviewDoesNotProjectUnboundedCollections(t *testing.T) {
	const collectionSize = actionPreviewLineLimit * 20
	targets := make([]string, collectionSize)
	hunks := make([]core.DiffHunk, collectionSize)
	hunks[0] = core.DiffHunk{OldStart: 11, OldLines: 12, NewStart: 21, NewLines: 22}
	lines := make([]core.DiffLine, collectionSize)
	for index := range lines {
		lines[index].Kind = core.DiffLineContext
	}

	aggregate := core.OptionalUint64{Known: true, Value: collectionSize}
	tests := []struct {
		name    string
		preview core.ActionPreview
	}{
		{
			name: "targets",
			preview: core.ActionPreview{
				Kind: core.ActionPreviewMetadata, Operation: core.ActionOperationCustom, Targets: targets,
			},
		},
		{
			name: "hunks",
			preview: core.ActionPreview{
				Kind: core.ActionPreviewFileDiff, Operation: core.ActionOperationModify,
				FileDiff: &core.FileDiffPreview{ChangedRegions: aggregate, Hunks: hunks},
			},
		},
		{
			name: "lines",
			preview: core.ActionPreview{
				Kind: core.ActionPreviewFileDiff, Operation: core.ActionOperationModify,
				FileDiff: &core.FileDiffPreview{
					ChangedRegions: aggregate,
					Hunks:          []core.DiffHunk{{OldStart: 11, OldLines: aggregate.Value, NewStart: 21, NewLines: aggregate.Value, Lines: lines}},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bounded := boundActionPreview(test.preview)
			bytes, logicalLines, records := projectedPreviewUsage(bounded)
			if bytes > actionPreviewByteLimit || logicalLines > actionPreviewLineLimit || records > actionPreviewLineLimit {
				t.Fatalf("projected bytes=%d lines=%d records=%d", bytes, logicalLines, records)
			}
			if cap(bounded.Targets) > actionPreviewLineLimit {
				t.Fatalf("target capacity = %d", cap(bounded.Targets))
			}
			if bounded.FileDiff != nil {
				if bounded.FileDiff.ChangedRegions != aggregate {
					t.Fatalf("aggregate changed regions = %+v", bounded.FileDiff.ChangedRegions)
				}
				if cap(bounded.FileDiff.Hunks) > actionPreviewLineLimit {
					t.Fatalf("hunk capacity = %d", cap(bounded.FileDiff.Hunks))
				}
				for _, hunk := range bounded.FileDiff.Hunks {
					if cap(hunk.Lines) > actionPreviewLineLimit {
						t.Fatalf("line capacity = %d", cap(hunk.Lines))
					}
				}
				if len(bounded.FileDiff.Hunks) == 0 || bounded.FileDiff.Hunks[0].OldStart != 11 {
					t.Fatalf("retained typed hunk aggregates = %+v", bounded.FileDiff.Hunks)
				}
			}
			if bounded.Omission == nil || bounded.Omission.Kind != core.OmissionPreviewBudget || !bounded.Omission.OriginalLines.Known || !bounded.Omission.RetainedLines.Known || bounded.Omission.OriginalLines.Value <= bounded.Omission.RetainedLines.Value || bounded.Omission.RetainedLines.Value > actionPreviewLineLimit {
				t.Fatalf("omission = %+v", bounded.Omission)
			}
		})
	}
}

func TestBoundActionPreviewBudgetsEveryVariableStringField(t *testing.T) {
	huge := strings.Repeat("x", actionPreviewByteLimit*2)
	base := func() core.ActionPreview {
		return core.ActionPreview{Kind: core.ActionPreviewText, Operation: core.ActionOperationCustom}
	}
	tests := []struct {
		name  string
		build func() core.ActionPreview
	}{
		{name: "summary", build: func() core.ActionPreview { value := base(); value.Summary = huge; return value }},
		{name: "target", build: func() core.ActionPreview { value := base(); value.Targets = []string{huge}; return value }},
		{name: "unavailable reason", build: func() core.ActionPreview { value := base(); value.UnavailableReason = huge; return value }},
		{name: "metadata key", build: func() core.ActionPreview {
			value := base()
			value.Metadata = map[string]string{huge: "value"}
			return value
		}},
		{name: "metadata value", build: func() core.ActionPreview {
			value := base()
			value.Metadata = map[string]string{"key": huge}
			return value
		}},
		{name: "text", build: func() core.ActionPreview { value := base(); value.Text = huge; return value }},
		{name: "file diff path", build: func() core.ActionPreview {
			value := base()
			value.FileDiff = &core.FileDiffPreview{Path: huge}
			return value
		}},
		{name: "file diff line", build: func() core.ActionPreview {
			value := base()
			value.FileDiff = &core.FileDiffPreview{Hunks: []core.DiffHunk{{Lines: []core.DiffLine{{Kind: core.DiffLineAdded, Text: huge}}}}}
			return value
		}},
		{name: "omission correlation id", build: func() core.ActionPreview {
			value := base()
			value.Omission = &core.Omission{Kind: core.OmissionOutputBudget, Scope: core.OmissionScopeToolOutput, CorrelationID: huge}
			return value
		}},
		{name: "omission call id", build: func() core.ActionPreview {
			value := base()
			value.Omission = &core.Omission{Kind: core.OmissionOutputBudget, Scope: core.OmissionScopeToolOutput, CallID: core.CallID(huge)}
			return value
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bounded := boundActionPreview(test.build())
			bytes, lines, _ := projectedPreviewUsage(bounded)
			if bytes > actionPreviewByteLimit || lines > actionPreviewLineLimit {
				t.Fatalf("projected bytes=%d lines=%d", bytes, lines)
			}
			if bounded.Omission == nil || bounded.Omission.Kind != core.OmissionPreviewBudget || !bounded.Omission.OriginalBytes.Known || !bounded.Omission.RetainedBytes.Known || bounded.Omission.OriginalBytes.Value <= bounded.Omission.RetainedBytes.Value {
				t.Fatalf("omission = %+v", bounded.Omission)
			}
		})
	}
}

func TestBoundActionPreviewNormalizesUnboundedTypedStringsAndOmissionIDs(t *testing.T) {
	huge := strings.Repeat("x", actionPreviewByteLimit*2)
	bounded := boundActionPreview(core.ActionPreview{
		Kind: core.ActionPreviewKind(huge), Operation: core.ActionOperation(huge),
		FileDiff: &core.FileDiffPreview{Hunks: []core.DiffHunk{{Lines: []core.DiffLine{{Kind: core.DiffLineKind(huge)}}}}},
		Omission: &core.Omission{
			Kind: core.OmissionKind(huge), Scope: core.OmissionScope(huge),
			CorrelationID: huge, CallID: core.CallID(huge),
			Recoverability: core.Recoverability(huge), Continuation: core.ContinuationMode(huge),
		},
	})
	if bounded.Kind != core.ActionPreviewKindUnknown || bounded.Operation != core.ActionOperationUnknown {
		t.Fatalf("typed preview fields = kind %q operation %q", bounded.Kind, bounded.Operation)
	}
	if bounded.FileDiff == nil || len(bounded.FileDiff.Hunks) != 1 || len(bounded.FileDiff.Hunks[0].Lines) != 1 || bounded.FileDiff.Hunks[0].Lines[0].Kind != unknownDiffLineKind {
		t.Fatalf("typed diff line fields = %+v", bounded.FileDiff)
	}
	if bounded.Omission == nil || bounded.Omission.Kind != core.OmissionPreviewBudget || bounded.Omission.CorrelationID != "" || bounded.Omission.CallID != "" || !bounded.Omission.OriginalBytes.Known || bounded.Omission.OriginalBytes.Value <= actionPreviewByteLimit {
		t.Fatalf("bounded omission = %+v", bounded.Omission)
	}
}

func TestBoundActionPreviewStopsOnNormalizedMetadataKeyCollision(t *testing.T) {
	bounded := boundActionPreview(core.ActionPreview{
		Kind: core.ActionPreviewMetadata, Operation: core.ActionOperationCustom,
		Metadata: map[string]string{"\xfe": "first", "\xff": "second"},
	})
	if len(bounded.Metadata) != 1 {
		t.Fatalf("normalized metadata = %#v", bounded.Metadata)
	}
	if bounded.Omission == nil || bounded.Omission.Kind != core.OmissionPreviewBudget {
		t.Fatalf("omission = %+v", bounded.Omission)
	}
}

func projectedPreviewUsage(preview core.ActionPreview) (bytes, lines, records int) {
	addString := func(value string, minimumLines int) {
		bytes += len(value)
		lines += max(minimumLines, logicalLineCount(value))
	}
	addString(preview.Summary, 0)
	for _, target := range preview.Targets {
		addString(target, 1)
		records++
	}
	addString(preview.UnavailableReason, 0)
	for key, value := range preview.Metadata {
		addString(key, 1)
		bytes += 2
		addString(value, 0)
		records++
	}
	if preview.FileDiff != nil {
		addString(preview.FileDiff.Path, 0)
		for _, hunk := range preview.FileDiff.Hunks {
			lines++
			records++
			for _, line := range hunk.Lines {
				addString(line.Text, 1)
				records++
			}
		}
	}
	addString(preview.Text, 0)
	if preview.Omission != nil && preview.Omission.Kind != core.OmissionPreviewBudget {
		addString(preview.Omission.CorrelationID, 0)
		addString(string(preview.Omission.CallID), 0)
	}
	return bytes, lines, records
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
