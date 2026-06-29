package agent

import (
	"context"
	"errors"
	"sync/atomic"

	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/executor"
	"github.com/blkcor/coragent/internal/loop"
	"github.com/blkcor/coragent/internal/tools"
)

// defaultMaxRounds bounds how many model rounds a run may take before a normal
// step-limit stop, guarding against a model stuck requesting tools forever.
const defaultMaxRounds = 25

// ErrRunInFlight is returned when a second run is started on a session that
// already has one in flight. The in-flight run is unaffected.
var ErrRunInFlight = errors.New("agent: a run is already in flight on this session")

// SessionConfig configures a Session.
type SessionConfig struct {
	// Provider is the model backend. Required.
	Provider Provider

	// SystemPrompt seeds the conversation's system framing.
	SystemPrompt string

	// Tools are the capabilities offered to the model. When left empty and no
	// custom Dispatcher is set, the built-in tools are advertised automatically.
	Tools []Tool

	// ToolHandlers are custom executable tools registered alongside the built-ins
	// in the default executor. Ignored when a custom Dispatcher is supplied.
	ToolHandlers []ToolHandler

	// MaxRounds caps model rounds before a normal step-limit stop. Zero uses a default.
	MaxRounds int

	// ContextBudgetTokens is the advisory over-budget threshold. Zero disables the warning.
	ContextBudgetTokens int

	// StreamOptions are the per-request model options.
	StreamOptions StreamOptions

	// Dispatcher is the single tool-dispatch seam. Nil builds the default executor
	// (the ordered chain with inert stages) seeded with the built-in tools.
	Dispatcher Dispatcher
}

// Session is one agent interaction lifecycle. It owns the conversation and runs
// the agent loop, exposing a single run entry point and a read-only snapshot of
// history. One run is in flight at a time; a second concurrent start is refused.
type Session struct {
	provider   Provider
	convo      *convo.Manager
	dispatcher Dispatcher
	tools      []Tool
	maxRounds  int
	budget     int
	opts       StreamOptions

	inFlight atomic.Bool
}

// NewSession creates a Session from the given configuration.
func NewSession(cfg SessionConfig) *Session {
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultMaxRounds
	}

	d, advertised := resolveDispatcher(cfg)

	return &Session{
		provider:   cfg.Provider,
		convo:      convo.New(cfg.SystemPrompt),
		dispatcher: d,
		tools:      advertised,
		maxRounds:  maxRounds,
		budget:     cfg.ContextBudgetTokens,
		opts:       cfg.StreamOptions,
	}
}

// resolveDispatcher picks the tool-dispatch seam and the tool list advertised to
// the model. A caller-supplied Dispatcher is used as-is with the caller's Tools.
// Otherwise the default executor is built — the one ordered chain with inert
// stages over a catalog of the built-ins plus any custom handlers — and, unless
// the caller pinned an explicit Tools list, the catalog's own set is advertised.
func resolveDispatcher(cfg SessionConfig) (Dispatcher, []Tool) {
	if cfg.Dispatcher != nil {
		return cfg.Dispatcher, cfg.Tools
	}

	catalog := tools.NewDefaultCatalog()
	for _, h := range cfg.ToolHandlers {
		catalog.MustRegister(h)
	}
	advertised := cfg.Tools
	if advertised == nil {
		advertised = catalog.Advertise()
	}
	return executor.NewDefault(catalog), advertised
}

// Run starts a run from the user's input and returns one live, read-only event
// stream the caller drains to completion. A second concurrent run on the same
// session is refused with ErrRunInFlight, leaving the first run and history
// untouched.
func (s *Session) Run(ctx context.Context, input string) (<-chan RunEvent, error) {
	if !s.inFlight.CompareAndSwap(false, true) {
		return nil, ErrRunInFlight
	}

	s.convo.AppendUser(input)

	// Buffered by one so the single terminal event always has room to enqueue,
	// even when the context is already cancelled (guaranteed terminal delivery).
	ch := make(chan RunEvent, 1)

	go func() {
		defer close(ch)
		defer s.inFlight.Store(false)

		emit := func(ev RunEvent) error {
			select {
			case ch <- ev:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		fin := loop.Run(ctx, loop.Deps{
			Provider:            s.provider,
			Context:             s.convo,
			Dispatcher:          s.dispatcher,
			Tools:               s.tools,
			MaxRounds:           s.maxRounds,
			ContextBudgetTokens: s.budget,
			StreamOptions:       s.opts,
		}, emit)

		emitTerminal(ctx, ch, RunEvent{Type: RunFinishedEvent, RunFinished: &fin})
	}()

	return ch, nil
}

// Conversation returns a deep-copied snapshot of the conversation. Callers can
// inspect, log, or render it but cannot mutate the live conversation.
func (s *Session) Conversation() Conversation {
	return s.convo.Snapshot()
}

// emitTerminal delivers the single terminal RunFinishedEvent. A live reader (even
// one that just cancelled) receives it; an abandoned reader cannot wedge the
// goroutine because the channel's one-slot buffer always has room for this final
// send.
func emitTerminal(ctx context.Context, ch chan RunEvent, ev RunEvent) {
	select {
	case ch <- ev:
	case <-ctx.Done():
		select {
		case ch <- ev:
		default:
		}
	}
}
