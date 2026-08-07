package tools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

const maxDiffBytes = 64 * 1024

type patchArgs struct {
	Path        string  `json:"path"`
	Target      string  `json:"target"`
	Replacement *string `json:"replacement"`
}

// lineRange is a parsed target. Line numbers are 1-based.
type lineRange struct {
	start   int
	end     int  // inclusive; meaningful only when !endOpen && !insert
	endOpen bool // "L{n}-"
	insert  bool // "L{n}-L{n}"
}

type patchTool struct{ base }

func (t *patchTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Name: "patch",
		Description: "Prepare an in-place line-range replacement in an existing " +
			"UTF-8 workspace file and return a diff preview for approval. " +
			"target is 1-based: L3 (replace line 3), L3-L5 (replace lines 3-5), " +
			"L3- (line 3 to EOF), L3-L3 (insert before line 3).",
		Schema: json.RawMessage(
			`{"type":"object","additionalProperties":false,` +
				`"required":["path","target","replacement"],` +
				`"properties":{"path":{"type":"string"},` +
				`"target":{"type":"string"},"replacement":{"type":"string"}}}`),
	}
}

func (t *patchTool) Prepare(ctx context.Context, raw json.RawMessage) (action.Prepared, error) {
	if err := ctx.Err(); err != nil {
		return action.Prepared{}, err
	}
	var args patchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return action.Prepared{}, err
	}
	if args.Path == "" {
		return action.Prepared{}, errors.New("path is required")
	}
	if args.Target == "" {
		return action.Prepared{}, errors.New("target is required")
	}
	if args.Replacement == nil {
		return action.Prepared{}, errors.New("replacement is required")
	}
	clean, err := t.fs.Clean(args.Path)
	if err != nil {
		return action.Prepared{}, patchError(err)
	}
	if dataproj.ProtectedPath(clean) {
		return action.Prepared{}, errors.New("path is protected and cannot be patched")
	}
	r, err := parseTarget(args.Target)
	if err != nil {
		return action.Prepared{}, err
	}
	f, _, err := t.fs.Read(args.Path)
	if err != nil {
		return action.Prepared{}, patchError(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return action.Prepared{}, err
	}
	if !info.Mode().IsRegular() {
		return action.Prepared{}, errors.New("patch target is not a regular file")
	}
	source, err := io.ReadAll(f)
	if err != nil {
		return action.Prepared{}, fmt.Errorf("read file: %w", err)
	}
	if !dataproj.IsText(source) {
		return action.Prepared{}, errors.New("binary or invalid UTF-8 file is not patchable")
	}
	oldLines, hadNL := splitSourceLines(source)
	if err := r.validate(len(oldLines)); err != nil {
		return action.Prepared{}, err
	}
	repLines := splitReplacementLines(*args.Replacement)
	newContent := apply(oldLines, repLines, r, hadNL)
	diff, _ := buildUnifiedDiff(clean, oldLines, r, repLines)
	diffDigest := sha256Hex([]byte(diff))
	d := t.projector.Detector()
	isSensitive := d.Contains(string(source)) || d.Contains(*args.Replacement) || d.Contains(diff)
	sourceHash := sha256Hex(source)
	expectedHash := sha256Hex(newContent)
	effective, _ := json.Marshal(args)
	return action.Prepared{
		Tool:      "patch",
		Arguments: effective,
		Effects:   []action.Effect{action.EffectWrite},
		Paths:     []string{clean},
		Patch: &action.PreparedPatch{
			RequestID:      newRequestID(),
			Path:           clean,
			Target:         args.Target,
			SourceSHA256:   sourceHash,
			ExpectedSHA256: expectedHash,
			Diff:           diff,
			DiffDigest:     diffDigest,
			IsSensitive:    isSensitive,
			CreatedAt:      time.Now(),
		},
	}, nil
}

func (t *patchTool) Execute(ctx context.Context, prepared action.Prepared) action.Execution {
	if prepared.Patch == nil {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution requires a PreparedPatch from the Prepare phase",
		}
	}
	if err := ctx.Err(); err != nil {
		return action.Execution{
			Outcome: transcript.ToolResultCancelled,
			Content: "patch execution cancelled",
		}
	}
	var args patchArgs
	if err := json.Unmarshal(prepared.Arguments, &args); err != nil {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: invalid stored arguments",
		}
	}
	f, _, err := t.fs.Read(args.Path)
	if err != nil {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: " + patchErrorMessage(err),
		}
	}
	defer func() { _ = f.Close() }()
	source, err := io.ReadAll(f)
	if err != nil {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: read file: " + err.Error(),
		}
	}
	currentHash := sha256Hex(source)
	if currentHash != prepared.Patch.SourceSHA256 {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: source file has changed since preparation (stale); re-read and prepare again",
		}
	}
	if !dataproj.IsText(source) {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: binary or invalid UTF-8 file is not patchable",
		}
	}
	oldLines, hadNL := splitSourceLines(source)
	r, err := parseTarget(args.Target)
	if err != nil {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: " + err.Error(),
		}
	}
	if err := r.validate(len(oldLines)); err != nil {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: " + err.Error(),
		}
	}
	repLines := splitReplacementLines(*args.Replacement)
	newContent := apply(oldLines, repLines, r, hadNL)
	if sha256Hex(newContent) != prepared.Patch.ExpectedSHA256 {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: content mismatch; expected hash does not match prepared action",
		}
	}
	if _, err := t.fs.Write(args.Path, newContent, prepared.Patch.ExpectedSHA256); err != nil {
		return action.Execution{
			Outcome: transcript.ToolResultError,
			Content: "patch execution: write failed: " + err.Error(),
		}
	}
	return action.Execution{
		Outcome: transcript.ToolResultSuccess,
		Content: "patch applied successfully",
	}
}

func patchErrorMessage(err error) string {
	switch {
	case errors.Is(err, workspace.ErrEscape):
		return "path is outside the workspace"
	case errors.Is(err, fs.ErrNotExist):
		return "file not found"
	case errors.Is(err, fs.ErrPermission):
		return "file is unreadable"
	default:
		return err.Error()
	}
}

// --- target parsing ---

func parseTarget(s string) (lineRange, error) {
	if s == "" || s[0] != 'L' {
		return lineRange{}, targetFormatError(s)
	}
	rest := s[1:]
	left, right, hasDash := strings.Cut(rest, "-")
	if !hasDash {
		n, err := parseLineNumber(left) // left == rest when no dash
		if err != nil {
			return lineRange{}, targetFormatError(s)
		}
		return lineRange{start: n, end: n}, nil
	}
	start, err := parseLineNumber(left)
	if err != nil {
		return lineRange{}, targetFormatError(s)
	}
	if right == "" {
		return lineRange{start: start, endOpen: true}, nil
	}
	if right[0] != 'L' {
		return lineRange{}, targetFormatError(s)
	}
	end, err := parseLineNumber(right[1:])
	if err != nil {
		return lineRange{}, targetFormatError(s)
	}
	if end < start {
		return lineRange{}, fmt.Errorf("invalid target %q: end line %d is before start line %d", s, end, start)
	}
	if start == end {
		return lineRange{start: start, insert: true}, nil
	}
	return lineRange{start: start, end: end}, nil
}

func parseLineNumber(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty line number")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid line number %q", s)
	}
	return n, nil
}

func targetFormatError(s string) error {
	return fmt.Errorf("invalid target %q: expected L3, L3-L5, L3-, or L3-L3", s)
}

func (r lineRange) validate(lineCount int) error {
	if r.insert {
		if r.start < 1 || r.start > lineCount+1 {
			return fmt.Errorf("target line range out of bounds: file has %d lines", lineCount)
		}
		return nil
	}
	if r.start < 1 || r.start > lineCount {
		return fmt.Errorf("target line range out of bounds: file has %d lines", lineCount)
	}
	if !r.endOpen && r.end > lineCount {
		return fmt.Errorf("target line range out of bounds: file has %d lines", lineCount)
	}
	return nil
}

// --- line splitting ---

func splitSourceLines(data []byte) (lines []string, hadTrailingNewline bool) {
	if len(data) == 0 {
		return nil, false
	}
	hadTrailingNewline = data[len(data)-1] == '\n'
	s := string(data)
	if hadTrailingNewline {
		s = s[:len(s)-1]
	}
	if s == "" {
		return []string{""}, true
	}
	return strings.Split(s, "\n"), hadTrailingNewline
}

func splitReplacementLines(s string) []string {
	if s == "" {
		return nil
	}
	if s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// --- replacement application ---

func apply(oldLines, repLines []string, r lineRange, hadTrailingNewline bool) []byte {
	var result []string
	switch {
	case r.insert:
		result = append(result, oldLines[:r.start-1]...)
		result = append(result, repLines...)
		result = append(result, oldLines[r.start-1:]...)
	case r.endOpen:
		result = append(result, oldLines[:r.start-1]...)
		result = append(result, repLines...)
	default:
		result = append(result, oldLines[:r.start-1]...)
		result = append(result, repLines...)
		result = append(result, oldLines[r.end:]...)
	}
	s := strings.Join(result, "\n")
	if hadTrailingNewline {
		s += "\n"
	}
	return []byte(s)
}

// --- diff generation ---

func buildUnifiedDiff(path string, oldLines []string, r lineRange, repLines []string) (string, bool) {
	var region []string
	var oldCount int
	var hunkStart int
	if r.insert {
		region = nil
		oldCount = 0
		hunkStart = r.start - 1
	} else {
		if r.endOpen {
			region = oldLines[r.start-1:]
		} else {
			region = oldLines[r.start-1 : r.end]
		}
		oldCount = len(region)
		hunkStart = r.start
	}
	newCount := len(repLines)
	var buf strings.Builder
	buf.WriteString("--- " + path + "\n")
	buf.WriteString("+++ " + path + "\n")
	fmt.Fprintf(&buf, "@@ -%d,%d +%d,%d @@\n", hunkStart, oldCount, r.start, newCount)
	for _, line := range region {
		buf.WriteString("-" + line + "\n")
	}
	for _, line := range repLines {
		buf.WriteString("+" + line + "\n")
	}
	diff := buf.String()
	if len(diff) <= maxDiffBytes {
		return diff, false
	}
	const marker = "[diff truncated=true; large patch preview]\n"
	cut := maxDiffBytes - len(marker)
	for cut > 0 && !utf8.RuneStart(diff[cut]) {
		cut--
	}
	return diff[:cut] + marker, true
}

// --- helpers ---

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("req-%x", b[:])
}

func patchError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrEscape):
		return errors.New("path is outside the workspace or crosses a disallowed symlink")
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("file not found")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("file is unreadable")
	default:
		return err
	}
}
