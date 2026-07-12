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
		"160x48/truecolor":      "a6752e39346624fb1c5c92749b979323cd850f5b48c2d3a6501c6854f553053d",
		"160x48/ansi256":      "6f30638537e7da0132ff062ee51127493ae398c35eadb3910161217818de42dd",
		"160x48/no-color":      "58c34193c808c5cbf62a3007c4a3c116b946a97c84f4136fa3365cef88dab63c",
		"160x48/reduced-motion":      "8c14b7a2f6e3ccc5113997161cd98caae65707c78faedbecf5528302a52e92fa",
		"160x48/ascii":      "1c63fd8ca5817a648d72bf0816b05054b74c2687e1597d4c4b7a71c8449c39e4",
		"120x36/truecolor":      "9d6123f08db9c1e34355ebc4ad9e15ae47626417327d7860f58c9d3bc90c6abd",
		"120x36/ansi256":      "5613a88ccdc6a42c92ae9fe43e4a18835e73771323955c2bde18ed288bc06444",
		"120x36/no-color":      "be2e18531d9773f6eb873f3090dff70046c37b1f407dbce7def5cea5e3fd7d1c",
		"120x36/reduced-motion":      "df624cc5de1fa3fed5c65000cba6d8df8f7dae17ae64c6fd6ab5e98e08218183",
		"120x36/ascii":      "8ecfa7b016d20959af5ab5876d002df8c37b08c5255cb7bb96a00b5c44c1e1c3",
		"80x24/truecolor":      "241b68908517391fd74306618f27f213bde53a99fedb3f8247423b080102c15a",
		"80x24/ansi256":      "e613fc90f462f429cc8e2603bd7e0b310c4a121c3a284940f156be95d0159acd",
		"80x24/no-color":      "06cd096f9402fc5506dd44fda668e46d6a29e13496ba7233da885236e691839f",
		"80x24/reduced-motion":      "241b68908517391fd74306618f27f213bde53a99fedb3f8247423b080102c15a",
		"80x24/ascii":      "a6a97582a25eb215f83a49bf3865aef9337e3f05b51d3f03f37d6ac6b597b3c8",
		"60x20/truecolor":      "3a31af3c19d5abc29d9dc4ed48b27efa4f12d1edec4ef2b6d4df18ba644a2bc9",
		"60x20/ansi256":      "72a468a99a3ae8cd7f7cd9c323f783963a50bf6be6f3baea70ee9c5a72a4d675",
		"60x20/no-color":      "91680d48efdf254a3980885ed32d7e829ee616a1d0913e6234e1b7cc40b8861a",
		"60x20/reduced-motion":      "3a31af3c19d5abc29d9dc4ed48b27efa4f12d1edec4ef2b6d4df18ba644a2bc9",
		"60x20/ascii":      "52d98072fcab7c5cb2bc621195b059b2bdd1d74a69dd510dd544d7d696d404d3",
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
		Reason: "writes a project file", Origin: "root agent", Protocol: "rich",
		Preview:       "custom · Preview unavailable: the tool handler does not support safe preparation",
		RememberScope: "edit edit_file", StructuredPreview: &ActionPreview{
			Kind: "unavailable", Operation: "custom", UnavailableReason: "the tool handler does not support safe preparation",
		},
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
