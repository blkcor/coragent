package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/blkcor/coragent/internal/core"
)

// SearchContent is the built-in that searches file contents by pattern, returning
// located `file:line:match` results. It shells out to ripgrep (rg) but is treated
// as a trusted read-only operation, so it does not declare RunsCommands and skips
// the sandbox stage.
type SearchContent struct{}

func (SearchContent) Descriptor() core.Tool {
	return core.Tool{
		Name: "search_content",
		Description: "Search file contents for a pattern and return file:line:match results. " +
			"Scope with path, glob (file filter), and ignore_case.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern": {"type": "string", "description": "The pattern to search for."},
				"path": {"type": "string", "description": "Directory or file to search (default '.')."},
				"glob": {"type": "string", "description": "Only search files matching this glob."},
				"ignore_case": {"type": "boolean", "description": "Case-insensitive search."}
			},
			"required": ["pattern"]
		}`),
	}
}

func (SearchContent) RunsCommands() bool { return false }

func (SearchContent) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	pattern, ok := stringArg(args, "pattern")
	if !ok || pattern == "" {
		return "", fmt.Errorf("search_content: pattern is required")
	}
	if _, err := exec.LookPath("rg"); err != nil {
		return "", fmt.Errorf("search_content: ripgrep (rg) not found in PATH; install it to search file contents")
	}

	path := "."
	if p, ok := stringArg(args, "path"); ok && p != "" {
		path = p
	}

	rgArgs := []string{"--no-heading", "--line-number", "--color", "never"}
	if boolArg(args, "ignore_case") {
		rgArgs = append(rgArgs, "-i")
	}
	if glob, ok := stringArg(args, "glob"); ok && glob != "" {
		rgArgs = append(rgArgs, "--glob", glob)
	}
	rgArgs = append(rgArgs, "--", pattern, path)

	cmd := exec.CommandContext(ctx, "rg", rgArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return strings.TrimRight(stdout.String(), "\n"), nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			// rg exit 1 means "no matches" — a successful finding, not an error.
			return "no matches", nil
		default:
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return "", fmt.Errorf("search_content: %s", msg)
		}
	}
	return "", fmt.Errorf("search_content: %w", err)
}
