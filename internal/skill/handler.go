package skill

import (
	"context"
	"encoding/json"

	"github.com/blkcor/coragent/internal/core"
)

// Handler wraps a Skill as a ToolHandler so the model can invoke it.
// Execute returns the skill body as the result, making the skill's instructions
// visible to the model in the conversation.
type Handler struct {
	skill *Skill
	desc  core.Tool
}

// NewHandler creates a ToolHandler for the given skill.
func NewHandler(s *Skill) *Handler {
	return &Handler{
		skill: s,
		desc: core.Tool{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  emptyParams(),
		},
	}
}

// Descriptor returns the model-facing tool descriptor.
func (h *Handler) Descriptor() core.Tool {
	return h.desc
}

// Execute returns the skill body as the tool result, making it visible to the
// model for the next round.
func (h *Handler) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return h.skill.Body, nil
}

// RunsCommands returns false — skills never run shell commands.
func (h *Handler) RunsCommands() bool {
	return false
}

// ActionKind classifies skill invocation as a read action.
func (h *Handler) ActionKind() core.ActionKind {
	return core.ActionRead
}

// Skill returns the underlying skill definition.
func (h *Handler) Skill() *Skill {
	return h.skill
}

func emptyParams() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
