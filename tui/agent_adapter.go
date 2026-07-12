package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blkcor/coragent/pkg/agent"
)

const eventObservedProtocolError UIEventKind = "observed_protocol_error"

var errNilAgentSession = errors.New("tui: agent session is nil")

// AgentSessionAdapter is the production SessionPort. It translates only the
// public pkg/agent contract and never reaches through to harness internals.
type AgentSessionAdapter struct {
	session *agent.Session
}

var _ SessionPort = (*AgentSessionAdapter)(nil)

// NewAgentSessionAdapter binds the Bubble Tea frontend to one public Session.
func NewAgentSessionAdapter(session *agent.Session) *AgentSessionAdapter {
	return &AgentSessionAdapter{session: session}
}

func (adapter *AgentSessionAdapter) Describe(ctx context.Context) (SessionInfo, error) {
	if adapter == nil || adapter.session == nil {
		return SessionInfo{}, errNilAgentSession
	}
	if err := contextErr(ctx); err != nil {
		return SessionInfo{}, err
	}
	description := adapter.session.Describe()
	mode, changeable := projectPermissionMode(description.Permission)
	return SessionInfo{
		Project:         description.WorkingDirectory,
		Model:           description.Model,
		Provider:        description.Provider,
		Mode:            mode,
		ModeChangeable:  changeable,
		PermissionOwner: string(description.Permission.Ownership),
		Sandbox:         projectSandbox(description.Sandbox.Posture),
		SandboxReason:   description.Sandbox.Reason,
		Context:         "ctx unknown",
		ContextWindow: OptionalCount{
			Known: description.ContextWindow.Known,
			Value: description.ContextWindow.Tokens,
		},
		ReasoningSummarySupport: projectSupport(description.ProviderFeatures.ReasoningSummary),
		UsageSupport:            projectSupport(description.ProviderFeatures.Usage),
		Capabilities:            projectCapabilities(description.Capabilities),
	}, nil
}

func projectSupport(value agent.CapabilitySupport) SupportState {
	switch value {
	case agent.CapabilitySupportSupported:
		return SupportSupported
	case agent.CapabilitySupportUnsupported:
		return SupportUnsupported
	default:
		return SupportUnknown
	}
}

func projectCapabilities(categories []agent.CapabilityCategory) []CapabilityCategory {
	projected := make([]CapabilityCategory, 0, len(categories))
	for _, category := range categories {
		entry := CapabilityCategory{
			Kind: string(category.Kind), Support: projectSupport(category.Support), Source: category.Source,
			Items: make([]CapabilityItem, 0, len(category.Items)),
		}
		for _, item := range category.Items {
			availability := AvailabilityUnknown
			switch item.Availability {
			case agent.CapabilityAvailabilityAvailable:
				availability = AvailabilityAvailable
			case agent.CapabilityAvailabilityUnavailable:
				availability = AvailabilityUnavailable
			}
			entry.Items = append(entry.Items, CapabilityItem{
				Name: item.Name, Source: item.Source, Availability: availability, Detail: item.Detail,
			})
		}
		projected = append(projected, entry)
	}
	return projected
}

func (adapter *AgentSessionAdapter) Run(ctx context.Context, input string) (<-chan UIEvent, error) {
	if adapter == nil || adapter.session == nil {
		return nil, errNilAgentSession
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	observed, err := adapter.session.RunObserved(ctx, input)
	if err != nil {
		return nil, err
	}
	if observed == nil {
		return nil, errors.New("tui: RunObserved returned a nil stream")
	}
	return bridgeObservedEvents(ctx, observed), nil
}

func (adapter *AgentSessionAdapter) SetMode(ctx context.Context, mode SessionMode) error {
	if adapter == nil || adapter.session == nil {
		return errNilAgentSession
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	publicMode, err := agentPermissionMode(mode)
	if err != nil {
		return err
	}
	return adapter.session.SetPermissionModeTyped(publicMode)
}

func (adapter *AgentSessionAdapter) Close(ctx context.Context) error {
	if adapter == nil || adapter.session == nil {
		return errNilAgentSession
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	return adapter.session.Close(ctx)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("tui: nil context")
	}
	return ctx.Err()
}

func projectPermissionMode(description agent.PermissionDescription) (SessionMode, bool) {
	switch description.Ownership {
	case agent.PermissionOwnershipEngine:
		switch description.Mode {
		case agent.PermissionModeDefault:
			return ModeDefault, true
		case agent.PermissionModeAutoAcceptEdits:
			return ModeAutoAcceptEdits, true
		case agent.PermissionModePlan:
			return ModePlan, true
		case agent.PermissionModeBypass:
			return ModeBypass, true
		default:
			return ModeUnsupported, false
		}
	case agent.PermissionOwnershipExternal:
		return ModeExternal, false
	default:
		return ModeUnsupported, false
	}
}

func projectSandbox(posture agent.SandboxPosture) string {
	switch posture {
	case agent.SandboxPostureOSEnforced:
		return "os"
	case agent.SandboxPosturePolicyFallback:
		return "fallback"
	case agent.SandboxPostureExternal:
		return "externally owned"
	default:
		return "unknown"
	}
}

func agentPermissionMode(mode SessionMode) (agent.PermissionMode, error) {
	switch mode {
	case ModeDefault:
		return agent.PermissionModeDefault, nil
	case ModeAutoAcceptEdits:
		return agent.PermissionModeAutoAcceptEdits, nil
	case ModePlan:
		return agent.PermissionModePlan, nil
	case ModeBypass:
		return agent.PermissionModeBypass, nil
	default:
		return "", fmt.Errorf("tui: mode %q is not selectable", mode)
	}
}

type observedSequence struct {
	runID    agent.RunID
	next     uint64
	terminal bool
}

func (sequence *observedSequence) accept(event agent.ObservedEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if sequence.terminal {
		return errors.New("observed event arrived after run terminal")
	}
	if sequence.runID == "" {
		if event.Kind != agent.ObservedKindRunStarted {
			return fmt.Errorf("observed stream starts with %q instead of run_started", event.Kind)
		}
		if event.Sequence != 1 {
			return fmt.Errorf("observed stream starts at sequence %d instead of 1", event.Sequence)
		}
		sequence.runID = event.RunID
		sequence.next = 2
	} else {
		if event.RunID != sequence.runID {
			return fmt.Errorf("observed run ID changed from %q to %q", sequence.runID, event.RunID)
		}
		if event.Sequence != sequence.next {
			return fmt.Errorf("observed sequence is %d, want %d", event.Sequence, sequence.next)
		}
		sequence.next++
	}
	if event.Kind == agent.ObservedKindRunFinished {
		sequence.terminal = true
	}
	return nil
}

func bridgeObservedEvents(ctx context.Context, source <-chan agent.ObservedEvent) <-chan UIEvent {
	// Preserve the structural start and terminal pair for an immediately
	// cancelled run even if the reducer has not begun draining yet.
	out := make(chan UIEvent, 2)
	go func() {
		defer close(out)
		var sequence observedSequence
		rejected := false
		for observed := range source {
			if rejected {
				// Continue draining to prevent producer backpressure. A separately
				// valid terminal remains authoritative to the reducer.
				if observed.Validate() == nil && observed.Kind == agent.ObservedKindRunFinished {
					projected, present, err := projectObservedEvent(observed)
					if err == nil && present {
						forwardUIEvent(ctx, out, projected)
					}
				}
				continue
			}
			if err := sequence.accept(observed); err != nil {
				forwardUIEvent(ctx, out, UIEvent{
					Kind:      eventObservedProtocolError,
					RunID:     string(observed.RunID),
					Timestamp: observed.Timestamp,
					Text:      err.Error(),
				})
				rejected = true
				continue
			}
			projected, present, err := projectObservedEvent(observed)
			if err != nil {
				forwardUIEvent(ctx, out, UIEvent{
					Kind:      eventObservedProtocolError,
					RunID:     string(observed.RunID),
					Timestamp: observed.Timestamp,
					Text:      err.Error(),
				})
				rejected = true
				continue
			}
			if present {
				forwardUIEvent(ctx, out, projected)
			}
		}
	}()
	return out
}

// forwardUIEvent preserves source order under bounded backpressure, including
// after cancellation. SessionPort streams, like their public source, are
// drain-to-completion contracts; removing buffered facts here would race the
// reducer and could turn a normal cancellation into a protocol failure.
func forwardUIEvent(_ context.Context, out chan UIEvent, event UIEvent) {
	out <- event
}

func projectObservedEvent(event agent.ObservedEvent) (UIEvent, bool, error) {
	base := UIEvent{RunID: string(event.RunID), Timestamp: event.Timestamp}
	switch event.Kind {
	case agent.ObservedKindRunStarted:
		base.Kind = EventRunStarted
		return base, true, nil
	case agent.ObservedKindStatusChanged:
		payload := event.Payload.(*agent.StatusChangedPayload)
		activity, err := projectActivity(payload.Status)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventStatusChanged
		base.Activity = activity
		return base, true, nil
	case agent.ObservedKindAssistantStarted:
		payload := event.Payload.(*agent.AssistantStartedPayload)
		assistantID, err := assistantID(event.RunID, payload.Round)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventAssistantStarted
		base.AssistantID = assistantID
		return base, true, nil
	case agent.ObservedKindAssistantTextDelta:
		payload := event.Payload.(*agent.AssistantTextDeltaPayload)
		assistantID, err := assistantID(event.RunID, payload.Round)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventAssistantTextDelta
		base.AssistantID = assistantID
		base.Text = payload.Delta
		return base, true, nil
	case agent.ObservedKindAssistantReasoningSummaryDelta:
		payload := event.Payload.(*agent.AssistantReasoningSummaryDeltaPayload)
		assistantID, err := assistantID(event.RunID, payload.Round)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventAssistantReasoningSummaryDelta
		base.AssistantID = assistantID
		base.Text = payload.Delta
		return base, true, nil
	case agent.ObservedKindAssistantFinished:
		payload := event.Payload.(*agent.AssistantFinishedPayload)
		assistantID, err := assistantID(event.RunID, payload.Round)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventAssistantFinished
		base.AssistantID = assistantID
		base.Termination = string(payload.Reason)
		return base, true, nil
	case agent.ObservedKindToolProposed:
		payload := event.Payload.(*agent.ToolProposedPayload)
		arguments, err := marshalArguments(payload.Call.Arguments)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventToolStarted
		base.CallID = string(payload.CallID)
		base.ToolName = payload.Call.ToolName
		base.Arguments = arguments
		return base, true, nil
	case agent.ObservedKindToolPrepared:
		payload := event.Payload.(*agent.ToolPreparedPayload)
		arguments, err := marshalArguments(payload.EffectiveCall.Arguments)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventToolPrepared
		base.CallID = string(payload.CallID)
		base.ToolName = payload.EffectiveCall.ToolName
		base.Arguments = arguments
		base.Revision = uint64(payload.Revision)
		preview := projectActionPreview(payload.Preview)
		base.Preview = &preview
		return base, true, nil
	case agent.ObservedKindPermissionRequested:
		payload := event.Payload.(*agent.PermissionRequestedPayload)
		prompt, err := projectPermissionPrompt(event.Origin, payload.Request)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventPermissionRequested
		base.CallID = prompt.CallID
		base.Permission = &prompt
		return base, true, nil
	case agent.ObservedKindToolExecuting:
		payload := event.Payload.(*agent.ToolExecutingPayload)
		base.Kind = EventToolExecuting
		base.CallID = string(payload.CallID)
		base.Revision = uint64(payload.Revision)
		return base, true, nil
	case agent.ObservedKindToolFinished:
		payload := event.Payload.(*agent.ToolFinishedPayload)
		outcome, err := projectToolOutcome(payload.Outcome)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventToolFinished
		base.CallID = string(payload.CallID)
		base.Result = payload.Result.Result
		base.Tool = outcome
		base.Revision = uint64(payload.Revision)
		base.Duration = payload.Duration
		return base, true, nil
	case agent.ObservedKindContextUsageUpdated:
		payload := event.Payload.(*agent.ContextUsageUpdatedPayload)
		base.Kind = EventContextUsage
		usage := projectContextUsage(payload.Usage)
		base.Usage = &usage
		return base, true, nil
	case agent.ObservedKindOmissionReported:
		payload := event.Payload.(*agent.OmissionReportedPayload)
		base.Kind = EventOmission
		base.CallID = string(payload.Omission.CallID)
		omission := projectOmission(payload.Omission)
		base.Omission = &omission
		return base, true, nil
	case agent.ObservedKindHookOutcome:
		payload := event.Payload.(*agent.HookOutcomePayload)
		base.Kind = EventHookOutcome
		base.CallID = string(payload.CallID)
		base.Hook = &HookOutcome{
			CallID: string(payload.CallID), Name: payload.Outcome.HookName,
			Moment: string(payload.Outcome.Moment), Action: string(payload.Outcome.Action), Reason: payload.Outcome.Reason,
		}
		return base, true, nil
	case agent.ObservedKindSubagentStarted:
		payload := event.Payload.(*agent.SubagentStartedPayload)
		base.Kind = EventSubagentStarted
		base.CallID = string(payload.Agent.DelegationCallID)
		subagent := projectSubagent(payload.Agent)
		base.Subagent = &subagent
		return base, true, nil
	case agent.ObservedKindSubagentFinished:
		payload := event.Payload.(*agent.SubagentFinishedPayload)
		base.Kind = EventSubagentFinished
		base.CallID = string(payload.Agent.DelegationCallID)
		subagent := projectSubagent(payload.Agent)
		subagent.Outcome = string(payload.Outcome)
		if payload.Error != nil && payload.Error.Message != "" {
			subagent.Error = payload.Error.Message
		}
		base.Subagent = &subagent
		return base, true, nil
	case agent.ObservedKindWarning:
		payload := event.Payload.(*agent.WarningPayload)
		base.Kind = EventWarning
		base.CallID = string(payload.CallID)
		base.Text = payload.Message
		return base, true, nil
	case agent.ObservedKindError:
		payload := event.Payload.(*agent.ErrorPayload)
		base.Kind = EventError
		base.CallID = string(payload.CallID)
		base.Text = payload.Error.Message
		base.Recoverable = payload.Error.Recoverable
		return base, true, nil
	case agent.ObservedKindRunFinished:
		payload := event.Payload.(*agent.RunFinishedPayload)
		outcome, err := projectRunOutcome(payload.Outcome)
		if err != nil {
			return UIEvent{}, false, err
		}
		base.Kind = EventRunFinished
		base.Terminal = outcome
		if payload.Error != nil {
			base.Err = payload.Error.Message
		}
		return base, true, nil
	default:
		return UIEvent{}, false, fmt.Errorf("tui: unsupported observed kind %q", event.Kind)
	}
}

func projectActivity(status agent.ActivityStatus) (RunActivity, error) {
	switch status {
	case agent.ActivityThinking:
		return ActivityThinking, nil
	case agent.ActivityPreparingTool, agent.ActivityCallingTool:
		return ActivityCallingTool, nil
	case agent.ActivityWaitingPermission:
		return ActivityPermission, nil
	case agent.ActivityCancelling:
		return ActivityCancelling, nil
	case agent.ActivityIdle:
		return ActivityIdle, nil
	default:
		return "", fmt.Errorf("tui: unsupported activity %q", status)
	}
}

func projectToolOutcome(outcome agent.ToolOutcome) (ToolOutcome, error) {
	switch outcome {
	case agent.ToolOutcomeSucceeded:
		return ToolSucceeded, nil
	case agent.ToolOutcomeFailed:
		return ToolFailed, nil
	case agent.ToolOutcomeDenied:
		return ToolDenied, nil
	case agent.ToolOutcomeCancelled:
		return ToolCancelled, nil
	case agent.ToolOutcomeHookBlocked:
		return ToolHookBlocked, nil
	default:
		return "", fmt.Errorf("tui: unsupported tool outcome %q", outcome)
	}
}

func projectRunOutcome(outcome agent.RunOutcome) (RunOutcome, error) {
	switch outcome {
	case agent.RunOutcomeCompleted:
		return RunCompleted, nil
	case agent.RunOutcomeReachedStepLimit:
		return RunReachedStepLimit, nil
	case agent.RunOutcomeCancelled:
		return RunCancelled, nil
	case agent.RunOutcomeFailed:
		return RunFailed, nil
	default:
		return "", fmt.Errorf("tui: unsupported run outcome %q", outcome)
	}
}

func assistantID(runID agent.RunID, round uint64) (string, error) {
	if round == 0 {
		return "", errors.New("tui: assistant event has no model round")
	}
	return fmt.Sprintf("%s/assistant/%d", runID, round), nil
}

func marshalArguments(arguments map[string]interface{}) (string, error) {
	if len(arguments) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("tui: encode effective tool arguments: %w", err)
	}
	return string(encoded), nil
}

func projectPermissionPrompt(origin agent.Origin, request agent.ObservedPermissionRequest) (PermissionPrompt, error) {
	arguments, err := marshalArguments(request.EffectiveCall.Arguments)
	if err != nil {
		return PermissionPrompt{}, err
	}
	action := strings.TrimSpace(strings.Join([]string{request.EffectiveCall.ToolName, arguments}, " "))
	if request.Preview.Summary != "" {
		action = request.Preview.Summary
	}
	boundRequest := request.Clone()
	prompt := PermissionPrompt{
		RequestID: string(request.RequestID),
		CallID:    string(request.CallID),
		Revision:  uint64(request.Revision),
		Tool:      request.EffectiveCall.ToolName,
		Action:    action,
		Reason:    request.Explanation,
		Origin:    formatOrigin(origin),
		Preview:   formatActionPreview(request.Preview),
		Protocol:  string(request.Protocol),
		Arguments: arguments,
		Capabilities: PermissionCapabilities{
			Allow: request.Capabilities.Allow, Deny: request.Capabilities.Deny,
			Remember: request.Capabilities.Remember, ReviseArguments: request.Capabilities.ReviseArguments,
			SchemaAwareEdit: request.Capabilities.SchemaAwareEdit, Preview: request.Capabilities.Preview,
			SandboxGrants: request.Capabilities.SandboxGrants,
		},
		GrantOptions: GrantOptions{
			Support: projectSupport(request.GrantOptions.Support), ReadRoots: request.GrantOptions.ReadRoots,
			WriteRoots: request.GrantOptions.WriteRoots, Network: request.GrantOptions.Network,
			SuggestedReads:  append([]string(nil), request.GrantOptions.SuggestedReads...),
			SuggestedWrites: append([]string(nil), request.GrantOptions.SuggestedWrites...),
		},
	}
	if request.RememberedScope != nil {
		prompt.RememberScope = strings.TrimSpace(formatActionKind(request.RememberedScope.Kind) + " " + request.RememberedScope.Match)
	}
	preview := projectActionPreview(request.Preview)
	prompt.StructuredPreview = &preview
	prompt.RichReply = func(ctx context.Context, response PermissionResponse) (PermissionReplyResult, error) {
		action, err := observedReplyAction(response.Decision)
		if err != nil {
			return PermissionReplyResult{}, err
		}
		result := boundRequest.Reply(ctx, agent.ObservedPermissionDecision{
			RequestID:        boundRequest.RequestID,
			CallID:           boundRequest.CallID,
			Revision:         boundRequest.Revision,
			Action:           action,
			Remember:         response.Remember || response.Decision == DecisionAllowRemember || response.Decision == DecisionDenyRemember,
			RevisedArguments: cloneArguments(response.RevisedArguments),
			SandboxGrants: agent.SandboxGrants{
				ExtraReadRoots:  append([]string(nil), response.Grants.ReadRoots...),
				ExtraWriteRoots: append([]string(nil), response.Grants.WriteRoots...),
				Network:         response.Grants.Network,
			},
		})
		return projectPermissionReply(result), nil
	}
	prompt.Reply = func(ctx context.Context, decision PermissionDecision) (PermissionReplyResult, error) {
		return prompt.RichReply(ctx, PermissionResponse{Decision: decision})
	}
	return prompt, nil
}

func formatActionKind(kind agent.ActionKind) string {
	switch kind {
	case agent.ActionRead:
		return "read"
	case agent.ActionEdit:
		return "edit"
	case agent.ActionCommand:
		return "command"
	default:
		return "unknown"
	}
}

func observedReplyAction(decision PermissionDecision) (agent.PermissionReplyAction, error) {
	switch decision {
	case DecisionAllowOnce, DecisionAllowRemember:
		return agent.PermissionReplyAllow, nil
	case DecisionDenyOnce, DecisionDenyRemember:
		return agent.PermissionReplyDeny, nil
	case DecisionReviseArguments:
		return agent.PermissionReplyReviseArguments, nil
	default:
		return "", fmt.Errorf("tui: unsupported permission decision %q", decision)
	}
}

func cloneArguments(arguments map[string]interface{}) map[string]interface{} {
	if arguments == nil {
		return nil
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return arguments
	}
	var cloned map[string]interface{}
	if json.Unmarshal(encoded, &cloned) != nil {
		return arguments
	}
	return cloned
}

func projectOptional(value agent.OptionalUint64) OptionalCount {
	return OptionalCount{Known: value.Known, Value: value.Value}
}

func projectContextUsage(value agent.ContextUsage) ContextUsage {
	return ContextUsage{
		Round: value.Round, Source: string(value.Source), MeasuredAt: value.MeasuredAt,
		Used: value.UsedTokens, Window: projectOptional(value.WindowTokens),
		Remaining: projectOptional(value.RemainingTokens), OverBudget: value.OverBudget,
	}
}

func projectOmission(value agent.Omission) Omission {
	return Omission{
		Kind: string(value.Kind), Scope: string(value.Scope), CorrelationID: value.CorrelationID,
		CallID: string(value.CallID), Revision: uint64(value.Revision),
		Recoverability: string(value.Recoverability), Continuation: string(value.Continuation),
		OriginalBytes: projectOptional(value.OriginalBytes), RetainedBytes: projectOptional(value.RetainedBytes),
		OriginalLines: projectOptional(value.OriginalLines), RetainedLines: projectOptional(value.RetainedLines),
	}
}

func projectActionPreview(value agent.ActionPreview) ActionPreview {
	projected := ActionPreview{
		Kind: string(value.Kind), Operation: string(value.Operation), Summary: value.Summary,
		Targets: append([]string(nil), value.Targets...), Text: value.Text,
		UnavailableReason: value.UnavailableReason,
	}
	if value.Omission != nil {
		omission := projectOmission(*value.Omission)
		projected.Omission = &omission
	}
	if value.Metadata != nil {
		projected.Metadata = make(map[string]string, len(value.Metadata))
		for key, item := range value.Metadata {
			projected.Metadata[key] = item
		}
	}
	if value.FileDiff != nil {
		diff := &FileDiff{
			Path: value.FileDiff.Path, BeforeBytes: projectOptional(value.FileDiff.BeforeBytes),
			CandidateBytes: projectOptional(value.FileDiff.CandidateBytes),
			AddedLines:     projectOptional(value.FileDiff.AddedLines), RemovedLines: projectOptional(value.FileDiff.RemovedLines),
			ChangedRegions: projectOptional(value.FileDiff.ChangedRegions), NonText: value.FileDiff.NonText,
			Hunks: make([]DiffHunk, 0, len(value.FileDiff.Hunks)),
		}
		for _, hunk := range value.FileDiff.Hunks {
			entry := DiffHunk{OldStart: hunk.OldStart, OldLines: hunk.OldLines, NewStart: hunk.NewStart, NewLines: hunk.NewLines}
			for _, line := range hunk.Lines {
				entry.Lines = append(entry.Lines, DiffLine{Kind: string(line.Kind), Text: line.Text})
			}
			diff.Hunks = append(diff.Hunks, entry)
		}
		projected.FileDiff = diff
	}
	return projected
}

func projectSubagent(value agent.SubagentProvenance) SubagentLifecycle {
	return SubagentLifecycle{
		AgentID: string(value.AgentID), ParentAgentID: string(value.ParentAgentID),
		DelegationCallID: string(value.DelegationCallID), Label: value.Label, Depth: value.Depth,
	}
}

func projectPermissionReply(result agent.PermissionReplyResult) PermissionReplyResult {
	projected := PermissionReplyResult{Feedback: formatPermissionFeedback(result.Feedback)}
	switch result.Status {
	case agent.PermissionReplyAccepted:
		projected.Status = ReplyAccepted
	case agent.PermissionReplyValidationRejected:
		projected.Status = ReplyValidationRejected
	case agent.PermissionReplyAlreadyResolved:
		projected.Status = ReplyAlreadyResolved
	default:
		projected.Status = ReplyValidationRejected
		if projected.Feedback == "" {
			projected.Feedback = "permission reply returned an unknown status"
		}
	}
	return projected
}

func formatPermissionFeedback(feedback []agent.PermissionReplyFeedback) string {
	parts := make([]string, 0, len(feedback))
	for _, item := range feedback {
		message := strings.TrimSpace(item.Message)
		if message == "" {
			message = strings.TrimSpace(item.Code)
		}
		if message != "" {
			if field := strings.TrimSpace(item.Field); field != "" {
				message = field + ": " + message
			}
			parts = append(parts, message)
		}
	}
	return strings.Join(parts, "; ")
}

func formatOrigin(origin agent.Origin) string {
	if origin.Depth == 0 {
		return "root agent"
	}
	return fmt.Sprintf("agent %s · depth %d", origin.AgentID, origin.Depth)
}

func formatActionPreview(preview agent.ActionPreview) string {
	prefix := ""
	if preview.Operation != agent.ActionOperationUnknown && preview.Operation != "" {
		prefix = string(preview.Operation) + " · "
	}
	switch preview.Kind {
	case agent.ActionPreviewUnavailable:
		if preview.UnavailableReason != "" {
			return prefix + "Preview unavailable: " + preview.UnavailableReason
		}
		return prefix + "Preview unavailable"
	case agent.ActionPreviewText:
		if preview.Text != "" {
			return prefix + preview.Text
		}
	case agent.ActionPreviewFileDiff:
		if preview.FileDiff != nil {
			var builder strings.Builder
			builder.WriteString(prefix + preview.FileDiff.Path)
			var counts []string
			if preview.FileDiff.AddedLines.Known {
				counts = append(counts, fmt.Sprintf("+%d", preview.FileDiff.AddedLines.Value))
			}
			if preview.FileDiff.RemovedLines.Known {
				counts = append(counts, fmt.Sprintf("-%d", preview.FileDiff.RemovedLines.Value))
			}
			if preview.FileDiff.ChangedRegions.Known {
				counts = append(counts, fmt.Sprintf("%d regions", preview.FileDiff.ChangedRegions.Value))
			}
			if len(counts) > 0 {
				builder.WriteString(" · " + strings.Join(counts, ", "))
			}
			for _, hunk := range preview.FileDiff.Hunks {
				fmt.Fprintf(&builder, "\n@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
				for _, line := range hunk.Lines {
					prefix := " "
					switch line.Kind {
					case agent.DiffLineAdded:
						prefix = "+"
					case agent.DiffLineRemoved:
						prefix = "-"
					}
					builder.WriteString("\n" + prefix + line.Text)
				}
			}
			if preview.Omission != nil {
				builder.WriteString("\n[preview incomplete; omitted content cannot be expanded]")
			}
			return builder.String()
		}
	}
	if preview.Summary != "" {
		return prefix + preview.Summary
	}
	if len(preview.Targets) > 0 {
		return prefix + strings.Join(preview.Targets, ", ")
	}
	return strings.TrimSuffix(prefix, " · ")
}
