package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/blkcor/coragent/internal/core"
)

// ReadFile is the built-in that reads a file by path, optionally a window of
// lines, returning line-referenced contents the model can cite. It is a pure file
// operation and does not run commands.
type ReadFile struct{}

func (ReadFile) Descriptor() core.Tool {
	return core.Tool{
		Name: "read_file",
		Description: "Read a file's contents by path, returned with line numbers. " +
			"Optionally pass offset (1-based start line) and limit (max lines) to read a window.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Path to the file to read."},
				"offset": {"type": "integer", "description": "1-based line to start from."},
				"limit": {"type": "integer", "description": "Maximum number of lines to return."}
			},
			"required": ["path"]
		}`),
	}
}

func (ReadFile) RunsCommands() bool { return false }

func (ReadFile) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	path, ok := stringArg(args, "path")
	if !ok || path == "" {
		return "", fmt.Errorf("read_file: path is required")
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read_file: %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read_file: %s is a directory, not a file", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: %s: %w", path, err)
	}
	if isBinary(data) {
		return "", fmt.Errorf("read_file: %s appears to be a binary file", path)
	}

	lines := strings.Split(string(data), "\n")
	// A trailing newline yields a final empty element; drop it so line counts
	// match the file's visible lines.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	start := 1
	if off, ok := intArg(args, "offset"); ok {
		if off < 1 {
			return "", fmt.Errorf("read_file: offset must be 1 or greater, got %d", off)
		}
		start = off
	}
	if start > len(lines) {
		return "", fmt.Errorf("read_file: offset %d is past end of file (%d lines)", start, len(lines))
	}

	end := len(lines)
	if lim, ok := intArg(args, "limit"); ok && lim >= 0 {
		if start-1+lim < end {
			end = start - 1 + lim
		}
	}

	var b strings.Builder
	for i := start - 1; i < end; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
	}
	return b.String(), nil
}

// isBinary reports whether data looks like a binary file. A NUL byte in the first
// chunk is a reliable, cheap signal that the bytes are not text.
func isBinary(data []byte) bool {
	const sniff = 8000
	if len(data) > sniff {
		data = data[:sniff]
	}
	return bytes.IndexByte(data, 0) >= 0
}
