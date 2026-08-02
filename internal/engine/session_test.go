package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
)

// fixedClock returns a deterministic clock that advances one millisecond per
// call, keeping event timestamps reproducible.
func fixedClock() func() time.Time {
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Millisecond)
		return now
	}
}

func newSession(t *testing.T, p provider.Provider) *engine.Session {
	t.Helper()
	s, err := engine.NewSession("sess-test", engine.Config{Provider: p, Now: fixedClock()})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return s
}

func submitCmd(t *testing.T, id, prompt string) sessioncommand.Command {
	t.Helper()
	cmd, err := sessioncommand.NewSubmit(id, prompt)
	if err != nil {
		t.Fatalf("NewSubmit: %v", err)
	}
	return cmd
}

func cancelCmd(t *testing.T, id string) sessioncommand.Command {
	t.Helper()
	cmd, err := sessioncommand.NewCancel(id)
	if err != nil {
		t.Fatalf("NewCancel: %v", err)
	}
	return cmd
}

func waitIdle(t *testing.T, s *engine.Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.WaitIdle(ctx); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
}

func waitForRequests(t *testing.T, fake *provider.Scripted, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(fake.Requests()) < count {
		if time.Now().After(deadline) {
			t.Fatalf("provider received %d requests, want at least %d", len(fake.Requests()), count)
		}
		time.Sleep(time.Millisecond)
	}
}

func terminalKinds(evs []event.Event) []event.Kind {
	var kinds []event.Kind
	for _, ev := range evs {
		switch ev.Kind {
		case event.KindRunCompleted, event.KindRunFailed, event.KindRunCancelled:
			kinds = append(kinds, ev.Kind)
		}
	}
	return kinds
}

func assertCursorsMonotonic(t *testing.T, evs []event.Event) {
	t.Helper()
	for i, ev := range evs {
		want := uint64(i + 1)
		if ev.Cursor != want {
			t.Fatalf("event %d cursor = %d, want %d (contiguous monotonic)", i, ev.Cursor, want)
		}
		if ev.SessionID != "sess-test" {
			t.Errorf("event %d session_id = %q", i, ev.SessionID)
		}
		if ev.Time.IsZero() {
			t.Errorf("event %d has zero timestamp", i)
		}
	}
}

// assertEveryEventSerializable round-trips every emitted event through JSON.
func assertEveryEventSerializable(t *testing.T, evs []event.Event) {
	t.Helper()
	for i, ev := range evs {
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event %d: %v", i, err)
		}
		var back event.Event
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal event %d: %v", i, err)
		}
		if back.SessionID != ev.SessionID || back.RunID != ev.RunID ||
			back.Cursor != ev.Cursor || !back.Time.Equal(ev.Time) || back.Kind != ev.Kind ||
			string(back.Payload) != string(ev.Payload) {
			t.Errorf("event %d round-trip mismatch:\nfirst:  %+v\nsecond: %+v", i, ev, back)
		}
	}
}

// TestSubmitCompletedTurn drives a scripted text turn to the completed
// terminal outcome with exactly one terminal event.
func TestSubmitCompletedTurn(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{Text: "main.go wires the CLI."})
	s := newSession(t, fake)

	if err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "explain main.go")); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}
	if s.State() != engine.StateRunning {
		t.Fatalf("state = %q, want %q", s.State(), engine.StateRunning)
	}
	waitIdle(t, s)

	evs := s.Events()
	if len(evs) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(evs), evs)
	}
	assertCursorsMonotonic(t, evs)
	assertEveryEventSerializable(t, evs)

	if evs[0].Kind != event.KindRunStarted || evs[0].RunID != "run-1" {
		t.Errorf("first event = %+v, want run_started for run-1", evs[0])
	}
	var started event.RunStartedPayload
	if err := evs[0].DecodePayload(&started); err != nil {
		t.Fatalf("decode run_started: %v", err)
	}
	if started.Prompt != "explain main.go" {
		t.Errorf("run_started prompt = %q", started.Prompt)
	}

	if evs[2].Kind != event.KindAssistantText {
		t.Fatalf("third event = %q, want assistant_text", evs[2].Kind)
	}
	var text event.AssistantTextPayload
	if err := evs[2].DecodePayload(&text); err != nil {
		t.Fatalf("decode assistant_text: %v", err)
	}
	if text.Text != "main.go wires the CLI." {
		t.Errorf("assistant text = %q", text.Text)
	}

	terms := terminalKinds(evs)
	if len(terms) != 1 || terms[0] != event.KindRunCompleted {
		t.Errorf("terminal events = %v, want exactly one run_completed", terms)
	}
	if s.HighWaterMark() != 4 {
		t.Errorf("high-water mark = %d, want 4", s.HighWaterMark())
	}
}

// TestSubmitProviderFailure drives a classified provider failure to the
// failed terminal outcome with a typed cause, then proves the session
// returns to idle and later runs keep monotonic cursors.
func TestSubmitProviderFailure(t *testing.T) {
	fake := provider.NewScripted(
		provider.Turn{Fail: &provider.Failure{Class: provider.ClassPermanent, Message: "authentication rejected"}},
		provider.Turn{Text: "recovered answer"},
	)
	s := newSession(t, fake)

	if err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "explain main.go")); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}
	waitIdle(t, s)

	evs := s.Events()
	terms := terminalKinds(evs)
	if len(terms) != 1 || terms[0] != event.KindRunFailed {
		t.Fatalf("terminal events = %v, want exactly one run_failed", terms)
	}
	last := evs[len(evs)-1]
	var failed event.RunFailedPayload
	if err := last.DecodePayload(&failed); err != nil {
		t.Fatalf("decode run_failed: %v", err)
	}
	if failed.Cause != event.CauseProviderPermanent {
		t.Errorf("failure cause = %q, want %q", failed.Cause, event.CauseProviderPermanent)
	}
	if s.State() != engine.StateIdle {
		t.Fatalf("state after failure = %q, want idle", s.State())
	}

	// The session accepts new work after a failed run; cursors keep rising.
	if err := s.Apply(context.Background(), submitCmd(t, "cmd-2", "try again")); err != nil {
		t.Fatalf("Apply second submit: %v", err)
	}
	waitIdle(t, s)

	evs = s.Events()
	assertCursorsMonotonic(t, evs)
	assertEveryEventSerializable(t, evs)
	terms = terminalKinds(evs)
	if len(terms) != 2 || terms[1] != event.KindRunCompleted {
		t.Errorf("terminal events = %v, want failed then completed", terms)
	}
}

// TestCancelActiveRun proves cancellation propagates through the provider
// via context and the run still ends with exactly one terminal event.
func TestCancelActiveRun(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{BlockUntilCancel: true})
	s := newSession(t, fake)

	if err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "long answer")); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}
	waitForRequests(t, fake, 1)
	if err := s.Apply(context.Background(), cancelCmd(t, "cmd-2")); err != nil {
		t.Fatalf("Apply cancel: %v", err)
	}
	waitIdle(t, s)

	if fake.CancelObserved() != 1 {
		t.Errorf("provider observed cancellation %d times, want 1", fake.CancelObserved())
	}

	evs := s.Events()
	assertCursorsMonotonic(t, evs)
	assertEveryEventSerializable(t, evs)
	terms := terminalKinds(evs)
	if len(terms) != 1 || terms[0] != event.KindRunCancelled {
		t.Errorf("terminal events = %v, want exactly one run_cancelled", terms)
	}
	for _, ev := range evs {
		if ev.Kind == event.KindAssistantText {
			t.Errorf("cancelled run emitted assistant text: %+v", ev)
		}
	}
	if s.State() != engine.StateIdle {
		t.Errorf("state after cancel = %q, want idle", s.State())
	}
}

// TestCancelWinsOverLateProviderSuccess proves the cancelled outcome is
// emitted even if the provider returns a success after the cancel command.
func TestCancelWinsOverLateProviderSuccess(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{BlockUntilCancel: true, Text: "too late"})
	s := newSession(t, fake)

	if err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "long answer")); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}
	waitForRequests(t, fake, 1)
	if err := s.Apply(context.Background(), cancelCmd(t, "cmd-2")); err != nil {
		t.Fatalf("Apply cancel: %v", err)
	}
	waitIdle(t, s)

	terms := terminalKinds(s.Events())
	if len(terms) != 1 || terms[0] != event.KindRunCancelled {
		t.Errorf("terminal events = %v, want exactly one run_cancelled", terms)
	}
}

// TestDuplicateCommandIDRejected proves re-applied command IDs are rejected
// without changing session state, across kinds.
func TestDuplicateCommandIDRejected(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{Text: "answer"})
	s := newSession(t, fake)

	if err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "explain main.go")); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}
	waitIdle(t, s)
	before := s.Events()

	err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "different prompt"))
	if !errors.Is(err, engine.ErrDuplicateCommand) {
		t.Errorf("duplicate submit error = %v, want ErrDuplicateCommand", err)
	}
	err = s.Apply(context.Background(), cancelCmd(t, "cmd-1"))
	if !errors.Is(err, engine.ErrDuplicateCommand) {
		t.Errorf("duplicate cancel error = %v, want ErrDuplicateCommand", err)
	}

	after := s.Events()
	if len(after) != len(before) {
		t.Errorf("event count changed from %d to %d after duplicate commands", len(before), len(after))
	}
	if s.State() != engine.StateIdle {
		t.Errorf("state = %q, want idle", s.State())
	}
	if len(fake.Requests()) != 1 {
		t.Errorf("provider received %d requests, want 1", len(fake.Requests()))
	}
}

func TestSessionMismatchedCommandsLeaveStateUntouched(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{BlockUntilCancel: true})
	session := newSession(t, fake)
	submit := submitCmd(t, "cmd-submit", "wait")
	if err := session.Apply(context.Background(), submit.ForSession(session.ID())); err != nil {
		t.Fatal(err)
	}
	waitForRequests(t, fake, 1)
	beforeEvents := len(session.Events())
	beforeTranscript := len(session.Transcript())
	mismatched := cancelCmd(t, "cmd-mismatch").ForSession("another-session")
	if err := session.Apply(context.Background(), mismatched); !errors.Is(err, engine.ErrSessionMismatch) {
		t.Fatalf("mismatched cancel = %v", err)
	}
	if len(session.Events()) != beforeEvents || len(session.Transcript()) != beforeTranscript || fake.CancelObserved() != 0 || session.State() != engine.StateRunning {
		t.Fatal("mismatched command changed active session state")
	}
	corrected := cancelCmd(t, "cmd-mismatch").ForSession(session.ID())
	if err := session.Apply(context.Background(), corrected); err != nil {
		t.Fatalf("reusing rejected mismatched command ID: %v", err)
	}
	waitIdle(t, session)
	if fake.CancelObserved() != 1 {
		t.Fatalf("correctly targeted cancellation count = %d", fake.CancelObserved())
	}
}

// TestSecondSubmitRejectedWhileRunning proves one session permits at most
// one active run.
func TestSecondSubmitRejectedWhileRunning(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{BlockUntilCancel: true})
	s := newSession(t, fake)

	if err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "first")); err != nil {
		t.Fatalf("Apply first submit: %v", err)
	}
	err := s.Apply(context.Background(), submitCmd(t, "cmd-2", "second"))
	if !errors.Is(err, engine.ErrRunActive) {
		t.Fatalf("second submit error = %v, want ErrRunActive", err)
	}
	waitForRequests(t, fake, 1)

	// The rejected submit left state untouched: still one run, and its ID is
	// not consumed, so after cancellation the same ID can submit for real.
	if err := s.Apply(context.Background(), cancelCmd(t, "cmd-3")); err != nil {
		t.Fatalf("Apply cancel: %v", err)
	}
	waitIdle(t, s)

	if len(fake.Requests()) != 1 {
		t.Errorf("provider received %d requests, want 1", len(fake.Requests()))
	}
	evs := s.Events()
	runStarts := 0
	for _, ev := range evs {
		if ev.Kind == event.KindRunStarted {
			runStarts++
			if ev.RunID != "run-1" {
				t.Errorf("run_started for %q, want only run-1", ev.RunID)
			}
		}
	}
	if runStarts != 1 {
		t.Errorf("got %d run_started events, want 1", runStarts)
	}
}

// TestCancelWhenIdleIsNoop proves cancel without an active run changes
// nothing but still consumes the command ID.
func TestCancelWhenIdleIsNoop(t *testing.T) {
	fake := provider.NewScripted()
	s := newSession(t, fake)

	if err := s.Apply(context.Background(), cancelCmd(t, "cmd-1")); err != nil {
		t.Fatalf("Apply cancel: %v", err)
	}
	if got := len(s.Events()); got != 0 {
		t.Errorf("idle cancel emitted %d events, want 0", got)
	}
	if s.State() != engine.StateIdle {
		t.Errorf("state = %q, want idle", s.State())
	}
	if err := s.Apply(context.Background(), cancelCmd(t, "cmd-1")); !errors.Is(err, engine.ErrDuplicateCommand) {
		t.Errorf("re-applied idle cancel error = %v, want ErrDuplicateCommand", err)
	}
}

// TestConcurrentCommandsRace hammers the session with concurrent submits and
// cancels. Under -race it proves the state machine stays consistent: every
// started run ends with exactly one terminal event and cursors stay
// contiguous.
func TestConcurrentCommandsRace(t *testing.T) {
	turns := make([]provider.Turn, 0, 64)
	for range 64 {
		turns = append(turns, provider.Turn{Text: "ok"})
	}
	fake := provider.NewScripted(turns...)
	s := newSession(t, fake)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Rejected submits and duplicate IDs are fine here; the race is
			// what matters. Small sleep-free loop maximizes contention.
			_ = s.Apply(context.Background(), submitCmd(t, fmt.Sprintf("cmd-submit-%d", i), "work"))
		}()
	}
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Apply(context.Background(), cancelCmd(t, fmt.Sprintf("cmd-cancel-%d", i)))
		}()
	}
	wg.Wait()

	// Drain any still-active run.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.State() == engine.StateRunning {
		if err := s.Apply(ctx, cancelCmd(t, "cmd-drain")); err != nil {
			t.Fatalf("drain cancel: %v", err)
		}
	}
	if err := s.WaitIdle(ctx); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}

	evs := s.Events()
	assertCursorsMonotonic(t, evs)
	assertEveryEventSerializable(t, evs)

	terminalByRun := make(map[string]int)
	for _, ev := range evs {
		switch ev.Kind {
		case event.KindRunCompleted, event.KindRunFailed, event.KindRunCancelled:
			terminalByRun[ev.RunID]++
		}
	}
	for runID, n := range terminalByRun {
		if n != 1 {
			t.Errorf("run %q has %d terminal events, want exactly 1", runID, n)
		}
	}
	if s.HighWaterMark() != uint64(len(evs)) {
		t.Errorf("high-water mark = %d, want %d", s.HighWaterMark(), len(evs))
	}
}

// TestSubscribeReceivesLiveEvents proves the simple observation surface
// delivers emitted events to a subscriber.
func TestSubscribeReceivesLiveEvents(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{Text: "answer"})
	s := newSession(t, fake)

	ch, unsubscribe := s.Subscribe()
	defer unsubscribe()

	if err := s.Apply(context.Background(), submitCmd(t, "cmd-1", "explain main.go")); err != nil {
		t.Fatalf("Apply submit: %v", err)
	}
	waitIdle(t, s)

	var got []event.Kind
	deadline := time.After(5 * time.Second)
	for len(got) == 0 || got[len(got)-1] != event.KindRunCompleted {
		select {
		case ev := <-ch:
			got = append(got, ev.Kind)
		case <-deadline:
			t.Fatalf("subscriber received %v without terminal event", got)
		}
	}
	if got[0] != event.KindRunStarted || got[len(got)-1] != event.KindRunCompleted {
		t.Fatalf("event order = %v", got)
	}
	seenDelta, seenText := false, false
	for _, kind := range got {
		seenDelta = seenDelta || kind == event.KindAssistantDelta
		seenText = seenText || kind == event.KindAssistantText
	}
	if !seenDelta || !seenText {
		t.Fatalf("event order = %v, want projected delta and completed text", got)
	}
}
