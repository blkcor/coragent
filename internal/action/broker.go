// Package action implements the single Action Broker route for all M1 tools.
package action

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/transcript"
)

type Effect string

const (
	EffectRead  Effect = "read"
	EffectWrite Effect = "write"
)

// Prepared is a validated, side-effect-free effective action.
type Prepared struct {
	Tool      string
	Arguments json.RawMessage
	Effects   []Effect
	Paths     []string
	// Patch is set by the patch tool's Prepare. Nil for all other tools.
	Patch *PreparedPatch
}

type Execution struct {
	Outcome transcript.ToolResultOutcome
	Content string
}

type Tool interface {
	Definition() provider.ToolDefinition
	Prepare(context.Context, json.RawMessage) (Prepared, error)
	Execute(context.Context, Prepared) Execution
}

type Broker struct {
	tools     map[string]Tool
	projector *dataproj.Projector
}

func NewBroker(tools ...Tool) (*Broker, error) {
	return NewBrokerWithProjector(dataproj.New(), tools...)
}

func NewBrokerWithProjector(projector *dataproj.Projector, tools ...Tool) (*Broker, error) {
	if projector == nil {
		projector = dataproj.New()
	}
	b := &Broker{tools: make(map[string]Tool, len(tools)), projector: projector}
	for _, tool := range tools {
		if tool == nil {
			return nil, errors.New("action: nil tool")
		}
		definition := tool.Definition()
		if definition.Name == "" || !json.Valid(definition.Schema) {
			return nil, errors.New("action: tool has invalid definition")
		}
		if _, exists := b.tools[definition.Name]; exists {
			return nil, fmt.Errorf("action: duplicate tool %q", definition.Name)
		}
		b.tools[definition.Name] = tool
	}
	return b, nil
}

// Catalog exposes only registered model-facing definitions in stable order.
func (b *Broker) Catalog() []provider.ToolDefinition {
	names := make([]string, 0, len(b.tools))
	for name := range b.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]provider.ToolDefinition, 0, len(names))
	for _, name := range names {
		out = append(out, b.tools[name].Definition())
	}
	return out
}

// ProjectCalls is the single pre-execution projection path for model-proposed
// action identity and arguments. It returns safe calls suitable for durable
// ToolCall records and marks calls that policy must block before preparation.
func (b *Broker) ProjectCalls(calls []provider.ToolCall) ([]provider.ToolCall, []bool) {
	safe := make([]provider.ToolCall, len(calls))
	blocked := make([]bool, len(calls))
	for index, call := range calls {
		safe[index] = call
		idProjection := b.projector.ProjectText(call.ID)
		nameProjection := b.projector.ProjectText(call.Name)
		argumentProjection := b.projector.ProjectText(string(call.Arguments))
		if idProjection.RedactedCount > 0 {
			digest := sha256.Sum256([]byte(call.ID))
			safe[index].ID = fmt.Sprintf("redacted-call-%x", digest[:8])
			blocked[index] = true
		}
		if nameProjection.RedactedCount > 0 {
			safe[index].Name = nameProjection.Content
			blocked[index] = true
		}
		if argumentProjection.RedactedCount > 0 {
			safe[index].Arguments = json.RawMessage(argumentProjection.Content)
			blocked[index] = true
		}
	}
	return safe, blocked
}

func (b *Broker) BlockedResult(callID string) transcript.ToolResultPayload {
	return b.project(transcript.ToolResultPayload{
		CallID: callID, Outcome: transcript.ToolResultBlocked,
		Content: "tool call blocked because detected credential material appeared in its identity or arguments",
	})
}

func (b *Broker) SkippedResult(callID string) transcript.ToolResultPayload {
	return b.project(transcript.ToolResultPayload{
		CallID: callID, Outcome: transcript.ToolResultSkipped,
		Content: "skipped because an earlier tool call did not succeed",
	})
}

// Execute routes resolution, preparation, and scoped execution. EffectRead
// tools execute immediately (prepare + execute in one call). EffectWrite tools
// stop after prepare; the caller must use ExecutePrepared to commit.
func (b *Broker) Execute(ctx context.Context, call provider.ToolCall) transcript.ToolResultPayload {
	projected, blocked := b.ProjectCalls([]provider.ToolCall{call})
	call = projected[0]
	if blocked[0] {
		return b.BlockedResult(call.ID)
	}
	result := transcript.ToolResultPayload{CallID: call.ID}
	tool, ok := b.tools[call.Name]
	if !ok {
		result.Outcome = transcript.ToolResultError
		result.Content = fmt.Sprintf("unknown tool %q", call.Name)
		return b.project(result)
	}
	if err := ctx.Err(); err != nil {
		result.Outcome = transcript.ToolResultCancelled
		result.Content = "tool call cancelled before preparation"
		return b.project(result)
	}
	prepared, err := tool.Prepare(ctx, call.Arguments)
	if err != nil {
		result.Outcome = transcript.ToolResultError
		result.Content = "invalid tool arguments: " + safeError(err)
		return b.project(result)
	}
	if prepared.Tool != call.Name || len(prepared.Effects) != 1 {
		result.Outcome = transcript.ToolResultError
		result.Content = "tool produced an invalid prepared action"
		return b.project(result)
	}
	switch prepared.Effects[0] {
	case EffectRead:
		exec := tool.Execute(ctx, prepared)
		result.Outcome = exec.Outcome
		result.Content = exec.Content
	case EffectWrite:
		result.Outcome = transcript.ToolResultSuccess
		result.Content = "action prepared; awaiting execution"
	default:
		result.Outcome = transcript.ToolResultBlocked
		result.Content = "tool effect is not allowed by current authority policy"
	}
	if result.Outcome == "" {
		result.Outcome = transcript.ToolResultError
		result.Content = "tool returned an invalid empty outcome"
	}
	return b.project(result)
}

// ExecutePrepared executes a previously prepared EffectWrite action. It
// performs no approval check (that is the caller's responsibility in S1.6)
// and delegates stale detection to the tool's Execute method.
func (b *Broker) ExecutePrepared(ctx context.Context, prepared Prepared) transcript.ToolResultPayload {
	tool, ok := b.tools[prepared.Tool]
	if !ok {
		return transcript.ToolResultPayload{
			Outcome: transcript.ToolResultError,
			Content: fmt.Sprintf("unknown tool %q", prepared.Tool),
		}
	}
	if len(prepared.Effects) != 1 || prepared.Effects[0] != EffectWrite {
		return transcript.ToolResultPayload{
			Outcome: transcript.ToolResultError,
			Content: "only write-effect actions can be executed via ExecutePrepared",
		}
	}
	if err := ctx.Err(); err != nil {
		return transcript.ToolResultPayload{
			Outcome: transcript.ToolResultCancelled,
			Content: "tool call cancelled before execution",
		}
	}
	exec := tool.Execute(ctx, prepared)
	result := transcript.ToolResultPayload{
		CallID:  prepared.PatchID(),
		Outcome: exec.Outcome,
		Content: exec.Content,
	}
	if result.Outcome == "" {
		result.Outcome = transcript.ToolResultError
		result.Content = "tool returned an invalid empty outcome"
	}
	return b.project(result)
}

func (b *Broker) project(result transcript.ToolResultPayload) transcript.ToolResultPayload {
	result.Content = b.projector.ProjectText(result.Content).Content
	const maxBytes = 64 * 1024
	const marker = "\n[truncated=true; narrow or continue the read-only request]"
	if len(result.Content) > maxBytes {
		cut := maxBytes - len(marker)
		for cut > 0 && !utf8.RuneStart(result.Content[cut]) {
			cut--
		}
		result.Content = result.Content[:cut] + marker
	}
	return result
}

// ExecuteBatch preserves Provider order. The first non-success stops execution
// and every remaining call receives one prior-result skipped result.
func (b *Broker) ExecuteBatch(ctx context.Context, calls []provider.ToolCall) []transcript.ToolResultPayload {
	results := make([]transcript.ToolResultPayload, 0, len(calls))
	stopped := false
	for _, call := range calls {
		if stopped {
			results = append(results, b.SkippedResult(call.ID))
			continue
		}
		result := b.Execute(ctx, call)
		results = append(results, result)
		if result.Outcome != transcript.ToolResultSuccess {
			stopped = true
		}
	}
	return results
}

func safeError(err error) string {
	// Tool validation errors are constructed from schema field names and safe
	// classifications, never from file content or runtime credentials.
	return err.Error()
}
