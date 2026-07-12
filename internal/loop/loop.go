// Package loop implements the agent turn loop: the gather→act→verify cycle that
// drives the model through repeated rounds until a deterministic stop fires.
//
// Run consults the model, runs any requested tools through the single dispatch
// seam, feeds the results back, and repeats — emitting every observable event
// through the caller's emit callback. It computes and returns the single
// terminal stop reason; the caller emits the RunFinishedEvent and closes the
// stream. Exactly one stop reason is returned on every path.
package loop

import (
	"context"
	"errors"

	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/core"
)

var (
	errProviderStreamIncomplete    = errors.New("provider stream closed without ReplyEndedEvent")
	errProviderStreamAfterReplyEnd = errors.New("provider stream emitted an event after ReplyEndedEvent")
	errProviderStreamInvalidEnd    = errors.New("provider stream emitted an invalid ReplyEndedEvent")
)

// Deps are the collaborators the loop drives. The caller owns the event stream
// and supplies emit; the loop owns the round cycle.
type Deps struct {
	// Provider is the model backend.
	Provider core.Provider

	// Context accumulates the conversation across rounds.
	Context *convo.Manager

	// Dispatcher is the single tool-dispatch seam.
	Dispatcher core.Dispatcher

	// Tools are the capabilities offered to the model.
	Tools []core.Tool

	// MaxRounds caps how many model rounds may run before a normal step-limit stop.
	MaxRounds int

	// ContextBudgetTokens is the advisory over-budget threshold; zero disables it.
	ContextBudgetTokens int

	// UseRichProvider opts this run into the additive rich provider protocol.
	// Legacy entry points leave it false even when Provider also implements
	// RichProvider so their wire behavior remains unchanged.
	UseRichProvider bool

	// StreamOptions are the per-request model options.
	StreamOptions core.StreamOptions

	// TransientContext is harness-provided system context visible only to this
	// run's provider calls. It is not written to durable conversation history.
	TransientContext []string
}

// Run drives the loop and returns the single terminal stop reason. emit is
// called for every non-terminal event; it returns a non-nil error when the
// stream can no longer accept events (context cancelled), which Run treats as a
// cancellation.
func Run(ctx context.Context, d Deps, emit func(core.RunEvent) error) core.RunFinished {
	if emit == nil {
		emit = func(core.RunEvent) error { return nil }
	}
	return RunRich(ctx, d, core.Origin{AgentID: core.AgentID(core.NewOpaqueID("agent"))}, func(event core.RichEvent) error {
		if event.Legacy == nil {
			return nil
		}
		return emit(*event.Legacy)
	})
}

// drainProviderStream lets a cooperative legacy Provider finish its historical
// cancellation sequence (often one final unbuffered ErrorEvent followed by
// close) without delaying the run's cancellation result. Drained events are
// deliberately discarded and can never reach conversation or frontend state.
// A Provider that ignores cancellation and never closes violates its contract;
// in that case only this isolated cleanup goroutine remains blocked.
func drainProviderStream(events <-chan core.RunEvent) {
	go func() {
		for range events {
		}
	}()
}

func validReplyEnded(ended *core.ReplyEnded) bool {
	if ended == nil {
		return false
	}
	switch ended.Reason {
	case core.Finished, core.StoppedToCallTools, core.CutOff:
		return true
	default:
		return false
	}
}

func withTransientContext(conv core.Conversation, injected []string) core.Conversation {
	if len(injected) == 0 {
		return conv
	}
	turns := make([]core.Turn, 0, len(conv.Turns)+len(injected))
	if len(conv.Turns) > 0 {
		turns = append(turns, conv.Turns[0])
		for _, content := range injected {
			turns = append(turns, core.Turn{Role: "system", Content: content})
		}
		turns = append(turns, conv.Turns[1:]...)
	} else {
		for _, content := range injected {
			turns = append(turns, core.Turn{Role: "system", Content: content})
		}
	}
	return core.Conversation{Turns: turns}
}
