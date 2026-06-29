package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blkcor/coragent/internal/core"
)

// WriteFile is the built-in that creates or replaces a whole file by path,
// optionally creating missing parent folders. It is a pure file operation.
type WriteFile struct{}

func (WriteFile) Descriptor() core.Tool {
	return core.Tool{
		Name: "write_file",
		Description: "Create or replace a file with the given content. " +
			"Set create_parents to true to create missing parent directories.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Path to write."},
				"content": {"type": "string", "description": "Full file contents."},
				"create_parents": {"type": "boolean", "description": "Create missing parent directories."}
			},
			"required": ["path", "content"]
		}`),
	}
}

func (WriteFile) RunsCommands() bool { return false }

func (WriteFile) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	path, ok := stringArg(args, "path")
	if !ok || path == "" {
		return "", fmt.Errorf("write_file: path is required")
	}
	content, ok := stringArg(args, "content")
	if !ok {
		return "", fmt.Errorf("write_file: content is required")
	}

	if boolArg(args, "create_parents") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("write_file: create parents for %s: %w", path, err)
		}
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %s: %w", path, err)
	}

	// Concise confirmation — never echo the content back.
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}
