package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestCanonicalRenderUsesExactTerminalGeometry(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		mode   VisualMode
	}{
		{name: "wide", width: 160, height: 48, mode: TrueColorMode()},
		{name: "standard", width: 120, height: 36, mode: TrueColorMode()},
		{name: "compact", width: 80, height: 24, mode: VisualMode{Color: ColorANSI256}},
		{name: "minimal", width: 60, height: 20, mode: ASCIIMode()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := canonicalPreviewModel(t, test.width, test.height, test.mode)
			rendered := model.render()
			lines := strings.Split(rendered, "\n")
			if len(lines) != test.height {
				t.Fatalf("rendered %d rows, want %d", len(lines), test.height)
			}
			for row, line := range lines {
				if width := ansi.StringWidth(line); width != test.width {
					t.Fatalf("row %d rendered %d cells, want %d: %q", row, width, test.width, line)
				}
			}
			if test.name == "standard" {
				if output := os.Getenv("CORAGENT_PERMISSION_PREVIEW_OUT"); output != "" {
					if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
						t.Fatalf("create preview directory: %v", err)
					}
					if err := os.WriteFile(output, []byte(rendered), 0o644); err != nil {
						t.Fatalf("write permission preview: %v", err)
					}
				}
			}
		})
	}
}

func TestConversationPreviewUsesClaudeStyleFlow(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		mode   VisualMode
	}{
		{name: "wide", width: 160, height: 48, mode: TrueColorMode()},
		{name: "standard", width: 120, height: 36, mode: TrueColorMode()},
		{name: "compact", width: 80, height: 24, mode: VisualMode{Color: ColorANSI256}},
		{name: "minimal", width: 60, height: 20, mode: ASCIIMode()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := conversationPreviewModel(t, test.width, test.height, test.mode)
			rendered := model.render()
			plain := ansi.Strip(rendered)
			lines := strings.Split(rendered, "\n")
			if len(lines) != test.height {
				t.Fatalf("rendered %d rows, want %d", len(lines), test.height)
			}
			for row, line := range lines {
				if width := ansi.StringWidth(line); width != test.width {
					t.Fatalf("row %d rendered %d cells, want %d: %q", row, width, test.width, line)
				}
			}
			for _, forbidden := range []string{"YOU", "AGENT"} {
				if strings.Contains(plain, forbidden) {
					t.Fatalf("rendered legacy role label %q\n%s", forbidden, rendered)
				}
			}
			if !strings.Contains(plain, "who are you") || !strings.Contains(plain, "I am Coragent") {
				t.Fatalf("conversation narrative is incomplete\n%s", rendered)
			}
			for _, raw := range []string{"## Coragent", "**Coragent**"} {
				if strings.Contains(plain, raw) {
					t.Fatalf("conversation preview leaked raw Markdown %q\n%s", raw, rendered)
				}
			}
			if !test.mode.ASCII && strings.Contains(plain, "- inspect") {
				t.Fatalf("conversation preview kept a raw list marker\n%s", rendered)
			}
			if test.mode.ASCII {
				for _, character := range ansi.Strip(rendered) {
					if character > 127 {
						t.Fatalf("ASCII fallback emitted %q\n%s", character, rendered)
					}
				}
			}

			output := ""
			if directory := os.Getenv("CORAGENT_PREVIEW_DIR"); directory != "" {
				output = filepath.Join(directory, fmt.Sprintf("coragent-claude-layout-%dx%d.ansi", test.width, test.height))
			} else if test.name == "standard" {
				output = os.Getenv("CORAGENT_PREVIEW_OUT")
			}
			if output != "" {
				if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
					t.Fatalf("create preview directory: %v", err)
				}
				if err := os.WriteFile(output, []byte(rendered), 0o644); err != nil {
					t.Fatalf("write preview: %v", err)
				}
			}
		})
	}
}

func conversationPreviewModel(t *testing.T, width, height int, mode VisualMode) *AppModel {
	t.Helper()
	now := time.Date(2026, time.July, 11, 19, 42, 0, 0, time.Local)
	model := NewAppModel(nil, WithVisualMode(mode))
	model.layout = LayoutForSize(width, height)
	model.terminal = TerminalState{Width: width, Height: height, Class: model.layout.Class}
	model.runState = RunIdle
	model.focus = FocusComposer
	model.mode = ModePlan
	model.info = SessionInfo{
		Project: "/Users/blkcor-bt/ai/project/coragent",
		Model:   "deepseek-v4-pro",
		Mode:    ModePlan,
		Sandbox: "os",
		Context: "ctx 18% est",
	}
	model.activity = ActivityIdle
	model.transcript.AddUser("who are you?", now)
	if err := model.transcript.StartAssistant("assistant-conversation", now); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.AppendAssistant("assistant-conversation", "## Coragent\n\nI am **Coragent**, a coding agent that can:\n\n- inspect this repository\n- change files\n- run tools and verify the result with you", now); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.FinishAssistant("assistant-conversation"); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.StartTool("call-read-layout", "read_file", "tui/app.go", now); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.FinishTool("call-read-layout", "Read 1,155 lines", ToolSucceeded, now); err != nil {
		t.Fatal(err)
	}
	return model
}

func canonicalPreviewModel(t *testing.T, width, height int, mode VisualMode) *AppModel {
	t.Helper()
	now := time.Date(2026, time.July, 11, 14, 32, 0, 0, time.Local)
	model := NewAppModel(nil, WithVisualMode(mode))
	model.layout = LayoutForSize(width, height)
	model.terminal = TerminalState{Width: width, Height: height, Class: model.layout.Class}
	model.runState = RunRunning
	model.focus = FocusPermission
	model.mode = ModePlan
	model.info = SessionInfo{
		Project: "/workspace/coragent/phase-7-tui",
		Model:   "gpt-5.4",
		Mode:    ModePlan,
		Sandbox: "os",
		Context: "ctx 22% est",
	}
	model.activity = ActivityPermission
	model.frame = 1
	model.transcript.AddUser("Implement the first TUI slice from the approved spec and design.", now)
	if err := model.transcript.StartAssistant("assistant-1", now); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.AppendAssistant("assistant-1", "I mapped the public session boundary and built the responsive shell. The focused tests are passing.", now); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.FinishAssistant("assistant-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.StartTool("call-read", "read_file", "openspec/changes/phase-7-tui/ui-design.md", now); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.FinishTool("call-read", "design loaded", ToolSucceeded, now); err != nil {
		t.Fatal(err)
	}
	if err := model.transcript.StartTool("call-edit", "edit_file", "cmd/coragent/main.go", now); err != nil {
		t.Fatal(err)
	}
	prompt := PermissionPrompt{
		RequestID: "permission-1",
		CallID:    "call-edit",
		Revision:  1,
		Tool:      "edit_file",
		Action:    "Modify cmd/coragent/main.go",
		Reason:    "wire the public bootstrap into the Bubble Tea application",
		Origin:    "root agent",
		Preview:   "r1 · +74 -2",
		Reply: func(_ context.Context, _ PermissionDecision) (PermissionReplyResult, error) {
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	if err := model.transcript.AwaitPermission(prompt, now); err != nil {
		t.Fatal(err)
	}
	model.permission = &permissionState{Prompt: prompt}
	return model
}
