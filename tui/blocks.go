package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type BlockKind uint8

const (
	BlockUser BlockKind = iota
	BlockAssistant
	BlockTool
	BlockSubagent
	BlockNotice
)

type ToolBlockState uint8

const (
	ToolPreparing ToolBlockState = iota
	ToolRunning
	ToolAwaitingPermission
	ToolDone
	ToolError
	ToolWasDenied
	ToolWasCancelled
	ToolWasHookBlocked
	ToolInconsistent
)

// TranscriptBlock is semantic content, never pre-rendered terminal output.
// Completed blocks can be cached later without changing ordering or identity.
type TranscriptBlock struct {
	ID        string
	RunID     string
	Kind      BlockKind
	Timestamp time.Time
	Text      string

	Streaming          bool
	Terminal           RunOutcome
	Reasoning          string
	ReasoningStreaming bool
	ReasoningExpanded  bool
	Termination        string

	CallID    string
	RequestID string
	ToolName  string
	Arguments string
	Result    string
	ToolState ToolBlockState
	Revision  uint64
	Preview   *ActionPreview
	Duration  time.Duration
	StartedAt time.Time
	Omissions []Omission
	Hooks     []HookOutcome
	Notices   []string

	AgentID         string
	ParentAgentID   string
	Depth           int
	SubagentLabel   string
	SubagentOutcome string
	SubagentError   string

	Expanded bool
	version  uint64

	sanitizer          Sanitizer
	reasoningSanitizer Sanitizer
}

func formatDuration(duration time.Duration) string {
	if duration < time.Millisecond {
		return "<1ms"
	}
	if duration < time.Second {
		return fmt.Sprintf("%dms", duration.Milliseconds())
	}
	return duration.Round(100 * time.Millisecond).String()
}

func renderActionPreview(theme Theme, preview ActionPreview, revision uint64, width int, expanded bool) []string {
	width = max(1, width)
	operation := firstNonEmpty(preview.Operation, "action")
	header := fmt.Sprintf("   %s preview", operation)
	if revision > 0 {
		header += fmt.Sprintf(" r%d", revision)
	}
	lines := []string{theme.InfoStyle.Render(ansi.Truncate(header, width, theme.Glyphs.Ellipsis))}
	switch preview.Kind {
	case "file_diff":
		if preview.FileDiff == nil {
			lines = append(lines, theme.WarningStyle.Render("   preview unavailable: missing diff payload"))
			break
		}
		diff := preview.FileDiff
		path := firstNonEmpty(diff.Path, strings.Join(preview.Targets, ", "))
		if path != "" {
			lines = append(lines, theme.StrongStyle.Render(ansi.Truncate("   "+SanitizeString(path), width, theme.Glyphs.Ellipsis)))
		}
		var counts []string
		if diff.AddedLines.Known {
			counts = append(counts, fmt.Sprintf("+%d", diff.AddedLines.Value))
		}
		if diff.RemovedLines.Known {
			counts = append(counts, fmt.Sprintf("-%d", diff.RemovedLines.Value))
		}
		if diff.ChangedRegions.Known {
			counts = append(counts, fmt.Sprintf("%d regions", diff.ChangedRegions.Value))
		}
		if len(counts) > 0 {
			lines = append(lines, theme.MutedStyle.Render("   "+strings.Join(counts, visualSeparator(theme))))
		}
		if diff.NonText {
			lines = append(lines, theme.WarningStyle.Render("   binary or undecodable content; metadata only"))
		}
		for _, hunk := range diff.Hunks {
			header := fmt.Sprintf("   @@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
			lines = append(lines, theme.InfoStyle.Render(ansi.Truncate(header, width, theme.Glyphs.Ellipsis)))
			for _, line := range hunk.Lines {
				marker := " "
				style := theme.MutedStyle
				switch line.Kind {
				case "added":
					marker = "+"
					style = theme.DiffAddStyle
				case "removed":
					marker = "-"
					style = theme.DiffRemoveStyle
				}
				value := "   " + marker + SanitizeString(line.Text)
				lines = append(lines, style.Render(ansi.Truncate(value, width, theme.Glyphs.Ellipsis)))
			}
		}
	case "text":
		for _, line := range wrapPlain(firstNonEmpty(preview.Text, preview.Summary), max(1, width-3)) {
			lines = append(lines, theme.MutedStyle.Render("   "+line))
		}
	case "metadata":
		keys := make([]string, 0, len(preview.Metadata))
		for key := range preview.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := SanitizeString(key) + ": " + SanitizeString(preview.Metadata[key])
			lines = append(lines, theme.MutedStyle.Render(ansi.Truncate("   "+value, width, theme.Glyphs.Ellipsis)))
		}
	case "unavailable":
		lines = append(lines, theme.WarningStyle.Render("   preview unavailable: "+SanitizeString(firstNonEmpty(preview.UnavailableReason, "not provided"))))
	default:
		value := firstNonEmpty(preview.Text, preview.Summary)
		if value == "" {
			value = "preview unavailable"
		}
		for _, line := range wrapPlain(value, max(1, width-3)) {
			lines = append(lines, theme.MutedStyle.Render("   "+line))
		}
	}
	if preview.Omission != nil {
		lines = append(lines, theme.WarningStyle.Render("   ! "+formatOmissionNotice(*preview.Omission)))
	}
	if !expanded && len(lines) > 14 {
		hidden := len(lines) - 12
		lines = append(lines[:12], theme.InfoStyle.Render(fmt.Sprintf("   [%d preview lines locally hidden; expandable in inspector]", hidden)))
	}
	return lines
}

func formatOmissionNotice(omission Omission) string {
	var label string
	switch omission.Kind {
	case "output_budget":
		label = "tool output tail omitted"
	case "preview_budget":
		label = "action preview incomplete"
	case "provider_length":
		label = "reply stopped at provider output limit"
	case "content_filter":
		label = "content unavailable: provider filter"
	case "redacted":
		label = "content redacted from the public payload"
	case "context_compaction":
		label = "conversation history compacted"
	default:
		label = "content omitted"
	}
	var counts []string
	if omission.OriginalBytes.Known && omission.RetainedBytes.Known && omission.OriginalBytes.Value >= omission.RetainedBytes.Value {
		counts = append(counts, fmt.Sprintf("%d bytes missing", omission.OriginalBytes.Value-omission.RetainedBytes.Value))
	} else if omission.OriginalLines.Known && omission.RetainedLines.Known && omission.OriginalLines.Value >= omission.RetainedLines.Value {
		counts = append(counts, fmt.Sprintf("%d lines missing", omission.OriginalLines.Value-omission.RetainedLines.Value))
	}
	if omission.Recoverability == "recoverable" {
		counts = append(counts, "recoverable")
	} else {
		counts = append(counts, "unavailable; cannot expand")
	}
	if omission.Continuation == "new_user_turn" {
		counts = append(counts, "Enter can prepare an editable continuation when idle")
	}
	if len(counts) > 0 {
		label += " (" + strings.Join(counts, "; ") + ")"
	}
	return label
}

func formatHookNotice(outcome HookOutcome) string {
	message := "hard hook " + firstNonEmpty(SanitizeString(outcome.Name), "hook") +
		" · " + firstNonEmpty(SanitizeString(outcome.Moment), "lifecycle") +
		" · " + firstNonEmpty(SanitizeString(outcome.Action), "reported")
	if outcome.Reason != "" {
		message += ": " + SanitizeString(outcome.Reason)
	}
	return message
}

// TranscriptStore retains one stable ordered slice and correlation indexes.
// Indexes update blocks in place; they never determine display order.
type TranscriptStore struct {
	Blocks []TranscriptBlock

	callIndex         map[string]int
	requestIndex      map[string]int
	agentIndex        map[string]int
	runIndex          map[string][]int
	assistantIndex    map[string]int
	currentRun        string
	nextID            uint64
	markdownCache     markdownRenderCache
	renderCache       map[transcriptRenderCacheKey][]string
	renderCacheHits   uint64
	renderCacheMisses uint64
}

type transcriptRenderCacheKey struct {
	BlockID string
	Version uint64
	Width   int
	Mode    VisualMode
}

func NewTranscriptStore() TranscriptStore {
	return TranscriptStore{
		callIndex:      make(map[string]int),
		requestIndex:   make(map[string]int),
		agentIndex:     make(map[string]int),
		runIndex:       make(map[string][]int),
		assistantIndex: make(map[string]int),
		renderCache:    make(map[transcriptRenderCacheKey][]string),
	}
}

func (store *TranscriptStore) ensureIndexes() {
	if store.callIndex == nil {
		store.callIndex = make(map[string]int)
	}
	if store.assistantIndex == nil {
		store.assistantIndex = make(map[string]int)
	}
	if store.requestIndex == nil {
		store.requestIndex = make(map[string]int)
	}
	if store.agentIndex == nil {
		store.agentIndex = make(map[string]int)
	}
	if store.runIndex == nil {
		store.runIndex = make(map[string][]int)
	}
	if store.renderCache == nil {
		store.renderCache = make(map[transcriptRenderCacheKey][]string)
	}
}

func (store *TranscriptStore) StartRun(runID string) error {
	store.ensureIndexes()
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run has no ID")
	}
	if _, exists := store.runIndex[runID]; exists {
		return fmt.Errorf("run %q started more than once", SanitizeString(runID))
	}
	store.runIndex[runID] = nil
	store.currentRun = runID
	// The user block is appended before SessionPort.Run can emit run_started.
	// Bind the nearest unassociated user request once the authoritative run ID
	// arrives so run-indexed settling and inspection cover the whole turn.
	for index := len(store.Blocks) - 1; index >= 0; index-- {
		block := &store.Blocks[index]
		if block.RunID != "" {
			break
		}
		if block.Kind == BlockUser {
			block.RunID = runID
			store.runIndex[runID] = append(store.runIndex[runID], index)
			touchBlock(block)
			break
		}
	}
	return nil
}

func (store *TranscriptStore) appendBlock(block TranscriptBlock) int {
	store.ensureIndexes()
	if block.RunID == "" {
		block.RunID = store.currentRun
	}
	block.version++
	index := len(store.Blocks)
	store.Blocks = append(store.Blocks, block)
	if block.RunID != "" {
		store.runIndex[block.RunID] = append(store.runIndex[block.RunID], index)
	}
	return index
}

func touchBlock(block *TranscriptBlock) {
	if block != nil {
		block.version++
	}
}

func (store *TranscriptStore) generatedID(prefix string) string {
	store.nextID++
	return fmt.Sprintf("%s-%d", prefix, store.nextID)
}

func (store *TranscriptStore) AddUser(text string, at time.Time) {
	store.ensureIndexes()
	store.appendBlock(TranscriptBlock{
		ID:        store.generatedID("user"),
		Kind:      BlockUser,
		Timestamp: at,
		Text:      SanitizeString(text),
	})
}

func (store *TranscriptStore) StartAssistant(id string, at time.Time) error {
	store.ensureIndexes()
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("assistant event has no assistant ID")
	}
	if _, exists := store.assistantIndex[id]; exists {
		return fmt.Errorf("assistant %q started more than once", SanitizeString(id))
	}
	store.assistantIndex[id] = store.appendBlock(TranscriptBlock{
		ID:        id,
		Kind:      BlockAssistant,
		Timestamp: at,
		Streaming: true,
	})
	return nil
}

func (store *TranscriptStore) AppendAssistant(id, chunk string, at time.Time) error {
	store.ensureIndexes()
	index, exists := store.assistantIndex[id]
	if !exists {
		if err := store.StartAssistant(id, at); err != nil {
			return err
		}
		index = store.assistantIndex[id]
	}
	block := &store.Blocks[index]
	if !block.Streaming {
		return fmt.Errorf("assistant %q received text after completion", SanitizeString(id))
	}
	block.Text += block.sanitizer.Write(chunk)
	touchBlock(block)
	return nil
}

func (store *TranscriptStore) AppendReasoning(id, chunk string, at time.Time) error {
	store.ensureIndexes()
	index, exists := store.assistantIndex[id]
	if !exists {
		if err := store.StartAssistant(id, at); err != nil {
			return err
		}
		index = store.assistantIndex[id]
	}
	block := &store.Blocks[index]
	if !block.Streaming {
		return fmt.Errorf("assistant %q received reasoning after completion", SanitizeString(id))
	}
	block.Reasoning += block.reasoningSanitizer.Write(chunk)
	block.ReasoningStreaming = true
	touchBlock(block)
	return nil
}

func (store *TranscriptStore) FinishAssistant(id string) error {
	return store.FinishAssistantWithReason(id, "")
}

func (store *TranscriptStore) FinishAssistantWithReason(id, termination string) error {
	store.ensureIndexes()
	index, exists := store.assistantIndex[id]
	if !exists {
		return fmt.Errorf("assistant %q finished before it started", SanitizeString(id))
	}
	block := &store.Blocks[index]
	if !block.Streaming {
		return fmt.Errorf("assistant %q finished more than once", SanitizeString(id))
	}
	block.Text += block.sanitizer.Flush()
	block.Reasoning += block.reasoningSanitizer.Flush()
	block.Streaming = false
	block.ReasoningStreaming = false
	block.Termination = SanitizeString(termination)
	touchBlock(block)
	return nil
}

func (store *TranscriptStore) StartTool(callID, name, arguments string, at time.Time) error {
	store.ensureIndexes()
	if strings.TrimSpace(callID) == "" {
		return fmt.Errorf("tool event has no call ID")
	}
	if _, exists := store.callIndex[callID]; exists {
		return fmt.Errorf("tool call %q started more than once", SanitizeString(callID))
	}
	store.callIndex[callID] = store.appendBlock(TranscriptBlock{
		ID:        callID,
		Kind:      BlockTool,
		Timestamp: at,
		CallID:    callID,
		ToolName:  SanitizeString(name),
		Arguments: SanitizeString(arguments),
		ToolState: ToolPreparing,
		StartedAt: at,
	})
	return nil
}

func (store *TranscriptStore) PrepareTool(callID, name, arguments string) error {
	return store.PrepareToolPreview(callID, name, arguments, 0, nil)
}

func (store *TranscriptStore) PrepareToolPreview(callID, name, arguments string, revision uint64, preview *ActionPreview) error {
	store.ensureIndexes()
	index, exists := store.callIndex[callID]
	if !exists {
		return fmt.Errorf("tool call %q was prepared before it was proposed", SanitizeString(callID))
	}
	block := &store.Blocks[index]
	if name != "" {
		block.ToolName = SanitizeString(name)
	}
	block.Arguments = SanitizeString(arguments)
	block.ToolState = ToolPreparing
	block.Revision = revision
	block.Preview = cloneActionPreview(preview)
	touchBlock(block)
	return nil
}

func (store *TranscriptStore) ExecuteTool(callID string) error {
	store.ensureIndexes()
	index, exists := store.callIndex[callID]
	if !exists {
		return fmt.Errorf("tool call %q executed before it was proposed", SanitizeString(callID))
	}
	store.Blocks[index].ToolState = ToolRunning
	touchBlock(&store.Blocks[index])
	return nil
}

func (store *TranscriptStore) ReprepareTool(callID string, resolvedRevision uint64) {
	store.ensureIndexes()
	index, ok := store.callIndex[callID]
	if !ok {
		return
	}
	block := &store.Blocks[index]
	if block.Revision > resolvedRevision {
		return
	}
	block.ToolState = ToolPreparing
	block.Preview = nil
	block.RequestID = ""
	block.Notices = append(block.Notices, "argument revision accepted; re-preparing the effective action")
	touchBlock(block)
}

func (store *TranscriptStore) AwaitPermission(prompt PermissionPrompt, at time.Time) error {
	store.ensureIndexes()
	if strings.TrimSpace(prompt.CallID) == "" {
		return fmt.Errorf("permission request has no call ID")
	}
	index, exists := store.callIndex[prompt.CallID]
	if !exists {
		name := prompt.Tool
		if strings.TrimSpace(name) == "" {
			name = "tool"
		}
		store.callIndex[prompt.CallID] = store.appendBlock(TranscriptBlock{
			ID:        prompt.CallID,
			Kind:      BlockTool,
			Timestamp: at,
			CallID:    prompt.CallID,
			ToolName:  SanitizeString(name),
			Arguments: SanitizeString(firstNonEmpty(prompt.Arguments, prompt.Action)),
			ToolState: ToolAwaitingPermission,
			RequestID: prompt.RequestID,
			Revision:  prompt.Revision,
			Preview:   cloneActionPreview(prompt.StructuredPreview),
			StartedAt: at,
		})
		store.requestIndex[prompt.RequestID] = store.callIndex[prompt.CallID]
		return nil
	}
	block := &store.Blocks[index]
	if prompt.Tool != "" {
		block.ToolName = SanitizeString(prompt.Tool)
	}
	if prompt.Arguments != "" || prompt.Action != "" {
		block.Arguments = SanitizeString(firstNonEmpty(prompt.Arguments, prompt.Action))
	}
	block.ToolState = ToolAwaitingPermission
	block.RequestID = prompt.RequestID
	block.Revision = prompt.Revision
	block.Preview = cloneActionPreview(prompt.StructuredPreview)
	store.requestIndex[prompt.RequestID] = index
	touchBlock(block)
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (store *TranscriptStore) FinishTool(callID, result string, outcome ToolOutcome, at time.Time) error {
	return store.FinishToolDetails(callID, result, outcome, at, 0, 0)
}

func (store *TranscriptStore) FinishToolDetails(callID, result string, outcome ToolOutcome, at time.Time, revision uint64, duration time.Duration) error {
	store.ensureIndexes()
	if strings.TrimSpace(callID) == "" {
		return fmt.Errorf("tool result has no call ID")
	}
	index, exists := store.callIndex[callID]
	if !exists {
		store.callIndex[callID] = store.appendBlock(TranscriptBlock{
			ID:        callID,
			Kind:      BlockTool,
			Timestamp: at,
			CallID:    callID,
			ToolName:  "tool",
		})
		index = len(store.Blocks) - 1
	}
	block := &store.Blocks[index]
	block.Result = SanitizeString(result)
	switch outcome {
	case ToolSucceeded:
		block.ToolState = ToolDone
	case ToolCancelled:
		block.ToolState = ToolWasCancelled
	case ToolDenied:
		block.ToolState = ToolWasDenied
	case ToolHookBlocked:
		block.ToolState = ToolWasHookBlocked
	case ToolFailed:
		block.ToolState = ToolError
	default:
		return fmt.Errorf("tool call %q has unknown outcome %q", SanitizeString(callID), SanitizeString(string(outcome)))
	}
	block.Revision = revision
	block.Duration = duration
	if block.Duration <= 0 && !block.StartedAt.IsZero() {
		block.Duration = at.Sub(block.StartedAt)
	}
	if block.RequestID != "" {
		delete(store.requestIndex, block.RequestID)
	}
	touchBlock(block)
	return nil
}

func (store *TranscriptStore) AddNotice(text string, at time.Time) {
	store.ensureIndexes()
	store.appendBlock(TranscriptBlock{
		ID:        store.generatedID("notice"),
		Kind:      BlockNotice,
		Timestamp: at,
		Text:      SanitizeString(text),
	})
}

func cloneActionPreview(preview *ActionPreview) *ActionPreview {
	if preview == nil {
		return nil
	}
	out := *preview
	out.Targets = append([]string(nil), preview.Targets...)
	if preview.Omission != nil {
		omission := *preview.Omission
		out.Omission = &omission
	}
	if preview.Metadata != nil {
		out.Metadata = make(map[string]string, len(preview.Metadata))
		for key, value := range preview.Metadata {
			out.Metadata[key] = value
		}
	}
	if preview.FileDiff != nil {
		diff := *preview.FileDiff
		diff.Hunks = make([]DiffHunk, len(preview.FileDiff.Hunks))
		for index, hunk := range preview.FileDiff.Hunks {
			diff.Hunks[index] = hunk
			diff.Hunks[index].Lines = append([]DiffLine(nil), hunk.Lines...)
		}
		out.FileDiff = &diff
	}
	return &out
}

func (store *TranscriptStore) ApplyOmission(omission Omission, at time.Time) {
	store.ensureIndexes()
	if omission.CallID != "" {
		if index, ok := store.callIndex[omission.CallID]; ok {
			block := &store.Blocks[index]
			block.Omissions = append(block.Omissions, omission)
			touchBlock(block)
			return
		}
	}
	if omission.CorrelationID != "" {
		if index, ok := store.assistantIndex[omission.CorrelationID]; ok {
			block := &store.Blocks[index]
			block.Omissions = append(block.Omissions, omission)
			touchBlock(block)
			return
		}
	}
	store.AddNotice(formatOmissionNotice(omission), at)
}

func (store *TranscriptStore) ApplyHook(outcome HookOutcome, at time.Time) {
	store.ensureIndexes()
	if outcome.CallID != "" {
		if index, ok := store.callIndex[outcome.CallID]; ok {
			block := &store.Blocks[index]
			block.Hooks = append(block.Hooks, outcome)
			if outcome.Action == "blocked" {
				block.ToolState = ToolWasHookBlocked
				block.Result = firstNonEmpty(outcome.Reason, "blocked by hard hook")
			}
			touchBlock(block)
			return
		}
	}
	store.AddNotice(formatHookNotice(outcome), at)
}

func (store *TranscriptStore) AddCorrelatedNotice(callID, message string, isError bool, at time.Time) {
	store.ensureIndexes()
	if callID != "" {
		if index, ok := store.callIndex[callID]; ok {
			block := &store.Blocks[index]
			block.Notices = append(block.Notices, SanitizeString(message))
			if isError && block.ToolState < ToolDone {
				block.ToolState = ToolError
			}
			touchBlock(block)
			return
		}
	}
	store.AddNotice(message, at)
}

func (store *TranscriptStore) StartSubagent(agent SubagentLifecycle, at time.Time) error {
	store.ensureIndexes()
	if strings.TrimSpace(agent.AgentID) == "" {
		return fmt.Errorf("subagent event has no agent ID")
	}
	if _, exists := store.agentIndex[agent.AgentID]; exists {
		return fmt.Errorf("subagent %q started more than once", SanitizeString(agent.AgentID))
	}
	index := store.appendBlock(TranscriptBlock{
		ID: agent.AgentID, Kind: BlockSubagent, Timestamp: at,
		CallID: agent.DelegationCallID, AgentID: agent.AgentID,
		ParentAgentID: agent.ParentAgentID, Depth: agent.Depth,
		SubagentLabel: SanitizeString(agent.Label), StartedAt: at,
	})
	store.agentIndex[agent.AgentID] = index
	return nil
}

func (store *TranscriptStore) FinishSubagent(agent SubagentLifecycle, at time.Time) error {
	store.ensureIndexes()
	index, exists := store.agentIndex[agent.AgentID]
	if !exists {
		return fmt.Errorf("subagent %q finished before it started", SanitizeString(agent.AgentID))
	}
	block := &store.Blocks[index]
	if block.SubagentOutcome != "" {
		return fmt.Errorf("subagent %q finished more than once", SanitizeString(agent.AgentID))
	}
	block.SubagentOutcome = SanitizeString(agent.Outcome)
	block.SubagentError = SanitizeString(agent.Error)
	block.Duration = at.Sub(block.StartedAt)
	touchBlock(block)
	return nil
}

func (store *TranscriptStore) ToggleReasoning(id string) bool {
	store.ensureIndexes()
	index, ok := store.assistantIndex[id]
	if !ok || store.Blocks[index].Reasoning == "" {
		return false
	}
	store.Blocks[index].ReasoningExpanded = !store.Blocks[index].ReasoningExpanded
	touchBlock(&store.Blocks[index])
	return true
}

func (store *TranscriptStore) ToggleExpanded(id string) bool {
	for index := range store.Blocks {
		if store.Blocks[index].ID != id {
			continue
		}
		store.Blocks[index].Expanded = !store.Blocks[index].Expanded
		touchBlock(&store.Blocks[index])
		return true
	}
	return false
}

// SettleActive fans the authoritative terminal outcome out to every still-live
// visual. It reports whether completed contradicted a dangling item.
func (store *TranscriptStore) SettleActive(outcome RunOutcome) (inconsistent bool) {
	for index := range store.Blocks {
		block := &store.Blocks[index]
		if block.Kind == BlockAssistant && block.Streaming {
			block.Text += block.sanitizer.Flush()
			block.Streaming = false
			block.Terminal = outcome
			block.Reasoning += block.reasoningSanitizer.Flush()
			block.ReasoningStreaming = false
			touchBlock(block)
			if outcome == RunCompleted {
				inconsistent = true
			}
		}
		if block.Kind != BlockTool {
			if block.Kind == BlockSubagent && block.SubagentOutcome == "" {
				switch outcome {
				case RunCancelled:
					block.SubagentOutcome = "cancelled"
				case RunReachedStepLimit:
					block.SubagentOutcome = "reached-step-limit"
				case RunCompleted:
					block.SubagentOutcome = "inconsistent"
					inconsistent = true
				default:
					block.SubagentOutcome = "failed"
				}
				touchBlock(block)
			}
			continue
		}
		switch block.ToolState {
		case ToolPreparing, ToolRunning, ToolAwaitingPermission:
			switch outcome {
			case RunCancelled:
				block.ToolState = ToolWasCancelled
			case RunCompleted:
				block.ToolState = ToolInconsistent
				inconsistent = true
			default:
				block.ToolState = ToolError
			}
			touchBlock(block)
		}
	}
	store.currentRun = ""
	return inconsistent
}

// RenderedTranscriptLine retains the semantic block identity behind one
// terminal row. Scrollback uses it to keep the same block anchored when rows
// are inserted, updated, or rewrapped above the viewport.
type RenderedTranscriptLine struct {
	BlockID string
	Line    int
	Text    string
}

func (store *TranscriptStore) RenderRows(theme Theme, width, frame int) []RenderedTranscriptLine {
	if width < 1 {
		return nil
	}
	store.ensureIndexes()
	lines := make([]RenderedTranscriptLine, 0, len(store.Blocks)*3)
	for index := range store.Blocks {
		if len(lines) > 0 && (index == 0 || store.Blocks[index-1].Kind != BlockTool || store.Blocks[index].Kind != BlockTool) {
			lines = append(lines, RenderedTranscriptLine{BlockID: store.Blocks[index].ID, Line: -1})
		}
		for line, text := range store.renderBlock(theme, store.Blocks[index], width, frame) {
			lines = append(lines, RenderedTranscriptLine{
				BlockID: store.Blocks[index].ID,
				Line:    line,
				Text:    text,
			})
		}
	}
	return lines
}

func (store *TranscriptStore) renderBlock(theme Theme, block TranscriptBlock, width, frame int) []string {
	completed := block.Kind == BlockUser || block.Kind == BlockNotice ||
		(block.Kind == BlockAssistant && !block.Streaming) ||
		(block.Kind == BlockTool && block.ToolState >= ToolDone) ||
		(block.Kind == BlockSubagent && block.SubagentOutcome != "")
	key := transcriptRenderCacheKey{BlockID: block.ID, Version: block.version, Width: width, Mode: theme.Mode}
	if completed && block.ID != "" {
		if cached, ok := store.renderCache[key]; ok {
			store.renderCacheHits++
			return append([]string(nil), cached...)
		}
	}
	store.renderCacheMisses++
	rendered := renderTranscriptBlockCached(theme, block, width, frame, &store.markdownCache)
	if completed && block.ID != "" {
		if len(store.renderCache) >= 512 {
			store.renderCache = make(map[transcriptRenderCacheKey][]string)
		}
		store.renderCache[key] = append([]string(nil), rendered...)
	}
	return rendered
}

func (store *TranscriptStore) RenderLines(theme Theme, width, frame int) []string {
	rows := store.RenderRows(theme, width, frame)
	lines := make([]string, len(rows))
	for index := range rows {
		lines[index] = rows[index].Text
	}
	return lines
}

func renderTranscriptBlock(theme Theme, block TranscriptBlock, width, frame int) []string {
	return renderTranscriptBlockCached(theme, block, width, frame, nil)
}

func renderTranscriptBlockCached(theme Theme, block TranscriptBlock, width, frame int, cache *markdownRenderCache) []string {
	switch block.Kind {
	case BlockUser:
		promptWidth := min(width, MaximumProseWidth)
		bodyWidth := max(1, promptWidth-2)
		wrapped := wrapProse(block.Text, bodyWidth)
		lines := make([]string, 0, len(wrapped))
		for index, line := range wrapped {
			prefix := "  "
			if index == 0 {
				prefix = theme.Glyphs.Focus + " "
			}
			lines = append(lines, theme.SurfaceStyle.Render(padCells(prefix+line, promptWidth)))
		}
		return lines
	case BlockAssistant:
		proseWidth := min(width, MaximumProseWidth)
		bodyWidth := max(1, proseWidth-3)
		var wrapped []string
		cacheKey := markdownRenderCacheKey{
			BlockID:   block.ID,
			Width:     bodyWidth,
			Mode:      theme.Mode,
			Streaming: block.Streaming,
		}
		if block.ID != "" && cache != nil {
			wrapped, _ = cache.get(cacheKey, block.Text)
		}
		if wrapped == nil {
			if block.Streaming {
				wrapped = renderStreamingMarkdownLines(theme, block.Text, bodyWidth, cache, cacheKey)
			} else {
				wrapped = renderMarkdownLines(theme, block.Text, bodyWidth)
			}
			if !block.Streaming && block.ID != "" && cache != nil {
				cache.put(cacheKey, block.Text, wrapped)
			}
		}
		glyph := theme.Glyphs.Success
		glyphStyle := theme.AccentStyle
		if block.Streaming {
			activeFrame := frame
			if theme.Mode.ReducedMotion {
				activeFrame = 0
			}
			glyph = theme.Glyphs.ActiveFrames[activeFrame%len(theme.Glyphs.ActiveFrames)]
		} else {
			switch block.Terminal {
			case RunCancelled:
				glyph = theme.Glyphs.Cancelled
				glyphStyle = theme.MutedStyle
			case RunFailed, RunReachedStepLimit:
				glyph = theme.Glyphs.Warning
				glyphStyle = theme.WarningStyle
			}
		}
		lines := make([]string, 0, len(wrapped)+4)
		if block.Reasoning != "" {
			disclosure := "▸ reasoning summary"
			if block.ReasoningStreaming || block.ReasoningExpanded {
				disclosure = "▾ reasoning summary"
			}
			if theme.Mode.ASCII {
				disclosure = "> reasoning summary"
				if block.ReasoningStreaming || block.ReasoningExpanded {
					disclosure = "v reasoning summary"
				}
			}
			if block.ReasoningStreaming {
				disclosure += visualSeparator(theme) + "streaming"
			}
			lines = append(lines, theme.InfoStyle.Render(disclosure))
			if block.ReasoningStreaming || block.ReasoningExpanded {
				for _, summaryLine := range wrapProse(block.Reasoning, max(1, bodyWidth-2)) {
					lines = append(lines, theme.MutedStyle.Render("  "+summaryLine))
				}
			}
		}
		for index, line := range wrapped {
			if index == 0 {
				lines = append(lines, glyphStyle.Render(glyph)+"  "+line)
				continue
			}
			lines = append(lines, "   "+line)
		}
		for _, omission := range block.Omissions {
			for _, notice := range wrapPlain("! "+formatOmissionNotice(omission), bodyWidth) {
				lines = append(lines, theme.WarningStyle.Render("   "+notice))
			}
		}
		if block.Termination == "length" && len(block.Omissions) == 0 {
			lines = append(lines, theme.WarningStyle.Render("   ! reply stopped at provider output limit"))
		} else if block.Termination == "content_filter" && len(block.Omissions) == 0 {
			lines = append(lines, theme.WarningStyle.Render("   ! reply content unavailable: provider filter"))
		}
		switch block.Terminal {
		case RunCancelled:
			lines = append(lines, theme.MutedStyle.Render("   ! assistant output cancelled"))
		case RunFailed:
			lines = append(lines, theme.DangerStyle.Render("   ! assistant output ended with run failure"))
		case RunReachedStepLimit:
			lines = append(lines, theme.WarningStyle.Render("   ! assistant output incomplete at step limit"))
		}
		return lines
	case BlockTool:
		railState := RailProposed
		switch block.ToolState {
		case ToolRunning:
			railState = RailActive
		case ToolAwaitingPermission:
			railState = RailPermission
		case ToolDone:
			railState = RailSuccess
		case ToolError, ToolInconsistent:
			railState = RailError
		case ToolWasDenied:
			railState = RailWarning
		case ToolWasCancelled:
			railState = RailCancelled
		case ToolWasHookBlocked:
			railState = RailHookBlocked
		}
		visual := theme.RailVisual(railState, frame)
		if block.ToolState == ToolWasDenied {
			visual.Label = "permission denied"
		}
		name := block.ToolName
		if name == "" {
			name = "tool"
		}
		name = SanitizeString(name)
		arguments := strings.TrimSpace(SanitizeString(block.Arguments))
		status := visualSeparator(theme) + visual.Label
		contentWidth := max(1, width-3)
		nameWidth := CellWidth(name)
		statusWidth := CellWidth(status)
		if nameWidth+statusWidth > contentWidth {
			name = ansi.Truncate(name, max(1, contentWidth-statusWidth), theme.Glyphs.Ellipsis)
			arguments = ""
		}
		argumentText := ""
		if arguments != "" {
			argumentWidth := max(0, contentWidth-CellWidth(name)-statusWidth)
			if argumentWidth > 2 {
				argumentText = "(" + ansi.Truncate(arguments, argumentWidth-2, theme.Glyphs.Ellipsis) + ")"
			}
		}
		if block.Duration > 0 && block.ToolState >= ToolDone {
			status += visualSeparator(theme) + formatDuration(block.Duration)
		}
		line := visual.Style.Render(visual.Glyph) + "  " + theme.StrongStyle.Render(name) +
			theme.MutedStyle.Render(argumentText) + visual.Style.Render(status)
		lines := []string{line}
		if block.Preview != nil {
			lines = append(lines, renderActionPreview(theme, *block.Preview, block.Revision, max(1, width-4), block.Expanded)...)
		}
		for _, hook := range block.Hooks {
			lines = append(lines, theme.WarningStyle.Render("   ! "+formatHookNotice(hook)))
		}
		for _, notice := range block.Notices {
			lines = append(lines, theme.WarningStyle.Render("   ! "+SanitizeString(notice)))
		}
		if block.Result != "" && (block.ToolState == ToolError || block.ToolState == ToolWasDenied || block.ToolState == ToolWasHookBlocked || block.ToolState == ToolDone) {
			branch := "└"
			continuation := "  "
			if theme.Mode.ASCII {
				branch = "->"
				continuation = "   "
			}
			resultStyle := theme.MutedStyle
			if block.ToolState == ToolError || block.ToolState == ToolWasHookBlocked {
				resultStyle = theme.DangerStyle
			}
			resultLines := wrapPlain(block.Result, max(1, width-4))
			hidden := 0
			if !block.Expanded && len(resultLines) > 10 {
				hidden = len(resultLines) - 8
				resultLines = resultLines[:8]
			}
			for index, resultLine := range resultLines {
				prefix := "   "
				if index == 0 {
					prefix = "  " + branch + " "
				} else if theme.Mode.ASCII {
					prefix = continuation
				}
				lines = append(lines, theme.MutedStyle.Render(prefix)+resultStyle.Render(resultLine))
			}
			if hidden > 0 {
				lines = append(lines, theme.InfoStyle.Render(fmt.Sprintf("     [%d lines locally hidden; expandable in inspector]", hidden)))
			}
		}
		for _, omission := range block.Omissions {
			lines = append(lines, theme.WarningStyle.Render("   ! "+formatOmissionNotice(omission)))
		}
		return lines
	case BlockSubagent:
		indent := strings.Repeat("  ", max(1, block.Depth))
		label := firstNonEmpty(block.SubagentLabel, "agent")
		state := block.SubagentOutcome
		glyph := theme.Glyphs.ActiveFrames[frame%len(theme.Glyphs.ActiveFrames)]
		style := theme.AccentStyle
		if state == "" {
			state = "running"
		} else {
			glyph = theme.Glyphs.Success
			style = theme.SuccessStyle
			if state != "completed" {
				glyph = theme.Glyphs.Warning
				style = theme.WarningStyle
			}
		}
		if theme.Mode.ReducedMotion && block.SubagentOutcome == "" {
			glyph = theme.Glyphs.Proposed
		}
		line := indent + style.Render(glyph) + " " + theme.StrongStyle.Render("subagent "+label) +
			style.Render(visualSeparator(theme)+state) + theme.MutedStyle.Render(fmt.Sprintf(" · depth %d", block.Depth))
		lines := []string{ansi.Truncate(line, width, "")}
		if block.ParentAgentID != "" {
			lines = append(lines, theme.MutedStyle.Render(ansi.Truncate(indent+"  parent "+block.ParentAgentID, width, "")))
		}
		if block.SubagentError != "" {
			lines = append(lines, theme.DangerStyle.Render(ansi.Truncate(indent+"  ! "+block.SubagentError, width, "")))
		}
		return lines
	case BlockNotice:
		wrapped := wrapPlain("! "+block.Text, width)
		for index := range wrapped {
			wrapped[index] = theme.WarningStyle.Render(wrapped[index])
		}
		return wrapped
	default:
		return []string{theme.DangerStyle.Render("! unsupported transcript block")}
	}
}

func wrapPlain(value string, width int) []string {
	if width < 1 {
		return nil
	}
	value = SanitizeString(value)
	if value == "" {
		return []string{""}
	}
	return strings.Split(ansi.Hardwrap(value, width, false), "\n")
}

func wrapProse(value string, width int) []string {
	if width < 1 {
		return nil
	}
	value = SanitizeString(value)
	if value == "" {
		return []string{""}
	}
	wordWrapped := strings.Split(ansi.Wordwrap(value, width, ""), "\n")
	lines := make([]string, 0, len(wordWrapped))
	for _, line := range wordWrapped {
		if ansi.StringWidth(line) <= width {
			lines = append(lines, line)
			continue
		}
		lines = append(lines, strings.Split(ansi.Hardwrap(line, width, false), "\n")...)
	}
	return lines
}

type permissionRenderOptions struct {
	Width       int
	MaxRows     int
	Prompt      PermissionPrompt
	Submitting  bool
	Feedback    string
	TooSmall    bool
	Scroll      int
	View        permissionView
	EditorLines []string
	Grants      SandboxGrants
}

func renderPermissionLines(theme Theme, options permissionRenderOptions) []string {
	width := options.Width
	if width < 1 {
		return nil
	}
	if options.TooSmall {
		return renderTinyPermissionLines(options)
	}
	if options.View != permissionDecision {
		return renderPermissionEditorLines(theme, options)
	}

	contentWidth := max(1, width-4)
	separator := visualSeparator(theme)
	title := "Permission required" + separator + SanitizeString(options.Prompt.Tool)
	if options.Prompt.Revision > 0 {
		title += fmt.Sprintf("%spreview r%d", separator, options.Prompt.Revision)
	}
	reviewValues := []string{"Action: " + SanitizeString(options.Prompt.Action)}
	if options.Prompt.Reason != "" {
		reviewValues = append(reviewValues, "Why: "+SanitizeString(options.Prompt.Reason))
	}
	if options.Prompt.Origin != "" {
		reviewValues = append(reviewValues, "Origin: "+SanitizeString(options.Prompt.Origin))
	}
	if options.Prompt.Protocol != "" {
		reviewValues = append(reviewValues, "Protocol: "+SanitizeString(options.Prompt.Protocol))
	}
	if options.Prompt.RememberScope != "" {
		reviewValues = append(reviewValues, "Remember scope: "+SanitizeString(options.Prompt.RememberScope))
	}
	if options.Prompt.Capabilities.SandboxGrants && options.Prompt.GrantOptions.Support == SupportSupported {
		grantDimensions := make([]string, 0, 3)
		if options.Prompt.GrantOptions.ReadRoots {
			grantDimensions = append(grantDimensions, "read roots")
		}
		if options.Prompt.GrantOptions.WriteRoots {
			grantDimensions = append(grantDimensions, "write roots")
		}
		if options.Prompt.GrantOptions.Network {
			grantDimensions = append(grantDimensions, "network")
		}
		reviewValues = append(reviewValues, "Available one-call grants: "+strings.Join(grantDimensions, ", "))
		if len(options.Prompt.GrantOptions.SuggestedReads) > 0 {
			reviewValues = append(reviewValues, "Suggested reads: "+strings.Join(options.Prompt.GrantOptions.SuggestedReads, ", "))
		}
		if len(options.Prompt.GrantOptions.SuggestedWrites) > 0 {
			reviewValues = append(reviewValues, "Suggested writes: "+strings.Join(options.Prompt.GrantOptions.SuggestedWrites, ", "))
		}
	}
	if len(options.Grants.ReadRoots) > 0 || len(options.Grants.WriteRoots) > 0 || options.Grants.Network {
		reviewValues = append(reviewValues, "One-call grants: "+formatSandboxGrants(options.Grants))
	}
	preview := SanitizeString(options.Prompt.Preview)
	if preview == "" {
		preview = "unavailable"
	}
	reviewValues = append(reviewValues, "Preview: "+preview)
	body := make([]string, 0, len(reviewValues)*2)
	for _, value := range reviewValues {
		body = append(body, wrapPlain(value, contentWidth)...)
	}

	feedback := ""
	if options.Feedback != "" {
		feedback = "! " + SanitizeString(options.Feedback)
	}

	maxRows := max(8, options.MaxRows)
	fixedRows := 6 // divider, title, question, baseline actions, and key hint
	extraActions := 0
	if options.Prompt.Capabilities.Remember {
		extraActions++
	}
	if options.Prompt.Capabilities.ReviseArguments || options.Prompt.Capabilities.SandboxGrants {
		extraActions++
	}
	fixedRows += extraActions
	if feedback != "" {
		fixedRows++
	}
	viewportRows := max(1, maxRows-fixedRows)
	clipped := len(body) > viewportRows
	if clipped {
		fixedRows++ // stable scroll-position row
		viewportRows = max(1, maxRows-fixedRows)
	}
	maxScroll := max(0, len(body)-viewportRows)
	start := min(max(0, options.Scroll), maxScroll)
	end := min(len(body), start+viewportRows)
	visible := body[start:end]

	rule := strings.Repeat(theme.Border.Top, width)
	lines := []string{theme.BorderStyle.Render(rule)}
	lines = append(lines, theme.AccentStyle.Render(theme.Glyphs.Permission+" "+ansi.Truncate(title, max(1, width-2), "")))
	for _, line := range visible {
		lines = append(lines, theme.TextStyle.Render("  "+ansi.Truncate(line, contentWidth, theme.Glyphs.Ellipsis)))
	}
	if clipped {
		scrollKeys := "↑/↓"
		if theme.Mode.ASCII {
			scrollKeys = "up/down"
		}
		position := fmt.Sprintf("review %d-%d of %d%s%s scroll", start+1, end, len(body), separator, scrollKeys)
		lines = append(lines, theme.MutedStyle.Render("  "+ansi.Truncate(position, contentWidth, "")))
	}
	if feedback != "" {
		lines = append(lines, theme.DangerStyle.Render("  "+ansi.Truncate(feedback, contentWidth, "")))
	}
	lines = append(lines, theme.StrongStyle.Render("Do you want to proceed?"))
	if options.Submitting {
		lines = append(lines, theme.AccentStyle.Render("  submitting decision..."))
	} else {
		allowLabel := theme.Glyphs.Focus + " [a] Allow once"
		if !permissionAllows(options.Prompt) {
			allowLabel = "  [a] Allow once (unavailable)"
		}
		lines = append(lines, theme.AccentStyle.Render(allowLabel))
	}
	lines = append(lines, theme.MutedStyle.Render("  [d] Deny"))
	if options.Prompt.Capabilities.Remember {
		remember := "  [A] Allow + remember  [D] Deny + remember"
		if options.Prompt.RememberScope == "" {
			remember = "  [A/D] Remember (unavailable: no safe scope)"
		}
		lines = append(lines, theme.MutedStyle.Render(ansi.Truncate(remember, contentWidth, theme.Glyphs.Ellipsis)))
	}
	var editors []string
	if options.Prompt.Capabilities.ReviseArguments && options.Prompt.Capabilities.SchemaAwareEdit {
		editors = append(editors, "[e] edit arguments")
	}
	if options.Prompt.Capabilities.SandboxGrants && options.Prompt.GrantOptions.Support == SupportSupported {
		editors = append(editors, "[s] one-call grants")
	}
	if len(editors) > 0 {
		lines = append(lines, theme.MutedStyle.Render("  "+strings.Join(editors, separator)))
	}
	keyHint := "Esc deny" + separator + "↑/↓ review" + separator + "Ctrl+C deny + cancel"
	if theme.Mode.ASCII {
		keyHint = "Esc deny | up/down review | Ctrl+C deny + cancel"
	}
	lines = append(lines, theme.MutedStyle.Render(keyHint))
	return lines
}

func renderPermissionEditorLines(theme Theme, options permissionRenderOptions) []string {
	width := max(1, options.Width)
	contentWidth := max(1, width-4)
	title := "Edit arguments"
	copy := "JSON object · Ctrl+S submit revision · Esc back"
	if options.View == permissionGrants {
		title = "Edit one-call sandbox grants"
		copy = "Additive for this call only · Ctrl+S save · Esc back"
	}
	rule := strings.Repeat(theme.Border.Top, width)
	lines := []string{
		theme.BorderStyle.Render(rule),
		theme.AccentStyle.Render(theme.Glyphs.Permission + " " + title),
		theme.MutedStyle.Render("  " + ansi.Truncate(copy, contentWidth, theme.Glyphs.Ellipsis)),
	}
	available := max(1, options.MaxRows-6)
	editorLines := options.EditorLines
	if len(editorLines) > available {
		editorLines = editorLines[len(editorLines)-available:]
	}
	for _, line := range editorLines {
		lines = append(lines, theme.TextStyle.Render("  "+ansi.Truncate(line, contentWidth, theme.Glyphs.Ellipsis)))
	}
	for len(editorLines) < available {
		lines = append(lines, "")
		editorLines = append(editorLines, "")
	}
	if options.Feedback != "" {
		lines = append(lines, theme.DangerStyle.Render("  ! "+ansi.Truncate(SanitizeString(options.Feedback), contentWidth, theme.Glyphs.Ellipsis)))
	} else if options.Submitting {
		lines = append(lines, theme.AccentStyle.Render("  submitting revision..."))
	}
	lines = append(lines, theme.MutedStyle.Render("Ctrl+S save · Esc back · Ctrl+C deny + cancel · Ctrl+Q quit"))
	if len(lines) > options.MaxRows && options.MaxRows > 0 {
		lines = append(lines[:options.MaxRows-1], lines[len(lines)-1])
	}
	return lines
}

func formatSandboxGrants(grants SandboxGrants) string {
	var parts []string
	if len(grants.ReadRoots) > 0 {
		parts = append(parts, fmt.Sprintf("read %d", len(grants.ReadRoots)))
	}
	if len(grants.WriteRoots) > 0 {
		parts = append(parts, fmt.Sprintf("write %d", len(grants.WriteRoots)))
	}
	if grants.Network {
		parts = append(parts, "network")
	}
	return strings.Join(parts, ", ")
}

func renderTinyPermissionLines(options permissionRenderOptions) []string {
	width := max(1, options.Width)
	height := max(1, options.MaxRows)
	resize := fmt.Sprintf("resize to %dx%d to review and allow", MinimumTerminalWidth, MinimumTerminalHeight)
	actions := []string{"[d/Esc] deny", "[Ctrl+C] deny + cancel", "[Ctrl+Q] quit"}

	var lines []string
	if height >= 5 {
		lines = []string{"PERMISSION PENDING"}
		if height >= 7 {
			lines = append(lines,
				SanitizeString(options.Prompt.Tool),
				SanitizeString(options.Prompt.Action),
			)
		}
		lines = append(lines, resize)
		lines = append(lines, actions...)
	} else {
		lines = []string{resize, "[d] deny | ^C cancel | ^Q quit"}
		if height >= 3 {
			lines = append([]string{"PERMISSION PENDING"}, lines...)
		}
	}

	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], width, "")
	}
	return lines
}
