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
	"fmt"

	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/core"
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

	// StreamOptions are the per-request model options.
	StreamOptions core.StreamOptions
}

// Run drives the loop and returns the single terminal stop reason. emit is
// called for every non-terminal event; it returns a non-nil error when the
// stream can no longer accept events (context cancelled), which Run treats as a
// cancellation.
func Run(ctx context.Context, d Deps, emit func(core.RunEvent) error) core.RunFinished {
	var warnedOverBudget bool
	for round := 0; ; round++ {
		// A model stuck requesting tools forever is halted by the step limit,
		// reported as a normal stop.
		if round >= d.MaxRounds {
			_ = emit(statusEvent(core.StatusIdle))
			return core.RunFinished{Reason: core.StopReachedStepLimit}
		}

		// Gather: assemble the conversation and warn once if it is over budget.
		snap := d.Context.Snapshot()
		if d.ContextBudgetTokens > 0 && !warnedOverBudget {
			if est := d.Context.EstimateTokens(); est > d.ContextBudgetTokens {
				warnedOverBudget = true
				if err := emit(core.RunEvent{
					Type:    core.OverBudgetWarningEvent,
					Warning: fmt.Sprintf("conversation exceeds context budget (~%d tokens > %d); proceeding", est, d.ContextBudgetTokens),
				}); err != nil {
					return core.RunFinished{Reason: core.StopCancelled}
				}
			}
		}

		if err := emit(statusEvent(core.StatusThinking)); err != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}

		// Consult the model, streaming text out and accumulating tool calls.
		text, calls, provErr, sendErr := consult(ctx, d, snap, emit)

		// Cancellation wins over a provider error (the fake/real backend
		// surfaces ctx.Err() as an error event on cancel).
		if ctx.Err() != nil || sendErr != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}
		if provErr != nil {
			_ = emit(statusEvent(core.StatusIdle))
			return core.RunFinished{Reason: core.StopFailed, Err: provErr}
		}

		// Record the assistant turn (a partial reply on error is never recorded).
		d.Context.AppendAssistant(text, calls)

		// Verify: no tool request means the task is finished.
		if len(calls) == 0 {
			if err := emit(statusEvent(core.StatusIdle)); err != nil {
				return core.RunFinished{Reason: core.StopCancelled}
			}
			return core.RunFinished{Reason: core.StopCompleted}
		}

		// Act: run each requested tool in order through the single seam.
		if err := emit(statusEvent(core.StatusCallingTool)); err != nil {
			return core.RunFinished{Reason: core.StopCancelled}
		}
		results, fin, done := dispatchAll(ctx, d, calls, emit)
		if done {
			return fin
		}
		d.Context.AppendToolResults(results)
		// Around again.
	}
}

// consult drains the provider stream for one round, emitting text incrementally
// and accumulating tool calls. It returns the assembled text, the tool calls,
// any provider error, and any emit (send) error.
func consult(ctx context.Context, d Deps, snap core.Conversation, emit func(core.RunEvent) error) (text string, calls []core.ToolCall, provErr, sendErr error) {
	var buf []byte
	for ev := range d.Provider.StreamReply(ctx, snap, d.Tools, d.StreamOptions) {
		if sendErr != nil {
			continue // drain remaining events so the provider goroutine completes
		}
		switch ev.Type {
		case core.TextDelta:
			buf = append(buf, ev.TextDelta...)
			sendErr = emit(ev)
		case core.ToolCallEvent:
			if ev.ToolCall != nil {
				calls = append(calls, *ev.ToolCall)
			}
		case core.ErrorEvent:
			provErr = ev.Error
		case core.ReplyEndedEvent:
			// Reply-end reason is provider-level; the loop decides the run outcome.
		}
	}
	return string(buf), calls, provErr, sendErr
}

// dispatchAll runs the round's tool calls sequentially. It returns the collected
// results, or a terminal stop (done=true) on cancellation or an unrecoverable
// dispatch condition.
func dispatchAll(ctx context.Context, d Deps, calls []core.ToolCall, emit func(core.RunEvent) error) (results []core.ToolResult, fin core.RunFinished, done bool) {
	results = make([]core.ToolResult, 0, len(calls))
	for i := range calls {
		call := calls[i]
		if err := emit(core.RunEvent{Type: core.ToolStartedEvent, ToolCall: &call}); err != nil {
			return nil, core.RunFinished{Reason: core.StopCancelled}, true
		}

		res, derr := d.Dispatcher.Dispatch(ctx, call, emit)

		// Cancellation mid-tool wins over the dispatch error.
		if ctx.Err() != nil {
			return nil, core.RunFinished{Reason: core.StopCancelled}, true
		}
		if derr != nil {
			_ = emit(statusEvent(core.StatusIdle))
			return nil, core.RunFinished{Reason: core.StopFailed, Err: derr}, true
		}

		result := res
		if err := emit(core.RunEvent{Type: core.ToolFinishedEvent, ToolResult: &result}); err != nil {
			return nil, core.RunFinished{Reason: core.StopCancelled}, true
		}
		results = append(results, res)
	}
	return results, core.RunFinished{}, false
}

func statusEvent(status string) core.RunEvent {
	return core.RunEvent{Type: core.StatusChange, Status: status}
}
