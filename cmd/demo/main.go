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
	if len(os.Args) > 1 && os.Args[1] == "permission" {
		os.Exit(runPermission())
	}
	os.Exit(runInteractive())
}

// runPermission is the Phase 3 "human in the loop" readout: three headline
// scenarios driven by a scripted provider and scripted answers — no credentials,
// no network, reproducible in CI.
//
//	go run ./cmd/demo permission
func runPermission() int {
	fmt.Println("=== Permission demo (scripted provider + scripted answers) ===")

	// Scenario 1: first prompt, then auto-allow after "remember".
	fmt.Println("\n[1] default mode: prompt once, remember, then run silently")
	cmds := func(ids ...string) []testutil.ScriptedReply {
		reps := make([]testutil.ScriptedReply, 0, len(ids)+1)
		for i, c := range ids {
			reps = append(reps, testutil.ScriptedReply{
				ToolCalls: []testutil.ScriptedToolCall{{ID: fmt.Sprintf("c%d", i+1), Name: "run_command", Arguments: fmt.Sprintf(`{"command":%q}`, c)}},
				EndReason: agent.StoppedToCallTools,
			})
		}
		return append(reps, testutil.ScriptedReply{TextDeltas: []string{"done"}, EndReason: agent.Finished})
	}
	s1 := agent.NewSession(agent.SessionConfig{
		Provider:     testutil.NewFakeProvider(cmds("git status", "git status --short")),
		SystemPrompt: "sys",
	})
	prompts := 0
	drainAnswering(mustEvents(s1.Run(context.Background(), "go")), func(req *agent.PermissionRequest) agent.PermissionDecision {
		prompts++
		fmt.Printf("    prompt: %s\n", req.Reason)
		return agent.PermissionDecision{Allow: true, Remember: true}
	})
	fmt.Printf("    → prompted %d time(s) for two git-status calls (remember silenced the second)\n", prompts)

	// Scenario 2: plan mode refuses a write with its reason.
	fmt.Println("\n[2] plan mode: a write is refused with a stated reason")
	s2 := agent.NewSession(agent.SessionConfig{
		Provider: testutil.NewFakeProvider([]testutil.ScriptedReply{
			{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"x.txt","content":"hi"}`}}, EndReason: agent.StoppedToCallTools},
			{TextDeltas: []string{"ok"}, EndReason: agent.Finished},
		}),
		SystemPrompt:   "sys",
		PermissionMode: "plan",
	})
	drainAnswering(mustEvents(s2.Run(context.Background(), "go")), denyNever)

	// Scenario 3: bypass allows with no prompt.
	fmt.Println("\n[3] bypass mode: a command runs with no prompt")
	s3 := agent.NewSession(agent.SessionConfig{
		Provider: testutil.NewFakeProvider([]testutil.ScriptedReply{
			{ToolCalls: []testutil.ScriptedToolCall{{ID: "c1", Name: "run_command", Arguments: `{"command":"echo trusted"}`}}, EndReason: agent.StoppedToCallTools},
			{TextDeltas: []string{"ok"}, EndReason: agent.Finished},
		}),
		SystemPrompt:   "sys",
		PermissionMode: "bypass",
	})
	bypassPrompts := 0
	drainAnswering(mustEvents(s3.Run(context.Background(), "go")), func(*agent.PermissionRequest) agent.PermissionDecision {
		bypassPrompts++
		return agent.PermissionDecision{Allow: true}
	})
	fmt.Printf("    → prompted %d time(s) under bypass\n", bypassPrompts)

	fmt.Println("\n--- permission demo complete ---")
	return 0
}

// mustEvents panics on a run-start error (the scripted demo never legitimately
// fails to start).
func mustEvents(ch <-chan agent.RunEvent, err error) <-chan agent.RunEvent {
	if err != nil {
		fmt.Fprintf(os.Stderr, "start run: %v\n", err)
		os.Exit(1)
	}
	return ch
}

// denyNever answers nothing useful — used where the engine resolves without ever
// prompting (plan/bypass), so any prompt would be a bug; it allows defensively.
func denyNever(*agent.PermissionRequest) agent.PermissionDecision {
	return agent.PermissionDecision{Allow: true}
}

// drainAnswering prints the stream and answers each permission request via decide.
func drainAnswering(events <-chan agent.RunEvent, decide func(*agent.PermissionRequest) agent.PermissionDecision) {
	for ev := range events {
		switch ev.Type {
		case agent.ToolStartedEvent:
			fmt.Printf("    [tool start: %s]\n", ev.ToolCall.ToolName)
		case agent.ToolFinishedEvent:
			status := "ok"
			if ev.ToolResult.IsError {
				status = "error"
			}
			fmt.Printf("    [tool finish (%s): %s]\n", status, ev.ToolResult.Result)
		case agent.PermissionRequestedEvent:
			ev.Permission.ReplyPath <- decide(ev.Permission)
		}
	}
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
	code := readout(events, autoApprove)
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
	code := readout(events, autoApprove)
	fmt.Println("--- stream closed ---")
	return code
}

// runInteractive is a multi-turn chat REPL against a real backend. One Session
// is reused so history accumulates across turns. Permission mode and rules load
// from settings, and each uncovered tool call prompts the human at the terminal.
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

	sc := agent.SessionConfig{
		Provider:               p,
		SystemPrompt:           "You are a concise coding assistant.",
		ExternalHooks:          settings.ExternalHooks(),
		PersistRememberedRules: true,
	}
	if settings.Permission != nil {
		sc.PermissionMode = settings.Permission.Mode
		sc.PermissionAllow = settings.Permission.Allow
		sc.PermissionDeny = settings.Permission.Deny
	}
	applySandboxSettings(&sc, settings)
	session, err := agent.NewSessionWithError(sc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create session: %v\n", err)
		return 1
	}

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
		readout(events, promptForPermission(scanner))
	}
}

func applySandboxSettings(sc *agent.SessionConfig, settings config.Settings) {
	if settings.Sandbox == nil {
		return
	}
	sc.SandboxExtraReadRoots = append([]string(nil), settings.Sandbox.ExtraReadRoots...)
	sc.SandboxExtraWriteRoots = append([]string(nil), settings.Sandbox.ExtraWriteRoots...)
	if settings.Sandbox.Network != nil {
		sc.SandboxNetwork = *settings.Sandbox.Network
	}
}

// autoApprove rules yes on every permission request with a one-line notice — used
// by the non-interactive readouts (fake, tools) where no human is at the keyboard.
func autoApprove(req *agent.PermissionRequest) agent.PermissionDecision {
	fmt.Printf("[permission asked for %s — auto-approving]\n", req.ToolCall.ToolName)
	return agent.PermissionDecision{Allow: true}
}

// promptForPermission asks the human at the terminal to rule on one call, reading
// a single line from the shared scanner. "always"/"never" also remember the
// decision as a rule; anything unrecognized (or EOF) denies once, erring safe.
func promptForPermission(scanner *bufio.Scanner) func(*agent.PermissionRequest) agent.PermissionDecision {
	return func(req *agent.PermissionRequest) agent.PermissionDecision {
		fmt.Printf("\n[permission] %s\n", req.Reason)
		if req.RememberedRule != "" {
			fmt.Printf("  (always/never will remember rule: %s)\n", req.RememberedRule)
		}
		fmt.Print("  allow? [y]es / [n]o / [a]lways / neve[r]: ")
		if !scanner.Scan() {
			return agent.PermissionDecision{Allow: false}
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "y", "yes":
			return agent.PermissionDecision{Allow: true}
		case "a", "always":
			return agent.PermissionDecision{Allow: true, Remember: true}
		case "r", "never":
			return agent.PermissionDecision{Allow: false, Remember: true}
		default:
			return agent.PermissionDecision{Allow: false}
		}
	}
}

// readout drains a run stream, printing each event and routing any
// wait-for-a-human permission request to decide. It returns a process exit code.
func readout(events <-chan agent.RunEvent, decide func(*agent.PermissionRequest) agent.PermissionDecision) int {
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
			ev.Permission.ReplyPath <- decide(ev.Permission)
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
