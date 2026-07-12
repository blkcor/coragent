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
	"time"

	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/loop"
)

// EmitFunc publishes one non-terminal run event. The owner of a Runtime decides
// where those events go and remains responsible for publishing the terminal
// RunFinishedEvent after Run returns.
type EmitFunc func(core.RunEvent) error

// RichEmitFunc publishes one event from the canonical internal envelope.
type RichEmitFunc func(core.RichEvent) error

// RichRunOptions supplies stable root correlation and an injectable observation
// clock. Zero values receive safe opaque IDs and time.Now defaults.
type RichRunOptions struct {
	RunID           core.RunID
	Origin          core.Origin
	Now             func() time.Time
	UseRichProvider bool
}

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
	return r.RunRich(ctx, input, RichRunOptions{}, func(event core.RichEvent) error {
		// Runtime.Run historically leaves terminal delivery to its owner. Keep
		// that internal compatibility while still executing through RunRich.
		if event.Kind == core.ObservedKindRunStarted || event.Kind == core.ObservedKindRunFinished || event.Legacy == nil {
			return nil
		}
		return emit(*event.Legacy)
	})
}

// RunRich is the one authoritative session lifecycle. It assigns envelope
// identity and ordering, runs prompt and run-finished hooks, drives the rich
// loop, and emits exactly one terminal fact last.
func (r *Runtime) RunRich(ctx context.Context, input string, options RichRunOptions, emit RichEmitFunc) core.RunFinished {
	if options.RunID == "" {
		options.RunID = core.RunID(core.NewOpaqueID("run"))
	}
	if options.Origin.AgentID == "" {
		options.Origin = core.Origin{AgentID: core.AgentID(core.NewOpaqueID("agent"))}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	boundary := richBoundary{options: options, emit: emit}
	// Run boundaries are structural even if the caller cancelled before start.
	_ = boundary.publish(core.RichEvent{Kind: core.ObservedKindRunStarted, Payload: &core.RunStartedPayload{}}, true)
	legacyEmit := func(event core.RunEvent) error {
		return boundary.publish(adaptLifecycleLegacy(ctx, options.Origin, event), false)
	}

	var transientContext []string
	if r.hooks != nil {
		prompt := r.hooks.PromptSubmit(ctx, input, r.convo.Snapshot(), legacyEmit)
		if prompt.Block {
			_ = legacyEmit(core.RunEvent{Type: core.ErrorEvent, Error: fmt.Errorf("prompt-submit hook blocked turn: %s", prompt.Reason)})
			_ = legacyEmit(core.RunEvent{Type: core.StatusChange, Status: core.StatusIdle})
			fin := core.RunFinished{Reason: core.StopFailed, Err: fmt.Errorf("%s", prompt.Reason)}
			_ = r.hooks.RunFinished(ctx, fin, r.convo.Snapshot(), legacyEmit)
			_ = boundary.publishTerminal(options.Origin, fin)
			return fin
		}
		transientContext = append(transientContext, prompt.InjectedContext...)
	}

	r.convo.AppendUser(input)
	fin := loop.RunRich(ctx, loop.Deps{
		Provider:            r.provider,
		Context:             r.convo,
		Dispatcher:          r.dispatcher,
		Tools:               r.tools,
		MaxRounds:           r.maxRounds,
		ContextBudgetTokens: r.budget,
		UseRichProvider:     options.UseRichProvider,
		StreamOptions:       r.opts,
		TransientContext:    transientContext,
	}, options.Origin, func(event core.RichEvent) error {
		return boundary.publish(event, false)
	})

	if r.hooks != nil {
		_ = r.hooks.RunFinished(ctx, fin, r.convo.Snapshot(), legacyEmit)
	}
	_ = boundary.publishTerminal(options.Origin, fin)
	return fin
}

type richBoundary struct {
	options           RichRunOptions
	emit              RichEmitFunc
	sequence          uint64
	terminalPublished bool
}

func (b *richBoundary) publish(event core.RichEvent, force bool) error {
	if b.emit == nil || b.terminalPublished {
		return nil
	}
	if event.Origin.AgentID == "" {
		event.Origin = b.options.Origin
	}
	event.RunID = b.options.RunID
	event.Sequence = b.sequence + 1
	if event.Timestamp.IsZero() {
		event.Timestamp = b.options.Now()
	}
	if err := b.emit(event); err != nil && !force {
		return err
	}
	b.sequence++
	return nil
}

func (b *richBoundary) publishTerminal(origin core.Origin, finished core.RunFinished) error {
	if b.terminalPublished {
		return nil
	}
	payload := &core.RunFinishedPayload{Outcome: observedOutcome(finished.Reason)}
	if finished.Err != nil {
		err := core.ObservedError{Code: "run_failed", Message: "the run encountered an error", Recoverable: false}
		payload.Error = &err
	}
	legacy := core.RunEvent{Type: core.RunFinishedEvent, RunFinished: &finished}
	event := core.RichEvent{Origin: origin, Kind: core.ObservedKindRunFinished, Payload: payload, Legacy: &legacy}
	// Mark only after construction so exactly this one event can be published.
	err := b.publish(event, true)
	b.terminalPublished = true
	return err
}

func adaptLifecycleLegacy(ctx context.Context, origin core.Origin, event core.RunEvent) core.RichEvent {
	legacy := event
	switch event.Type {
	case core.HookOutcomeEvent:
		if event.HookOutcome != nil {
			outcome := *event.HookOutcome
			return core.RichEvent{Origin: origin, Kind: core.ObservedKindHookOutcome, Payload: &core.HookOutcomePayload{Outcome: outcome}, Legacy: &legacy}
		}
	case core.ErrorEvent:
		message := "the run encountered an error"
		if ctx.Err() != nil {
			message = "the run was cancelled"
		}
		return core.RichEvent{Origin: origin, Kind: core.ObservedKindError, Payload: &core.ErrorPayload{Error: core.ObservedError{Code: "lifecycle_error", Message: message, Recoverable: true}}, Legacy: &legacy}
	case core.StatusChange:
		status := core.ActivityUnknown
		switch event.Status {
		case core.StatusThinking:
			status = core.ActivityThinking
		case core.StatusCallingTool:
			status = core.ActivityCallingTool
		case core.StatusIdle:
			status = core.ActivityIdle
		}
		return core.RichEvent{Origin: origin, Kind: core.ObservedKindStatusChanged, Payload: &core.StatusChangedPayload{Status: status, Detail: event.Status}, Legacy: &legacy}
	case core.OverBudgetWarningEvent:
		return core.RichEvent{Origin: origin, Kind: core.ObservedKindWarning, Payload: &core.WarningPayload{Code: "legacy_context_budget", Message: event.Warning}, Legacy: &legacy}
	}
	return core.RichEvent{Origin: origin, Kind: core.ObservedKindWarning, Payload: &core.WarningPayload{Code: "legacy_lifecycle_event", Message: "a legacy lifecycle event was observed"}, Legacy: &legacy}
}

func observedOutcome(reason core.StopReason) core.RunOutcome {
	switch reason {
	case core.StopCompleted:
		return core.RunOutcomeCompleted
	case core.StopReachedStepLimit:
		return core.RunOutcomeReachedStepLimit
	case core.StopCancelled:
		return core.RunOutcomeCancelled
	case core.StopFailed:
		return core.RunOutcomeFailed
	default:
		return core.RunOutcomeUnknown
	}
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
