package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contextmanager "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/core"
)

// RunRich drives the canonical rich lifecycle for one turn. It emits no run
// boundary; sessionrun adds run_started/run_finished once around lifecycle hooks
// so every public projection shares one authoritative terminal computation.
func RunRich(ctx context.Context, d Deps, origin core.Origin, emit func(core.RichEvent) error) core.RunFinished {
	state := richRunState{
		ctx:     ctx,
		emit:    emit,
		origin:  origin,
		callIDs: make(map[string]core.CallID),
	}
	var warnedOverBudget bool
	for roundIndex := 0; ; roundIndex++ {
		if roundIndex >= d.MaxRounds {
			_ = state.status(core.ActivityIdle, core.StatusIdle, "")
			return core.RunFinished{Reason: core.StopReachedStepLimit}
		}

		state.round = uint64(roundIndex + 1)
		snap := withTransientContext(d.Context.Snapshot(), d.TransientContext)
		estimate := contextmanager.EstimateRequestTokens(snap, d.Tools, d.StreamOptions)
		modelWindow := core.ModelContextWindow(d.StreamOptions.Model)
			estimatedUsage := contextmanager.UsageSnapshot(state.round, core.ContextUsageEstimated, time.Now(), estimate, modelWindow)
		if err := state.fact(core.ObservedKindContextUsageUpdated, &core.ContextUsageUpdatedPayload{Usage: estimatedUsage}, nil); err != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}
		if d.ContextBudgetTokens > 0 && !warnedOverBudget {
			if estimate := d.Context.EstimateTokens(); estimate > d.ContextBudgetTokens {
				warnedOverBudget = true
				message := fmt.Sprintf("conversation exceeds context budget (~%d tokens > %d); proceeding", estimate, d.ContextBudgetTokens)
				legacy := core.RunEvent{Type: core.OverBudgetWarningEvent, Warning: message}
				if err := state.fact(core.ObservedKindWarning, &core.WarningPayload{Code: "legacy_context_budget", Message: message}, &legacy); err != nil {
					return core.RunFinished{Reason: core.StopCancelled}
				}
			}
		}

		if err := state.status(core.ActivityThinking, core.StatusThinking, ""); err != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}
		if err := state.fact(core.ObservedKindAssistantStarted, &core.AssistantStartedPayload{Round: state.round}, nil); err != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}

		result := state.consult(d, snap)
		if ctx.Err() != nil || result.sendErr != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}
		if result.providerErr != nil {
			_ = state.observedError("provider_error", result.providerErr, false, "")
			_ = state.status(core.ActivityIdle, core.StatusIdle, "")
			return core.RunFinished{Reason: core.StopFailed, Err: result.providerErr}
		}
		if result.protocolErr != nil {
			_ = state.observedError("provider_protocol", result.protocolErr, false, "")
			_ = state.status(core.ActivityIdle, core.StatusIdle, "")
			return core.RunFinished{Reason: core.StopFailed, Err: result.protocolErr}
		}
		if !result.replyEnded {
			_ = state.observedError("provider_protocol", errProviderStreamIncomplete, false, "")
			_ = state.status(core.ActivityIdle, core.StatusIdle, "")
			return core.RunFinished{Reason: core.StopFailed, Err: errProviderStreamIncomplete}
		}

		d.Context.AppendAssistant(result.text, result.calls)
		if len(result.calls) == 0 {
			if err := state.status(core.ActivityIdle, core.StatusIdle, ""); err != nil {
				return core.RunFinished{Reason: core.StopCancelled}
			}
			return core.RunFinished{Reason: core.StopCompleted}
		}

		if err := state.status(core.ActivityCallingTool, core.StatusCallingTool, ""); err != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}
		results, terminal, done := state.dispatchAll(d, result.calls)
		if done {
			return terminal
		}
		d.Context.AppendToolResults(results)
	}
}

type richRunState struct {
	ctx      context.Context
	emit     func(core.RichEvent) error
	origin   core.Origin
	round    uint64
	callIDs  map[string]core.CallID
	activeID core.CallID
}

func (s *richRunState) fact(kind core.ObservedEventKind, payload core.ObservedEventPayload, legacy *core.RunEvent) error {
	if s.emit == nil {
		return nil
	}
	return s.emit(core.RichEvent{Origin: s.origin, Kind: kind, Payload: payload, Legacy: legacy})
}

func (s *richRunState) status(status core.ActivityStatus, legacyStatus, detail string) error {
	legacy := core.RunEvent{Type: core.StatusChange, Status: legacyStatus}
	return s.fact(core.ObservedKindStatusChanged, &core.StatusChangedPayload{Status: status, Detail: detail}, &legacy)
}

type consultResult struct {
	text        string
	calls       []core.ToolCall
	replyEnded  bool
	protocolErr error
	providerErr error
	sendErr     error
}

func (s *richRunState) consult(d Deps, snapshot core.Conversation) consultResult {
	if rich, ok := d.Provider.(core.RichProvider); d.UseRichProvider && ok {
		return s.consultRich(rich.StreamRichReply(s.ctx, snapshot, d.Tools, d.StreamOptions))
	}
	return s.consultLegacy(d.Provider.StreamReply(s.ctx, snapshot, d.Tools, d.StreamOptions))
}

func (s *richRunState) consultLegacy(events <-chan core.RunEvent) consultResult {
	var result consultResult
	var text strings.Builder
	for {
		var event core.RunEvent
		select {
		case <-s.ctx.Done():
			drainProviderStream(events)
			result.text = text.String()
			return result
		case next, ok := <-events:
			if !ok {
				result.text = text.String()
				return result
			}
			if s.ctx.Err() != nil {
				drainProviderStream(events)
				result.text = text.String()
				return result
			}
			event = next
		}

		if result.sendErr != nil {
			continue
		}
		if result.replyEnded {
			if result.protocolErr == nil {
				result.protocolErr = errProviderStreamAfterReplyEnd
			}
			if event.Type == core.ErrorEvent && event.Error != nil {
				result.providerErr = event.Error
			}
			continue
		}
		switch event.Type {
		case core.TextDelta:
			text.WriteString(event.TextDelta)
			legacy := event
			result.sendErr = s.fact(core.ObservedKindAssistantTextDelta, &core.AssistantTextDeltaPayload{Round: s.round, Delta: event.TextDelta}, &legacy)
		case core.ToolCallEvent:
			if event.ToolCall != nil {
				result.calls = append(result.calls, cloneToolCall(*event.ToolCall))
			}
		case core.ErrorEvent:
			result.providerErr = event.Error
		case core.ReplyEndedEvent:
			result.replyEnded = true
			if !validReplyEnded(event.ReplyEnded) {
				result.protocolErr = errProviderStreamInvalidEnd
				continue
			}
			reason := legacyProviderTermination(event.ReplyEnded.Reason)
			if event.ReplyEnded.Reason == core.CutOff {
				result.sendErr = s.providerOmission(core.OmissionProviderLength, core.ContinuationNewUserTurn)
			}
			if result.sendErr == nil {
				result.sendErr = s.fact(core.ObservedKindAssistantFinished, &core.AssistantFinishedPayload{Round: s.round, Reason: reason}, nil)
			}
		}
	}
}

func (s *richRunState) consultRich(events <-chan core.RichProviderEvent) consultResult {
	var result consultResult
	var text strings.Builder
	for {
		var event core.RichProviderEvent
		select {
		case <-s.ctx.Done():
			drainRichProviderStream(events)
			result.text = text.String()
			return result
		case next, ok := <-events:
			if !ok {
				result.text = text.String()
				return result
			}
			if s.ctx.Err() != nil {
				drainRichProviderStream(events)
				result.text = text.String()
				return result
			}
			event = next
		}

		if result.sendErr != nil {
			continue
		}
		if result.replyEnded {
			if result.protocolErr == nil {
				result.protocolErr = errProviderStreamAfterReplyEnd
			}
			if event.Type == core.RichProviderError && event.Error != nil {
				result.providerErr = event.Error
			}
			continue
		}

		switch event.Type {
		case core.RichProviderTextDelta:
			text.WriteString(event.TextDelta)
			legacy := core.RunEvent{Type: core.TextDelta, TextDelta: event.TextDelta}
			result.sendErr = s.fact(core.ObservedKindAssistantTextDelta, &core.AssistantTextDeltaPayload{Round: s.round, Delta: event.TextDelta}, &legacy)
		case core.RichProviderReasoningSummaryDelta:
			if event.ReasoningSummaryDelta != "" {
				result.sendErr = s.fact(core.ObservedKindAssistantReasoningSummaryDelta, &core.AssistantReasoningSummaryDeltaPayload{Round: s.round, Delta: event.ReasoningSummaryDelta}, nil)
			}
		case core.RichProviderToolCall:
			if event.ToolCall != nil {
				result.calls = append(result.calls, cloneToolCall(*event.ToolCall))
			}
		case core.RichProviderUsage:
			// Context-source projection is performed by the context manager slice.
			// The provider protocol still validates counts here so malformed usage
			// never becomes a trustworthy observation.
			if event.Usage != nil {
				usage := *event.Usage
				if usage.Round == 0 {
					usage.Round = s.round
				}
				if !validProviderUsage(usage, s.round) {
					_ = s.fact(core.ObservedKindWarning, &core.WarningPayload{Code: "invalid_provider_usage", Message: "provider usage was ignored because it was malformed"}, nil)
				} else {
					contextUsage := providerContextUsage(usage)
					result.sendErr = s.fact(core.ObservedKindContextUsageUpdated, &core.ContextUsageUpdatedPayload{Usage: contextUsage}, nil)
				}
			}
		case core.RichProviderWarning:
			code := event.WarningCode
			if code == "" {
				code = "provider_warning"
			}
			result.sendErr = s.fact(core.ObservedKindWarning, &core.WarningPayload{Code: code, Message: event.Warning}, nil)
		case core.RichProviderError:
			result.providerErr = event.Error
		case core.RichProviderReplyEnded:
			result.replyEnded = true
			if event.ReplyEnded == nil {
				result.protocolErr = errProviderStreamInvalidEnd
				continue
			}
			reason := event.ReplyEnded.Reason
			switch reason {
			case core.ProviderTerminationStop, core.ProviderTerminationToolCalls:
			case core.ProviderTerminationLength:
				result.sendErr = s.providerOmission(core.OmissionProviderLength, core.ContinuationNewUserTurn)
			case core.ProviderTerminationContentFilter:
				result.sendErr = s.providerOmission(core.OmissionContentFilter, core.ContinuationUnavailable)
			case core.ProviderTerminationFailure:
				result.providerErr = errors.New("provider reported a failed reply")
			case core.ProviderTerminationProviderSpecific, core.ProviderTerminationUnknown:
				result.protocolErr = fmt.Errorf("provider reply ended incompletely (%s)", safeProviderReason(event.ReplyEnded.ProviderReasonCode))
			default:
				result.protocolErr = errProviderStreamInvalidEnd
			}
			if result.sendErr == nil {
				result.sendErr = s.fact(core.ObservedKindAssistantFinished, &core.AssistantFinishedPayload{
					Round: s.round, Reason: reason, ProviderReasonCode: safeProviderReason(event.ReplyEnded.ProviderReasonCode),
				}, nil)
			}
		default:
			result.protocolErr = fmt.Errorf("provider stream emitted unknown rich event type %d", event.Type)
		}
	}
}

func (s *richRunState) providerOmission(kind core.OmissionKind, continuation core.ContinuationMode) error {
	omission := core.Omission{
		Kind: kind, Scope: core.OmissionScopeAssistantReply,
		CorrelationID:  fmt.Sprintf("round-%d", s.round),
		Recoverability: core.RecoverabilityUnrecoverable,
		Continuation:   continuation,
	}
	return s.fact(core.ObservedKindOmissionReported, &core.OmissionReportedPayload{Omission: omission}, nil)
}

func (s *richRunState) dispatchAll(d Deps, calls []core.ToolCall) ([]core.ToolResult, core.RunFinished, bool) {
	results := make([]core.ToolResult, 0, len(calls))
	for index := range calls {
		call := cloneToolCall(calls[index])
		callID := core.CallID(core.NewOpaqueID("call"))
		s.callIDs[call.ID] = callID
		s.activeID = callID
		legacyStarted := core.RunEvent{Type: core.ToolStartedEvent, ToolCall: &call}
		if err := s.fact(core.ObservedKindToolProposed, &core.ToolProposedPayload{Round: s.round, CallID: callID, Call: call}, &legacyStarted); err != nil {
			return nil, core.RunFinished{Reason: core.StopCancelled}, true
		}

		started := time.Now()
		result := core.ToolResult{}
		revision := core.PreviewRevision(1)
		var outcome core.ToolOutcome
		var dispatchErr error
		if rich, ok := d.Dispatcher.(core.RichDispatcher); ok {
			richResult, err := rich.DispatchRich(s.ctx, call, callID, s.origin, s.emit)
			result, revision, outcome, dispatchErr = richResult.Result, richResult.Revision, richResult.Outcome, err
		} else {
			result, dispatchErr = d.Dispatcher.Dispatch(s.ctx, call, s.bridgeLegacy)
			outcome = legacyToolOutcome(result, s.ctx.Err())
		}
		duration := time.Since(started)

		if s.ctx.Err() != nil {
			return nil, core.RunFinished{Reason: core.StopCancelled}, true
		}
		if dispatchErr != nil {
			_ = s.observedError("dispatcher_error", dispatchErr, false, callID)
			_ = s.status(core.ActivityIdle, core.StatusIdle, "")
			return nil, core.RunFinished{Reason: core.StopFailed, Err: dispatchErr}, true
		}
		if revision == 0 {
			revision = 1
		}
		if outcome == "" || outcome == core.ToolOutcomeUnknown {
			outcome = legacyToolOutcome(result, nil)
		}
		legacyFinished := core.RunEvent{Type: core.ToolFinishedEvent, ToolResult: &result}
		if err := s.fact(core.ObservedKindToolFinished, &core.ToolFinishedPayload{
			CallID: callID, Revision: revision, Outcome: outcome, Result: result, Duration: duration,
		}, &legacyFinished); err != nil {
			return nil, core.RunFinished{Reason: core.StopCancelled}, true
		}
		results = append(results, result)
	}
	s.activeID = ""
	return results, core.RunFinished{}, false
}

// bridgeLegacy adapts facts emitted by caller-owned dispatchers and existing
// hook/permission stages. It preserves their exact reply path and legacy value.
func (s *richRunState) bridgeLegacy(event core.RunEvent) error {
	legacy := event
	switch event.Type {
	case core.PermissionRequestedEvent:
		if event.Permission == nil {
			return nil
		}
		callID := s.callID(event.Permission.ToolCall.ID)
		request := core.NewLegacyObservedPermissionRequestWithContext(s.ctx, core.RequestID(core.NewOpaqueID("permission")), callID, *event.Permission)
		request.Action = actionKindForTool(event.Permission.ToolCall.ToolName)
		return s.fact(core.ObservedKindPermissionRequested, &core.PermissionRequestedPayload{Request: request}, &legacy)
	case core.HookOutcomeEvent:
		if event.HookOutcome == nil {
			return nil
		}
		outcome := *event.HookOutcome
		return s.fact(core.ObservedKindHookOutcome, &core.HookOutcomePayload{CallID: s.activeID, Outcome: outcome}, &legacy)
	case core.OverBudgetWarningEvent:
		return s.fact(core.ObservedKindWarning, &core.WarningPayload{Code: "legacy_context_budget", Message: event.Warning, CallID: s.activeID}, &legacy)
	case core.ErrorEvent:
		return s.fact(core.ObservedKindError, &core.ErrorPayload{Error: safeObservedError("runtime_error", event.Error, true), CallID: s.activeID}, &legacy)
	case core.StatusChange:
		status := core.ActivityUnknown
		detail := event.Status
		switch event.Status {
		case core.StatusThinking:
			status, detail = core.ActivityThinking, ""
		case core.StatusCallingTool, core.StatusSubagentStarted, core.StatusSubagentFinished:
			status = core.ActivityCallingTool
		case core.StatusIdle:
			status, detail = core.ActivityIdle, ""
		}
		return s.fact(core.ObservedKindStatusChanged, &core.StatusChangedPayload{Status: status, Detail: detail}, &legacy)
	default:
		// Tool and assistant lifecycle are owned by the loop, so a custom
		// dispatcher cannot duplicate them through its auxiliary emit callback.
		return nil
	}
}

func (s *richRunState) callID(providerID string) core.CallID {
	if id := s.callIDs[providerID]; id != "" {
		return id
	}
	if s.activeID != "" {
		return s.activeID
	}
	id := core.CallID(core.NewOpaqueID("call"))
	s.callIDs[providerID] = id
	return id
}

func (s *richRunState) observedError(code string, err error, recoverable bool, callID core.CallID) error {
	return s.fact(core.ObservedKindError, &core.ErrorPayload{Error: safeObservedError(code, err, recoverable), CallID: callID}, nil)
}

func safeObservedError(code string, err error, recoverable bool) core.ObservedError {
	message := "the run encountered an error"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		message = "the run was cancelled"
	}
	return core.ObservedError{Code: code, Message: message, Recoverable: recoverable}
}

func cloneToolCall(call core.ToolCall) core.ToolCall {
	out := call
	if call.Arguments != nil {
		out.Arguments = cloneMap(call.Arguments)
	}
	return out
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case map[string]interface{}:
			out[key] = cloneMap(typed)
		case []interface{}:
			items := make([]interface{}, len(typed))
			for index, item := range typed {
				if nested, ok := item.(map[string]interface{}); ok {
					items[index] = cloneMap(nested)
				} else {
					items[index] = item
				}
			}
			out[key] = items
		default:
			out[key] = value
		}
	}
	return out
}

func legacyProviderTermination(reason core.ReplyEndReason) core.ProviderTerminationReason {
	switch reason {
	case core.Finished:
		return core.ProviderTerminationStop
	case core.StoppedToCallTools:
		return core.ProviderTerminationToolCalls
	case core.CutOff:
		return core.ProviderTerminationLength
	default:
		return core.ProviderTerminationUnknown
	}
}

func legacyToolOutcome(result core.ToolResult, ctxErr error) core.ToolOutcome {
	if ctxErr != nil {
		return core.ToolOutcomeCancelled
	}
	if !result.IsError {
		return core.ToolOutcomeSucceeded
	}
	lower := strings.ToLower(result.Result)
	switch {
	case strings.Contains(lower, "permission denied"):
		return core.ToolOutcomeDenied
	case strings.Contains(lower, "blocked by hard"):
		return core.ToolOutcomeHookBlocked
	default:
		return core.ToolOutcomeFailed
	}
}

func actionKindForTool(name string) core.ActionKind {
	switch name {
	case "read_file", "search_content", "find_files":
		return core.ActionRead
	case "write_file", "edit_file":
		return core.ActionEdit
	case "run_command", "shell":
		return core.ActionCommand
	default:
		return core.ActionUnknown
	}
}

func validProviderUsage(usage core.ProviderUsage, round uint64) bool {
	return usage.Round == round && usage.PromptTokens.Known
}

func providerContextUsage(usage core.ProviderUsage) core.ContextUsage {
	snapshot := core.ContextUsage{
		Round: usage.Round, Source: core.ContextUsageProvider, MeasuredAt: time.Now(), UsedTokens: usage.PromptTokens.Value,
	}
	window := uint64(0)
	if usage.ContextWindowTokens.Known {
		window = usage.ContextWindowTokens.Value
	}
	if window > 0 {
		snapshot.WindowTokens = core.OptionalUint64{Known: true, Value: window}
		snapshot.RemainingTokens.Known = true
		if snapshot.UsedTokens >= window {
			snapshot.OverBudget = snapshot.UsedTokens > window
		} else {
			snapshot.RemainingTokens.Value = window - snapshot.UsedTokens
		}
	}
	return snapshot
}

func safeProviderReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 64 {
		reason = reason[:64]
	}
	for _, r := range reason {
		if r < 0x20 || r == 0x7f {
			return "unknown"
		}
	}
	if reason == "" {
		return "unknown"
	}
	return reason
}

func drainRichProviderStream(events <-chan core.RichProviderEvent) {
	go func() {
		for range events {
		}
	}()
}
