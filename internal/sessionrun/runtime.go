// Package sessionrun owns the frontend-agnostic mechanics of one agent session.
//
// A Runtime owns conversation history and the lifecycle around the shared agent
// loop. Frontends remain responsible for concurrency policy, event transport,
// and terminal-event delivery; child-agent orchestration can therefore reuse the
// same runtime without importing pkg/agent or copying Session.Run.
package sessionrun

import (
	"context"
	"fmt"

	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/loop"
)

// EmitFunc publishes one non-terminal run event. The owner of a Runtime decides
// where those events go and remains responsible for publishing the terminal
// RunFinishedEvent after Run returns.
type EmitFunc func(core.RunEvent) error

// Config contains the resolved collaborators and limits for a Runtime. Root
// sessions and subagent sessions use the same shape; neither needs frontend
// knowledge to drive the loop.
type Config struct {
	Provider            core.Provider
	Dispatcher          core.Dispatcher
	Tools               []core.Tool
	SystemPrompt        string
	MaxRounds           int
	ContextBudgetTokens int
	StreamOptions       core.StreamOptions
	Hooks               core.LifecycleHooks
}

// Runtime owns one conversation and drives its lifecycle hooks and agent loop.
// Callers serialize Run calls for a given Runtime.
type Runtime struct {
	provider   core.Provider
	convo      *convo.Manager
	dispatcher core.Dispatcher
	tools      []core.Tool
	maxRounds  int
	budget     int
	opts       core.StreamOptions
	hooks      core.LifecycleHooks
}

// New constructs a Runtime with a fresh conversation seeded by SystemPrompt.
func New(cfg Config) *Runtime {
	return &Runtime{
		provider:   cfg.Provider,
		convo:      convo.New(cfg.SystemPrompt),
		dispatcher: cfg.Dispatcher,
		tools:      append([]core.Tool(nil), cfg.Tools...),
		maxRounds:  cfg.MaxRounds,
		budget:     cfg.ContextBudgetTokens,
		opts:       cfg.StreamOptions,
		hooks:      cfg.Hooks,
	}
}

// BindExecution replaces the dispatcher and advertised tool list during
// composition, before the first Run. Root session construction uses this narrow
// setup hook so session-start can retain its historical ordering ahead of
// dispatcher construction; child runtimes normally provide both in Config.
func (r *Runtime) BindExecution(dispatcher core.Dispatcher, tools []core.Tool) {
	r.dispatcher = dispatcher
	r.tools = append([]core.Tool(nil), tools...)
}

// Start runs the session-start lifecycle point and persists any injected system
// context in this Runtime's private conversation. A blocking hook prevents the
// session from starting.
func (r *Runtime) Start(ctx context.Context, emit EmitFunc) error {
	if r.hooks == nil {
		return nil
	}
	start := r.hooks.SessionStart(ctx, r.convo.Snapshot(), emit)
	for _, injected := range start.InjectedContext {
		r.convo.AppendSystem(injected)
	}
	if start.Block {
		return fmt.Errorf("session-start hook blocked startup: %s", start.Reason)
	}
	return nil
}

// Run submits one user turn, drives the shared agent loop, and invokes the
// prompt-submit and run-finished lifecycle points. It returns the authoritative
// terminal outcome but does not emit RunFinishedEvent; the runtime owner does so
// through its own event transport.
func (r *Runtime) Run(ctx context.Context, input string, emit EmitFunc) core.RunFinished {
	emit = normalizedEmit(emit)

	var transientContext []string
	if r.hooks != nil {
		prompt := r.hooks.PromptSubmit(ctx, input, r.convo.Snapshot(), emit)
		if prompt.Block {
			_ = emit(core.RunEvent{Type: core.ErrorEvent, Error: fmt.Errorf("prompt-submit hook blocked turn: %s", prompt.Reason)})
			_ = emit(core.RunEvent{Type: core.StatusChange, Status: core.StatusIdle})
			fin := core.RunFinished{Reason: core.StopFailed, Err: fmt.Errorf("%s", prompt.Reason)}
			_ = r.hooks.RunFinished(ctx, fin, r.convo.Snapshot(), emit)
			return fin
		}
		transientContext = append(transientContext, prompt.InjectedContext...)
	}

	r.convo.AppendUser(input)
	fin := loop.Run(ctx, loop.Deps{
		Provider:            r.provider,
		Context:             r.convo,
		Dispatcher:          r.dispatcher,
		Tools:               r.tools,
		MaxRounds:           r.maxRounds,
		ContextBudgetTokens: r.budget,
		StreamOptions:       r.opts,
		TransientContext:    transientContext,
	}, emit)

	if r.hooks != nil {
		_ = r.hooks.RunFinished(ctx, fin, r.convo.Snapshot(), emit)
	}
	return fin
}

// Stop runs the session-stop lifecycle point. A blocking cleanup hook is
// returned to the owner without changing any already-completed Run outcome.
func (r *Runtime) Stop(ctx context.Context, emit EmitFunc) error {
	if r.hooks == nil {
		return nil
	}
	stop := r.hooks.SessionStop(ctx, r.convo.Snapshot(), emit)
	if stop.Block {
		return fmt.Errorf("session-stop hook failed: %s", stop.Reason)
	}
	return nil
}

// Conversation returns a deep-copied snapshot of the Runtime's private history.
func (r *Runtime) Conversation() core.Conversation {
	if r == nil || r.convo == nil {
		return core.Conversation{}
	}
	return r.convo.Snapshot()
}

func normalizedEmit(emit EmitFunc) EmitFunc {
	if emit != nil {
		return emit
	}
	return func(core.RunEvent) error { return nil }
}
