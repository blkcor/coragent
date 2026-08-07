// Package tools implements M1's pure-Go read-only repository tools.
package tools

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

const maxResultBytes = 64 * 1024

type base struct {
	fs        workspace.FileService
	projector *dataproj.Projector
}

// NewCatalog creates exactly read, list, and search. No mutation, command, or
// network capability is reachable from the returned tools.
func NewCatalog(fs workspace.FileService, projector *dataproj.Projector) []action.Tool {
	if projector == nil {
		projector = dataproj.New()
	}
	b := base{fs: fs, projector: projector}
	return []action.Tool{&listTool{base: b}, &readTool{base: b}, &searchTool{base: b}}
}

type readArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type readTool struct{ base }

func (t *readTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{Name: "read", Description: "Read a UTF-8 workspace file with stable line numbers. Paths must be workspace-relative.", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}}}`)}
}

func (t *readTool) Prepare(ctx context.Context, raw json.RawMessage) (action.Prepared, error) {
	var args readArgs
	if err := decodeArgs(raw, &args); err != nil {
		return action.Prepared{}, err
	}
	if args.Path == "" {
		return action.Prepared{}, errors.New("path is required")
	}
	clean, err := t.fs.Clean(args.Path)
	if err != nil {
		return action.Prepared{}, err
	}
	if args.StartLine == 0 {
		args.StartLine = 1
	}
	if args.EndLine != 0 && args.EndLine < args.StartLine {
		return action.Prepared{}, errors.New("end_line is before start_line")
	}
	effective, _ := json.Marshal(args)
	return action.Prepared{Tool: "read", Arguments: effective, Effects: []action.Effect{action.EffectRead}, Paths: []string{clean}}, nil
}

func (t *readTool) Execute(ctx context.Context, prepared action.Prepared) action.Execution {
	var args readArgs
	_ = json.Unmarshal(prepared.Arguments, &args)
	f, clean, err := t.fs.Read(args.Path)
	if err != nil {
		return toolError(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return toolError(err)
	}
	if !info.Mode().IsRegular() {
		return action.Execution{Outcome: transcript.ToolResultError, Content: "read target is not a regular file"}
	}
	if dataproj.ProtectedPath(clean) {
		hash := sha256.New()
		buffer := make([]byte, 32*1024)
		for {
			if err := ctx.Err(); err != nil {
				return cancelled()
			}
			n, readErr := f.Read(buffer)
			if n > 0 {
				_, _ = hash.Write(buffer[:n])
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				return toolError(readErr)
			}
		}
		return action.Execution{Outcome: transcript.ToolResultSuccess, Content: dataproj.ProtectedProjectionDigest(clean, info.Size(), info.Mode().String(), hex.EncodeToString(hash.Sum(nil)))}
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	redactor := dataproj.NewLineRedactor(t.projector.Detector())
	var out strings.Builder
	lineNo := 0
	written := 0
	truncated := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return cancelled()
		}
		lineNo++
		line := scanner.Text()
		if !dataproj.IsText([]byte(line)) {
			return action.Execution{Outcome: transcript.ToolResultError, Content: "binary or invalid UTF-8 file is not readable"}
		}
		projected, _ := redactor.Redact(line)
		if lineNo < args.StartLine {
			continue
		}
		if args.EndLine != 0 && lineNo > args.EndLine {
			break
		}
		entry := strconv.Itoa(lineNo) + ": " + projected + "\n"
		if written >= 1000 || out.Len()+len(entry) > maxResultBytes {
			truncated = true
			break
		}
		out.WriteString(entry)
		written++
	}
	if err := scanner.Err(); err != nil {
		return action.Execution{Outcome: transcript.ToolResultError, Content: "file contains an oversized line or could not be read"}
	}
	if truncated {
		_, _ = fmt.Fprintf(&out, "[truncated=true; continue with start_line=%d]", lineNo)
	}
	return action.Execution{Outcome: transcript.ToolResultSuccess, Content: strings.TrimSuffix(out.String(), "\n")}
}

type listArgs struct {
	Path      string `json:"path,omitempty"`
	Recursive *bool  `json:"recursive,omitempty"`
	After     string `json:"after,omitempty"`
}

type listTool struct{ base }

func (t *listTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{Name: "list", Description: "List workspace-relative repository paths in deterministic lexical order.", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"path":{"type":"string"},"recursive":{"type":"boolean"},"after":{"type":"string"}}}`)}
}

func (t *listTool) Prepare(ctx context.Context, raw json.RawMessage) (action.Prepared, error) {
	var args listArgs
	if err := decodeArgs(raw, &args); err != nil {
		return action.Prepared{}, err
	}
	if args.Path == "" {
		args.Path = "."
	}
	clean, err := t.fs.Clean(args.Path)
	if err != nil {
		return action.Prepared{}, err
	}
	args.Path = clean
	if args.After != "" {
		if _, err := t.fs.Clean(args.After); err != nil {
			return action.Prepared{}, errors.New("after must be a workspace-relative path")
		}
	}
	effective, _ := json.Marshal(args)
	return action.Prepared{Tool: "list", Arguments: effective, Effects: []action.Effect{action.EffectRead}, Paths: []string{clean}}, nil
}

func (t *listTool) Execute(ctx context.Context, prepared action.Prepared) action.Execution {
	var args listArgs
	_ = json.Unmarshal(prepared.Arguments, &args)
	if err := ctx.Err(); err != nil {
		return cancelled()
	}
	walkFS, _, err := t.fs.List(args.Path)
	if err != nil {
		return toolError(err)
	}
	recursive := true
	if args.Recursive != nil {
		recursive = *args.Recursive
	}
	var entries []string
	walkErr := fs.WalkDir(walkFS, args.Path, func(name string, entry fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		name = path.Clean(name)
		if name != args.Path && name > args.After {
			if entry.IsDir() {
				entries = append(entries, name+"/")
			} else {
				entries = append(entries, name)
			}
		}
		if entry.IsDir() && name != args.Path {
			if dataproj.ProtectedPath(name) || !recursive {
				return fs.SkipDir
			}
		}
		return nil
	})
	if errors.Is(walkErr, context.Canceled) {
		return cancelled()
	}
	if walkErr != nil {
		return toolError(walkErr)
	}
	sort.Strings(entries)
	var out strings.Builder
	truncated := false
	next := ""
	for i, entry := range entries {
		line := entry + "\n"
		if i >= 2000 || out.Len()+len(line) > maxResultBytes {
			truncated = true
			next = entries[i-1]
			break
		}
		out.WriteString(line)
	}
	if truncated {
		_, _ = fmt.Fprintf(&out, "[truncated=true; continue with after=%q]", next)
	}
	return action.Execution{Outcome: transcript.ToolResultSuccess, Content: strings.TrimSuffix(out.String(), "\n")}
}

type searchArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type searchTool struct{ base }

func (t *searchTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{Name: "search", Description: "Search UTF-8 workspace files using Go regular expressions; results include path and line number.", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["pattern"],"properties":{"pattern":{"type":"string"},"path":{"type":"string"}}}`)}
}

func (t *searchTool) Prepare(ctx context.Context, raw json.RawMessage) (action.Prepared, error) {
	var args searchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return action.Prepared{}, err
	}
	if args.Pattern == "" {
		return action.Prepared{}, errors.New("pattern is required")
	}
	if _, err := regexp.Compile(args.Pattern); err != nil {
		return action.Prepared{}, errors.New("pattern is not a valid Go regular expression")
	}
	if args.Path == "" {
		args.Path = "."
	}
	clean, err := t.fs.Clean(args.Path)
	if err != nil {
		return action.Prepared{}, err
	}
	args.Path = clean
	effective, _ := json.Marshal(args)
	return action.Prepared{Tool: "search", Arguments: effective, Effects: []action.Effect{action.EffectRead}, Paths: []string{clean}}, nil
}

func (t *searchTool) Execute(ctx context.Context, prepared action.Prepared) action.Execution {
	var args searchArgs
	_ = json.Unmarshal(prepared.Arguments, &args)
	if err := ctx.Err(); err != nil {
		return cancelled()
	}
	walkFS, _, err := t.fs.Search(args.Path)
	if err != nil {
		return toolError(err)
	}
	re := regexp.MustCompile(args.Pattern)
	var out strings.Builder
	matches := 0
	truncated := false
	protectedSkipped := 0
	nonTextSkipped := 0
	walkErr := fs.WalkDir(walkFS, args.Path, func(name string, entry fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		name = path.Clean(name)
		if entry.IsDir() {
			if name != args.Path && dataproj.ProtectedPath(name) {
				protectedSkipped++
				return fs.SkipDir
			}
			return nil
		}
		if dataproj.ProtectedPath(name) {
			protectedSkipped++
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			nonTextSkipped++
			return nil
		}
		f, _, err := t.fs.Read(name)
		if err != nil {
			return err
		}
		return func() error {
			defer func() { _ = f.Close() }()
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)
			redactor := dataproj.NewLineRedactor(t.projector.Detector())
			lineNo := 0
			for scanner.Scan() {
				if err := ctx.Err(); err != nil {
					return err
				}
				lineNo++
				line := scanner.Text()
				if !dataproj.IsText([]byte(line)) {
					nonTextSkipped++
					return nil
				}
				projected, _ := redactor.Redact(line)
				if !re.MatchString(projected) {
					continue
				}
				excerpt := strings.TrimSpace(projected)
				entryText := fmt.Sprintf("%s:%d: %s\n", name, lineNo, excerpt)
				if matches >= 200 || out.Len()+len(entryText) > maxResultBytes {
					truncated = true
					return fs.SkipAll
				}
				out.WriteString(entryText)
				matches++
			}
			return scanner.Err()
		}()
	})
	if errors.Is(walkErr, context.Canceled) {
		return cancelled()
	}
	if walkErr != nil {
		return toolError(walkErr)
	}
	if protectedSkipped > 0 {
		_, _ = fmt.Fprintf(&out, "[protected_paths_skipped=%d]\n", protectedSkipped)
	}
	if nonTextSkipped > 0 {
		_, _ = fmt.Fprintf(&out, "[binary_or_nonregular_paths_skipped=%d]\n", nonTextSkipped)
	}
	if truncated {
		out.WriteString("[truncated=true; narrow the path or pattern]\n")
	}
	return action.Execution{Outcome: transcript.ToolResultSuccess, Content: strings.TrimSuffix(out.String(), "\n")}
}

func decodeArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return errors.New("arguments must be a JSON object")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return errors.New("arguments do not match the tool schema")
	}
	if dec.More() {
		return errors.New("arguments contain trailing data")
	}
	return nil
}

func toolError(err error) action.Execution {
	content := "workspace read failed"
	switch {
	case errors.Is(err, workspace.ErrEscape):
		content = "path is outside the workspace or crosses a disallowed symlink"
	case errors.Is(err, fs.ErrNotExist):
		content = "path does not exist"
	case errors.Is(err, fs.ErrPermission):
		content = "path is unreadable"
	}
	return action.Execution{Outcome: transcript.ToolResultError, Content: content}
}

func cancelled() action.Execution {
	return action.Execution{Outcome: transcript.ToolResultCancelled, Content: "tool call cancelled"}
}
