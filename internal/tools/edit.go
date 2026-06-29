package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/blkcor/coragent/internal/core"
)

// EditFile is the built-in that replaces an exact snippet in a file. Its
// uniqueness contract is the key safety property: an ambiguous match is rejected,
// never guessed, leaving the file byte-for-byte unchanged. It is a pure file
// operation.
type EditFile struct{}

func (EditFile) Descriptor() core.Tool {
	return core.Tool{
		Name: "edit_file",
		Description: "Replace an exact snippet in a file. old_string must match exactly " +
			"once unless replace_all is true. Fails (leaving the file unchanged) if the " +
			"snippet is missing, ambiguous, or identical to new_string.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Path to the file to edit."},
				"old_string": {"type": "string", "description": "Exact snippet to replace."},
				"new_string": {"type": "string", "description": "Replacement text."},
				"replace_all": {"type": "boolean", "description": "Replace every occurrence."}
			},
			"required": ["path", "old_string", "new_string"]
		}`),
	}
}

func (EditFile) RunsCommands() bool { return false }

func (EditFile) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	path, ok := stringArg(args, "path")
	if !ok || path == "" {
		return "", fmt.Errorf("edit_file: path is required")
	}
	oldStr, ok := stringArg(args, "old_string")
	if !ok {
		return "", fmt.Errorf("edit_file: old_string is required")
	}
	newStr, ok := stringArg(args, "new_string")
	if !ok {
		return "", fmt.Errorf("edit_file: new_string is required")
	}
	replaceAll := boolArg(args, "replace_all")

	if oldStr == newStr {
		return "", fmt.Errorf("edit_file: old_string and new_string are identical; nothing to do")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %s: %w", path, err)
	}
	content := string(data)

	count := strings.Count(content, oldStr)
	switch {
	case count == 0:
		return "", fmt.Errorf("edit_file: old_string not found in %s; file unchanged", path)
	case count > 1 && !replaceAll:
		return "", fmt.Errorf("edit_file: old_string is ambiguous — it appears %d times in %s; "+
			"set replace_all to replace every occurrence; file unchanged", count, path)
	}

	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		updated = strings.Replace(content, oldStr, newStr, 1)
	}

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("edit_file: write %s: %w", path, err)
	}

	replaced := 1
	if replaceAll {
		replaced = count
	}
	return fmt.Sprintf("edited %s (%d replacement(s))", path, replaced), nil
}
