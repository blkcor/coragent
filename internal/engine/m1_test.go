package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/prompt"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

type durableFixture struct {
	root        string
	workspace   string
	durable     *store.Session
	broker      *action.Broker
	assembler   *prompt.Assembler
	workspaceFS *workspace.FS
}

func TestDiagnosticsNeverContainPromptOrModelContent(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	fake := provider.NewScripted(provider.Turn{Text: "private model response"})
	s, err := engine.NewSession("sess-log", engine.Config{Provider: fake, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	cmd, _ := sessioncommand.NewSubmit("cmd-log", "private user prompt")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	if strings.Contains(logs.String(), "private user prompt") || strings.Contains(logs.String(), "private model response") {
		t.Fatalf("content entered logs: %s", logs.String())
	}
}

func newDurableFixture(t *testing.T) durableFixture {
	t.Helper()
	root := t.TempDir()
	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "main.go"), []byte("package main\n\nfunc Alpha() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	durable, err := store.Create(root, "sess-durable", workspaceDir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Open(workspaceDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	broker, err := action.NewBroker(tools.NewCatalog(w, dataproj.New())...)
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.Config{Workspace: workspaceDir, ActivePath: ".", ContextWindow: 32000, MaxOutputTokens: 8000})
	if err != nil {
		t.Fatal(err)
	}
	return durableFixture{root: root, workspace: workspaceDir, durable: durable, broker: broker, assembler: assembler, workspaceFS: w}
}

func newDurableEngineSession(t *testing.T, fixture durableFixture, fake provider.Provider, opts ...func(*engine.Config)) *engine.Session {
	t.Helper()
	cfg := engine.Config{Provider: fake, Durable: fixture.durable, Broker: fixture.broker, Assembler: fixture.assembler, Projector: dataproj.New()}
	for _, opt := range opts {
		opt(&cfg)
	}
	s, err := engine.NewSession("sess-durable", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDurableMultiToolLoopPairsBeforeNextRequest(t *testing.T) {
	fixture := newDurableFixture(t)
	fake := provider.NewScripted(
		provider.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "list", Arguments: json.RawMessage(`{}`)}}, Reason: provider.ReasonStop},
		provider.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "search", Arguments: json.RawMessage(`{"pattern":"Alpha"}`)}}},
		provider.Turn{ToolCalls: []provider.ToolCall{{ID: "c3", Name: "read", Arguments: json.RawMessage(`{"path":"main.go","start_line":3,"end_line":3}`)}}},
		provider.Turn{Text: "Alpha is declared at main.go:3-3.", Reason: provider.ReasonStop},
	)
	s := newDurableEngineSession(t, fixture, fake)
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "Where is Alpha?")
	if err := s.Apply(context.Background(), cmd.ForSession(s.ID())); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	if got := len(fake.Requests()); got != 4 {
		t.Fatalf("provider requests = %d", got)
	}
	if err := transcript.ValidateTranscript(s.Transcript()); err != nil {
		t.Fatalf("paired transcript: %v", err)
	}
	for i, req := range fake.Requests()[1:] {
		toolResults := 0
		for _, msg := range req.Messages {
			if msg.Role == provider.RoleTool {
				toolResults++
			}
		}
		if toolResults != i+1 {
			t.Fatalf("request %d has %d tool results, want %d", i+2, toolResults, i+1)
		}
	}
	events := s.Events()
	terminal := 0
	for _, ev := range events {
		if ev.Kind == event.KindRunCompleted {
			terminal++
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal completed events = %d", terminal)
	}
	reopened, err := store.Open(fixture.root, s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := transcript.ValidateTranscript(reopened.Transcript()); err != nil {
		t.Fatalf("reopened transcript: %v", err)
	}
}

func TestDurableResumeCloseAndReplay(t *testing.T) {
	fixture := newDurableFixture(t)
	first := provider.NewScripted(provider.Turn{Text: "first answer"})
	s := newDurableEngineSession(t, fixture, first)
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "first question")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	prior := s.Transcript()

	durable, err := store.Open(fixture.root, s.ID())
	if err != nil {
		t.Fatal(err)
	}
	fixture.durable = durable
	second := provider.NewScripted(provider.Turn{Text: "follow-up answer"})
	resumed := newDurableEngineSession(t, fixture, second)
	if len(resumed.Transcript()) != len(prior) {
		t.Fatalf("replay length = %d, want %d", len(resumed.Transcript()), len(prior))
	}
	resume, _ := sessioncommand.NewResume("cmd-resume")
	if err := resumed.Apply(context.Background(), resume); err != nil {
		t.Fatal(err)
	}
	follow, _ := sessioncommand.NewSubmit("cmd-2", "follow-up")
	if err := resumed.Apply(context.Background(), follow); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, resumed)
	if len(second.Requests()) != 1 || len(second.Requests()[0].Messages) < 3 {
		t.Fatalf("resumed request context = %+v", second.Requests())
	}
	closeCmd, _ := sessioncommand.NewClose("cmd-close")
	if err := resumed.Apply(context.Background(), closeCmd); err != nil {
		t.Fatal(err)
	}
	if resumed.State() != engine.StateClosed {
		t.Fatalf("state = %s", resumed.State())
	}
	again, _ := sessioncommand.NewSubmit("cmd-3", "not allowed")
	if err := resumed.Apply(context.Background(), again); !errors.Is(err, engine.ErrSessionClosed) {
		t.Fatalf("submit after close = %v", err)
	}
	if err := resumed.Apply(context.Background(), closeCmd); !errors.Is(err, engine.ErrDuplicateCommand) {
		t.Fatalf("duplicate close = %v", err)
	}
}

func TestSensitivePromptAndStreamNeverCrossBoundaries(t *testing.T) {
	fixture := newDurableFixture(t)
	secret := strings.Join([]string{"sk", "0123456789abcdefghij012345"}, "-")
	fake := provider.NewScripted(provider.Turn{Deltas: []string{"safe ", secret[:9], secret[9:] + " done"}})
	s := newDurableEngineSession(t, fixture, fake)
	bad, _ := sessioncommand.NewSubmit("cmd-bad", "use "+secret)
	if err := s.Apply(context.Background(), bad); !errors.Is(err, engine.ErrSensitivePrompt) {
		t.Fatalf("sensitive submit = %v", err)
	}
	if len(fake.Requests()) != 0 {
		t.Fatal("sensitive prompt reached Provider")
	}
	good, _ := sessioncommand.NewSubmit("cmd-good", "safe question")
	if err := s.Apply(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	encoded, _ := json.Marshal(struct {
		Transcript []transcript.Record
		Events     []event.Event
	}{s.Transcript(), s.Events()})
	if strings.Contains(string(encoded), secret) {
		t.Fatal("detected credential crossed Transcript or Event boundary")
	}
	if !strings.Contains(string(encoded), "REDACTED") {
		t.Fatal("redaction marker absent")
	}
}

func TestSensitiveToolCallIsRedactedPairedAndNotContinued(t *testing.T) {
	fixture := newDurableFixture(t)
	secret := strings.Join([]string{"sk", "0123456789abcdefghij012345"}, "-")
	fake := provider.NewScripted(
		provider.Turn{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"` + secret + `"}`)}}},
		provider.Turn{Text: "must not be requested"},
	)
	s := newDurableEngineSession(t, fixture, fake)
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "safe prompt")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	if len(fake.Requests()) != 1 {
		t.Fatalf("Provider continuation count = %d, want 1 total request", len(fake.Requests()))
	}
	if err := transcript.ValidateTranscript(s.Transcript()); err != nil {
		t.Fatalf("paired transcript: %v", err)
	}
	encoded, _ := json.Marshal(struct {
		Records []transcript.Record
		Events  []event.Event
	}{s.Transcript(), s.Events()})
	if strings.Contains(string(encoded), secret) {
		t.Fatal("sensitive ToolCall crossed a projection boundary")
	}
	blocked := 0
	for _, record := range s.Transcript() {
		if record.Kind == transcript.KindToolResult {
			var result transcript.ToolResultPayload
			_ = record.DecodePayload(&result)
			if result.Outcome == transcript.ToolResultBlocked {
				blocked++
			}
		}
	}
	if blocked != 1 {
		t.Fatalf("blocked results = %d", blocked)
	}
}

func TestRetrySequenceBudgetAndCancellation(t *testing.T) {
	fixture := newDurableFixture(t)
	var mu sync.Mutex
	var delays []time.Duration
	fake := provider.NewScripted(
		provider.Turn{Fail: &provider.Failure{Class: provider.ClassRateLimit, Message: "rate"}},
		provider.Turn{Fail: &provider.Failure{Class: provider.ClassTransient, Message: "transport"}},
		provider.Turn{Text: "ok"},
	)
	s := newDurableEngineSession(t, fixture, fake, func(cfg *engine.Config) {
		cfg.Jitter = func(delay time.Duration) time.Duration { return delay }
		cfg.Sleep = func(ctx context.Context, delay time.Duration) error {
			mu.Lock()
			delays = append(delays, delay)
			mu.Unlock()
			return nil
		}
	})
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "retry")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	mu.Lock()
	if len(delays) != 2 || delays[0] != 500*time.Millisecond || delays[1] != time.Second {
		t.Fatalf("delays = %v", delays)
	}
	mu.Unlock()
	b := fixture.durable.Manifest().Budgets["run-1"]
	if b.LogicalModelCalls != 1 || b.TransportAttempts != 3 || b.RetryDelay != 1500*time.Millisecond {
		t.Fatalf("budget = %+v", b)
	}

	fixture2 := newDurableFixture(t)
	backoffStarted := make(chan struct{})
	blocking := provider.NewScripted(
		provider.Turn{Fail: &provider.Failure{Class: provider.ClassOverloaded, Message: "busy"}},
		provider.Turn{Text: "must not run"},
	)
	s2 := newDurableEngineSession(t, fixture2, blocking, func(cfg *engine.Config) {
		cfg.Jitter = func(delay time.Duration) time.Duration { return delay }
		cfg.Sleep = func(ctx context.Context, _ time.Duration) error {
			close(backoffStarted)
			<-ctx.Done()
			return ctx.Err()
		}
	})
	cmd2, _ := sessioncommand.NewSubmit("cmd-1", "cancel retry")
	if err := s2.Apply(context.Background(), cmd2); err != nil {
		t.Fatal(err)
	}
	select {
	case <-backoffStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("retry backoff did not start")
	}
	cancel, _ := sessioncommand.NewCancel("cmd-cancel")
	if err := s2.Apply(context.Background(), cancel); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s2)
	if len(blocking.Requests()) != 1 {
		t.Fatalf("attempts after backoff cancellation = %d", len(blocking.Requests()))
	}
}

func TestRetryAfterCapAndMaximumAttempts(t *testing.T) {
	fixture := newDurableFixture(t)
	var delays []time.Duration
	turns := make([]provider.Turn, 9)
	for i := range turns {
		turns[i] = provider.Turn{Fail: &provider.Failure{Class: provider.ClassRateLimit, Message: "rate", RetryAfter: 200 * time.Second}}
	}
	fake := provider.NewScripted(turns...)
	s := newDurableEngineSession(t, fixture, fake, func(cfg *engine.Config) {
		cfg.Sleep = func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		}
	})
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "bounded retry")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	if len(fake.Requests()) != 6 {
		// The durable 10-minute cumulative delay is the first exhausted bound:
		// five reserved 120-second waits permit six transport attempts.
		t.Fatalf("transport attempts = %d, want 6 at cumulative delay bound", len(fake.Requests()))
	}
	if len(delays) != 5 {
		t.Fatalf("delays = %v", delays)
	}
	for _, delay := range delays {
		if delay != 120*time.Second {
			t.Fatalf("Retry-After was not capped: %v", delays)
		}
	}
	fixture2 := newDurableFixture(t)
	turns = make([]provider.Turn, 10)
	for i := range turns {
		turns[i] = provider.Turn{Fail: &provider.Failure{Class: provider.ClassTransient, Message: "transport"}}
	}
	fake2 := provider.NewScripted(turns...)
	s2 := newDurableEngineSession(t, fixture2, fake2, func(cfg *engine.Config) {
		cfg.Jitter = func(time.Duration) time.Duration { return 0 }
		cfg.Sleep = func(context.Context, time.Duration) error { return nil }
	})
	cmd2, _ := sessioncommand.NewSubmit("cmd-1", "maximum retry")
	if err := s2.Apply(context.Background(), cmd2); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s2)
	if len(fake2.Requests()) != 9 {
		t.Fatalf("transport attempts = %d, want initial plus eight retries", len(fake2.Requests()))
	}
}

func TestLogicalRunBudgetStopsBeforeAnotherProviderRequest(t *testing.T) {
	fixture := newDurableFixture(t)
	turns := make([]provider.Turn, 0, store.MaxLogicalModelCalls)
	for i := uint64(0); i < store.MaxLogicalModelCalls; i++ {
		turns = append(turns, provider.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("budget-call-%d", i+1), Name: "read",
			Arguments: json.RawMessage(`{"path":"main.go","start_line":1,"end_line":1}`),
		}}})
	}
	fake := provider.NewScripted(turns...)
	s := newDurableEngineSession(t, fixture, fake)
	cmd, _ := sessioncommand.NewSubmit("budget-command", "consume the bounded investigation loop")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.WaitIdle(waitCtx); err != nil {
		t.Fatal(err)
	}
	if got := uint64(len(fake.Requests())); got != store.MaxLogicalModelCalls {
		t.Fatalf("Provider requests = %d, want exact logical bound %d", got, store.MaxLogicalModelCalls)
	}
	budget := fixture.durable.Manifest().Budgets["run-1"]
	if budget.LogicalModelCalls != store.MaxLogicalModelCalls || budget.TransportAttempts != store.MaxLogicalModelCalls {
		t.Fatalf("budget at exhaustion = %+v", budget)
	}
	events := s.Events()
	terminal := 0
	for _, ev := range events {
		if ev.Kind != event.KindRunFailed {
			continue
		}
		terminal++
		var failed event.RunFailedPayload
		if err := ev.DecodePayload(&failed); err != nil {
			t.Fatal(err)
		}
		if failed.Cause != event.CauseBudgetExhausted {
			t.Fatalf("failure cause = %s", failed.Cause)
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal failures = %d, want 1", terminal)
	}
	if err := transcript.ValidateTranscript(s.Transcript()); err != nil {
		t.Fatalf("budget-exhausted transcript = %v", err)
	}
}

func TestOutputLengthDoesNotPersistPartialAssistantBlock(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{Deltas: []string{"partial output"}, Reason: provider.ReasonLength})
	s := newSession(t, fake)
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "truncate")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	for _, record := range s.Transcript() {
		if record.Kind == transcript.KindAssistantBlock {
			t.Fatal("output-length response persisted a completed assistant block")
		}
	}
	events := s.Events()
	if events[len(events)-1].Kind != event.KindRunFailed {
		t.Fatalf("terminal event = %s", events[len(events)-1].Kind)
	}
	var failed event.RunFailedPayload
	_ = events[len(events)-1].DecodePayload(&failed)
	if failed.Cause != event.CauseProviderOutput {
		t.Fatalf("failure cause = %s", failed.Cause)
	}
}

func TestFailedStreamingAttemptIsDiscardedBeforeRetry(t *testing.T) {
	fixture := newDurableFixture(t)
	fake := provider.NewScripted(
		provider.Turn{
			Deltas:          []string{"failed attempt output"},
			FailAfterDeltas: &provider.Failure{Class: provider.ClassTransient, Message: "stream interrupted"},
		},
		provider.Turn{Deltas: []string{"successful answer"}},
	)
	s := newDurableEngineSession(t, fixture, fake, func(cfg *engine.Config) {
		cfg.Jitter = func(time.Duration) time.Duration { return 0 }
		cfg.Sleep = func(context.Context, time.Duration) error { return nil }
	})
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "retry streamed response")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)

	var assistant []string
	for _, record := range s.Transcript() {
		if record.Kind != transcript.KindAssistantBlock {
			continue
		}
		var payload transcript.AssistantBlockPayload
		if err := record.DecodePayload(&payload); err != nil {
			t.Fatal(err)
		}
		assistant = append(assistant, payload.Text)
	}
	if len(assistant) != 1 || assistant[0] != "successful answer" {
		t.Fatalf("completed assistant blocks = %q", assistant)
	}
	observation, unsubscribe := s.Observe(s.HighWaterMark())
	unsubscribe()
	if observation.Snapshot.PartialAssistant != "" {
		t.Fatalf("partial assistant survived retry = %q", observation.Snapshot.PartialAssistant)
	}
}

func TestFailedStreamingTailIsNotFlushedToEvents(t *testing.T) {
	fake := provider.NewScripted(provider.Turn{
		Deltas:          []string{"short failed tail"},
		FailAfterDeltas: &provider.Failure{Class: provider.ClassPermanent, Message: "authentication rejected"},
	})
	s := newSession(t, fake)
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "fail after a partial stream")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	for _, ev := range s.Events() {
		if ev.Kind == event.KindAssistantDelta || ev.Kind == event.KindAssistantText {
			t.Fatalf("failed stream tail crossed event boundary: %+v", ev)
		}
	}
}

func TestAtomicObserveAndInterruptedReconciliation(t *testing.T) {
	fixture := newDurableFixture(t)
	fake := provider.NewScripted(provider.Turn{BlockUntilCancel: true})
	s := newDurableEngineSession(t, fixture, fake)
	cmd, _ := sessioncommand.NewSubmit("cmd-1", "observe")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	waitForRequests(t, fake, 1)
	observation, unsubscribe := s.Observe(0)
	defer unsubscribe()
	cancel, _ := sessioncommand.NewCancel("cmd-cancel")
	if err := s.Apply(context.Background(), cancel); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	var cursors []uint64
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-observation.Events:
			cursors = append(cursors, ev.Cursor)
			if ev.Kind == event.KindRunCancelled {
				for i, cursor := range cursors {
					if cursor != uint64(i+1) {
						t.Fatalf("cursor gap: %v", cursors)
					}
				}
				goto reconciled
			}
		case <-deadline:
			t.Fatal("observation missed terminal event")
		}
	}

reconciled:
	root := t.TempDir()
	workspaceDir := t.TempDir()
	durable, err := store.Create(root, "sess-crash", workspaceDir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	runID, err := durable.BeginRun("cmd-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	user, _ := transcript.New(runID, time.Now(), transcript.KindUserMessage, transcript.UserMessagePayload{Text: "read"})
	call, _ := transcript.New(runID, time.Now(), transcript.KindToolCall, transcript.ToolCallPayload{CallID: "open-1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)})
	if _, err := durable.AppendTranscript(user, call); err != nil {
		t.Fatal(err)
	}
	started, _ := event.New("sess-crash", runID, 1, time.Now(), event.KindRunStarted, event.RunStartedPayload{Prompt: "read"})
	if err := durable.AppendEvent(started); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(root, "sess-crash")
	if err != nil {
		t.Fatal(err)
	}
	never := provider.NewScripted()
	recovered, err := engine.NewSession("sess-crash", engine.Config{Provider: never, Durable: reopened})
	if err != nil {
		t.Fatal(err)
	}
	if len(never.Requests()) != 0 {
		t.Fatal("reconciliation repeated Provider work")
	}
	if err := transcript.ValidateTranscript(recovered.Transcript()); err != nil {
		t.Fatalf("reconciled transcript = %v", err)
	}
	open, err := transcript.OpenToolCalls(recovered.Transcript())
	if err != nil || len(open) != 0 {
		t.Fatalf("open calls after reconciliation = %+v, %v", open, err)
	}
	againStore, err := store.Open(root, "sess-crash")
	if err != nil {
		t.Fatal(err)
	}
	again, err := engine.NewSession("sess-crash", engine.Config{Provider: never, Durable: againStore})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Transcript()) != len(recovered.Transcript()) {
		t.Fatal("reconciliation was not idempotent")
	}
	followUp, _ := sessioncommand.NewSubmit("cmd-follow-up", "continue after recovery")
	if err := again.Apply(context.Background(), followUp); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, again)
	if len(never.Requests()) != 1 {
		t.Fatalf("explicit follow-up Provider requests = %d, want 1", len(never.Requests()))
	}
}

func TestObserveReconnectOldAndCurrentCursorMatchesTranscript(t *testing.T) {
	fixture := newDurableFixture(t)
	fake := provider.NewScripted(
		provider.Turn{Text: "first answer", Reason: provider.ReasonStop},
		provider.Turn{Text: "second answer", Reason: provider.ReasonStop},
	)
	session := newDurableEngineSession(t, fixture, fake)
	first, _ := sessioncommand.NewSubmit("cmd-first", "first question")
	if err := session.Apply(context.Background(), first.ForSession(session.ID())); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, session)
	current := session.HighWaterMark()
	currentObservation, unsubscribeCurrent := session.Observe(current)
	defer unsubscribeCurrent()
	select {
	case ev := <-currentObservation.Events:
		t.Fatalf("current cursor replayed event %+v", ev)
	default:
	}

	old := current - 2
	oldObservation, unsubscribeOld := session.Observe(old)
	defer unsubscribeOld()
	second, _ := sessioncommand.NewSubmit("cmd-second", "second question")
	if err := session.Apply(context.Background(), second.ForSession(session.ID())); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, session)
	latest := session.HighWaterMark()
	currentEvents := collectThroughCursor(t, currentObservation.Events, latest)
	assertCursorRange(t, currentEvents, current+1, session.HighWaterMark())
	oldEvents := collectThroughCursor(t, oldObservation.Events, latest)
	assertCursorRange(t, oldEvents, old+1, session.HighWaterMark())

	nowObservation, unsubscribeNow := session.Observe(session.HighWaterMark())
	defer unsubscribeNow()
	select {
	case ev := <-nowObservation.Events:
		t.Fatalf("latest cursor replayed event %+v", ev)
	default:
	}
	if got, want := semanticConversationFromEvents(session.Events()), semanticConversationFromTranscript(session.Transcript()); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("event conversation %q differs from Transcript replay %q", got, want)
	}
}

func TestObserveRaceAgainstToolCompletionAndRunTermination(t *testing.T) {
	fixture := newDurableFixture(t)
	const runs = 24
	turns := make([]provider.Turn, 0, runs*2)
	for run := 0; run < runs; run++ {
		turns = append(turns,
			provider.Turn{ToolCalls: []provider.ToolCall{{ID: fmt.Sprintf("race-tool-%d", run), Name: "read", Arguments: json.RawMessage(`{"path":"main.go","start_line":3,"end_line":3}`)}}},
			provider.Turn{Text: fmt.Sprintf("answer %d", run), Reason: provider.ReasonStop},
		)
	}
	session := newDurableEngineSession(t, fixture, provider.NewScripted(turns...))
	for run := 0; run < runs; run++ {
		after := session.HighWaterMark()
		observationResult := make(chan engine.Observation, 1)
		unsubscribeResult := make(chan func(), 1)
		applyResult := make(chan error, 1)
		start := make(chan struct{})
		go func() {
			<-start
			observation, unsubscribe := session.Observe(after)
			observationResult <- observation
			unsubscribeResult <- unsubscribe
		}()
		go func(run int) {
			<-start
			command, _ := sessioncommand.NewSubmit(fmt.Sprintf("cmd-race-%d", run), fmt.Sprintf("question %d", run))
			applyResult <- session.Apply(context.Background(), command.ForSession(session.ID()))
		}(run)
		close(start)
		observation := <-observationResult
		unsubscribe := <-unsubscribeResult
		if err := <-applyResult; err != nil {
			unsubscribe()
			t.Fatal(err)
		}
		waitIdle(t, session)
		events := collectUntilTerminal(t, observation.Events, event.KindRunCompleted)
		unsubscribe()
		assertCursorRange(t, events, after+1, session.HighWaterMark())
	}
}

func TestObserveRaceAgainstCancellation(t *testing.T) {
	const runs = 16
	for run := 0; run < runs; run++ {
		fixture := newDurableFixture(t)
		fake := provider.NewScripted(provider.Turn{BlockUntilCancel: true})
		session := newDurableEngineSession(t, fixture, fake)
		submit, _ := sessioncommand.NewSubmit("cmd-submit", "wait")
		if err := session.Apply(context.Background(), submit.ForSession(session.ID())); err != nil {
			t.Fatal(err)
		}
		waitForRequests(t, fake, 1)
		after := session.HighWaterMark()
		observationResult := make(chan engine.Observation, 1)
		unsubscribeResult := make(chan func(), 1)
		cancelResult := make(chan error, 1)
		start := make(chan struct{})
		go func() {
			<-start
			observation, unsubscribe := session.Observe(after)
			observationResult <- observation
			unsubscribeResult <- unsubscribe
		}()
		go func() {
			<-start
			command, _ := sessioncommand.NewCancel("cmd-cancel")
			cancelResult <- session.Apply(context.Background(), command.ForSession(session.ID()))
		}()
		close(start)
		observation := <-observationResult
		unsubscribe := <-unsubscribeResult
		if err := <-cancelResult; err != nil {
			unsubscribe()
			t.Fatal(err)
		}
		waitIdle(t, session)
		events := collectUntilTerminal(t, observation.Events, event.KindRunCancelled)
		unsubscribe()
		assertCursorRange(t, events, after+1, session.HighWaterMark())
	}
}

func collectUntilTerminal(t *testing.T, events <-chan event.Event, terminal event.Kind) []event.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var collected []event.Event
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("observation closed before terminal event")
			}
			collected = append(collected, ev)
			if ev.Kind == terminal {
				return collected
			}
		case <-ctx.Done():
			t.Fatalf("observation timed out before %s", terminal)
		}
	}
}

func collectThroughCursor(t *testing.T, events <-chan event.Event, last uint64) []event.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var collected []event.Event
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("observation closed before target cursor")
			}
			collected = append(collected, ev)
			if ev.Cursor == last {
				return collected
			}
		case <-ctx.Done():
			t.Fatalf("observation timed out before cursor %d", last)
		}
	}
}

func assertCursorRange(t *testing.T, events []event.Event, first, last uint64) {
	t.Helper()
	if len(events) != int(last-first+1) {
		t.Fatalf("cursor range %d..%d has %d events: %+v", first, last, len(events), events)
	}
	for index, ev := range events {
		if want := first + uint64(index); ev.Cursor != want {
			t.Fatalf("event %d cursor=%d want=%d", index, ev.Cursor, want)
		}
	}
}

func semanticConversationFromEvents(events []event.Event) []string {
	var conversation []string
	for _, ev := range events {
		switch ev.Kind {
		case event.KindRunStarted:
			var payload event.RunStartedPayload
			if ev.DecodePayload(&payload) == nil {
				conversation = append(conversation, "user:"+payload.Prompt)
			}
		case event.KindAssistantText:
			var payload event.AssistantTextPayload
			if ev.DecodePayload(&payload) == nil {
				conversation = append(conversation, "assistant:"+payload.Text)
			}
		}
	}
	return conversation
}

func semanticConversationFromTranscript(records []transcript.Record) []string {
	var conversation []string
	for _, record := range records {
		switch record.Kind {
		case transcript.KindUserMessage:
			var payload transcript.UserMessagePayload
			if record.DecodePayload(&payload) == nil {
				conversation = append(conversation, "user:"+payload.Text)
			}
		case transcript.KindAssistantBlock:
			var payload transcript.AssistantBlockPayload
			if record.DecodePayload(&payload) == nil {
				conversation = append(conversation, "assistant:"+payload.Text)
			}
		}
	}
	return conversation
}

func TestInterruptedReconciliationRejectsOpenCallFromNonActiveRunWithoutWriting(t *testing.T) {
	root := t.TempDir()
	workspaceDir := t.TempDir()
	durable, err := store.Create(root, "sess-corrupt-recovery", workspaceDir, dataproj.ProjectionVersion, testStoreBinding(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	run1, err := durable.BeginRun("cmd-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	call, _ := transcript.New(run1, time.Now(), transcript.KindToolCall, transcript.ToolCallPayload{
		CallID: "orphaned-open-call", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`),
	})
	if _, err := durable.AppendTranscript(call); err != nil {
		t.Fatal(err)
	}
	if err := durable.FinishRun(run1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := durable.BeginRun("cmd-2", time.Now()); err != nil {
		t.Fatal(err)
	}
	beforeRecords := len(durable.Transcript())
	beforeEvents := len(durable.Events())
	never := provider.NewScripted()
	if _, err := engine.NewSession("sess-corrupt-recovery", engine.Config{Provider: never, Durable: durable}); !errors.Is(err, transcript.ErrUnpairedToolCall) {
		t.Fatalf("ambiguous recovery = %v", err)
	}
	if len(durable.Transcript()) != beforeRecords || len(durable.Events()) != beforeEvents {
		t.Fatal("ambiguous recovery appended facts before failing closed")
	}
	if len(never.Requests()) != 0 {
		t.Fatal("ambiguous recovery contacted Provider")
	}
}
