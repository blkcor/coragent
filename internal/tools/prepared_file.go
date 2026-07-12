package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/blkcor/coragent/internal/core"
)

const (
	previewByteLimit         = 64 * 1024
	previewLineLimit         = 800
	preparedFileByteLimit    = 16 * 1024 * 1024
	diffComputationByteLimit = 1 * 1024 * 1024
	diffComputationLineLimit = 20_000
	diffMatrixCellLimit      = 4_000_000
)

var (
	ErrStalePreparedAction       = errors.New("prepared action is stale")
	ErrHardLinkAliasUnsupported  = errors.New("hard_link_alias_unsupported")
	ErrIdentityCommitUnsupported = errors.New("identity-bound file commit is unsupported")
	ErrPreparedFileTooLarge      = errors.New("prepared file exceeds the safe byte limit")
)

type preparedFileToken struct {
	path           string
	before         []byte
	candidate      []byte
	exists         bool
	createParents  bool
	operation      core.ActionOperation
	platform       platformFileSnapshot
	toolName       string
	successMessage string
}

type fileIdentity struct {
	device uint64
	inode  uint64
}

func (identity fileIdentity) safeString() string {
	if identity.device == 0 && identity.inode == 0 {
		return "unavailable"
	}
	return fmt.Sprintf("%x:%x", identity.device, identity.inode)
}

func (identity fileIdentity) equal(other fileIdentity) bool {
	return identity.device == other.device && identity.inode == other.inode
}

type directorySnapshot struct {
	name     string
	identity fileIdentity
}

type platformFileSnapshot struct {
	rootIdentity   fileIdentity
	parents        []directorySnapshot
	missingParents []string
	parentIdentity fileIdentity
	targetExists   bool
	targetIdentity fileIdentity
}

func prepareWriteFile(ctx context.Context, args map[string]interface{}) (core.PreparedAction, error) {
	path, ok := stringArg(args, "path")
	if !ok || path == "" {
		return core.PreparedAction{}, fmt.Errorf("write_file: path is required")
	}
	content, ok := stringArg(args, "content")
	if !ok {
		return core.PreparedAction{}, fmt.Errorf("write_file: content is required")
	}
	if len(content) > preparedFileByteLimit {
		return core.PreparedAction{}, fmt.Errorf("write_file: %w: candidate is %d bytes (limit %d)", ErrPreparedFileTooLarge, len(content), preparedFileByteLimit)
	}
	prepared, err := prepareFileCandidate(ctx, args, path, []byte(content), boolArg(args, "create_parents"))
	if err == nil {
		token := prepared.CommitToken.(*preparedFileToken)
		token.toolName = "write_file"
		token.successMessage = fmt.Sprintf("wrote %d bytes to %s", len(content), path)
	}
	return prepared, err
}

func prepareEditFile(ctx context.Context, args map[string]interface{}) (core.PreparedAction, error) {
	path, ok := stringArg(args, "path")
	if !ok || path == "" {
		return core.PreparedAction{}, fmt.Errorf("edit_file: path is required")
	}
	oldString, ok := stringArg(args, "old_string")
	if !ok {
		return core.PreparedAction{}, fmt.Errorf("edit_file: old_string is required")
	}
	newString, ok := stringArg(args, "new_string")
	if !ok {
		return core.PreparedAction{}, fmt.Errorf("edit_file: new_string is required")
	}
	if oldString == newString {
		return core.PreparedAction{}, fmt.Errorf("edit_file: old_string and new_string are identical; nothing to do")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return core.PreparedAction{}, fmt.Errorf("edit_file: resolve %s: %w", path, err)
	}
	absolute, err = canonicalPreparedPath(filepath.Clean(absolute))
	if err != nil {
		return core.PreparedAction{}, fmt.Errorf("edit_file: resolve parent for %s: %w", path, err)
	}
	before, snapshot, err := readFileSnapshot(ctx, absolute, false)
	if err != nil {
		return core.PreparedAction{}, fmt.Errorf("edit_file: %s: %w", path, err)
	}
	count := bytes.Count(before, []byte(oldString))
	switch {
	case count == 0:
		return core.PreparedAction{}, fmt.Errorf("edit_file: old_string not found in %s; file unchanged", path)
	case count > 1 && !boolArg(args, "replace_all"):
		return core.PreparedAction{}, fmt.Errorf("edit_file: old_string is ambiguous — it appears %d times in %s; set replace_all to replace every occurrence; file unchanged", count, path)
	}
	limit := 1
	replacements := 1
	if boolArg(args, "replace_all") {
		limit = -1
		replacements = count
	}
	if candidateSizeExceedsLimit(len(before), len(oldString), len(newString), replacements) {
		return core.PreparedAction{}, fmt.Errorf("edit_file: %w: candidate would exceed %d bytes", ErrPreparedFileTooLarge, preparedFileByteLimit)
	}
	candidate := bytes.Replace(before, []byte(oldString), []byte(newString), limit)
	prepared := preparedFileCandidateFromSnapshot(args, absolute, candidate, false, before, snapshot)
	token := prepared.CommitToken.(*preparedFileToken)
	token.toolName = "edit_file"
	token.successMessage = fmt.Sprintf("edited %s (%d replacement(s))", path, replacements)
	return prepared, nil
}

func prepareFileCandidate(ctx context.Context, args map[string]interface{}, path string, candidate []byte, createParents bool) (core.PreparedAction, error) {
	if err := ctx.Err(); err != nil {
		return core.PreparedAction{}, err
	}
	if len(candidate) > preparedFileByteLimit {
		return core.PreparedAction{}, fmt.Errorf("%w: candidate is %d bytes (limit %d)", ErrPreparedFileTooLarge, len(candidate), preparedFileByteLimit)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return core.PreparedAction{}, fmt.Errorf("resolve target %s: %w", path, err)
	}
	absolute, err = canonicalPreparedPath(filepath.Clean(absolute))
	if err != nil {
		return core.PreparedAction{}, fmt.Errorf("resolve parent for %s: %w", path, err)
	}
	before, snapshot, err := readFileSnapshot(ctx, absolute, createParents)
	if err != nil {
		return core.PreparedAction{}, err
	}
	return preparedFileCandidateFromSnapshot(args, absolute, candidate, createParents, before, snapshot), nil
}

func preparedFileCandidateFromSnapshot(args map[string]interface{}, absolute string, candidate []byte, createParents bool, before []byte, snapshot platformFileSnapshot) core.PreparedAction {
	operation := core.ActionOperationModify
	if !snapshot.targetExists {
		operation = core.ActionOperationCreate
	}
	preview := buildFilePreview(absolute, before, candidate, operation, snapshot)
	token := &preparedFileToken{
		path: absolute, before: append([]byte(nil), before...), candidate: append([]byte(nil), candidate...),
		exists: snapshot.targetExists, createParents: createParents, operation: operation, platform: snapshot,
	}
	return core.PreparedAction{
		EffectiveArguments: cloneArgumentMap(args),
		Operation:          operation,
		Preview:            preview,
		CommitToken:        token,
	}
}

func executePreparedFile(ctx context.Context, prepared core.PreparedAction, expectedTool string) (string, error) {
	token, ok := prepared.CommitToken.(*preparedFileToken)
	if !ok || token == nil {
		return "", fmt.Errorf("prepared file action: invalid commit token")
	}
	if token.toolName != expectedTool {
		return "", fmt.Errorf("prepared file action: commit token belongs to %s", token.toolName)
	}
	if prepared.Operation != token.operation {
		return "", fmt.Errorf("%w: operation changed", ErrStalePreparedAction)
	}
	if err := commitFileCandidate(ctx, token); err != nil {
		return "", err
	}
	if token.successMessage != "" {
		return token.successMessage, nil
	}
	return fmt.Sprintf("updated %d bytes in %s", len(token.candidate), token.path), nil
}

func buildFilePreview(path string, before, candidate []byte, operation core.ActionOperation, snapshot platformFileSnapshot) core.ActionPreview {
	diff := &core.FileDiffPreview{
		Path:                     path,
		BeforeBytes:              core.OptionalUint64{Known: true, Value: uint64(len(before))},
		CandidateBytes:           core.OptionalUint64{Known: true, Value: uint64(len(candidate))},
		BeforeHasTrailingNewline: len(before) > 0 && before[len(before)-1] == '\n',
		AfterHasTrailingNewline:  len(candidate) > 0 && candidate[len(candidate)-1] == '\n',
	}
	preview := core.ActionPreview{
		Kind: core.ActionPreviewFileDiff, Operation: operation,
		Summary: fmt.Sprintf("%s %s", operation, path), Targets: []string{path}, FileDiff: diff,
		Metadata: map[string]string{
			"target_identity": snapshot.targetIdentity.safeString(),
			"parent_identity": snapshot.parentIdentity.safeString(),
		},
	}
	if !textRepresentable(before) || !textRepresentable(candidate) {
		diff.NonText = true
		preview.Kind = core.ActionPreviewMetadata
		preview.Summary = fmt.Sprintf("%s non-text file %s (%d → %d bytes)", operation, path, len(before), len(candidate))
		preview.Metadata["encoding"] = "binary_or_undecodable"
		return preview
	}
	if diffInputExceedsBudget(before, candidate) {
		preview.Kind = core.ActionPreviewMetadata
		preview.Summary = fmt.Sprintf("%s text file %s (%d → %d bytes); diff omitted by computation budget", operation, path, len(before), len(candidate))
		preview.Metadata["diff"] = "omitted_over_computation_budget"
		preview.Omission = &core.Omission{
			Kind:           core.OmissionPreviewBudget,
			Scope:          core.OmissionScopeActionPreview,
			Recoverability: core.RecoverabilityUnrecoverable,
			Continuation:   core.ContinuationUnavailable,
			RetainedBytes:  core.OptionalUint64{Known: true, Value: 0},
			RetainedLines:  core.OptionalUint64{Known: true, Value: 0},
		}
		return preview
	}

	oldLines := splitLogicalLines(string(before))
	newLines := splitLogicalLines(string(candidate))
	operations := lineDiff(oldLines, newLines)
	hunks, added, removed, regions := buildHunks(operations, 3)
	diff.AddedLines = core.OptionalUint64{Known: true, Value: uint64(added)}
	diff.RemovedLines = core.OptionalUint64{Known: true, Value: uint64(removed)}
	diff.ChangedRegions = core.OptionalUint64{Known: true, Value: uint64(regions)}

	fullText, fullLines := renderHunks(hunks)
	retainedHunks, retainedText, retainedLines := boundHunks(hunks)
	diff.Hunks = retainedHunks
	preview.Text = retainedText
	if len(retainedText) < len(fullText) || retainedLines < fullLines {
		omission := core.Omission{
			Kind: core.OmissionPreviewBudget, Scope: core.OmissionScopeActionPreview,
			Recoverability: core.RecoverabilityUnrecoverable, Continuation: core.ContinuationUnavailable,
			OriginalBytes: core.OptionalUint64{Known: true, Value: uint64(len(fullText))},
			RetainedBytes: core.OptionalUint64{Known: true, Value: uint64(len(retainedText))},
			OriginalLines: core.OptionalUint64{Known: true, Value: uint64(fullLines)},
			RetainedLines: core.OptionalUint64{Known: true, Value: uint64(retainedLines)},
		}
		preview.Omission = &omission
	}
	return preview
}

func textRepresentable(value []byte) bool {
	return utf8.Valid(value) && !bytes.ContainsRune(value, '\x00')
}

func splitLogicalLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	if strings.HasSuffix(value, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type diffOperation struct {
	kind core.DiffLineKind
	text string
}

func lineDiff(oldLines, newLines []string) []diffOperation {
	if len(oldLines) == 0 {
		out := make([]diffOperation, len(newLines))
		for index, line := range newLines {
			out[index] = diffOperation{kind: core.DiffLineAdded, text: line}
		}
		return out
	}
	if len(newLines) == 0 {
		out := make([]diffOperation, len(oldLines))
		for index, line := range oldLines {
			out[index] = diffOperation{kind: core.DiffLineRemoved, text: line}
		}
		return out
	}
	// Bound the dynamic-programming matrix. Huge replacements still retain
	// truthful totals and a bounded preview without quadratic memory.
	if len(newLines) > 0 && len(oldLines) > diffMatrixCellLimit/len(newLines) {
		out := make([]diffOperation, 0, len(oldLines)+len(newLines))
		for _, line := range oldLines {
			out = append(out, diffOperation{kind: core.DiffLineRemoved, text: line})
		}
		for _, line := range newLines {
			out = append(out, diffOperation{kind: core.DiffLineAdded, text: line})
		}
		return out
	}

	columns := len(newLines) + 1
	matrix := make([]int, (len(oldLines)+1)*columns)
	for oldIndex := len(oldLines) - 1; oldIndex >= 0; oldIndex-- {
		for newIndex := len(newLines) - 1; newIndex >= 0; newIndex-- {
			position := oldIndex*columns + newIndex
			if oldLines[oldIndex] == newLines[newIndex] {
				matrix[position] = matrix[(oldIndex+1)*columns+newIndex+1] + 1
			} else if matrix[(oldIndex+1)*columns+newIndex] >= matrix[oldIndex*columns+newIndex+1] {
				matrix[position] = matrix[(oldIndex+1)*columns+newIndex]
			} else {
				matrix[position] = matrix[oldIndex*columns+newIndex+1]
			}
		}
	}
	var out []diffOperation
	oldIndex, newIndex := 0, 0
	for oldIndex < len(oldLines) && newIndex < len(newLines) {
		switch {
		case oldLines[oldIndex] == newLines[newIndex]:
			out = append(out, diffOperation{kind: core.DiffLineContext, text: oldLines[oldIndex]})
			oldIndex++
			newIndex++
		case matrix[(oldIndex+1)*columns+newIndex] >= matrix[oldIndex*columns+newIndex+1]:
			out = append(out, diffOperation{kind: core.DiffLineRemoved, text: oldLines[oldIndex]})
			oldIndex++
		default:
			out = append(out, diffOperation{kind: core.DiffLineAdded, text: newLines[newIndex]})
			newIndex++
		}
	}
	for ; oldIndex < len(oldLines); oldIndex++ {
		out = append(out, diffOperation{kind: core.DiffLineRemoved, text: oldLines[oldIndex]})
	}
	for ; newIndex < len(newLines); newIndex++ {
		out = append(out, diffOperation{kind: core.DiffLineAdded, text: newLines[newIndex]})
	}
	return out
}

func candidateSizeExceedsLimit(beforeBytes, oldBytes, newBytes, replacements int) bool {
	if beforeBytes < 0 || oldBytes < 0 || newBytes < 0 || replacements < 0 {
		return true
	}
	size := uint64(beforeBytes)
	if size > preparedFileByteLimit {
		return true
	}
	if newBytes <= oldBytes || replacements == 0 {
		return false
	}
	delta := uint64(newBytes - oldBytes)
	return delta > (uint64(preparedFileByteLimit)-size)/uint64(replacements)
}

func diffInputExceedsBudget(before, candidate []byte) bool {
	if len(before) > diffComputationByteLimit-len(candidate) {
		return true
	}
	return logicalLineCount(before) > diffComputationLineLimit || logicalLineCount(candidate) > diffComputationLineLimit
}

func logicalLineCount(value []byte) int {
	if len(value) == 0 {
		return 0
	}
	count := bytes.Count(value, []byte{'\n'})
	if value[len(value)-1] != '\n' {
		count++
	}
	return count
}

func buildHunks(operations []diffOperation, contextLines int) ([]core.DiffHunk, int, int, int) {
	var changeIndexes []int
	added, removed := 0, 0
	for index, operation := range operations {
		switch operation.kind {
		case core.DiffLineAdded:
			added++
			changeIndexes = append(changeIndexes, index)
		case core.DiffLineRemoved:
			removed++
			changeIndexes = append(changeIndexes, index)
		}
	}
	if len(changeIndexes) == 0 {
		return nil, 0, 0, 0
	}
	type span struct{ start, end int }
	spans := []span{{start: maxInt(0, changeIndexes[0]-contextLines), end: minInt(len(operations), changeIndexes[0]+contextLines+1)}}
	for _, index := range changeIndexes[1:] {
		start, end := maxInt(0, index-contextLines), minInt(len(operations), index+contextLines+1)
		last := &spans[len(spans)-1]
		if start <= last.end {
			if end > last.end {
				last.end = end
			}
		} else {
			spans = append(spans, span{start: start, end: end})
		}
	}

	oldPositions := make([]int, len(operations)+1)
	newPositions := make([]int, len(operations)+1)
	oldLine, newLine := 1, 1
	for index, operation := range operations {
		oldPositions[index], newPositions[index] = oldLine, newLine
		if operation.kind != core.DiffLineAdded {
			oldLine++
		}
		if operation.kind != core.DiffLineRemoved {
			newLine++
		}
	}
	oldPositions[len(operations)], newPositions[len(operations)] = oldLine, newLine

	hunks := make([]core.DiffHunk, 0, len(spans))
	for _, current := range spans {
		hunk := core.DiffHunk{OldStart: uint64(oldPositions[current.start]), NewStart: uint64(newPositions[current.start])}
		for _, operation := range operations[current.start:current.end] {
			hunk.Lines = append(hunk.Lines, core.DiffLine{Kind: operation.kind, Text: operation.text})
			if operation.kind != core.DiffLineAdded {
				hunk.OldLines++
			}
			if operation.kind != core.DiffLineRemoved {
				hunk.NewLines++
			}
		}
		hunks = append(hunks, hunk)
	}
	return hunks, added, removed, len(spans)
}

func renderHunks(hunks []core.DiffHunk) (string, int) {
	var builder strings.Builder
	lines := 0
	for _, hunk := range hunks {
		fmt.Fprintf(&builder, "@@ -%d,%d +%d,%d @@\n", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
		lines++
		for _, line := range hunk.Lines {
			prefix := " "
			switch line.Kind {
			case core.DiffLineAdded:
				prefix = "+"
			case core.DiffLineRemoved:
				prefix = "-"
			}
			builder.WriteString(prefix)
			builder.WriteString(line.Text)
			builder.WriteByte('\n')
			lines++
		}
	}
	return builder.String(), lines
}

func boundHunks(hunks []core.DiffHunk) ([]core.DiffHunk, string, int) {
	var retained []core.DiffHunk
	var builder strings.Builder
	lineCount := 0
	for _, hunk := range hunks {
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
		if lineCount+1 > previewLineLimit || builder.Len()+len(header) > previewByteLimit {
			break
		}
		kept := hunk
		kept.Lines = nil
		builder.WriteString(header)
		lineCount++
		for _, line := range hunk.Lines {
			prefix := " "
			switch line.Kind {
			case core.DiffLineAdded:
				prefix = "+"
			case core.DiffLineRemoved:
				prefix = "-"
			}
			rendered := prefix + line.Text + "\n"
			if lineCount+1 > previewLineLimit || builder.Len()+len(rendered) > previewByteLimit {
				break
			}
			builder.WriteString(rendered)
			lineCount++
			kept.Lines = append(kept.Lines, line)
		}
		retained = append(retained, kept)
		if len(kept.Lines) != len(hunk.Lines) {
			break
		}
	}
	return retained, builder.String(), lineCount
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// canonicalPreparedPath resolves only the existing parent prefix once during
// preparation. This accommodates standard macOS aliases such as /var while the
// commit token binds every real directory identity and never follows the target.
func canonicalPreparedPath(path string) (string, error) {
	target := filepath.Base(path)
	parent := filepath.Dir(path)
	var missing []string
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				return "", fmt.Errorf("parent %s is not a directory", parent)
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Join(resolved, target), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", err
		}
		missing = append(missing, filepath.Base(parent))
		parent = next
	}
}
