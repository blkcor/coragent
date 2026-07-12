package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTranscriptStoreIndexesRichBlocksCachesAndSettles(t *testing.T) {
	store := NewTranscriptStore()
	at := time.Unix(100, 0)
	store.AddUser("do it", at)
	if err := store.StartRun("run-1"); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := store.StartAssistant("assistant-1", at); err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	if err := store.AppendReasoning("assistant-1", "public summary", at); err != nil {
		t.Fatalf("AppendReasoning: %v", err)
	}
	if err := store.StartTool("call-1", "write_file", `{"path":"safe.txt"}`, at); err != nil {
		t.Fatalf("StartTool: %v", err)
	}
	prompt := PermissionPrompt{RequestID: "request-1", CallID: "call-1", Revision: 2, Tool: "write_file"}
	if err := store.AwaitPermission(prompt, at); err != nil {
		t.Fatalf("AwaitPermission: %v", err)
	}
	agent := SubagentLifecycle{AgentID: "agent-2", ParentAgentID: "agent-1", DelegationCallID: "call-1", Label: "review", Depth: 1}
	if err := store.StartSubagent(agent, at); err != nil {
		t.Fatalf("StartSubagent: %v", err)
	}

	if got := store.runIndex["run-1"]; len(got) != 4 {
		t.Fatalf("run index = %v, want user + assistant + tool + subagent", got)
	}
	if store.callIndex["call-1"] != store.requestIndex["request-1"] {
		t.Fatal("call and request indexes do not resolve to one tool block")
	}
	if _, ok := store.agentIndex["agent-2"]; !ok {
		t.Fatal("agent index missing")
	}

	if err := store.FinishAssistantWithReason("assistant-1", "stop"); err != nil {
		t.Fatalf("FinishAssistant: %v", err)
	}
	theme := ThemeForMode(NoColorMode())
	store.RenderLines(theme, 80, 0)
	cacheEntries := len(store.renderCache)
	store.RenderLines(theme, 80, 99)
	if cacheEntries == 0 || len(store.renderCache) != cacheEntries {
		t.Fatalf("completed block render cache changed on frame-only render: %d -> %d", cacheEntries, len(store.renderCache))
	}

	if inconsistent := store.SettleActive(RunCancelled); inconsistent {
		t.Fatal("cancelled terminal was reported inconsistent")
	}
	tool := store.Blocks[store.callIndex["call-1"]]
	if tool.ToolState != ToolWasCancelled {
		t.Fatalf("tool state = %v, want cancelled", tool.ToolState)
	}
	child := store.Blocks[store.agentIndex["agent-2"]]
	if child.SubagentOutcome != "cancelled" {
		t.Fatalf("subagent outcome = %q", child.SubagentOutcome)
	}
}

func TestRichTranscriptRenderingAndContextSemantics(t *testing.T) {
	model, _ := newReadyApp(t, 120, 36)
	at := time.Unix(200, 0)
	events := []UIEvent{
		{Kind: EventRunStarted, RunID: "run-rich", Timestamp: at},
		{Kind: EventAssistantStarted, RunID: "run-rich", AssistantID: "a-1", Timestamp: at},
		{Kind: EventAssistantReasoningSummaryDelta, RunID: "run-rich", AssistantID: "a-1", Text: "Checked the public constraints.", Timestamp: at},
		{Kind: EventAssistantTextDelta, RunID: "run-rich", AssistantID: "a-1", Text: "Done.", Timestamp: at},
		{Kind: EventAssistantFinished, RunID: "run-rich", AssistantID: "a-1", Termination: "stop", Timestamp: at},
		{Kind: EventToolStarted, RunID: "run-rich", CallID: "call-rich", ToolName: "edit_file", Arguments: `{"path":"安全.txt"}`, Timestamp: at},
		{Kind: EventToolPrepared, RunID: "run-rich", CallID: "call-rich", ToolName: "edit_file", Arguments: `{"path":"安全.txt"}`, Revision: 3, Preview: richDiffPreview(), Timestamp: at},
		{Kind: EventHookOutcome, RunID: "run-rich", CallID: "call-rich", Hook: &HookOutcome{CallID: "call-rich", Name: "policy", Moment: "before-tool", Action: "replaced", Reason: "normalized path"}, Timestamp: at},
		{Kind: EventToolExecuting, RunID: "run-rich", CallID: "call-rich", Revision: 3, Timestamp: at},
		{Kind: EventToolFinished, RunID: "run-rich", CallID: "call-rich", Revision: 3, Tool: ToolSucceeded, Result: "updated", Duration: 1250 * time.Millisecond, Timestamp: at},
		{Kind: EventContextUsage, RunID: "run-rich", Usage: &ContextUsage{Round: 1, Source: "estimated", Used: 8100, Window: OptionalCount{Known: true, Value: 10_000}}, Timestamp: at},
		{Kind: EventOmission, RunID: "run-rich", Omission: &Omission{Kind: "provider_length", Scope: "assistant_reply", CorrelationID: "a-1", Recoverability: "unrecoverable", Continuation: "new_user_turn"}, Timestamp: at},
		{Kind: EventSubagentStarted, RunID: "run-rich", Subagent: &SubagentLifecycle{AgentID: "child-1", ParentAgentID: "root", DelegationCallID: "delegate-1", Label: "review", Depth: 1}, Timestamp: at},
		{Kind: EventSubagentFinished, RunID: "run-rich", Subagent: &SubagentLifecycle{AgentID: "child-1", ParentAgentID: "root", DelegationCallID: "delegate-1", Label: "review", Depth: 1, Outcome: "completed"}, Timestamp: at.Add(time.Second)},
		{Kind: EventRunFinished, RunID: "run-rich", Terminal: RunCompleted, Timestamp: at.Add(2 * time.Second)},
	}
	for _, event := range events {
		model.applyEvent(event)
	}

	view := ansi.Strip(model.render())
	for _, want := range []string{"reasoning summary", "edit_file", "normalized path", "1.3s", "subagent review", "81% · 8.1k est"} {
		if !strings.Contains(view, want) {
			t.Fatalf("render missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Checked the public constraints") {
		t.Fatal("completed reasoning summary was not collapsed by default")
	}
	if model.runState != RunIdle || !model.pendingContinuation {
		t.Fatalf("terminal state=%v continuation=%v", model.runState, model.pendingContinuation)
	}
	model.composer.Reset()
	model.submitDraft()
	if got := model.composer.Value(); !strings.Contains(got, "continue") {
		t.Fatalf("continuation draft = %q", got)
	}
	if len(model.transcript.Blocks) != 3 {
		t.Fatalf("block count = %d, want user-free assistant/tool/subagent plus no duplicate hook", len(model.transcript.Blocks))
	}
	model.pendingContinuation = true
	model.composer.SetValue("keep my draft")
	model.submitDraft()
	if got := model.pendingSubmission; got != "keep my draft" {
		t.Fatalf("existing draft was overwritten instead of submitted explicitly: %q", got)
	}
}

func richDiffPreview() *ActionPreview {
	return &ActionPreview{
		Kind: "file_diff", Operation: "modify", Summary: "modify 安全.txt",
		FileDiff: &FileDiff{
			Path: "安全.txt", AddedLines: OptionalCount{Known: true, Value: 1}, RemovedLines: OptionalCount{Known: true, Value: 1},
			ChangedRegions: OptionalCount{Known: true, Value: 1},
			Hunks:          []DiffHunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []DiffLine{{Kind: "removed", Text: "old"}, {Kind: "added", Text: "new"}}}},
		},
	}
}

func TestRichPermissionRevisionGrantRememberAndDraftRetention(t *testing.T) {
	model, _ := newReadyApp(t, 100, 30)
	model.applyEvent(UIEvent{Kind: EventRunStarted, RunID: "run-permission"})
	var (
		mu        sync.Mutex
		responses []PermissionResponse
	)
	prompt := PermissionPrompt{
		RequestID: "request-rich", CallID: "call-rich", Revision: 4, Tool: "run_shell",
		Action: "run command", Arguments: `{"command":"pwd"}`, Reason: "runs a command", Origin: "agent child · depth 1",
		Protocol: "rich", RememberScope: "command run_shell", StructuredPreview: &ActionPreview{Kind: "text", Operation: "command", Text: "pwd"},
		Capabilities: PermissionCapabilities{Allow: true, Deny: true, Remember: true, ReviseArguments: true, SchemaAwareEdit: true, Preview: true, SandboxGrants: true},
		GrantOptions: GrantOptions{Support: SupportSupported, ReadRoots: true, WriteRoots: true, Network: true},
		RichReply: func(_ context.Context, response PermissionResponse) (PermissionReplyResult, error) {
			mu.Lock()
			defer mu.Unlock()
			responses = append(responses, response)
			if response.Decision == DecisionReviseArguments {
				return PermissionReplyResult{Status: ReplyValidationRejected, Feedback: "command: rejected for test"}, nil
			}
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, RunID: "run-permission", CallID: "call-rich", Permission: &prompt})

	model.Update(typeKey("e"))
	if model.permission.View != permissionArguments {
		t.Fatal("argument editor did not open")
	}
	if model.View().Cursor == nil {
		t.Fatal("focused argument editor did not expose a real terminal caret")
	}
	model.permission.Editor.SetValue("{")
	_, command := model.Update(press('s', tea.ModCtrl))
	if command != nil || !strings.Contains(model.permission.Feedback, "arguments") {
		t.Fatalf("malformed JSON command=%v feedback=%q", command, model.permission.Feedback)
	}
	model.permission.Editor.SetValue(`{"command":"echo safe"}`)
	_, command = model.Update(press('s', tea.ModCtrl))
	if command == nil {
		t.Fatal("valid revision did not submit")
	}
	message := command()
	model.Update(message)
	if model.permission == nil || model.permission.View != permissionArguments || model.permission.Submitting {
		t.Fatal("validation rejection did not restore the argument editor")
	}
	if got := model.permission.Editor.Value(); !strings.Contains(got, "echo safe") {
		t.Fatalf("revision draft was lost: %q", got)
	}

	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model.Update(typeKey("s"))
	model.permission.Editor.SetValue(`{"read_roots":["/tmp"],"write_roots":[],"network":true}`)
	_, command = model.Update(press('s', tea.ModCtrl))
	if command != nil || model.permission.View != permissionDecision {
		t.Fatal("grant editor should save locally without approving")
	}
	_, command = model.Update(typeKey("A"))
	if command == nil {
		t.Fatal("allow-and-remember did not submit")
	}
	model.Update(command())
	if model.permission != nil {
		t.Fatal("accepted allow-and-remember did not close modal")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want revision + final allow", len(responses))
	}
	final := responses[1]
	if final.Decision != DecisionAllowRemember || !final.Remember || !final.Grants.Network || len(final.Grants.ReadRoots) != 1 {
		t.Fatalf("final rich response = %+v", final)
	}
}

func TestBypassConfirmationHelpInspectorAndUnknownContext(t *testing.T) {
	model, port := newReadyApp(t, 100, 30)
	model.info.Provider = "test-provider"
	model.info.PermissionOwner = "engine"
	model.info.Capabilities = []CapabilityCategory{{Kind: "skill", Support: SupportUnsupported}}

	// Cycle into bypass via shift+tab (Default → AutoAcceptEdits → Plan → Bypass).
	_, cmd := model.Update(shiftTab())
	model.Update(cmd())
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	if model.mode != ModeBypass || model.overlay != nil {
		t.Fatalf("bypass via shift+tab mode=%q overlay=%v", model.mode, model.overlay)
	}
	port.mu.Lock()
	if len(port.modes) != 3 || port.modes[2] != ModeBypass {
		t.Fatalf("mode requests = %v", port.modes)
	}
	port.mu.Unlock()

	model.Update(press('i', tea.ModCtrl))
	inspector := ansi.Strip(model.render())
	for _, want := range []string{"INSPECT / RUN LEDGER", "test-provider", "skill: unsupported", "not reported", "context: 0%"} {
		if !strings.Contains(inspector, want) {
			t.Fatalf("inspector missing %q:\n%s", want, inspector)
		}
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model.Update(tea.KeyPressMsg{Code: '/', Mod: tea.ModCtrl})
	help := ansi.Strip(model.render())
	if !strings.Contains(help, "Wheel  browse task history") || !strings.Contains(help, "Shift/Option+drag") {
		t.Fatalf("help lacks pointer-only history/copy fallback:\n%s", help)
	}

	model.overlay = nil
	model.usage = &ContextUsage{Source: "estimated", Used: 1234}
	label, level := model.contextUsageLabel()
	if label != "0.6% · 1.2k est" || level != 0 {
		t.Fatalf("estimated label = %q level=%d", label, level)
	}
	model.usage = &ContextUsage{Source: "provider", Used: 9500, Window: OptionalCount{Known: true, Value: 10_000}}
	if label, level = model.contextUsageLabel(); label != "95% · 9.5k" || level != 2 {
		t.Fatalf("95%% label = %q level=%d", label, level)
	}
}

func TestMarkdownNeutralizesHTMLImagesAndHyperlinkControls(t *testing.T) {
	theme := ThemeForMode(NoColorMode())
	lines := renderMarkdownLines(theme, `<img src=x onerror=bad> ![secret](https://bad.invalid/x) [safe label](javascript:bad)`, 80)
	got := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(got, "‹img src=x onerror=bad›") || !strings.Contains(got, "image omitted: secret") || !strings.Contains(got, "safe label") {
		t.Fatalf("neutralized markdown = %q", got)
	}
	if strings.Contains(strings.Join(lines, "\n"), "\x1b]") || strings.Contains(got, "javascript:bad") || strings.Contains(got, "https://bad.invalid") {
		t.Fatalf("unsafe link destination escaped filtering: %q", strings.Join(lines, "\n"))
	}
}

func TestReducerTransitionSnapshots(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	snapshot := func() string {
		permission := "none"
		if model.permission != nil {
			permission = fmt.Sprintf("%s/%d/%t", model.permission.Prompt.RequestID, model.permission.View, model.permission.Submitting)
		}
		last := "none"
		if len(model.transcript.Blocks) > 0 {
			block := model.transcript.Blocks[len(model.transcript.Blocks)-1]
			last = fmt.Sprintf("%d/%d/%s", block.Kind, block.ToolState, block.SubagentOutcome)
		}
		return fmt.Sprintf("run=%d activity=%s blocks=%d last=%s permission=%s", model.runState, model.activity, len(model.transcript.Blocks), last, permission)
	}

	want := []string{
		"run=1 activity=idle blocks=0 last=none permission=none",
		"run=2 activity=thinking blocks=0 last=none permission=none",
		"run=2 activity=thinking blocks=1 last=1/0/ permission=none",
		"run=2 activity=calling tool blocks=2 last=2/0/ permission=none",
		"run=2 activity=waiting for approval blocks=2 last=2/2/ permission=req/0/false",
		"run=2 activity=waiting for approval blocks=2 last=2/2/ permission=req/0/true",
		"run=2 activity=waiting for approval blocks=2 last=2/2/ permission=req/0/false",
		"run=1 activity=idle blocks=3 last=4/0/ permission=none",
	}
	got := []string{snapshot()}
	model.applyEvent(UIEvent{Kind: EventRunStarted, RunID: "run-snapshot"})
	got = append(got, snapshot())
	model.applyEvent(UIEvent{Kind: EventAssistantStarted, AssistantID: "assistant-snapshot"})
	model.applyEvent(UIEvent{Kind: EventAssistantReasoningSummaryDelta, AssistantID: "assistant-snapshot", Text: "summary"})
	got = append(got, snapshot())
	model.applyEvent(UIEvent{Kind: EventToolStarted, CallID: "call-snapshot", ToolName: "write_file"})
	got = append(got, snapshot())
	prompt := PermissionPrompt{
		RequestID: "req", CallID: "call-snapshot", Tool: "write_file",
		RichReply: func(context.Context, PermissionResponse) (PermissionReplyResult, error) {
			return PermissionReplyResult{Status: ReplyValidationRejected, Feedback: "retry"}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	got = append(got, snapshot())
	command := model.submitPermission(DecisionAllowOnce)
	got = append(got, snapshot())
	model.handlePermissionReply(command().(permissionReplyMsg))
	got = append(got, snapshot())
	model.applyEvent(UIEvent{Kind: EventRunFinished, Terminal: RunFailed, Err: "fixture failure"})
	got = append(got, snapshot())

	if len(got) != len(want) {
		t.Fatalf("snapshot count = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("snapshot %d\n got: %s\nwant: %s", index, got[index], want[index])
		}
	}
}

func TestEveryOmissionHasReasonSpecificHonestCopy(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"output_budget", "tool output tail omitted"},
		{"preview_budget", "action preview incomplete"},
		{"provider_length", "reply stopped at provider output limit"},
		{"content_filter", "content unavailable: provider filter"},
		{"redacted", "content redacted"},
		{"context_compaction", "history compacted"},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			omission := Omission{Kind: test.kind, Recoverability: "unrecoverable"}
			got := formatOmissionNotice(omission)
			if !strings.Contains(got, test.want) || !strings.Contains(got, "cannot expand") {
				t.Fatalf("copy = %q", got)
			}
			if strings.Contains(got, "0 bytes") || strings.Contains(got, "0 lines") {
				t.Fatalf("unknown count was fabricated: %q", got)
			}
		})
	}
}

func TestOperationalFailureFixtures(t *testing.T) {
	t.Run("startup error", func(t *testing.T) {
		model := NewAppModel(nil, WithVisualMode(NoColorMode()))
		model.handleStartup(startupLoadedMsg{Err: errors.New("bad settings")})
		if model.runState != RunStartupError || model.startupError == nil || !strings.Contains(model.composerPlaceholder(), "Startup failed") {
			t.Fatalf("startup state=%v error=%v placeholder=%q", model.runState, model.startupError, model.composerPlaceholder())
		}
	})

	t.Run("step limit", func(t *testing.T) {
		model, _ := newReadyApp(t, 80, 24)
		model.applyEvent(UIEvent{Kind: EventRunStarted, RunID: "run-limit"})
		model.applyEvent(UIEvent{Kind: EventAssistantStarted, AssistantID: "a-limit"})
		model.applyEvent(UIEvent{Kind: EventRunFinished, Terminal: RunReachedStepLimit})
		if model.runState != RunIdle || !strings.Contains(ansi.Strip(model.render()), "step limit") {
			t.Fatalf("step-limit render:\n%s", ansi.Strip(model.render()))
		}
	})

	t.Run("close error", func(t *testing.T) {
		model, port := newReadyApp(t, 80, 24)
		port.closeErr = errors.New("close failed")
		command := model.beginQuit()
		message := command()
		model.Update(message)
		if model.closeErr == nil || model.Status().Close == nil {
			t.Fatalf("close status = %+v", model.Status())
		}
	})

	t.Run("cancel fans out", func(t *testing.T) {
		model, _ := newReadyApp(t, 80, 24)
		model.applyEvent(UIEvent{Kind: EventRunStarted, RunID: "run-cancel"})
		model.applyEvent(UIEvent{Kind: EventAssistantStarted, AssistantID: "a-cancel"})
		model.applyEvent(UIEvent{Kind: EventAssistantReasoningSummaryDelta, AssistantID: "a-cancel", Text: "summary"})
		model.applyEvent(UIEvent{Kind: EventSubagentStarted, Subagent: &SubagentLifecycle{AgentID: "child-cancel", ParentAgentID: "root", DelegationCallID: "delegate", Label: "worker", Depth: 1}})
		model.applyEvent(UIEvent{Kind: EventRunFinished, Terminal: RunCancelled})
		if model.transcript.Blocks[0].Streaming || model.transcript.Blocks[0].ReasoningStreaming || model.transcript.Blocks[1].SubagentOutcome != "cancelled" || model.animationOn {
			t.Fatalf("cancel did not settle rich state: %+v %+v", model.transcript.Blocks[0], model.transcript.Blocks[1])
		}
	})
}

func TestPermissionDecisionKeysAndAlreadyResolved(t *testing.T) {
	tests := []struct {
		name     string
		key      tea.KeyPressMsg
		expected PermissionDecision
		remember bool
	}{
		{"allow once", typeKey("a"), DecisionAllowOnce, false},
		{"deny once", typeKey("d"), DecisionDenyOnce, false},
		{"allow remember", typeKey("A"), DecisionAllowRemember, true},
		{"deny remember", typeKey("D"), DecisionDenyRemember, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, _ := newReadyApp(t, 80, 24)
			var replies []PermissionResponse
			prompt := PermissionPrompt{
				RequestID: "request", CallID: "call", Tool: "tool", RememberScope: "edit tool",
				Capabilities: PermissionCapabilities{Allow: true, Deny: true, Remember: true},
				RichReply: func(_ context.Context, response PermissionResponse) (PermissionReplyResult, error) {
					replies = append(replies, response)
					return PermissionReplyResult{Status: ReplyAlreadyResolved}, nil
				},
			}
			model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
			_, command := model.Update(test.key)
			if command == nil || !model.permission.Submitting {
				t.Fatal("decision did not enter submission guard")
			}
			_, duplicate := model.Update(typeKey("d"))
			if duplicate != nil {
				t.Fatal("duplicate key escaped submission guard")
			}
			model.Update(command())
			if model.permission != nil || len(replies) != 1 {
				t.Fatalf("already-resolved reconciliation: modal=%v replies=%d", model.permission, len(replies))
			}
			if replies[0].Decision != test.expected || replies[0].Remember != test.remember {
				t.Fatalf("response = %+v", replies[0])
			}
		})
	}
}

func TestAcceptedRevisionShowsRepreparingWithoutStalePreview(t *testing.T) {
	model, _ := newReadyApp(t, 80, 24)
	prompt := PermissionPrompt{
		RequestID: "request-r1", CallID: "call-reprepare", Revision: 1, Tool: "edit_file", Arguments: `{}`,
		StructuredPreview: richDiffPreview(),
		Capabilities:      PermissionCapabilities{Allow: true, Deny: true, ReviseArguments: true, SchemaAwareEdit: true},
		RichReply: func(context.Context, PermissionResponse) (PermissionReplyResult, error) {
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
	model.Update(typeKey("e"))
	_, command := model.Update(press('s', tea.ModCtrl))
	model.Update(command())
	block := model.transcript.Blocks[model.transcript.callIndex["call-reprepare"]]
	if block.ToolState != ToolPreparing || block.Preview != nil || !strings.Contains(strings.Join(block.Notices, " "), "re-preparing") {
		t.Fatalf("re-preparing block = %+v", block)
	}
}

func TestQuitAndControlCFromPermissionEditorDenyExactlyOnce(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{press('q', tea.ModCtrl), press('c', tea.ModCtrl)} {
		name := key.String()
		t.Run(name, func(t *testing.T) {
			model, _ := newReadyApp(t, 80, 24)
			model.runState = RunRunning
			runContext, cancel := context.WithCancel(context.Background())
			model.runCancel = cancel
			var decisions []PermissionResponse
			prompt := PermissionPrompt{
				RequestID: "request-editor", CallID: "call-editor", Tool: "edit_file", Arguments: `{}`,
				Capabilities: PermissionCapabilities{Allow: true, Deny: true, ReviseArguments: true, SchemaAwareEdit: true},
				RichReply: func(_ context.Context, response PermissionResponse) (PermissionReplyResult, error) {
					decisions = append(decisions, response)
					return PermissionReplyResult{Status: ReplyAccepted}, nil
				},
			}
			model.applyEvent(UIEvent{Kind: EventPermissionRequested, Permission: &prompt})
			model.Update(typeKey("e"))
			_, command := model.Update(key)
			if command == nil {
				t.Fatal("global safety key returned no command")
			}
			message := command()
			batch, ok := message.(tea.BatchMsg)
			if !ok {
				batch = tea.BatchMsg{func() tea.Msg { return message }}
			}
			for _, item := range batch {
				if result := item(); result != nil {
					model.Update(result)
				}
			}
			if len(decisions) != 1 || decisions[0].Decision != DecisionDenyOnce {
				t.Fatalf("decisions = %+v", decisions)
			}
			select {
			case <-runContext.Done():
			default:
				t.Fatal("global safety key did not cancel run context")
			}
		})
	}
}

func TestBypassDismissalAndSetterRejectionPreserveMode(t *testing.T) {
	model, port := newReadyApp(t, 80, 24)
	// Cycle Default → AutoAcceptEdits → Plan → Bypass → Default.
	_, cmd := model.Update(shiftTab())
	model.Update(cmd())
	if model.mode != ModeAutoAcceptEdits || len(port.modes) != 1 {
		t.Fatalf("first cycle mode=%q requests=%v", model.mode, port.modes)
	}
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	if model.mode != ModePlan || len(port.modes) != 2 {
		t.Fatalf("second cycle mode=%q requests=%v", model.mode, port.modes)
	}
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	if model.mode != ModeBypass || len(port.modes) != 3 {
		t.Fatalf("third cycle mode=%q requests=%v", model.mode, port.modes)
	}
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	if model.mode != ModeDefault || len(port.modes) != 4 {
		t.Fatalf("fourth cycle back to default mode=%q requests=%v", model.mode, port.modes)
	}

	// SetMode rejection preserves the current mode.
	port.modeErr = errors.New("externally controlled")
	model.mode = ModeDefault
	_, cmd = model.Update(shiftTab())
	model.Update(cmd())
	if model.mode != ModeDefault || model.modeChangePending {
		t.Fatalf("rejected SetMode should preserve mode: mode=%q pending=%v", model.mode, model.modeChangePending)
	}
}
