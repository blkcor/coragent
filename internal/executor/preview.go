package executor

import (
	"container/heap"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/blkcor/coragent/internal/core"
)

const (
	actionPreviewByteLimit = 64 * 1024
	actionPreviewLineLimit = 800
	unknownDiffLineKind    = core.DiffLineKind("unknown")
)

type actionPreviewMeasurement struct {
	bytes      uint64
	lines      uint64
	normalized bool
}

func (measurement *actionPreviewMeasurement) addString(value string, minimumLines int) {
	bytes, lines, normalized := measurePreviewString(value, minimumLines)
	measurement.bytes = saturatingAdd(measurement.bytes, bytes)
	measurement.lines = saturatingAdd(measurement.lines, lines)
	measurement.normalized = measurement.normalized || normalized
}

func (measurement *actionPreviewMeasurement) addStructure(bytes, lines uint64) {
	measurement.bytes = saturatingAdd(measurement.bytes, bytes)
	measurement.lines = saturatingAdd(measurement.lines, lines)
}

type actionPreviewBudget struct {
	bytesLeft    int
	linesLeft    int
	originalByte uint64
	retainedByte uint64
	originalLine uint64
	retainedLine uint64
	truncated    bool
}

func newActionPreviewBudget(measurement actionPreviewMeasurement) *actionPreviewBudget {
	return &actionPreviewBudget{
		bytesLeft: actionPreviewByteLimit, linesLeft: actionPreviewLineLimit,
		originalByte: measurement.bytes, originalLine: measurement.lines,
		truncated: measurement.normalized,
	}
}

// retainString copies a string into independently bounded storage. A positive
// minimumLines charges structural list/map entries even when their text is
// empty, so empty values cannot create an unbounded projected collection.
func (budget *actionPreviewBudget) retainString(value string, minimumLines int) (string, bool) {
	if value == "" {
		if minimumLines > budget.linesLeft {
			budget.truncated = true
			return "", false
		}
		budget.linesLeft -= minimumLines
		budget.retainedLine += uint64(minimumLines)
		return "", true
	}
	if budget.bytesLeft == 0 || budget.linesLeft < minimumLines {
		budget.truncated = true
		return "", false
	}

	var retained strings.Builder
	retained.Grow(min(len(value), budget.bytesLeft))
	naturalLines := 0
	hasContent := false
	lastWasNewline := false
	normalized := false
	index := 0
	for index < len(value) {
		r, size := utf8.DecodeRuneInString(value[index:])
		encodedBytes := size
		invalid := r == utf8.RuneError && size == 1
		if invalid {
			encodedBytes = utf8.RuneLen(utf8.RuneError)
		}
		if retained.Len()+encodedBytes > budget.bytesLeft {
			break
		}

		nextLines := naturalLines
		if !hasContent {
			nextLines = 1
		} else if lastWasNewline {
			nextLines++
		}
		if max(minimumLines, nextLines) > budget.linesLeft {
			break
		}

		if invalid {
			retained.WriteRune(utf8.RuneError)
			normalized = true
		} else {
			retained.WriteString(value[index : index+size])
		}
		naturalLines = nextLines
		hasContent = true
		lastWasNewline = r == '\n'
		index += size
	}
	if index == 0 {
		budget.truncated = true
		return "", false
	}

	result := retained.String()
	retainedLines := max(minimumLines, naturalLines)
	budget.bytesLeft -= len(result)
	budget.linesLeft -= retainedLines
	budget.retainedByte += uint64(len(result))
	budget.retainedLine += uint64(retainedLines)
	if index != len(value) || normalized {
		budget.truncated = true
	}
	return result, true
}

func (budget *actionPreviewBudget) canRetainString(value string, minimumLines, extraBytes int) bool {
	bytes, lines, _ := measurePreviewString(value, minimumLines)
	return bytes <= uint64(max(0, budget.bytesLeft-extraBytes)) && lines <= uint64(budget.linesLeft)
}

func (budget *actionPreviewBudget) retainStructure(bytes, lines int) bool {
	if bytes > budget.bytesLeft || lines > budget.linesLeft {
		budget.truncated = true
		return false
	}
	budget.bytesLeft -= bytes
	budget.linesLeft -= lines
	budget.retainedByte += uint64(bytes)
	budget.retainedLine += uint64(lines)
	return true
}

func (budget *actionPreviewBudget) finish(out *core.ActionPreview) {
	if budget.originalByte != budget.retainedByte || budget.originalLine != budget.retainedLine {
		budget.truncated = true
	}
	if !budget.truncated {
		return
	}
	out.Omission = &core.Omission{
		Kind: core.OmissionPreviewBudget, Scope: core.OmissionScopeActionPreview,
		Recoverability: core.RecoverabilityUnrecoverable, Continuation: core.ContinuationUnavailable,
		OriginalBytes: core.OptionalUint64{Known: true, Value: budget.originalByte},
		RetainedBytes: core.OptionalUint64{Known: true, Value: budget.retainedByte},
		OriginalLines: core.OptionalUint64{Known: true, Value: budget.originalLine},
		RetainedLines: core.OptionalUint64{Known: true, Value: budget.retainedLine},
	}
}

// boundActionPreview projects a preview into a new, bounded value without first
// cloning attacker-controlled slices or maps. The byte and logical-line budget
// covers every variable-length body field and every projected collection entry.
func boundActionPreview(preview core.ActionPreview) core.ActionPreview {
	return boundActionPreviewWithIdentity(preview, previewOmissionIdentity{})
}

type previewOmissionIdentity struct {
	valid       bool
	callID      core.CallID
	revision    core.PreviewRevision
	correlation string
}

func boundActionPreviewWithIdentity(preview core.ActionPreview, identity previewOmissionIdentity) core.ActionPreview {
	if identity.valid && preview.Omission != nil {
		omission := *preview.Omission
		omission.CallID = identity.callID
		omission.Revision = identity.revision
		omission.CorrelationID = identity.correlation
		preview.Omission = &omission
	}
	measurement := measureActionPreview(preview)
	willGenerateOmission := measurement.normalized || measurement.bytes > actionPreviewByteLimit || measurement.lines > actionPreviewLineLimit
	if identity.valid && preview.Omission == nil && willGenerateOmission {
		measurement.addString(string(identity.callID), 0)
		measurement.addString(identity.correlation, 0)
	}
	budget := newActionPreviewBudget(measurement)
	kind, _ := canonicalActionPreviewKind(preview.Kind)
	operation, _ := canonicalActionOperation(preview.Operation)
	out := core.ActionPreview{Kind: kind, Operation: operation}
	retainedCallID := ""
	retainedCorrelation := ""
	if identity.valid && (preview.Omission != nil || willGenerateOmission) {
		retainedCallID, _ = budget.retainString(string(identity.callID), 0)
		retainedCorrelation, _ = budget.retainString(identity.correlation, 0)
	}

	out.Summary, _ = budget.retainString(preview.Summary, 0)
	if preview.Targets != nil && budget.linesLeft > 0 {
		out.Targets = make([]string, 0, min(len(preview.Targets), budget.linesLeft))
		for _, target := range preview.Targets {
			retained, ok := budget.retainString(target, 1)
			if !ok {
				break
			}
			out.Targets = append(out.Targets, retained)
		}
	}
	out.UnavailableReason, _ = budget.retainString(preview.UnavailableReason, 0)

	retainPreviewMetadata(&out, preview.Metadata, budget)
	retainFileDiff(&out, preview.FileDiff, budget)
	out.Text, _ = budget.retainString(preview.Text, 0)

	// An incoming omission is retained only when the complete preview fits. Once
	// this projection loses anything, the preview-budget omission is authoritative.
	if preview.Omission != nil && measurement.bytes <= actionPreviewByteLimit && measurement.lines <= actionPreviewLineLimit && !measurement.normalized {
		omission := core.Omission{
			Kind: canonicalOmissionKind(preview.Omission.Kind), Scope: canonicalOmissionScope(preview.Omission.Scope),
			Revision: preview.Omission.Revision, Recoverability: canonicalRecoverability(preview.Omission.Recoverability),
			Continuation:  canonicalContinuation(preview.Omission.Continuation),
			OriginalBytes: preview.Omission.OriginalBytes, RetainedBytes: preview.Omission.RetainedBytes,
			OriginalLines: preview.Omission.OriginalLines, RetainedLines: preview.Omission.RetainedLines,
		}
		if identity.valid {
			omission.CorrelationID = retainedCorrelation
			omission.CallID = core.CallID(retainedCallID)
		} else {
			omission.CorrelationID, _ = budget.retainString(preview.Omission.CorrelationID, 0)
			callID, _ := budget.retainString(string(preview.Omission.CallID), 0)
			omission.CallID = core.CallID(callID)
		}
		out.Omission = &omission
	}

	budget.finish(&out)
	if identity.valid && out.Omission != nil {
		out.Omission.CallID = core.CallID(retainedCallID)
		out.Omission.Revision = identity.revision
		out.Omission.CorrelationID = retainedCorrelation
	}
	return out
}

func retainPreviewMetadata(out *core.ActionPreview, metadata map[string]string, budget *actionPreviewBudget) {
	if len(metadata) == 0 || budget.linesLeft == 0 || budget.bytesLeft < 2 {
		return
	}
	keys := boundedMetadataKeys(metadata, min(actionPreviewLineLimit, budget.linesLeft))
	for _, key := range keys {
		// Metadata keys must be complete: a partial key could collide with another
		// entry and would not truthfully identify its value. The rendered ": "
		// separator participates in the shared byte budget.
		if !budget.canRetainString(key, 1, 2) {
			budget.truncated = true
			continue
		}
		before := *budget
		retainedKey, ok := budget.retainString(key, 1)
		if !ok {
			continue
		}
		if _, collision := out.Metadata[retainedKey]; collision {
			*budget = before
			budget.truncated = true
			return
		}
		if !budget.retainStructure(2, 0) {
			*budget = before
			budget.truncated = true
			return
		}
		retainedValue, _ := budget.retainString(metadata[key], 0)
		if out.Metadata == nil {
			out.Metadata = make(map[string]string)
		}
		out.Metadata[retainedKey] = retainedValue
		if budget.linesLeft == 0 || budget.bytesLeft == 0 {
			break
		}
	}
}

func retainFileDiff(out *core.ActionPreview, fileDiff *core.FileDiffPreview, budget *actionPreviewBudget) {
	if fileDiff == nil {
		return
	}
	out.FileDiff = &core.FileDiffPreview{
		BeforeBytes: fileDiff.BeforeBytes, CandidateBytes: fileDiff.CandidateBytes,
		AddedLines: fileDiff.AddedLines, RemovedLines: fileDiff.RemovedLines,
		ChangedRegions:           fileDiff.ChangedRegions,
		BeforeHasTrailingNewline: fileDiff.BeforeHasTrailingNewline,
		AfterHasTrailingNewline:  fileDiff.AfterHasTrailingNewline,
		NonText:                  fileDiff.NonText,
	}
	out.FileDiff.Path, _ = budget.retainString(fileDiff.Path, 0)
	if len(fileDiff.Hunks) == 0 || budget.linesLeft == 0 {
		return
	}
	out.FileDiff.Hunks = make([]core.DiffHunk, 0, min(len(fileDiff.Hunks), budget.linesLeft))
	for _, sourceHunk := range fileDiff.Hunks {
		if !budget.retainStructure(0, 1) {
			break
		}
		hunk := core.DiffHunk{
			OldStart: sourceHunk.OldStart, OldLines: sourceHunk.OldLines,
			NewStart: sourceHunk.NewStart, NewLines: sourceHunk.NewLines,
		}
		if len(sourceHunk.Lines) > 0 && budget.linesLeft > 0 {
			hunk.Lines = make([]core.DiffLine, 0, min(len(sourceHunk.Lines), budget.linesLeft))
			for _, sourceLine := range sourceHunk.Lines {
				text, ok := budget.retainString(sourceLine.Text, 1)
				if !ok {
					break
				}
				kind, _ := canonicalDiffLineKind(sourceLine.Kind)
				hunk.Lines = append(hunk.Lines, core.DiffLine{Kind: kind, Text: text})
			}
		}
		out.FileDiff.Hunks = append(out.FileDiff.Hunks, hunk)
		if budget.linesLeft == 0 {
			break
		}
	}
}

func measureActionPreview(preview core.ActionPreview) actionPreviewMeasurement {
	var measurement actionPreviewMeasurement
	_, validKind := canonicalActionPreviewKind(preview.Kind)
	measurement.addUnknownTypedString(string(preview.Kind), validKind)
	_, validOperation := canonicalActionOperation(preview.Operation)
	measurement.addUnknownTypedString(string(preview.Operation), validOperation)
	measurement.addString(preview.Summary, 0)
	for _, target := range preview.Targets {
		measurement.addString(target, 1)
	}
	measurement.addString(preview.UnavailableReason, 0)
	for key, value := range preview.Metadata {
		measurement.addString(key, 1)
		measurement.addStructure(2, 0)
		measurement.addString(value, 0)
	}
	if preview.FileDiff != nil {
		measurement.addString(preview.FileDiff.Path, 0)
		for _, hunk := range preview.FileDiff.Hunks {
			measurement.addStructure(0, 1)
			for _, line := range hunk.Lines {
				_, validLineKind := canonicalDiffLineKind(line.Kind)
				measurement.addUnknownTypedString(string(line.Kind), validLineKind)
				measurement.addString(line.Text, 1)
			}
		}
	}
	measurement.addString(preview.Text, 0)
	if preview.Omission != nil {
		measurement.addUnknownTypedString(string(preview.Omission.Kind), knownOmissionKind(preview.Omission.Kind))
		measurement.addUnknownTypedString(string(preview.Omission.Scope), knownOmissionScope(preview.Omission.Scope))
		measurement.addUnknownTypedString(string(preview.Omission.Recoverability), knownRecoverability(preview.Omission.Recoverability))
		measurement.addUnknownTypedString(string(preview.Omission.Continuation), knownContinuation(preview.Omission.Continuation))
		measurement.addString(preview.Omission.CorrelationID, 0)
		measurement.addString(string(preview.Omission.CallID), 0)
	}
	return measurement
}

func (measurement *actionPreviewMeasurement) addUnknownTypedString(value string, known bool) {
	if known || value == "" {
		return
	}
	measurement.addString(value, 0)
	measurement.normalized = true
}

func canonicalActionPreviewKind(kind core.ActionPreviewKind) (core.ActionPreviewKind, bool) {
	switch kind {
	case "", core.ActionPreviewKindUnknown:
		return core.ActionPreviewKindUnknown, true
	case core.ActionPreviewUnavailable:
		return core.ActionPreviewUnavailable, true
	case core.ActionPreviewText:
		return core.ActionPreviewText, true
	case core.ActionPreviewFileDiff:
		return core.ActionPreviewFileDiff, true
	case core.ActionPreviewMetadata:
		return core.ActionPreviewMetadata, true
	default:
		return core.ActionPreviewKindUnknown, false
	}
}

func canonicalActionOperation(operation core.ActionOperation) (core.ActionOperation, bool) {
	switch operation {
	case "", core.ActionOperationUnknown:
		return core.ActionOperationUnknown, true
	case core.ActionOperationCreate:
		return core.ActionOperationCreate, true
	case core.ActionOperationModify:
		return core.ActionOperationModify, true
	case core.ActionOperationDelete:
		return core.ActionOperationDelete, true
	case core.ActionOperationCommand:
		return core.ActionOperationCommand, true
	case core.ActionOperationCustom:
		return core.ActionOperationCustom, true
	default:
		return core.ActionOperationUnknown, false
	}
}

func canonicalDiffLineKind(kind core.DiffLineKind) (core.DiffLineKind, bool) {
	switch kind {
	case "":
		return unknownDiffLineKind, true
	case core.DiffLineContext:
		return core.DiffLineContext, true
	case core.DiffLineAdded:
		return core.DiffLineAdded, true
	case core.DiffLineRemoved:
		return core.DiffLineRemoved, true
	default:
		return unknownDiffLineKind, false
	}
}

func knownOmissionKind(kind core.OmissionKind) bool {
	switch kind {
	case "", core.OmissionKindUnknown, core.OmissionOutputBudget, core.OmissionPreviewBudget,
		core.OmissionProviderLength, core.OmissionContentFilter, core.OmissionRedacted, core.OmissionContextCompaction:
		return true
	default:
		return false
	}
}

func canonicalOmissionKind(kind core.OmissionKind) core.OmissionKind {
	switch kind {
	case "", core.OmissionKindUnknown:
		return core.OmissionKindUnknown
	case core.OmissionOutputBudget:
		return core.OmissionOutputBudget
	case core.OmissionPreviewBudget:
		return core.OmissionPreviewBudget
	case core.OmissionProviderLength:
		return core.OmissionProviderLength
	case core.OmissionContentFilter:
		return core.OmissionContentFilter
	case core.OmissionRedacted:
		return core.OmissionRedacted
	case core.OmissionContextCompaction:
		return core.OmissionContextCompaction
	default:
		return core.OmissionKindUnknown
	}
}

func knownOmissionScope(scope core.OmissionScope) bool {
	switch scope {
	case "", core.OmissionScopeUnknown, core.OmissionScopeAssistantReply, core.OmissionScopeToolOutput,
		core.OmissionScopeActionPreview, core.OmissionScopeConversation, core.OmissionScopePublicPayload:
		return true
	default:
		return false
	}
}

func canonicalOmissionScope(scope core.OmissionScope) core.OmissionScope {
	switch scope {
	case "", core.OmissionScopeUnknown:
		return core.OmissionScopeUnknown
	case core.OmissionScopeAssistantReply:
		return core.OmissionScopeAssistantReply
	case core.OmissionScopeToolOutput:
		return core.OmissionScopeToolOutput
	case core.OmissionScopeActionPreview:
		return core.OmissionScopeActionPreview
	case core.OmissionScopeConversation:
		return core.OmissionScopeConversation
	case core.OmissionScopePublicPayload:
		return core.OmissionScopePublicPayload
	default:
		return core.OmissionScopeUnknown
	}
}

func knownRecoverability(recoverability core.Recoverability) bool {
	switch recoverability {
	case "", core.RecoverabilityUnknown, core.RecoverabilityRecoverable, core.RecoverabilityUnrecoverable:
		return true
	default:
		return false
	}
}

func canonicalRecoverability(recoverability core.Recoverability) core.Recoverability {
	switch recoverability {
	case "", core.RecoverabilityUnknown:
		return core.RecoverabilityUnknown
	case core.RecoverabilityRecoverable:
		return core.RecoverabilityRecoverable
	case core.RecoverabilityUnrecoverable:
		return core.RecoverabilityUnrecoverable
	default:
		return core.RecoverabilityUnknown
	}
}

func knownContinuation(continuation core.ContinuationMode) bool {
	switch continuation {
	case "", core.ContinuationUnknown, core.ContinuationUnavailable, core.ContinuationNewUserTurn:
		return true
	default:
		return false
	}
}

func canonicalContinuation(continuation core.ContinuationMode) core.ContinuationMode {
	switch continuation {
	case "", core.ContinuationUnknown:
		return core.ContinuationUnknown
	case core.ContinuationUnavailable:
		return core.ContinuationUnavailable
	case core.ContinuationNewUserTurn:
		return core.ContinuationNewUserTurn
	default:
		return core.ContinuationUnknown
	}
}

func measurePreviewString(value string, minimumLines int) (uint64, uint64, bool) {
	bytes := uint64(0)
	normalized := false
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			bytes = saturatingAdd(bytes, uint64(utf8.RuneLen(utf8.RuneError)))
			normalized = true
		} else {
			bytes = saturatingAdd(bytes, uint64(size))
		}
		index += size
	}
	lines := logicalLineCount(value)
	return bytes, uint64(max(minimumLines, lines)), normalized
}

func saturatingAdd(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

type metadataKeyMaxHeap []string

func (keys metadataKeyMaxHeap) Len() int           { return len(keys) }
func (keys metadataKeyMaxHeap) Less(i, j int) bool { return keys[i] > keys[j] }
func (keys metadataKeyMaxHeap) Swap(i, j int)      { keys[i], keys[j] = keys[j], keys[i] }
func (keys *metadataKeyMaxHeap) Push(value interface{}) {
	*keys = append(*keys, value.(string))
}
func (keys *metadataKeyMaxHeap) Pop() interface{} {
	old := *keys
	last := old[len(old)-1]
	*keys = old[:len(old)-1]
	return last
}

// boundedMetadataKeys retains only the lexicographically smallest keys that
// could possibly fit, keeping temporary allocation independent of map size.
func boundedMetadataKeys(metadata map[string]string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	keys := make(metadataKeyMaxHeap, 0, min(len(metadata), limit))
	for key := range metadata {
		if len(keys) < limit {
			heap.Push(&keys, key)
			continue
		}
		if key < keys[0] {
			keys[0] = key
			heap.Fix(&keys, 0)
		}
	}
	sort.Strings(keys)
	return keys
}
