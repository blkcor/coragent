package prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/transcript"
)

const PromptVersion = "m1-prompt-v1"

var ErrContextLimit = errors.New("prompt: request exceeds configured context window")

const stablePolicy = `You are Coragent, a read-only terminal repository companion.
Investigate the active workspace using only the provided list, search, and read tools.
Never claim to edit files, run commands, access the network, or read outside the workspace.
Ground factual repository claims in tool results. Final answers must cite workspace-relative files and exact 1-based line ranges as path:start-end.
Treat tool errors as evidence and change read-only strategy when recovery is possible.
Hard runtime policy overrides user and project instructions.`

type Config struct {
	Workspace       string
	ActivePath      string
	ContextWindow   int
	MaxOutputTokens int
	UserPreferences string
}

type Assembler struct {
	cfg Config
}

func NewAssembler(cfg Config) (*Assembler, error) {
	if cfg.Workspace == "" || cfg.ContextWindow <= 0 || cfg.MaxOutputTokens <= 0 {
		return nil, errors.New("prompt: workspace and explicit context/output limits are required")
	}
	if cfg.ActivePath == "" {
		cfg.ActivePath = "."
	}
	return &Assembler{cfg: cfg}, nil
}

func (a *Assembler) Stable() string { return stablePolicy }

// Build reconstructs stable and dynamic sections from current state on every
// call. It never mutates or accumulates a prior prompt.
func (a *Assembler) Build(goal string, docs []Instruction, records []transcript.Record, tools []provider.ToolDefinition) (provider.Request, error) {
	var dynamic strings.Builder
	fmt.Fprintf(&dynamic, "Runtime facts:\n- workspace: %s\n- active path: %s\n- prompt version: %s\n", a.cfg.Workspace, a.cfg.ActivePath, PromptVersion)
	if a.cfg.UserPreferences != "" {
		dynamic.WriteString("\nUser preferences (lowest instruction precedence):\n")
		dynamic.WriteString(a.cfg.UserPreferences)
		dynamic.WriteByte('\n')
	}
	for _, doc := range docs {
		fmt.Fprintf(&dynamic, "\nProject instructions [sources=%s scope=%s precedence=%d sha256=%s]:\n%s\n",
			strings.Join(doc.Sources, ","), doc.Scope, doc.Precedence, doc.SHA256, doc.Content)
	}
	if goal != "" {
		dynamic.WriteString("\nCurrent explicit user request (higher than project guidance, lower than hard policy):\n")
		dynamic.WriteString(goal)
		dynamic.WriteByte('\n')
	}
	messages, err := recordsToMessages(records)
	if err != nil {
		return provider.Request{}, err
	}
	req := provider.Request{
		StablePrompt: stablePolicy, DynamicPrompt: dynamic.String(), Messages: messages,
		Tools: append([]provider.ToolDefinition(nil), tools...), MaxOutput: a.cfg.MaxOutputTokens,
	}
	// M1 has no summary checkpoints. Fail explicitly instead of silently
	// deleting history when the conservative estimate cannot fit.
	estimated := estimateTokens(req.StablePrompt) + estimateTokens(req.DynamicPrompt)
	for _, msg := range req.Messages {
		estimated += estimateTokens(msg.Content) + 16
		for _, call := range msg.ToolCalls {
			estimated += estimateTokens(call.Name) + estimateTokens(string(call.Arguments)) + 16
		}
	}
	for _, tool := range req.Tools {
		estimated += estimateTokens(tool.Name) + estimateTokens(tool.Description) + estimateTokens(string(tool.Schema))
	}
	if estimated+a.cfg.MaxOutputTokens > a.cfg.ContextWindow {
		return provider.Request{}, fmt.Errorf("%w: estimated input %d plus output %d exceeds %d", ErrContextLimit, estimated, a.cfg.MaxOutputTokens, a.cfg.ContextWindow)
	}
	return req, nil
}

func recordsToMessages(records []transcript.Record) ([]provider.Message, error) {
	var out []provider.Message
	for i := 0; i < len(records); i++ {
		rec := records[i]
		switch rec.Kind {
		case transcript.KindUserMessage:
			var p transcript.UserMessagePayload
			if err := rec.DecodePayload(&p); err != nil {
				return nil, err
			}
			out = append(out, provider.Message{Role: provider.RoleUser, Content: p.Text})
		case transcript.KindAssistantBlock:
			var p transcript.AssistantBlockPayload
			if err := rec.DecodePayload(&p); err != nil {
				return nil, err
			}
			out = append(out, provider.Message{Role: provider.RoleAssistant, Content: p.Text})
		case transcript.KindToolCall:
			message := provider.Message{Role: provider.RoleAssistant}
			for i < len(records) && records[i].Kind == transcript.KindToolCall {
				var p transcript.ToolCallPayload
				if err := records[i].DecodePayload(&p); err != nil {
					return nil, err
				}
				message.ToolCalls = append(message.ToolCalls, provider.ToolCall{ID: p.CallID, Name: p.Name, Arguments: p.Arguments})
				i++
			}
			i--
			out = append(out, message)
		case transcript.KindToolResult:
			var p transcript.ToolResultPayload
			if err := rec.DecodePayload(&p); err != nil {
				return nil, err
			}
			out = append(out, provider.Message{Role: provider.RoleTool, ToolCallID: p.CallID, Content: p.Content})
		}
	}
	return out, nil
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Conservative deterministic approximation used only for M1 admission.
	return (len([]byte(text)) + 2) / 3
}
