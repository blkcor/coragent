package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/blkcor/coragent/internal/core"
)

// defaultShellTimeout bounds a command that does not specify its own budget, so a
// runaway process never freezes the session.
const defaultShellTimeout = 30 * time.Second

// ShellCommand is the built-in that runs a shell command, returning its combined
// output and exit code. It is the single built-in that declares RunsCommands, so
// it is the one routed through the sandbox stage (Phase 5's OS confinement).
type ShellCommand struct{}

func (ShellCommand) Descriptor() core.Tool {
	return core.Tool{
		Name: "run_command",
		Description: "Run a shell command and return its combined stdout+stderr and exit code. " +
			"Optionally pass timeout_ms to bound how long it may run.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "The shell command to run."},
				"timeout_ms": {"type": "integer", "description": "Time budget in milliseconds."}
			},
			"required": ["command"]
		}`),
	}
}

func (ShellCommand) RunsCommands() bool { return true }

func (ShellCommand) ActionKind() core.ActionKind { return core.ActionCommand }

// PreviewAction resolves the same timeout ExecuteCommand will use while never
// constructing or launching a process.
func (ShellCommand) PreviewAction(_ context.Context, args map[string]interface{}) (core.ActionPreview, error) {
	command, ok := stringArg(args, "command")
	if !ok || command == "" {
		return core.ActionPreview{}, fmt.Errorf("run_command: command is required")
	}
	timeout := defaultShellTimeout
	if ms, exists := intArg(args, "timeout_ms"); exists && ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}
	return core.ActionPreview{
		Kind:      core.ActionPreviewText,
		Operation: core.ActionOperationCommand,
		Summary:   "Run a shell command",
		Text:      fmt.Sprintf("%s\ntimeout: %s", command, timeout),
		Metadata:  map[string]string{"timeout": timeout.String()},
	}, nil
}

func (ShellCommand) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return (ShellCommand{}).ExecuteCommand(ctx, args, directCommandRunner{})
}

func (ShellCommand) ExecuteCommand(ctx context.Context, args map[string]interface{}, runner core.CommandRunner) (string, error) {
	command, ok := stringArg(args, "command")
	if !ok || command == "" {
		return "", fmt.Errorf("run_command: command is required")
	}

	timeout := defaultShellTimeout
	if ms, ok := intArg(args, "timeout_ms"); ok && ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}
	return runner.Run(ctx, core.CommandSpec{Command: command, Timeout: timeout})
}

type directCommandRunner struct{}

func (directCommandRunner) Run(ctx context.Context, spec core.CommandSpec) (string, error) {
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultShellTimeout
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "sh", "-c", spec.Command)
	// Put the child in its own process group so we can kill the whole group on
	// timeout or cancellation — no orphaned grandchildren left behind.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	output := buf.String()

	// Parent cancellation wins over a timeout: report it and return any partial
	// output. The executor surfaces this as an error result; the loop's own
	// cancellation precedence governs the run outcome.
	if ctx.Err() != nil {
		return compose(output, "[cancelled]"), fmt.Errorf("run_command: cancelled: %w", ctx.Err())
	}
	if cctx.Err() == context.DeadlineExceeded {
		note := fmt.Sprintf("[timed out after %s; exit code: -1]", timeout)
		return compose(output, note), fmt.Errorf("run_command: timed out after %s", timeout)
	}

	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if runErr != nil {
		// Failure to start the process (e.g. shell missing) — no exit code.
		return compose(output, "[error starting command]"), fmt.Errorf("run_command: %w", runErr)
	}

	text := compose(output, fmt.Sprintf("[exit code: %d]", exitCode))
	if exitCode != 0 {
		return text, fmt.Errorf("run_command: exited with code %d", exitCode)
	}
	return text, nil
}

var _ core.CommandToolHandler = ShellCommand{}
var _ core.ActionPreviewer = ShellCommand{}

// compose joins captured output with a trailing status note, keeping the note
// present even when the command produced no output.
func compose(output, note string) string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return note
	}
	return output + "\n" + note
}
