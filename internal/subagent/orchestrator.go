package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
	"github.com/blkcor/coragent/internal/sessionrun"
	"github.com/blkcor/coragent/internal/tools"
)

const (
	// ToolName is the model-facing name of the delegation capability.
	ToolName = "task"

	// MaxDepth is the v1 ceiling on delegation edges from the root session.
	MaxDepth = 3

	childSystemPrompt = "You are a focused subagent. Complete only the delegated task and return a concise final answer."
)

// BlueprintConfig captures the immutable runtime pieces inherited by every
// child. Advertised may contain caller-provided descriptors; only names that
// also resolve in Catalog are eligible for a child.
type BlueprintConfig struct {
	Provider            core.Provider
	Catalog             *tools.Catalog
	Advertised          []core.Tool
	Stages              executor.Stages
	Hooks               core.LifecycleHooks
	MaxRounds           int
	ContextBudgetTokens int
	StreamOptions       core.StreamOptions
}

// Blueprint is an immutable recipe for constructing isolated child runtimes.
type Blueprint struct {
	provider            core.Provider
	catalog             *tools.Catalog
	advertised          []core.Tool
	stages              executor.Stages
	hooks               core.LifecycleHooks
	maxRounds           int
	contextBudgetTokens int
	streamOptions       core.StreamOptions
}

// NewBlueprint snapshots the inherited runtime inputs. The task capability is
// control-plane wiring and is deliberately excluded from the ordinary tool set.
func NewBlueprint(cfg BlueprintConfig) *Blueprint {
	advertised := make([]core.Tool, 0, len(cfg.Advertised))
	for _, descriptor := range cfg.Advertised {
		if descriptor.Name == ToolName {
			continue
		}
		copyDescriptor := descriptor
		copyDescriptor.Parameters = append(json.RawMessage(nil), descriptor.Parameters...)
		advertised = append(advertised, copyDescriptor)
	}
	return &Blueprint{
		provider:            cfg.Provider,
		catalog:             cfg.Catalog,
		advertised:          advertised,
		stages:              cfg.Stages,
		hooks:               cfg.Hooks,
		maxRounds:           cfg.MaxRounds,
		contextBudgetTokens: cfg.ContextBudgetTokens,
		streamOptions:       cfg.StreamOptions,
	}
}

// TaskHandler is the ordinary executor handler that delegates to a child. Depth
// is the number of delegation edges between the root and the handler's session.
type TaskHandler struct {
	blueprint *Blueprint
	depth     int
}

// NewTaskHandler constructs the root task handler at depth zero.
func NewTaskHandler(blueprint *Blueprint) *TaskHandler {
	return newTaskHandler(blueprint, 0)
}

func newTaskHandler(blueprint *Blueprint, depth int) *TaskHandler {
	return &TaskHandler{blueprint: blueprint, depth: depth}
}

// Descriptor returns the model-facing task contract.
func (*TaskHandler) Descriptor() core.Tool {
	return core.Tool{
		Name: ToolName,
		Description: "Delegate a focused task to an isolated child agent. " +
			"Optionally choose the ordinary tools the child may use; omitted or empty tools use safe read-only defaults.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"label": {"type": "string", "description": "Short human-readable label for the delegated work."},
				"instruction": {"type": "string", "description": "Focused instruction the child should complete."},
				"tools": {"type": "array", "items": {"type": "string"}, "description": "Optional ordinary tool names allowed for the child."}
			},
			"required": ["label", "instruction"]
		}`),
	}
}

// RunsCommands reports that delegation itself does not launch a command. Child
// command tools are classified and sandboxed independently.
func (*TaskHandler) RunsCommands() bool { return false }

// ActionKind classifies delegation as orchestration that is safe to begin in
// plan mode. Mutating child tools still receive their own permission decision.
func (*TaskHandler) ActionKind() core.ActionKind { return core.ActionRead }

// Execute satisfies core.ToolHandler. The executor normally selects
// ExecuteWithEvents for this internal handler so lifecycle and permission events
// can share the parent stream.
func (h *TaskHandler) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return "", errors.New("task: live event stream is required")
}

// ExecuteWithEvents runs one isolated child while using the parent dispatch's
// live emitter for the two labeled statuses and child permission requests.
func (h *TaskHandler) ExecuteWithEvents(ctx context.Context, args map[string]interface{}, emit func(core.RunEvent) error) (result string, err error) {
	request, err := parseRequest(args)
	if err != nil {
		return "", err
	}
	if h == nil || h.blueprint == nil || h.blueprint.catalog == nil || h.blueprint.provider == nil {
		return "", errors.New("task: subagent runtime is unavailable")
	}
	if h.depth >= MaxDepth {
		return "", fmt.Errorf("task: maximum delegation depth %d exceeded", MaxDepth)
	}

	statusCall := &core.ToolCall{
		ToolName:  ToolName,
		Arguments: map[string]interface{}{"label": request.label},
	}
	if emit != nil {
		if err := emit(core.RunEvent{Type: core.StatusChange, Status: core.StatusSubagentStarted, ToolCall: statusCall}); err != nil {
			return "", err
		}
	}

	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()
	var childEventErr error
	childEmit := func(ev core.RunEvent) error {
		forward := ev.Type == core.PermissionRequestedEvent ||
			(ev.Type == core.StatusChange &&
				(ev.Status == core.StatusSubagentStarted || ev.Status == core.StatusSubagentFinished))
		if !forward {
			return nil
		}
		if emit == nil {
			childEventErr = errors.New("task: child requested permission without a parent event stream")
			childCancel()
			return childEventErr
		}
		if err := emit(ev); err != nil {
			childEventErr = err
			childCancel()
			return err
		}
		return nil
	}

	childCatalog := h.blueprint.catalog.RestrictedView(h.blueprint.advertised, request.tools)
	childBlueprint := h.blueprint.descendant(childCatalog, childCatalog.Advertise())
	childCatalog.MustRegister(newTaskHandler(childBlueprint, h.depth+1))
	childRuntime := sessionrun.New(sessionrun.Config{
		Provider:            h.blueprint.provider,
		Dispatcher:          executor.New(childCatalog, h.blueprint.stages, 0),
		Tools:               childCatalog.Advertise(),
		SystemPrompt:        childSystemPrompt,
		MaxRounds:           h.blueprint.maxRounds,
		ContextBudgetTokens: h.blueprint.contextBudgetTokens,
		StreamOptions:       h.blueprint.streamOptions,
		Hooks:               h.blueprint.hooks,
	})

	started := false
	defer func() {
		if !started || emit == nil || ctx.Err() != nil {
			return
		}
		if emitErr := emit(core.RunEvent{Type: core.StatusChange, Status: core.StatusSubagentFinished, ToolCall: statusCall}); emitErr != nil && err == nil {
			err = emitErr
			result = ""
		}
	}()

	started = true
	if startErr := childRuntime.Start(childCtx, childEmit); startErr != nil {
		stopErr := childRuntime.Stop(childCtx, childEmit)
		return "", combineErrors("child startup failed", startErr, stopErr)
	}

	fin := childRuntime.Run(childCtx, request.instruction, childEmit)
	stopErr := childRuntime.Stop(childCtx, childEmit)

	if ctx.Err() != nil || childCtx.Err() != nil || fin.Reason == core.StopCancelled {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if childEventErr != nil {
			return "", fmt.Errorf("task: forward child event: %w", childEventErr)
		}
		return "", errors.New("task: child run cancelled")
	}

	switch fin.Reason {
	case core.StopCompleted:
		if stopErr != nil {
			return "", fmt.Errorf("task: child cleanup failed: %w", stopErr)
		}
		answer, ok := finalAssistant(childRuntime.Conversation())
		if !ok {
			return "", errors.New("task: child completed without a final assistant answer")
		}
		return answer, nil
	case core.StopReachedStepLimit:
		return "", combineErrors("task: child reached the step limit", nil, stopErr)
	case core.StopFailed:
		cause := fin.Err
		if cause == nil {
			cause = errors.New("unknown child failure")
		}
		return "", combineErrors("task: child failed", cause, stopErr)
	default:
		return "", combineErrors("task: child ended with an unknown outcome", nil, stopErr)
	}
}

// descendant carries the shared runtime and safety collaborators forward while
// replacing the ordinary capability ceiling with the child that was just
// derived. Nested delegation can therefore narrow that set again, but can never
// recover a handler or descriptor that an earlier generation removed.
func (b *Blueprint) descendant(catalog *tools.Catalog, advertised []core.Tool) *Blueprint {
	return NewBlueprint(BlueprintConfig{
		Provider:            b.provider,
		Catalog:             catalog,
		Advertised:          advertised,
		Stages:              b.stages,
		Hooks:               b.hooks,
		MaxRounds:           b.maxRounds,
		ContextBudgetTokens: b.contextBudgetTokens,
		StreamOptions:       b.streamOptions,
	})
}

type taskRequest struct {
	label       string
	instruction string
	tools       []string
}

func parseRequest(args map[string]interface{}) (taskRequest, error) {
	label, ok := args["label"].(string)
	if !ok || strings.TrimSpace(label) == "" {
		return taskRequest{}, errors.New("task: label must be a non-blank string")
	}
	instruction, ok := args["instruction"].(string)
	if !ok || strings.TrimSpace(instruction) == "" {
		return taskRequest{}, errors.New("task: instruction must be a non-blank string")
	}

	request := taskRequest{
		label:       strings.TrimSpace(label),
		instruction: strings.TrimSpace(instruction),
	}
	value, exists := args["tools"]
	if !exists || value == nil {
		return request, nil
	}
	switch list := value.(type) {
	case []string:
		request.tools = append([]string(nil), list...)
	case []interface{}:
		request.tools = make([]string, 0, len(list))
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				return taskRequest{}, errors.New("task: tools must contain only strings")
			}
			request.tools = append(request.tools, name)
		}
	default:
		return taskRequest{}, errors.New("task: tools must be an array of strings")
	}
	return request, nil
}

func finalAssistant(conv core.Conversation) (string, bool) {
	if len(conv.Turns) == 0 {
		return "", false
	}
	last := conv.Turns[len(conv.Turns)-1]
	if last.Role != "assistant" || len(last.ToolCalls) != 0 {
		return "", false
	}
	return last.Content, true
}

func combineErrors(prefix string, primary, cleanup error) error {
	switch {
	case primary != nil && cleanup != nil:
		return fmt.Errorf("%s: %v (cleanup: %v)", prefix, primary, cleanup)
	case primary != nil:
		return fmt.Errorf("%s: %w", prefix, primary)
	case cleanup != nil:
		return fmt.Errorf("%s (cleanup: %v)", prefix, cleanup)
	default:
		return errors.New(prefix)
	}
}

var (
	_ core.ToolHandler      = (*TaskHandler)(nil)
	_ core.ActionClassifier = (*TaskHandler)(nil)
)
