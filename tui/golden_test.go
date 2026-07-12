package tui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestRichPermissionGoldenMatrix(t *testing.T) {
	sizes := []struct{ width, height int }{{160, 48}, {120, 36}, {80, 24}, {60, 20}, {50, 16}}
	modes := []struct {
		name string
		mode VisualMode
	}{
		{"truecolor", TrueColorMode()},
		{"ansi256", VisualMode{Color: ColorANSI256}},
		{"no-color", NoColorMode()},
		{"reduced-motion", VisualMode{Color: ColorTrueColor, ReducedMotion: true}},
		{"ascii", ASCIIMode()},
	}

	golden := map[string]string{
		// Filled with deterministic SHA-256 render snapshots below. A digest is
		// used instead of a terminal-control-heavy text file so every visual mode
		// remains reviewable in ordinary diffs.
		"160x48/truecolor":      "0c3cd5e0f8d0d03061cc05a2d301a9ee61720d749eb95bada0da1ecefaff7ed9",
		"160x48/ansi256":        "66801bb228aa473b2952600e256f1db892ad13ec48872bcd61589a0fd8ffcc67",
		"160x48/no-color":       "3c64931c385d3c2a25e6edca93788b2da9bddb21bbb420038dda1a87645c417c",
		"160x48/reduced-motion": "225d37d16a637563dc47ca740865e7a136890b157ea20f965b6579e977aa433a",
		"160x48/ascii":          "3998af2e9e1a37106872c4a96a6496d508a741e38c4afede556a0dcd429bc1b4",
		"120x36/truecolor":      "bf3d223fa58e5e4e51f81dfe9dffd3ccb8321c0917c1e271646b7939b7374b95",
		"120x36/ansi256":        "d14d1a7f35439f97b7aa7e9643c4428355807f5a374e9aa4cbf80916b4558c53",
		"120x36/no-color":       "dc43223e33c8ab06164d57129b8e73871b83622461ec767f8f897919ee375be8",
		"120x36/reduced-motion": "93842d081d22138278d73dae9577353a38f9afb0bd3cd1f94bbf370b11ed117a",
		"120x36/ascii":          "bff0a5d47f493df4b93fc3029d4df674f849b9085b3ab3850127134eda5d028d",
		"80x24/truecolor":       "8143654c1084315972672f2bf700b5808620a89e745c36fb84a52e445336e07b",
		"80x24/ansi256":         "43943d09747331d4b179deaf63dc7b45f550801435fcb5b631e441fa554410f8",
		"80x24/no-color":        "f349e0226b2fa7eb87dc3814654047524b282f24d804548c7d7c5db77970239e",
		"80x24/reduced-motion":  "8143654c1084315972672f2bf700b5808620a89e745c36fb84a52e445336e07b",
		"80x24/ascii":           "bdde4f7818eb737c009befeb5d210af6664c6db27ab60d6a143f36d16307ae70",
		"60x20/truecolor":       "04de2c2e84a8587bf86bfcea119418e626f92f8a4b1a7813ec491dccac8e9428",
		"60x20/ansi256":         "5aae4590f99a97048e73eee0381a30b700338caf787ca340b36f072d967190e4",
		"60x20/no-color":        "91a9984a73e4527026dcf524c4b2b3ed982ccd5b9352b2a3a49a041a61e9d187",
		"60x20/reduced-motion":  "04de2c2e84a8587bf86bfcea119418e626f92f8a4b1a7813ec491dccac8e9428",
		"60x20/ascii":           "c676411eda0404ed5c95f0b7713ae348c4692607af12b41d22fd70d8bfd95915",
		"50x16/truecolor":       "7849fb4fc9c0a80f913201164e4a4139642746627c7ef4f85a55c3626dc8685e",
		"50x16/ansi256":         "7849fb4fc9c0a80f913201164e4a4139642746627c7ef4f85a55c3626dc8685e",
		"50x16/no-color":        "7849fb4fc9c0a80f913201164e4a4139642746627c7ef4f85a55c3626dc8685e",
		"50x16/reduced-motion":  "7849fb4fc9c0a80f913201164e4a4139642746627c7ef4f85a55c3626dc8685e",
		"50x16/ascii":           "7849fb4fc9c0a80f913201164e4a4139642746627c7ef4f85a55c3626dc8685e",
	}
	for _, size := range sizes {
		for _, visual := range modes {
			name := fmt.Sprintf("%dx%d/%s", size.width, size.height, visual.name)
			t.Run(name, func(t *testing.T) {
				model := richGoldenModel(size.width, size.height, visual.mode)
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(model.render())))
				want, ok := golden[name]
				if !ok {
					t.Fatalf("missing golden %q = %q", name, digest)
				}
				if digest != want {
					t.Fatalf("golden %q changed\n got: %s\nwant: %s", name, digest, want)
				}
			})
		}
	}
}

func richGoldenModel(width, height int, mode VisualMode) *AppModel {
	clock := &fakeClock{now: time.Date(2026, 7, 12, 9, 30, 0, 0, time.UTC)}
	model := NewAppModel(nil, WithClock(clock), WithVisualMode(mode))
	model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model.handleStartup(startupLoadedMsg{Info: SessionInfo{
		Project: "/Users/example/coragent", Model: "gpt-test", Provider: "fixture",
		Mode: ModeDefault, ModeChangeable: true, PermissionOwner: "engine",
		Sandbox: "fallback", SandboxReason: "fixture fallback", ReasoningSummarySupport: SupportSupported,
		UsageSupport: SupportSupported,
	}})
	at := clock.Now()
	model.transcript.AddUser("Update the parser safely, then explain the result.", at)
	model.applyEvent(UIEvent{Kind: EventRunStarted, RunID: "run-golden", Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventAssistantStarted, RunID: "run-golden", AssistantID: "assistant-golden", Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventAssistantReasoningSummaryDelta, RunID: "run-golden", AssistantID: "assistant-golden", Text: "I checked the public contract and the bounded diff.", Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventAssistantTextDelta, RunID: "run-golden", AssistantID: "assistant-golden", Text: "## Working\n\n- validating **Unicode**\n- preparing the edit", Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventToolStarted, RunID: "run-golden", CallID: "call-done", ToolName: "read_file", Arguments: `{"path":"internal/parser.go"}`, Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventToolExecuting, RunID: "run-golden", CallID: "call-done", Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventToolFinished, RunID: "run-golden", CallID: "call-done", Tool: ToolFailed, Result: "recoverable fixture error", Duration: 42 * time.Millisecond, Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventOmission, RunID: "run-golden", CallID: "call-done", Omission: &Omission{Kind: "output_budget", Scope: "tool_output", CallID: "call-done", Recoverability: "unrecoverable", OriginalLines: OptionalCount{Known: true, Value: 120}, RetainedLines: OptionalCount{Known: true, Value: 20}}, Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventToolStarted, RunID: "run-golden", CallID: "call-review", ToolName: "edit_file", Arguments: `{"path":"internal/parser.go"}`, Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventToolPrepared, RunID: "run-golden", CallID: "call-review", ToolName: "edit_file", Arguments: `{"path":"internal/parser.go"}`, Revision: 2, Preview: richDiffPreview(), Timestamp: at})
	model.applyEvent(UIEvent{Kind: EventContextUsage, RunID: "run-golden", Usage: &ContextUsage{Source: "provider", Used: 9500, Window: OptionalCount{Known: true, Value: 10_000}}, Timestamp: at})
	prompt := PermissionPrompt{
		RequestID: "request-golden", CallID: "call-review", Revision: 2, Tool: "edit_file",
		Action: "modify internal/parser.go", Arguments: `{"path":"internal/parser.go"}`,
		Reason: "writes a project file", Origin: "root agent", Protocol: "rich", Preview: "modify parser with one bounded hunk",
		RememberScope: "edit edit_file", StructuredPreview: richDiffPreview(),
		Capabilities: PermissionCapabilities{Allow: true, Deny: true, Remember: true, ReviseArguments: true, SchemaAwareEdit: true, Preview: true, SandboxGrants: true},
		GrantOptions: GrantOptions{Support: SupportSupported, ReadRoots: true, WriteRoots: true},
		RichReply: func(context.Context, PermissionResponse) (PermissionReplyResult, error) {
			return PermissionReplyResult{Status: ReplyAccepted}, nil
		},
	}
	model.applyEvent(UIEvent{Kind: EventPermissionRequested, RunID: "run-golden", CallID: "call-review", Permission: &prompt, Timestamp: at})
	model.frame = 1
	return model
}
