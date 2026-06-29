// Package main is the M1 "It talks" readout: a throwaway frontend that starts
// agent runs and prints the event stream as plain lines. It is not the TUI (that
// is Phase 7) — it exists to prove the loop end-to-end.
//
//	go run ./cmd/demo fake   # scripted model, no credentials, reproduces US-030
//	go run ./cmd/demo        # interactive multi-turn chat with a real backend
//
// In interactive mode one Session is reused across turns, so the model sees the
// full prior conversation each turn. Type "exit", "quit", or send EOF (Ctrl-D)
// to stop.
//
// TODO: interactive mode imports internal/config to load settings, and fake mode
// imports internal/provider/testutil (test scaffolding). The Provider itself is
// built via the public agent.NewOpenAIProvider. Once a public settings loader
// exists, the real path will import only pkg/agent.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blkcor/coragent/internal/config"
	"github.com/blkcor/coragent/internal/provider/testutil"
	"github.com/blkcor/coragent/pkg/agent"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "fake" {
		os.Exit(runFake())
	}
	if len(os.Args) > 1 && os.Args[1] == "tools" {
		os.Exit(runTools())
	}
	os.Exit(runInteractive())
}

// runTools drives all six built-in tools through the one execution path against a
// scripted provider — the M2 "It acts" readout. No credentials, no network: a
// fake model issues a tool call per round into a throwaway workspace, and each
// call travels the real executor chain (inert pre/permission/sandbox stages).
func runTools() int {
	work, err := os.MkdirTemp("", "coragent-demo-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "make workspace: %v\n", err)
		return 1
	}
	defer os.RemoveAll(work)
	file := filepath.Join(work, "notes.txt")

	steps := []struct {
		say  string
		tool string
		args map[string]interface{}
	}{
		{"Creating a file…", "write_file", map[string]interface{}{"path": file, "content": "hello\nworld\n", "create_parents": true}},
		{"Reading it back…", "read_file", map[string]interface{}{"path": file}},
		{"Making a precise edit…", "edit_file", map[string]interface{}{"path": file, "old_string": "world", "new_string": "coragent"}},
		{"Listing project files…", "find_files", map[string]interface{}{"pattern": "*.txt", "root": work}},
		{"Searching contents…", "search_content", map[string]interface{}{"pattern": "coragent", "path": work}},
		{"Running a command…", "run_command", map[string]interface{}{"command": "echo all six tools exercised"}},
	}

	replies := make([]testutil.ScriptedReply, 0, len(steps)+1)
	for i, s := range steps {
		args, _ := json.Marshal(s.args)
		replies = append(replies, testutil.ScriptedReply{
			TextDeltas: []string{s.say},
			ToolCalls:  []testutil.ScriptedToolCall{{ID: fmt.Sprintf("c%d", i+1), Name: s.tool, Arguments: string(args)}},
			EndReason:  agent.StoppedToCallTools,
		})
	}
	replies = append(replies, testutil.ScriptedReply{TextDeltas: []string{"Done — all six built-ins ran through the one path."}, EndReason: agent.Finished})

	fmt.Printf("Using fake provider; workspace %s\n", work)
	session := agent.NewSession(agent.SessionConfig{
		Provider:     testutil.NewFakeProvider(replies),
		SystemPrompt: "You are a coding assistant exercising the built-in tools.",
	})

	events, err := session.Run(context.Background(), "Exercise every built-in tool.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "start run: %v\n", err)
		return 1
	}
	code := readout(events)
	fmt.Println("--- stream closed ---")
	return code
}

// runFake plays the canned headline scenario against the scripted provider.
func runFake() int {
	p := testutil.NewFakeProvider([]testutil.ScriptedReply{
		{
			TextDeltas: []string{"Let me ", "read that file."},
			ToolCalls:  []testutil.ScriptedToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			EndReason:  agent.StoppedToCallTools,
		},
		{TextDeltas: []string{"Done — ", "the file is a placeholder."}, EndReason: agent.Finished},
	})
	fmt.Println("Using fake provider (no credentials needed)")

	session := agent.NewSession(agent.SessionConfig{
		Provider:     p,
		SystemPrompt: "You are a concise coding assistant.",
	})

	input := "Please read a.txt and summarize it."
	fmt.Printf("\n--- Run: %q ---\n", input)
	events, err := session.Run(context.Background(), input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start run: %v\n", err)
		return 1
	}
	code := readout(events)
	fmt.Println("--- stream closed ---")
	return code
}

// runInteractive is a multi-turn chat REPL against a real backend. One Session
// is reused so history accumulates across turns.
func runInteractive() int {
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load settings: %v\n", err)
		return 1
	}
	if settings.Model == nil {
		fmt.Fprintln(os.Stderr, "No model settings configured")
		return 1
	}
	p := agent.NewOpenAIProvider(settings.Model.BaseURL, settings.Model.APIKey, settings.Model.Name)
	fmt.Printf("Using real endpoint: %s (model %s)\n", settings.Model.BaseURL, settings.Model.Name)
	fmt.Println(`Multi-turn chat. Type "exit" or "quit" (or Ctrl-D) to stop.`)

	session := agent.NewSession(agent.SessionConfig{
		Provider:     p,
		SystemPrompt: "You are a concise coding assistant.",
	})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Print("\nyou> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "\nread input: %v\n", err)
				return 1
			}
			fmt.Println("\n--- bye ---")
			return 0
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Println("--- bye ---")
			return 0
		}

		events, err := session.Run(context.Background(), input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "start run: %v\n", err)
			continue
		}
		fmt.Print("agent> ")
		readout(events)
	}
}

// readout drains a run stream, printing each event, auto-approving any
// wait-for-a-human question. It returns a process exit code.
func readout(events <-chan agent.RunEvent) int {
	exitCode := 0
	for ev := range events {
		switch ev.Type {
		case agent.StatusChange:
			// Status is advisory; keep the chat readable by showing only tool work.
			if ev.Status == agent.StatusCallingTool {
				fmt.Printf("\n[status: %s]\n", ev.Status)
			}
		case agent.TextDelta:
			fmt.Print(ev.TextDelta)
		case agent.ToolStartedEvent:
			fmt.Printf("\n[tool start: %s(%v)]\n", ev.ToolCall.ToolName, ev.ToolCall.Arguments)
		case agent.ToolFinishedEvent:
			status := "ok"
			if ev.ToolResult.IsError {
				status = "error"
			}
			fmt.Printf("[tool finish (%s): %s]\n", status, ev.ToolResult.Result)
		case agent.PermissionRequestedEvent:
			fmt.Printf("[permission asked for %s — auto-approving]\n", ev.Permission.ToolCall.ToolName)
			ev.Permission.ReplyPath <- agent.PermissionDecision{Allow: true}
		case agent.OverBudgetWarningEvent:
			fmt.Printf("\n[warning: %s]\n", ev.Warning)
		case agent.RunFinishedEvent:
			if ev.RunFinished.Reason != agent.StopCompleted {
				fmt.Printf("\n[run finished: %s]", stopReasonLabel(ev.RunFinished))
			}
			fmt.Println()
			if ev.RunFinished.Reason.IsError() {
				exitCode = 1
			}
		case agent.ErrorEvent:
			fmt.Fprintf(os.Stderr, "\n[error: %v]\n", ev.Error)
		}
	}
	return exitCode
}

func stopReasonLabel(f *agent.RunFinished) string {
	switch f.Reason {
	case agent.StopCompleted:
		return "completed"
	case agent.StopReachedStepLimit:
		return "reached the step limit"
	case agent.StopCancelled:
		return "cancelled"
	case agent.StopFailed:
		return fmt.Sprintf("failed: %v", f.Err)
	default:
		return "unknown"
	}
}
