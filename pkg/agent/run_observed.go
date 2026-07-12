package agent

import (
	"context"

	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/sessionrun"
)

// RunObserved starts one run through the same canonical rich runtime as Run and
// returns its direct schema-v1 projection. The entry points share the same
// one-run-in-flight guard and never duplicate provider or tool work. Callers
// must drain the returned stream through closure, including after cancelling
// ctx, so the gap-free terminal boundary can be delivered.
func (s *Session) RunObserved(ctx context.Context, input string) (<-chan ObservedEvent, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.startupErr != nil {
		return nil, s.startupErr
	}
	if s.closed.Load() {
		return nil, ErrSessionClosed
	}
	if !s.inFlight.CompareAndSwap(false, true) {
		return nil, ErrRunInFlight
	}

	// The boundary slots keep run_started and a cancellation terminal observable
	// even if the caller does not begin draining until after cancellation.
	stream := make(chan ObservedEvent, 2)
	runID := RunID(newOpaqueID("run"))
	origin := Origin{AgentID: s.description.RootAgentID}

	go func() {
		defer close(stream)
		defer s.inFlight.Store(false)

		_ = s.runtime.RunRich(ctx, input, sessionrun.RichRunOptions{RunID: runID, Origin: origin, UseRichProvider: true}, func(event core.RichEvent) error {
			observed := event.Observed()
			switch event.Kind {
			case ObservedKindRunStarted:
				// Structural start is delivered even for an already-cancelled context.
				stream <- observed
				return nil
			case ObservedKindRunFinished:
				// Keep ownership of the session run guard until the caller drains
				// enough space for this gap-free terminal and the goroutine exits.
				// Releasing it here would let a later deferred Store(false) clear the
				// guard belonging to a newer run (an atomic-bool ABA race).
				stream <- observed
				return nil
			default:
				select {
				case stream <- observed:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		})
	}()

	return stream, nil
}
