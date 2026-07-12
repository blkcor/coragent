// Package executor owns the single tool-execution chokepoint.
package executor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/tools"
)

// Stages bundles the four replaceable gate implementations around execution.
type Stages struct {
	Pre        core.PreToolCheck
	Permission core.Permission
	Sandbox    core.Sandbox
	Post       core.PostToolCheck
}

// Executor resolves calls and drives the one ordered chain.
type Executor struct {
	catalog *tools.Catalog
	stages  Stages
	budget  int
}

func New(catalog *tools.Catalog, stages Stages, budget int) *Executor {
	if budget <= 0 {
		budget = DefaultOutputBudget
	}
	return &Executor{catalog: catalog, stages: stages, budget: budget}
}

func NewDefault(catalog *tools.Catalog) *Executor {
	return New(catalog, InertStages(), DefaultOutputBudget)
}

// Dispatch preserves the required legacy seam while using the same dispatch
// implementation as DispatchRich.
func (e *Executor) Dispatch(ctx context.Context, call core.ToolCall, emit func(core.RunEvent) error) (core.ToolResult, error) {
	execution, err := e.dispatch(ctx, call, "", core.Origin{}, nil, emit)
	return execution.Result, err
}

// DispatchRich adds prepared/executing/omission facts without creating another
// execution path.
func (e *Executor) DispatchRich(ctx context.Context, call core.ToolCall, callID core.CallID, origin core.Origin, emit func(core.RichEvent) error) (core.RichDispatchResult, error) {
	return e.dispatch(ctx, call, callID, origin, emit, nil)
}

func (e *Executor) dispatch(
	ctx context.Context,
	providerCall core.ToolCall,
	callID core.CallID,
	origin core.Origin,
	richEmit func(core.RichEvent) error,
	legacyEmit func(core.RunEvent) error,
) (core.RichDispatchResult, error) {
	result := core.RichDispatchResult{Revision: 1, Outcome: core.ToolOutcomeUnknown}
	handler, ok := e.catalog.Lookup(providerCall.ToolName)
	if !ok {
		result.Result = e.finalResult(providerCall.ID, "unknown tool: "+providerCall.ToolName, true, callID, richEmit)
		result.Outcome = core.ToolOutcomeFailed
		return result, nil
	}

	call := cloneCall(providerCall)
	if err := validateArgs(handler.Descriptor().Parameters, call.Arguments); err != nil {
		result.Result = e.finalResult(call.ID, "invalid arguments: "+err.Error(), true, callID, richEmit)
		result.Outcome = core.ToolOutcomeFailed
		return result, nil
	}
	action := classify(handler)
	bridge := e.eventBridge(ctx, callID, origin, action, richEmit, legacyEmit)

	checked, _, blocked := e.applyPreCheck(ctx, handler, call, bridge)
	if blocked != "" {
		result.Result = e.finalResult(call.ID, blocked, true, callID, richEmit)
		result.Outcome = core.ToolOutcomeHookBlocked
		return result, nil
	}
	call = checked

	prepared, err := e.prepare(ctx, handler, call, callID, result.Revision, origin, richEmit)
	if err != nil {
		result.Result = e.finalResult(call.ID, "prepare action: "+err.Error(), true, callID, richEmit)
		result.Outcome = core.ToolOutcomeFailed
		return result, nil
	}
	call.Arguments = cloneArgumentMap(prepared.EffectiveArguments)

	var grants core.SandboxGrants
	if richPermission, ok := e.stages.Permission.(core.RichPermission); ok && richEmit != nil {
		for {
			grantOptions := e.grantOptions(handler)
			permission := richPermission.DecideRich(ctx, core.RichPermissionInput{
				RunContext: ctx, RequestID: core.RequestID(core.NewOpaqueID("permission")),
				CallID: callID, Revision: result.Revision, Origin: origin,
				EffectiveCall: cloneCall(call), Action: action, Preview: prepared.Preview.Clone(),
				GrantOptions: grantOptions, ValidateReply: e.richReplyValidator(handler, grantOptions),
			}, richEmit)
			switch permission.Action {
			case core.PermissionReplyDeny:
				result.Result = e.finalResult(call.ID, "permission denied: "+permission.Reason, true, callID, richEmit)
				result.Outcome = core.ToolOutcomeDenied
				return result, nil
			case core.PermissionReplyAllow:
				grants = permission.SandboxGrants.Clone()
				if permission.LegacyEditedApproval {
					if err := validateArgs(handler.Descriptor().Parameters, permission.RevisedArguments); err != nil {
						result.Result = e.finalResult(call.ID, "invalid edited arguments: "+err.Error(), true, callID, richEmit)
						result.Outcome = core.ToolOutcomeFailed
						return result, nil
					}
					result.Revision++
					edited := cloneCall(call)
					edited.Arguments = cloneArgumentMap(permission.RevisedArguments)
					checked, replaced, blocked := e.applyPreCheck(ctx, handler, edited, bridge)
					if blocked != "" {
						result.Result = e.finalResult(call.ID, blocked, true, callID, richEmit)
						result.Outcome = core.ToolOutcomeHookBlocked
						return result, nil
					}
					call = checked
					prepared, err = e.prepare(ctx, handler, call, callID, result.Revision, origin, richEmit)
					if err != nil {
						result.Result = e.finalResult(call.ID, "prepare edited action: "+err.Error(), true, callID, richEmit)
						result.Outcome = core.ToolOutcomeFailed
						return result, nil
					}
					call.Arguments = cloneArgumentMap(prepared.EffectiveArguments)
					if replaced {
						continue
					}
				}
				goto permissionAccepted
			case core.PermissionReplyReviseArguments:
				if err := validateArgs(handler.Descriptor().Parameters, permission.RevisedArguments); err != nil {
					result.Result = e.finalResult(call.ID, "invalid revised arguments: "+err.Error(), true, callID, richEmit)
					result.Outcome = core.ToolOutcomeFailed
					return result, nil
				}
				result.Revision++
				revised := cloneCall(call)
				revised.Arguments = cloneArgumentMap(permission.RevisedArguments)
				checked, _, blocked := e.applyPreCheck(ctx, handler, revised, bridge)
				if blocked != "" {
					result.Result = e.finalResult(call.ID, blocked, true, callID, richEmit)
					result.Outcome = core.ToolOutcomeHookBlocked
					return result, nil
				}
				call = checked
				prepared, err = e.prepare(ctx, handler, call, callID, result.Revision, origin, richEmit)
				if err != nil {
					result.Result = e.finalResult(call.ID, "prepare revised action: "+err.Error(), true, callID, richEmit)
					result.Outcome = core.ToolOutcomeFailed
					return result, nil
				}
				call.Arguments = cloneArgumentMap(prepared.EffectiveArguments)
				// A rich revision is deliberately non-approving. Loop to a fresh
				// request ID for the newly prepared preview.
			default:
				result.Result = e.finalResult(call.ID, "permission denied: invalid rich permission result", true, callID, richEmit)
				result.Outcome = core.ToolOutcomeDenied
				return result, nil
			}
		}
	} else {
		for {
			permission := e.stages.Permission.Decide(ctx, call, action, bridge)
			if !permission.Allow {
				result.Result = e.finalResult(call.ID, "permission denied: "+permission.Reason, true, callID, richEmit)
				result.Outcome = core.ToolOutcomeDenied
				return result, nil
			}
			grants = permission.SandboxGrants.Clone()
			if permission.EditedArguments == nil {
				break
			}

			if err := validateArgs(handler.Descriptor().Parameters, permission.EditedArguments); err != nil {
				result.Result = e.finalResult(call.ID, "invalid edited arguments: "+err.Error(), true, callID, richEmit)
				result.Outcome = core.ToolOutcomeFailed
				return result, nil
			}
			result.Revision++
			edited := cloneCall(call)
			edited.Arguments = cloneArgumentMap(permission.EditedArguments)
			checked, replaced, blocked := e.applyPreCheck(ctx, handler, edited, bridge)
			if blocked != "" {
				result.Result = e.finalResult(call.ID, blocked, true, callID, richEmit)
				result.Outcome = core.ToolOutcomeHookBlocked
				return result, nil
			}
			call = checked
			prepared, err = e.prepare(ctx, handler, call, callID, result.Revision, origin, richEmit)
			if err != nil {
				result.Result = e.finalResult(call.ID, "prepare revised action: "+err.Error(), true, callID, richEmit)
				result.Outcome = core.ToolOutcomeFailed
				return result, nil
			}
			call.Arguments = cloneArgumentMap(prepared.EffectiveArguments)
			grants = core.SandboxGrants{}
			if replaced {
				continue
			}
			break
		}
	}

permissionAccepted:
	if richEmit != nil {
		if err := richEmit(core.RichEvent{
			Origin: origin, Kind: core.ObservedKindToolExecuting,
			Payload: &core.ToolExecutingPayload{CallID: callID, Revision: result.Revision},
		}); err != nil {
			return result, err
		}
	}

	output, executeErr := e.execute(ctx, handler, prepared, call.Arguments, grants, callID, origin, richEmit, bridge)
	rawResult := core.ToolResult{ToolCallID: call.ID, Result: output, IsError: executeErr != nil}
	if executeErr != nil && rawResult.Result == "" {
		rawResult.Result = executeErr.Error()
	}
	if executeErr != nil {
		result.Outcome = core.ToolOutcomeFailed
		result.Result = e.finalToolResult(rawResult, callID, richEmit)
		return result, nil
	}

	decision := e.postCheck(ctx, call, rawResult, bridge)
	e.emitHookOutcome(bridge, core.HookAfterTool, decision.Outcome)
	switch {
	case decision.Block:
		rawResult = core.ToolResult{ToolCallID: call.ID, Result: "blocked by hard post-check: " + decision.Reason, IsError: true}
		result.Outcome = core.ToolOutcomeHookBlocked
	case decision.ReplacementResult != nil:
		rawResult = *decision.ReplacementResult
		rawResult.ToolCallID = call.ID
	default:
		if rawResult.IsError {
			result.Outcome = core.ToolOutcomeFailed
		} else {
			result.Outcome = core.ToolOutcomeSucceeded
		}
	}
	result.Result = e.finalToolResult(rawResult, callID, richEmit)
	return result, nil
}

func (e *Executor) grantOptions(handler core.ToolHandler) core.SandboxGrantOptions {
	if !handler.RunsCommands() {
		return core.SandboxGrantOptions{Support: core.CapabilitySupportUnsupported}
	}
	if _, ok := e.stages.Sandbox.(sandboxWithGrants); !ok {
		return core.SandboxGrantOptions{Support: core.CapabilitySupportUnsupported}
	}
	return core.SandboxGrantOptions{
		Support:   core.CapabilitySupportSupported,
		ReadRoots: true, WriteRoots: true, Network: true,
	}
}

func (e *Executor) richReplyValidator(handler core.ToolHandler, options core.SandboxGrantOptions) func(core.ObservedPermissionDecision) []core.PermissionReplyFeedback {
	return func(decision core.ObservedPermissionDecision) []core.PermissionReplyFeedback {
		feedback := func(field, code, message string) []core.PermissionReplyFeedback {
			return []core.PermissionReplyFeedback{{Field: field, Code: code, Message: message}}
		}
		if decision.Action == core.PermissionReplyReviseArguments {
			if err := validateArgs(handler.Descriptor().Parameters, decision.RevisedArguments); err != nil {
				return feedback("revised_arguments", "schema_invalid", err.Error())
			}
		}
		if len(decision.SandboxGrants.ExtraReadRoots) > 0 && !options.ReadRoots {
			return feedback("sandbox_grants.extra_read_roots", "unsupported", "extra read roots are unavailable")
		}
		if len(decision.SandboxGrants.ExtraWriteRoots) > 0 && !options.WriteRoots {
			return feedback("sandbox_grants.extra_write_roots", "unsupported", "extra write roots are unavailable")
		}
		if decision.SandboxGrants.Network && !options.Network {
			return feedback("sandbox_grants.network", "unsupported", "network access is unavailable")
		}
		for field, roots := range map[string][]string{
			"sandbox_grants.extra_read_roots":  decision.SandboxGrants.ExtraReadRoots,
			"sandbox_grants.extra_write_roots": decision.SandboxGrants.ExtraWriteRoots,
		} {
			seen := make(map[string]struct{}, len(roots))
			for _, root := range roots {
				if root == "" || strings.ContainsRune(root, '\x00') || !filepath.IsAbs(root) || filepath.Clean(root) != root {
					return feedback(field, "invalid_root", "sandbox grant roots must be clean absolute paths")
				}
				if _, duplicate := seen[root]; duplicate {
					return feedback(field, "duplicate_root", "sandbox grant roots must be unique")
				}
				seen[root] = struct{}{}
			}
		}
		return nil
	}
}

func (e *Executor) applyPreCheck(ctx context.Context, handler core.ToolHandler, call core.ToolCall, emit func(core.RunEvent) error) (core.ToolCall, bool, string) {
	decision := e.preCheck(ctx, call, emit)
	e.emitHookOutcome(emit, core.HookBeforeTool, decision.Outcome)
	if decision.Block {
		return call, false, "blocked by hard pre-check: " + decision.Reason
	}
	if decision.EditedArguments == nil {
		return call, false, ""
	}
	if err := validateArgs(handler.Descriptor().Parameters, decision.EditedArguments); err != nil {
		return call, true, "invalid hook-edited arguments: " + err.Error()
	}
	call.Arguments = cloneArgumentMap(decision.EditedArguments)
	return call, true, ""
}

func (e *Executor) prepare(ctx context.Context, handler core.ToolHandler, call core.ToolCall, callID core.CallID, revision core.PreviewRevision, origin core.Origin, emit func(core.RichEvent) error) (core.PreparedAction, error) {
	prepared := core.PreparedAction{
		EffectiveArguments: cloneArgumentMap(call.Arguments),
		Operation:          operationForAction(classify(handler)),
		Preview: core.ActionPreview{
			Kind: core.ActionPreviewUnavailable, Operation: operationForAction(classify(handler)),
			UnavailableReason: "the tool handler does not support side-effect-free action preparation",
		},
	}
	rawPreview := prepared.Preview
	if handlerWithPreparation, ok := handler.(core.PreparedActionHandler); ok {
		candidate, err := handlerWithPreparation.Prepare(ctx, cloneArgumentMap(call.Arguments))
		if err != nil {
			return core.PreparedAction{}, err
		}
		// Preserve the handler's opaque identity token exactly, but never Clone the
		// untrusted preview before it passes through the shared projection budget.
		prepared = core.PreparedAction{
			EffectiveArguments: cloneArgumentMap(candidate.EffectiveArguments),
			Operation:          candidate.Operation,
			CommitToken:        candidate.CommitToken,
		}
		rawPreview = candidate.Preview
		if prepared.EffectiveArguments == nil {
			prepared.EffectiveArguments = cloneArgumentMap(call.Arguments)
		}
		if err := validateArgs(handler.Descriptor().Parameters, prepared.EffectiveArguments); err != nil {
			return core.PreparedAction{}, fmt.Errorf("prepared effective arguments are invalid: %w", err)
		}
		if prepared.Operation == core.ActionOperationUnknown {
			prepared.Operation = operationForAction(classify(handler))
		}
	} else if previewer, ok := handler.(core.ActionPreviewer); ok {
		preview, err := previewer.PreviewAction(ctx, cloneArgumentMap(call.Arguments))
		if err != nil {
			return core.PreparedAction{}, err
		}
		rawPreview = preview
	}
	if rawPreview.Operation == core.ActionOperationUnknown {
		rawPreview.Operation = prepared.Operation
	}
	if emit == nil {
		prepared.Preview = boundActionPreview(rawPreview)
		return prepared, nil
	}
	prepared.Preview = boundActionPreviewWithIdentity(rawPreview, previewOmissionIdentity{
		valid: true, callID: callID, revision: revision, correlation: string(callID),
	})
	preview := prepared.Preview.Clone()
	effectiveCall := cloneCall(call)
	effectiveCall.Arguments = cloneArgumentMap(prepared.EffectiveArguments)
	if err := emit(core.RichEvent{
		Origin: origin, Kind: core.ObservedKindToolPrepared,
		Payload: &core.ToolPreparedPayload{CallID: callID, Revision: revision, EffectiveCall: effectiveCall, Preview: preview},
	}); err != nil {
		return core.PreparedAction{}, err
	}
	if preview.Omission != nil {
		if err := emit(core.RichEvent{Origin: origin, Kind: core.ObservedKindOmissionReported, Payload: &core.OmissionReportedPayload{Omission: *preview.Omission}}); err != nil {
			return core.PreparedAction{}, err
		}
	}
	prepared.Preview = preview
	return prepared, nil
}

func (e *Executor) execute(ctx context.Context, handler core.ToolHandler, prepared core.PreparedAction, arguments map[string]interface{}, grants core.SandboxGrants, callID core.CallID, origin core.Origin, richEmit func(core.RichEvent) error, emit func(core.RunEvent) error) (string, error) {
	if handler.RunsCommands() {
		if sandbox, ok := e.stages.Sandbox.(sandboxWithGrants); ok {
			return sandbox.RunWithGrants(ctx, handler, arguments, grants)
		}
		return e.stages.Sandbox.Run(ctx, handler, arguments)
	}
	if preparedHandler, ok := handler.(core.PreparedActionHandler); ok {
		return preparedHandler.ExecutePrepared(ctx, prepared)
	}
	if richEmit != nil {
		if eventAware, ok := handler.(richEventAwareHandler); ok {
			return eventAware.ExecuteWithRichEvents(ctx, arguments, callID, origin, richEmit)
		}
	}
	if eventAware, ok := handler.(eventAwareHandler); ok {
		return eventAware.ExecuteWithEvents(ctx, arguments, emit)
	}
	return handler.Execute(ctx, arguments)
}

func (e *Executor) finalResult(callID, output string, isError bool, observedCallID core.CallID, emit func(core.RichEvent) error) core.ToolResult {
	return e.finalToolResult(core.ToolResult{ToolCallID: callID, Result: output, IsError: isError}, observedCallID, emit)
}

func (e *Executor) finalToolResult(result core.ToolResult, callID core.CallID, emit func(core.RichEvent) error) core.ToolResult {
	bounded, omission := truncateDetailed(result.Result, e.budget)
	result.Result = bounded
	if omission != nil && emit != nil {
		omission.CallID = callID
		omission.CorrelationID = string(callID)
		_ = emit(core.RichEvent{Kind: core.ObservedKindOmissionReported, Payload: &core.OmissionReportedPayload{Omission: *omission}})
	}
	return result
}

func (e *Executor) eventBridge(ctx context.Context, callID core.CallID, origin core.Origin, action core.ActionKind, richEmit func(core.RichEvent) error, legacyEmit func(core.RunEvent) error) func(core.RunEvent) error {
	return func(event core.RunEvent) error {
		if richEmit == nil {
			if legacyEmit == nil {
				return nil
			}
			return legacyEmit(event)
		}
		legacy := event
		switch event.Type {
		case core.PermissionRequestedEvent:
			if event.Permission == nil {
				return nil
			}
			request := core.NewLegacyObservedPermissionRequestWithContext(ctx, core.RequestID(core.NewOpaqueID("permission")), callID, *event.Permission)
			request.Action = action
			return richEmit(core.RichEvent{Origin: origin, Kind: core.ObservedKindPermissionRequested, Payload: &core.PermissionRequestedPayload{Request: request}, Legacy: &legacy})
		case core.HookOutcomeEvent:
			if event.HookOutcome == nil {
				return nil
			}
			outcome := *event.HookOutcome
			return richEmit(core.RichEvent{Origin: origin, Kind: core.ObservedKindHookOutcome, Payload: &core.HookOutcomePayload{CallID: callID, Outcome: outcome}, Legacy: &legacy})
		case core.ErrorEvent:
			return richEmit(core.RichEvent{Origin: origin, Kind: core.ObservedKindError, Payload: &core.ErrorPayload{Error: core.ObservedError{Code: "executor_error", Message: "the tool executor encountered an error", Recoverable: true}, CallID: callID}, Legacy: &legacy})
		case core.StatusChange:
			return richEmit(core.RichEvent{Origin: origin, Kind: core.ObservedKindStatusChanged, Payload: &core.StatusChangedPayload{Status: core.ActivityCallingTool, Detail: event.Status}, Legacy: &legacy})
		default:
			return nil
		}
	}
}

func (e *Executor) emitHookOutcome(emit func(core.RunEvent) error, moment core.HookMoment, outcome *core.HookOutcome) {
	if emit == nil || outcome == nil {
		return
	}
	value := *outcome
	if value.Moment == "" {
		value.Moment = moment
	}
	_ = emit(core.RunEvent{Type: core.HookOutcomeEvent, HookOutcome: &value})
}

type preToolCheckWithEmit interface {
	PreCheckWithEmit(context.Context, core.ToolCall, func(core.RunEvent) error) core.StageDecision
}

type postToolCheckWithEmit interface {
	PostCheckWithEmit(context.Context, core.ToolCall, core.ToolResult, func(core.RunEvent) error) core.StageDecision
}

type eventAwareHandler interface {
	ExecuteWithEvents(context.Context, map[string]interface{}, func(core.RunEvent) error) (string, error)
}

type richEventAwareHandler interface {
	ExecuteWithRichEvents(context.Context, map[string]interface{}, core.CallID, core.Origin, func(core.RichEvent) error) (string, error)
}

func (e *Executor) preCheck(ctx context.Context, call core.ToolCall, emit func(core.RunEvent) error) core.StageDecision {
	if stage, ok := e.stages.Pre.(preToolCheckWithEmit); ok {
		return stage.PreCheckWithEmit(ctx, call, emit)
	}
	return e.stages.Pre.PreCheck(ctx, call)
}

func (e *Executor) postCheck(ctx context.Context, call core.ToolCall, result core.ToolResult, emit func(core.RunEvent) error) core.StageDecision {
	if stage, ok := e.stages.Post.(postToolCheckWithEmit); ok {
		return stage.PostCheckWithEmit(ctx, call, result, emit)
	}
	return e.stages.Post.PostCheck(ctx, call, result)
}

func classify(handler core.ToolHandler) core.ActionKind {
	if classifier, ok := handler.(core.ActionClassifier); ok {
		return classifier.ActionKind()
	}
	if handler.RunsCommands() {
		return core.ActionCommand
	}
	return core.ActionUnknown
}

func operationForAction(action core.ActionKind) core.ActionOperation {
	switch action {
	case core.ActionEdit:
		return core.ActionOperationModify
	case core.ActionCommand:
		return core.ActionOperationCommand
	default:
		return core.ActionOperationCustom
	}
}

func cloneCall(call core.ToolCall) core.ToolCall {
	out := call
	out.Arguments = cloneArgumentMap(call.Arguments)
	return out
}

func cloneArgumentMap(arguments map[string]interface{}) map[string]interface{} {
	if arguments == nil {
		return nil
	}
	out := make(map[string]interface{}, len(arguments))
	for key, value := range arguments {
		out[key] = value
	}
	return out
}

type sandboxWithGrants interface {
	RunWithGrants(context.Context, core.ToolHandler, map[string]interface{}, core.SandboxGrants) (string, error)
}

var (
	_ core.Dispatcher     = (*Executor)(nil)
	_ core.RichDispatcher = (*Executor)(nil)
)
