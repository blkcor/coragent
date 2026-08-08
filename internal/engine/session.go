// Package engine implements the M1 Session state machine and multi-tool loop.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/prompt"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateClosed  State = "closed"
	StateFaulted State = "faulted"
)

var (
	ErrDuplicateCommand = errors.New("engine: duplicate command ID")
	ErrRunActive        = errors.New("engine: a run is already active in this session")
	ErrSessionMismatch  = errors.New("engine: command targets a different session")
	ErrSessionClosed    = errors.New("engine: session is closed")
	ErrSessionFaulted   = errors.New("engine: session durability fault")
	ErrSensitivePrompt  = dataproj.ErrSensitivePrompt
)

type SleepFunc func(context.Context, time.Duration) error
type JitterFunc func(time.Duration) time.Duration

type Config struct {
	Provider          provider.Provider
	Now               func() time.Time
	Logger            *slog.Logger
	Durable           *store.Session
	Broker            *action.Broker
	Assembler         *prompt.Assembler
	Instructions      []prompt.Instruction
	InstructionSource func(context.Context) ([]prompt.Instruction, error)
	Projector         *dataproj.Projector
	Sleep             SleepFunc
	Jitter            JitterFunc
	Resource          io.Closer
	FileService       workspace.FileService
}

type ActiveTool struct {
	CallID string
	Name   string
}

type Snapshot struct {
	SessionID        string
	State            State
	Closed           bool
	Cursor           uint64
	Transcript       []transcript.Record
	PartialAssistant string
	ActiveTool       *ActiveTool
}

type Observation struct {
	Snapshot Snapshot
	Events   <-chan event.Event
}

type subscriber struct {
	mu     sync.Mutex
	out    chan event.Event
	wake   chan struct{}
	done   chan struct{}
	queue  []event.Event
	closed bool
	once   sync.Once
}

func newSubscriber(initial []event.Event) *subscriber {
	s := &subscriber{out: make(chan event.Event), wake: make(chan struct{}, 1), done: make(chan struct{}), queue: append([]event.Event(nil), initial...)}
	go s.run()
	if len(initial) > 0 {
		s.wake <- struct{}{}
	}
	return s
}

func (s *subscriber) enqueue(ev event.Event) {
	s.mu.Lock()
	if !s.closed {
		s.queue = append(s.queue, ev)
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *subscriber) close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.queue = nil
		s.mu.Unlock()
		close(s.done)
	})
}

func (s *subscriber) run() {
	defer close(s.out)
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
		}
		for {
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				return
			}
			if len(s.queue) == 0 {
				s.mu.Unlock()
				break
			}
			ev := s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()
			select {
			case s.out <- ev:
			case <-s.done:
				return
			}
		}
	}
}

type Session struct {
	id                string
	provider          provider.Provider
	broker            *action.Broker
	assembler         *prompt.Assembler
	docs              []prompt.Instruction
	instructionSource func(context.Context) ([]prompt.Instruction, error)
	projector         *dataproj.Projector
	durable           *store.Session
	resource          io.Closer
	now               func() time.Time
	logger            *slog.Logger
	sleep             SleepFunc
	jitter            JitterFunc

	mu           sync.Mutex
	state        State
	seen         map[string]struct{}
	cursor       uint64
	runs         uint64
	history      []event.Event
	records      []transcript.Record
	subs         map[int]*subscriber
	nextSub      int
	partial      string
	activeTool   *ActiveTool
	memoryBudget map[string]store.RunBudget

	runCancel      context.CancelFunc
	runDone        chan struct{}
	terminalErr    error
	terminalHook   func(terminalStage) error
	eventHook      func(event.Kind) error
	transcriptHook func(transcript.Kind) error

	approvalCh  map[string]chan sessioncommand.Command
	fileService workspace.FileService
}

type terminalStage string

const (
	terminalCancellation terminalStage = "cancellation_boundary"
	terminalOutcome      terminalStage = "run_outcome"
	terminalEvent        terminalStage = "terminal_event"
	terminalFinish       terminalStage = "finish_run"
)

func NewSession(id string, cfg Config) (*Session, error) {
	if cfg.Provider == nil {
		return nil, errors.New("engine: a Provider is required")
	}
	if cfg.Durable != nil {
		manifest := cfg.Durable.Manifest()
		if id == "" {
			id = manifest.SessionID
		}
		if id != manifest.SessionID {
			return nil, errors.New("engine: durable session ID mismatch")
		}
	}
	if id == "" {
		return nil, errors.New("engine: session ID is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	projector := cfg.Projector
	if projector == nil {
		projector = dataproj.New()
	}
	broker := cfg.Broker
	if broker == nil {
		var err error
		broker, err = action.NewBrokerWithProjector(projector)
		if err != nil {
			return nil, err
		}
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	jitter := cfg.Jitter
	if jitter == nil {
		jitter = defaultJitter
	}
	s := &Session{
		id: id, provider: cfg.Provider, broker: broker, assembler: cfg.Assembler,
		docs: append([]prompt.Instruction(nil), cfg.Instructions...), instructionSource: cfg.InstructionSource, projector: projector,
		durable: cfg.Durable, resource: cfg.Resource, now: now, logger: logger, sleep: sleep, jitter: jitter,
		state: StateIdle, seen: make(map[string]struct{}), subs: make(map[int]*subscriber),
		memoryBudget: make(map[string]store.RunBudget), approvalCh: make(map[string]chan sessioncommand.Command),
		fileService: cfg.FileService,
	}
	if cfg.Durable != nil {
		m := cfg.Durable.Manifest()
		s.cursor = m.EventCursor
		s.runs = m.NextRun
		s.history = cfg.Durable.Events()
		s.records = cfg.Durable.Transcript()
		for commandID := range m.SeenCommands {
			s.seen[commandID] = struct{}{}
		}
		closedFacts := 0
		for _, record := range s.records {
			if record.Kind == transcript.KindSessionClosed {
				closedFacts++
			}
		}
		closedEvents := 0
		for _, ev := range s.history {
			if ev.Kind == event.KindSessionClosed {
				closedEvents++
			}
		}
		if closedFacts > 1 || closedEvents > 1 || (m.Closed && closedFacts != 1) || (closedEvents > 0 && closedFacts != 1) || (closedFacts == 1 && m.ActiveRun != nil) {
			return nil, fmt.Errorf("%w: inconsistent session close facts", store.ErrCorrupt)
		}
		if m.ActiveRun != nil {
			if err := s.reconcileInterrupted(m.ActiveRun.ID); err != nil {
				return nil, err
			}
		}
		if closedFacts == 1 {
			if !m.Closed {
				if err := cfg.Durable.Close(s.now()); err != nil {
					return nil, err
				}
			}
			s.state = StateClosed
			if closedEvents == 0 {
				if err := s.emitLocked("", event.KindSessionClosed, event.SessionClosedPayload{}); err != nil {
					return nil, err
				}
			}
		}
	}
	return s, nil
}

// Shutdown cancels in-process work, waits for the run boundary, closes
// observation subscribers, and releases scoped filesystem resources. It does
// not close the durable session.
func (s *Session) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.state == StateRunning && s.runCancel != nil {
		s.runCancel()
	}
	s.mu.Unlock()
	waitErr := s.WaitIdle(ctx)
	s.mu.Lock()
	for id, sub := range s.subs {
		delete(s.subs, id)
		sub.close()
	}
	resource := s.resource
	s.resource = nil
	s.mu.Unlock()
	var closeErr error
	if resource != nil {
		closeErr = resource.Close()
	}
	return errors.Join(waitErr, closeErr)
}

func (s *Session) ID() string { return s.id }

func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) HighWaterMark() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor
}

func (s *Session) Events() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.history...)
}

func (s *Session) Transcript() []transcript.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]transcript.Record(nil), s.records...)
}

// Observe atomically captures Session truth and registers live delivery while
// holding the same lock. Events after the caller's cursor are queued in order.
func (s *Session) Observe(after uint64) (Observation, func()) {
	s.mu.Lock()
	initial := make([]event.Event, 0)
	for _, ev := range s.history {
		if ev.Cursor > after {
			initial = append(initial, ev)
		}
	}
	sub := newSubscriber(initial)
	id := s.nextSub
	s.nextSub++
	s.subs[id] = sub
	snapshot := Snapshot{
		SessionID: s.id, State: s.state, Closed: s.state == StateClosed,
		Cursor: s.cursor, Transcript: append([]transcript.Record(nil), s.records...),
		PartialAssistant: s.partial,
	}
	if s.activeTool != nil {
		active := *s.activeTool
		snapshot.ActiveTool = &active
	}
	s.mu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, id)
			s.mu.Unlock()
			sub.close()
		})
	}
	return Observation{Snapshot: snapshot, Events: sub.out}, unsubscribe
}

func (s *Session) Subscribe() (<-chan event.Event, func()) {
	s.mu.Lock()
	cursor := s.cursor
	s.mu.Unlock()
	observation, unsubscribe := s.Observe(cursor)
	return observation.Events, unsubscribe
}

func (s *Session) Apply(ctx context.Context, cmd sessioncommand.Command) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("engine: apply command: %w", err)
	}
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("engine: apply command: %w", err)
	}
	if cmd.SessionID != "" && cmd.SessionID != s.id {
		return fmt.Errorf("%w: got %s, want %s", ErrSessionMismatch, cmd.SessionID, s.id)
	}

	s.mu.Lock()
	if s.state == StateFaulted {
		err := s.terminalErr
		s.mu.Unlock()
		return fmt.Errorf("%w: %v", ErrSessionFaulted, err)
	}
	if _, ok := s.seen[cmd.ID]; ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDuplicateCommand, cmd.ID)
	}
	switch cmd.Kind {
	case sessioncommand.KindSubmit:
		if s.state == StateClosed {
			s.mu.Unlock()
			return ErrSessionClosed
		}
		if s.state == StateRunning {
			s.mu.Unlock()
			return ErrRunActive
		}
		payload, err := cmd.DecodeSubmit()
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if err := s.projector.ValidatePrompt(payload.Prompt); err != nil {
			if err := s.recordCommandLocked(cmd.ID); err != nil {
				s.mu.Unlock()
				return err
			}
			emitErr := s.emitLocked("", event.KindWarning, event.WarningPayload{Code: "detected_credential_in_prompt"})
			if emitErr != nil {
				s.faultSessionLocked("", emitErr)
			}
			s.mu.Unlock()
			if emitErr != nil {
				return emitErr
			}
			return ErrSensitivePrompt
		}
		if s.instructionSource != nil {
			docs, err := s.instructionSource(ctx)
			if err != nil {
				s.mu.Unlock()
				return fmt.Errorf("engine: refresh project instructions: %w", err)
			}
			s.docs = append([]prompt.Instruction(nil), docs...)
		}
		runID, err := s.beginRunLocked(cmd.ID)
		if err != nil {
			if !errors.Is(err, ErrDuplicateCommand) && !errors.Is(err, ErrSessionClosed) {
				s.faultSessionLocked("", err)
			}
			s.mu.Unlock()
			return err
		}
		if err := s.appendPayloadLocked(runID, transcript.KindUserMessage, transcript.UserMessagePayload{Text: payload.Prompt}); err != nil {
			s.abortRunStartLocked(runID, err)
			s.mu.Unlock()
			return err
		}
		if len(s.docs) > 0 {
			if err := s.appendPayloadLocked(runID, transcript.KindInstructionsLoaded, prompt.TranscriptPayload(s.docs)); err != nil {
				s.abortRunStartLocked(runID, err)
				s.mu.Unlock()
				return err
			}
		}
		if err := s.emitLocked(runID, event.KindRunStarted, event.RunStartedPayload{Prompt: payload.Prompt}); err != nil {
			s.abortRunStartLocked(runID, err)
			s.mu.Unlock()
			return err
		}
		runCtx, cancel := context.WithCancel(context.Background())
		s.runCancel = cancel
		s.runDone = make(chan struct{})
		done := s.runDone
		s.state = StateRunning
		s.mu.Unlock()
		s.logger.Debug("run starting", "session", s.id, "run", runID, "command", cmd.ID)
		go s.run(runCtx, done, runID, payload.Prompt)
		return nil

	case sessioncommand.KindCancel:
		if err := s.recordCommandLocked(cmd.ID); err != nil {
			s.mu.Unlock()
			return err
		}
		if s.state == StateRunning && s.runCancel != nil {
			s.runCancel()
		}
		s.mu.Unlock()
		return nil

	case sessioncommand.KindResume:
		if s.state == StateClosed {
			s.mu.Unlock()
			return ErrSessionClosed
		}
		if s.state == StateRunning {
			s.mu.Unlock()
			return ErrRunActive
		}
		if err := s.recordCommandLocked(cmd.ID); err != nil {
			s.mu.Unlock()
			return err
		}
		err := s.emitLocked("", event.KindSessionResumed, event.SessionResumedPayload{})
		if err != nil {
			s.faultSessionLocked("", err)
		}
		s.mu.Unlock()
		return err

	case sessioncommand.KindClose:
		if s.state == StateRunning {
			s.mu.Unlock()
			return ErrRunActive
		}
		if err := s.recordCommandLocked(cmd.ID); err != nil {
			s.mu.Unlock()
			return err
		}
		if s.state == StateClosed {
			s.mu.Unlock()
			return nil
		}
		if err := s.appendPayloadLocked("", transcript.KindSessionClosed, transcript.SessionClosedPayload{}); err != nil {
			s.faultSessionLocked("", err)
			s.mu.Unlock()
			return err
		}
		if s.durable != nil {
			if err := s.durable.Close(s.now()); err != nil {
				s.faultSessionLocked("", err)
				s.mu.Unlock()
				return err
			}
		}
		s.state = StateClosed
		err := s.emitLocked("", event.KindSessionClosed, event.SessionClosedPayload{})
		if err != nil {
			s.faultSessionLocked("", err)
		}
		s.mu.Unlock()
		return err

	case sessioncommand.KindApprove:
		payload, err := cmd.DecodeApprove()
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if err := s.recordCommandLocked(cmd.ID); err != nil {
			s.mu.Unlock()
			return err
		}
		ch, ok := s.approvalCh[payload.RequestID]
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("engine: no pending approval for request %q", payload.RequestID)
		}
		ch <- cmd.ForSession(s.id)
		s.mu.Unlock()
		return nil

	case sessioncommand.KindDeny:
		payload, err := cmd.DecodeDeny()
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if err := s.recordCommandLocked(cmd.ID); err != nil {
			s.mu.Unlock()
			return err
		}
		ch, ok := s.approvalCh[payload.RequestID]
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("engine: no pending approval for request %q", payload.RequestID)
		}
		ch <- cmd.ForSession(s.id)
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return fmt.Errorf("engine: unsupported command kind %q", cmd.Kind)
}

func (s *Session) WaitIdle(ctx context.Context) error {
	for {
		s.mu.Lock()
		if s.state != StateRunning {
			err := s.terminalErr
			s.mu.Unlock()
			if err != nil {
				return err
			}
			return nil
		}
		done := s.runDone
		s.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("engine: wait for idle: %w", ctx.Err())
		}
	}
}

func (s *Session) run(ctx context.Context, done chan struct{}, runID, goal string) {
	for {
		if ctx.Err() != nil {
			s.finishRun(runID, done, transcript.RunOutcomeCancelled, "")
			return
		}
		req, err := s.buildRequest(goal)
		if err != nil {
			s.finishRun(runID, done, transcript.RunOutcomeFailed, event.CauseContextLimit)
			return
		}
		if err := s.reserve(runID, store.BudgetLogicalModelCall, 0); err != nil {
			s.finishRun(runID, done, transcript.RunOutcomeFailed, event.CauseBudgetExhausted)
			return
		}
		redactor := dataproj.NewStreamRedactor(s.projector.Detector())
		streamed := ""
		onText := func(raw string) error {
			safe := redactor.Write(raw)
			if safe == "" {
				return nil
			}
			streamed += safe
			s.mu.Lock()
			s.partial += safe
			err := s.emitLocked(runID, event.KindAssistantDelta, event.AssistantDeltaPayload{Text: safe})
			s.mu.Unlock()
			return err
		}
		resetStream := func() {
			// Never join bytes from two transport attempts. In particular, a
			// withheld detector tail from a failed attempt must not become the
			// prefix of the retry response.
			redactor = dataproj.NewStreamRedactor(s.projector.Detector())
			streamed = ""
			s.mu.Lock()
			s.partial = ""
			s.mu.Unlock()
		}
		resp, err := s.completeWithRetry(ctx, runID, req, onText, resetStream)
		if err == nil {
			trailing := redactor.Close()
			if trailing != "" {
				streamed += trailing
				s.mu.Lock()
				s.partial += trailing
				err = s.emitLocked(runID, event.KindAssistantDelta, event.AssistantDeltaPayload{Text: trailing})
				s.mu.Unlock()
			}
		}
		if ctx.Err() != nil {
			s.finishRun(runID, done, transcript.RunOutcomeCancelled, "")
			return
		}
		if err != nil {
			s.finishRun(runID, done, transcript.RunOutcomeFailed, failureCause(err))
			return
		}
		projected := s.projector.ProjectText(resp.Text).Content
		if streamed != "" && streamed != projected {
			s.finishRun(runID, done, transcript.RunOutcomeFailed, event.CauseEngineInvariant)
			return
		}
		if resp.Reason == provider.ReasonLength && len(resp.ToolCalls) == 0 {
			s.mu.Lock()
			s.partial = ""
			s.mu.Unlock()
			s.finishRun(runID, done, transcript.RunOutcomeFailed, event.CauseProviderOutput)
			return
		}
		if projected != "" && resp.Reason != provider.ReasonLength {
			s.mu.Lock()
			err = s.appendPayloadLocked(runID, transcript.KindAssistantBlock, transcript.AssistantBlockPayload{Text: projected})
			if err == nil {
				err = s.emitLocked(runID, event.KindAssistantText, event.AssistantTextPayload{Text: projected})
			}
			s.partial = ""
			s.mu.Unlock()
			if err != nil {
				s.finishRun(runID, done, transcript.RunOutcomeFailed, event.CauseEngineInvariant)
				return
			}
		} else if projected != "" {
			s.mu.Lock()
			s.partial = ""
			s.mu.Unlock()
		}
		if len(resp.ToolCalls) == 0 {
			s.finishRun(runID, done, transcript.RunOutcomeCompleted, "")
			return
		}
		if err := validateToolCalls(resp.ToolCalls); err != nil {
			s.finishRun(runID, done, transcript.RunOutcomeFailed, event.CauseProviderProtocol)
			return
		}
		safeCalls, sensitiveCalls := s.broker.ProjectCalls(resp.ToolCalls)
		if err := s.appendToolCalls(runID, safeCalls); err != nil {
			s.faultRun(runID, done, err)
			return
		}
		stopped := false
		blockedSensitive := false
		for index, call := range safeCalls {
			var result transcript.ToolResultPayload
			if stopped {
				result = s.broker.SkippedResult(call.ID)
			} else if sensitiveCalls[index] {
				result = s.broker.BlockedResult(call.ID)
				blockedSensitive = true
			} else {
				s.mu.Lock()
				s.activeTool = &ActiveTool{CallID: call.ID, Name: call.Name}
				emitErr := s.emitLocked(runID, event.KindToolStarted, event.ToolStartedPayload{CallID: call.ID, Name: call.Name})
				s.mu.Unlock()
				if emitErr != nil {
					s.faultRun(runID, done, emitErr)
					return
				}
				prepared, prepareErr := s.broker.Prepare(ctx, call)
				if prepareErr != nil {
					if errors.Is(prepareErr, context.Canceled) {
						result = transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultCancelled, Content: "cancelled"}
					} else {
						result = transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultError, Content: prepareErr.Error()}
					}
				} else if prepared.NeedsApproval() {
					result = s.approvalFlow(ctx, runID, call, prepared)
				} else {
					result = s.broker.ExecuteRead(ctx, prepared, call.ID)
				}
			}
			s.mu.Lock()
			err := s.appendPayloadLocked(runID, transcript.KindToolResult, result)
			s.activeTool = nil
			if err == nil {
				err = s.emitLocked(runID, event.KindToolFinished, event.ToolFinishedPayload{CallID: call.ID, Outcome: string(result.Outcome)})
			}
			s.mu.Unlock()
			if err != nil {
				s.faultRun(runID, done, err)
				return
			}
			if result.Outcome != transcript.ToolResultSuccess {
				stopped = true
			}
		}
		if ctx.Err() != nil {
			s.finishRun(runID, done, transcript.RunOutcomeCancelled, "")
			return
		}
		if blockedSensitive {
			s.finishRun(runID, done, transcript.RunOutcomeFailed, event.CauseSensitiveContent)
			return
		}
	}
}

// approvalFlow handles the full approval lifecycle for a prepared write action:
// write action_prepared → emit approval_required → wait for approve/deny →
// on approve: action_approved → action_committing → execute → action_committed
// on deny: action_denied → return denied result
func (s *Session) approvalFlow(ctx context.Context, runID string, call provider.ToolCall, prepared action.Prepared) transcript.ToolResultPayload {
	reqID := prepared.Patch.RequestID
	s.mu.Lock()
	if err := s.appendPayloadLocked(runID, transcript.KindActionPrepared, transcript.ActionPreparedPayload{
		RequestID:      reqID,
		ToolCallID:     call.ID,
		Path:           prepared.Patch.Path,
		SourceSHA256:   prepared.Patch.SourceSHA256,
		ExpectedSHA256: prepared.Patch.ExpectedSHA256,
		DiffDigest:     prepared.Patch.DiffDigest,
	}); err != nil {
		s.faultRunLocked(runID, nil, err)
		s.mu.Unlock()
		return transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultError, Content: "failed to persist action_prepared"}
	}
	ch := make(chan sessioncommand.Command, 1)
	s.approvalCh[reqID] = ch
	if err := s.emitLocked(runID, event.KindApprovalRequired, event.ApprovalRequiredPayload{
		RequestID: reqID, ToolCallID: call.ID, Path: prepared.Patch.Path,
		Target: prepared.Patch.Target, Diff: prepared.Patch.Diff, IsSensitive: prepared.Patch.IsSensitive,
	}); err != nil {
		delete(s.approvalCh, reqID)
		s.faultRunLocked(runID, nil, err)
		s.mu.Unlock()
		return transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultError, Content: "failed to emit approval_required"}
	}
	s.mu.Unlock()

	var cmd sessioncommand.Command
	select {
	case cmd = <-ch:
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.approvalCh, reqID)
		_ = s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
			RequestID: reqID, Reason: transcript.AbortCancelled,
		})
		s.mu.Unlock()
		return transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultCancelled, Content: "approval cancelled"}
	}

	s.mu.Lock()
	delete(s.approvalCh, reqID)
	if cmd.Kind == sessioncommand.KindDeny {
		_ = s.appendPayloadLocked(runID, transcript.KindActionDenied, transcript.ActionDeniedPayload{
			RequestID: reqID, CommandID: cmd.ID,
		})
		s.mu.Unlock()
		return transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultBlocked, Content: "action denied by user"}
	}

	// Approve path
	if err := s.appendPayloadLocked(runID, transcript.KindActionApproved, transcript.ActionApprovedPayload{
		RequestID: reqID, CommandID: cmd.ID,
	}); err != nil {
		s.faultRunLocked(runID, nil, err)
		s.mu.Unlock()
		return transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultError, Content: "failed to persist action_approved"}
	}
	if err := s.appendPayloadLocked(runID, transcript.KindActionCommitting, transcript.ActionCommittingPayload{
		RequestID: reqID,
	}); err != nil {
		s.faultRunLocked(runID, nil, err)
		s.mu.Unlock()
		return transcript.ToolResultPayload{CallID: call.ID, Outcome: transcript.ToolResultError, Content: "failed to persist action_committing"}
	}
	s.mu.Unlock()

	result := s.broker.ExecutePrepared(ctx, prepared, call.ID)

	s.mu.Lock()
	if result.Outcome == transcript.ToolResultSuccess {
		_ = s.appendPayloadLocked(runID, transcript.KindActionCommitted, transcript.ActionCommittedPayload{
			RequestID: reqID, ActualSHA256: prepared.Patch.ExpectedSHA256,
		})
	} else {
		_ = s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
			RequestID: reqID, Reason: transcript.AbortStale,
		})
	}
	s.mu.Unlock()
	return result
}

func (s *Session) buildRequest(goal string) (provider.Request, error) {
	s.mu.Lock()
	records := append([]transcript.Record(nil), s.records...)
	s.mu.Unlock()
	if s.assembler == nil {
		return provider.Request{Prompt: goal}, nil
	}
	var catalog []provider.ToolDefinition
	if s.broker != nil {
		catalog = s.broker.Catalog()
	}
	return s.assembler.Build(goal, s.docs, records, catalog)
}

func (s *Session) completeWithRetry(ctx context.Context, runID string, req provider.Request, onText func(string) error, resetStream func()) (provider.Response, error) {
	for retry := 0; ; retry++ {
		if err := s.reserve(runID, store.BudgetTransportAttempt, 0); err != nil {
			return provider.Response{}, err
		}
		var resp provider.Response
		var err error
		if streaming, ok := s.provider.(provider.StreamProvider); ok {
			resp, err = streaming.Stream(ctx, req, onText)
		} else {
			resp, err = s.provider.Complete(ctx, req)
		}
		if err == nil || ctx.Err() != nil {
			return resp, err
		}
		var failure *provider.Failure
		if !errors.As(err, &failure) || !retryable(failure.Class) || retry >= 8 {
			return provider.Response{}, err
		}
		delay := retryDelay(retry)
		if failure.RetryAfter > 0 {
			delay = failure.RetryAfter
			if delay > 120*time.Second {
				delay = 120 * time.Second
			}
		} else {
			delay = s.jitter(delay)
		}
		if delay < 0 {
			delay = 0
		}
		if err := s.reserve(runID, store.BudgetRetryDelay, delay); err != nil {
			return provider.Response{}, err
		}
		resetStream()
		s.mu.Lock()
		emitErr := s.emitLocked(runID, event.KindRetryScheduled, event.RetryScheduledPayload{Attempt: retry + 1, DelayMillis: delay.Milliseconds(), Class: string(failure.Class)})
		s.mu.Unlock()
		if emitErr != nil {
			return provider.Response{}, emitErr
		}
		if err := s.sleep(ctx, delay); err != nil {
			return provider.Response{}, err
		}
	}
}

func retryable(class provider.FailureClass) bool {
	return class == provider.ClassRateLimit || class == provider.ClassTransient || class == provider.ClassOverloaded
}

func retryDelay(retry int) time.Duration {
	delay := 500 * time.Millisecond
	for i := 0; i < retry; i++ {
		if delay >= 30*time.Second {
			return 30 * time.Second
		}
		delay *= 2
	}
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func defaultJitter(delay time.Duration) time.Duration {
	factor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(delay) * factor)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Session) reserve(runID string, counter store.BudgetCounter, amount time.Duration) error {
	if s.durable != nil {
		_, err := s.durable.ReserveBudget(runID, counter, amount)
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.memoryBudget[runID]
	switch counter {
	case store.BudgetLogicalModelCall:
		if b.LogicalModelCalls >= store.MaxLogicalModelCalls {
			return store.ErrBudgetExhausted
		}
		b.LogicalModelCalls++
	case store.BudgetTransportAttempt:
		if b.TransportAttempts >= store.MaxTransportAttempts {
			return store.ErrBudgetExhausted
		}
		b.TransportAttempts++
	case store.BudgetRetryDelay:
		if amount < 0 || b.RetryDelay > store.MaxRetryDelay-amount {
			return store.ErrBudgetExhausted
		}
		b.RetryDelay += amount
	}
	s.memoryBudget[runID] = b
	return nil
}

func (s *Session) finishRun(runID string, done chan struct{}, outcome transcript.RunOutcome, cause event.FailureCause) {
	s.mu.Lock()
	var finishErr error
	if outcome == transcript.RunOutcomeCancelled {
		finishErr = s.runTerminalStepLocked(terminalCancellation, func() error {
			return s.appendPayloadLocked(runID, transcript.KindCancellationBoundary, transcript.CancellationBoundaryPayload{})
		})
	}
	if finishErr == nil {
		finishErr = s.runTerminalStepLocked(terminalOutcome, func() error {
			return s.appendPayloadLocked(runID, transcript.KindRunOutcome, transcript.RunOutcomePayload{Outcome: outcome, Cause: transcript.FailureCause(cause)})
		})
	}
	kind := event.KindRunCompleted
	payload := any(event.RunCompletedPayload{})
	switch outcome {
	case transcript.RunOutcomeCancelled:
		kind, payload = event.KindRunCancelled, event.RunCancelledPayload{}
	case transcript.RunOutcomeFailed, transcript.RunOutcomeInterrupted:
		kind, payload = event.KindRunFailed, event.RunFailedPayload{Cause: cause}
	}
	var terminalFact event.Event
	if finishErr == nil {
		finishErr = s.runTerminalStepLocked(terminalEvent, func() error {
			var err error
			terminalFact, err = s.persistEventLocked(runID, kind, payload)
			return err
		})
	}
	if finishErr == nil && s.durable != nil {
		finishErr = s.runTerminalStepLocked(terminalFinish, func() error {
			return s.durable.FinishRun(runID, s.now())
		})
	}
	if finishErr == nil {
		s.publishEventLocked(terminalFact)
		s.state = StateIdle
		s.terminalErr = nil
	} else {
		s.faultRunLocked(runID, done, finishErr)
		s.mu.Unlock()
		return
	}
	s.runCancel = nil
	s.partial = ""
	s.activeTool = nil
	close(done)
	s.mu.Unlock()
	s.logger.Debug("run finished", "session", s.id, "run", runID, "outcome", outcome)
}

func (s *Session) faultRun(runID string, done chan struct{}, err error) {
	s.mu.Lock()
	s.faultRunLocked(runID, done, err)
	s.mu.Unlock()
}

func (s *Session) faultRunLocked(runID string, done chan struct{}, err error) {
	s.faultSessionLocked(runID, err)
	close(done)
}

func (s *Session) faultSessionLocked(runID string, err error) {
	s.state = StateFaulted
	s.terminalErr = fmt.Errorf("%w: %w", ErrSessionFaulted, err)
	for id, sub := range s.subs {
		delete(s.subs, id)
		sub.close()
	}
	s.runCancel = nil
	s.partial = ""
	s.activeTool = nil
	s.logger.Error("persist run facts", "session", s.id, "run", runID, "cause", "store_failure")
}

func (s *Session) runTerminalStepLocked(stage terminalStage, operation func() error) error {
	if s.terminalHook != nil {
		if err := s.terminalHook(stage); err != nil {
			return err
		}
	}
	return operation()
}

func (s *Session) beginRunLocked(commandID string) (string, error) {
	now := s.now()
	var runID string
	if s.durable != nil {
		var err error
		runID, err = s.durable.BeginRun(commandID, now)
		if err != nil {
			if errors.Is(err, store.ErrDuplicateCommand) {
				return "", ErrDuplicateCommand
			}
			if errors.Is(err, store.ErrClosed) {
				return "", ErrSessionClosed
			}
			return "", err
		}
		s.runs = s.durable.Manifest().NextRun
	} else {
		s.runs++
		runID = fmt.Sprintf("run-%d", s.runs)
		s.memoryBudget[runID] = store.RunBudget{}
	}
	s.seen[commandID] = struct{}{}
	return runID, nil
}

func (s *Session) recordCommandLocked(commandID string) error {
	if s.durable != nil {
		if err := s.durable.RecordCommand(commandID, s.now()); err != nil {
			if errors.Is(err, store.ErrDuplicateCommand) {
				return ErrDuplicateCommand
			}
			return err
		}
	}
	s.seen[commandID] = struct{}{}
	return nil
}

func (s *Session) abortRunStartLocked(runID string, err error) {
	s.faultSessionLocked(runID, err)
}

func (s *Session) appendPayloadLocked(runID string, kind transcript.Kind, payload any) error {
	if s.transcriptHook != nil {
		if err := s.transcriptHook(kind); err != nil {
			return err
		}
	}
	rec, err := transcript.New(runID, s.now(), kind, payload)
	if err != nil {
		return err
	}
	if s.durable != nil {
		assigned, err := s.durable.AppendTranscript(rec)
		if err != nil {
			return err
		}
		rec = assigned[0]
	} else {
		rec.Seq = uint64(len(s.records) + 1)
	}
	s.records = append(s.records, rec)
	return nil
}

func (s *Session) appendToolCalls(runID string, calls []provider.ToolCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]transcript.Record, 0, len(calls))
	for _, call := range calls {
		rec, err := transcript.New(runID, s.now(), transcript.KindToolCall, transcript.ToolCallPayload{CallID: call.ID, Name: call.Name, Arguments: call.Arguments})
		if err != nil {
			return err
		}
		records = append(records, rec)
	}
	if s.durable != nil {
		assigned, err := s.durable.AppendTranscript(records...)
		if err != nil {
			return err
		}
		records = assigned
	} else {
		for i := range records {
			records[i].Seq = uint64(len(s.records) + i + 1)
		}
	}
	s.records = append(s.records, records...)
	return nil
}

func (s *Session) emitLocked(runID string, kind event.Kind, payload any) error {
	ev, err := s.persistEventLocked(runID, kind, payload)
	if err != nil {
		return err
	}
	s.publishEventLocked(ev)
	return nil
}

func (s *Session) persistEventLocked(runID string, kind event.Kind, payload any) (event.Event, error) {
	if s.eventHook != nil {
		if err := s.eventHook(kind); err != nil {
			return event.Event{}, err
		}
	}
	s.cursor++
	ev, err := event.New(s.id, runID, s.cursor, s.now(), kind, payload)
	if err != nil {
		s.cursor--
		return event.Event{}, fmt.Errorf("engine: encode event: %w", err)
	}
	if s.durable != nil {
		if err := s.durable.AppendEvent(ev); err != nil {
			s.cursor--
			return event.Event{}, err
		}
	}
	return ev, nil
}

func (s *Session) publishEventLocked(ev event.Event) {
	s.history = append(s.history, ev)
	for _, sub := range s.subs {
		sub.enqueue(ev)
	}
}

func (s *Session) reconcileInterrupted(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	terminalRecord := false
	for _, rec := range s.records {
		if rec.RunID == runID && rec.Kind == transcript.KindRunOutcome {
			terminalRecord = true
		}
	}
	if !terminalRecord {
		// Reconcile action records before tool calls, since action recovery
		// may add tool results that close open calls.
		if err := s.reconcileActionRecordsLocked(runID); err != nil {
			return err
		}
		open, err := transcript.OpenToolCallsForRun(s.records, runID)
		if err != nil {
			return err
		}
		for _, call := range open {
			if err := s.appendPayloadLocked(runID, transcript.KindToolResult, transcript.ToolResultPayload{CallID: call.CallID, Outcome: transcript.ToolResultInterrupted, Content: "read-only tool call interrupted by process exit; not repeated automatically"}); err != nil {
				return err
			}
		}
		if err := s.appendPayloadLocked(runID, transcript.KindRunOutcome, transcript.RunOutcomePayload{Outcome: transcript.RunOutcomeInterrupted, Cause: transcript.CauseInterrupted}); err != nil {
			return err
		}
	}
	terminalEvent := false
	for _, ev := range s.history {
		if ev.RunID == runID && isTerminal(ev.Kind) {
			terminalEvent = true
		}
	}
	if !terminalEvent {
		if err := s.emitLocked(runID, event.KindRunFailed, event.RunFailedPayload{Cause: event.CauseInterrupted}); err != nil {
			return err
		}
	}
	if s.durable != nil {
		if err := s.durable.ReconcileRun(runID, s.now()); err != nil {
			return err
		}
	}
	return transcript.ValidateTranscript(s.records)
}

// reconcileActionRecordsLocked scans action lifecycle records and applies the
// crash recovery matrix for unclosed mutations.
func (s *Session) reconcileActionRecordsLocked(runID string) error {
	// Build a map of request_id -> its last lifecycle state
	type actionRecovery struct {
		prepared    *transcript.ActionPreparedPayload
		approved    bool
		denied      bool
		committing  bool
		committed   bool
		aborted     bool
		abortReason transcript.AbortReason
	}
	actions := make(map[string]*actionRecovery)
	// Calls that already have a tool result must never receive a second one;
	// a crash can land after the result was persisted but before the run
	// outcome, and recovery runs again on the next resume.
	hasResult := make(map[string]bool)
	for _, rec := range s.records {
		if rec.RunID != runID {
			continue
		}
		switch rec.Kind {
		case transcript.KindToolResult:
			var p transcript.ToolResultPayload
			if err := rec.DecodePayload(&p); err != nil {
				return err
			}
			hasResult[p.CallID] = true
		case transcript.KindActionPrepared:
			var p transcript.ActionPreparedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return err
			}
			actions[p.RequestID] = &actionRecovery{prepared: &p}
		case transcript.KindActionApproved:
			var p transcript.ActionApprovedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return err
			}
			if a, ok := actions[p.RequestID]; ok {
				a.approved = true
			}
		case transcript.KindActionDenied:
			var p transcript.ActionDeniedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return err
			}
			if a, ok := actions[p.RequestID]; ok {
				a.denied = true
			}
		case transcript.KindActionAborted:
			var p transcript.ActionAbortedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return err
			}
			if a, ok := actions[p.RequestID]; ok {
				a.aborted = true
				a.abortReason = p.Reason
			}
		case transcript.KindActionCommitting:
			var p transcript.ActionCommittingPayload
			if err := rec.DecodePayload(&p); err != nil {
				return err
			}
			if a, ok := actions[p.RequestID]; ok {
				a.committing = true
			}
		case transcript.KindActionCommitted:
			var p transcript.ActionCommittedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return err
			}
			if a, ok := actions[p.RequestID]; ok {
				a.committed = true
			}
		}
	}

	// ensureResult appends a tool result only when the call has none yet.
	ensureResult := func(payload transcript.ToolResultPayload) error {
		if hasResult[payload.CallID] {
			return nil
		}
		if err := s.appendPayloadLocked(runID, transcript.KindToolResult, payload); err != nil {
			return err
		}
		hasResult[payload.CallID] = true
		return nil
	}

	for reqID, a := range actions {
		switch {
		case a.committed:
			// Committed but may be missing tool_result — add success result
			if err := ensureResult(transcript.ToolResultPayload{
				CallID: a.prepared.ToolCallID, Outcome: transcript.ToolResultSuccess,
				Content: "patch recovered: the write was already committed before the crash",
			}); err != nil {
				return err
			}
			continue
		case a.aborted:
			// Abort was already journaled; only the tool result may be missing.
			outcome := transcript.ToolResultError
			if a.abortReason == transcript.AbortCancelled {
				outcome = transcript.ToolResultCancelled
			}
			if err := ensureResult(transcript.ToolResultPayload{
				CallID: a.prepared.ToolCallID, Outcome: outcome,
				Content: "patch recovery: action was aborted before the crash",
			}); err != nil {
				return err
			}
			continue
		case a.denied:
			// Denial was already journaled; only the tool result may be missing.
			if err := ensureResult(transcript.ToolResultPayload{
				CallID: a.prepared.ToolCallID, Outcome: transcript.ToolResultBlocked,
				Content: "patch recovery: action was denied before the crash",
			}); err != nil {
				return err
			}
			continue
		}
		if a.committing {
			// Check disk to determine recovery path
			diskSHA256, fsErr := "", ""
			if s.fileService != nil {
				h, err := s.fileService.Identity(a.prepared.Path)
				if err != nil {
					fsErr = err.Error()
				} else {
					diskSHA256 = h
				}
			}
			switch {
			case fsErr != "":
				if err := s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
					RequestID: reqID, Reason: transcript.AbortStale,
				}); err != nil {
					return err
				}
				if err := ensureResult(transcript.ToolResultPayload{
					CallID: a.prepared.ToolCallID, Outcome: transcript.ToolResultError,
					Content: "patch recovery: cannot read file: " + fsErr,
				}); err != nil {
					return err
				}
			case diskSHA256 == a.prepared.ExpectedSHA256:
				// Write already happened — record committed
				if err := s.appendPayloadLocked(runID, transcript.KindActionCommitted, transcript.ActionCommittedPayload{
					RequestID: reqID, ActualSHA256: diskSHA256,
				}); err != nil {
					return err
				}
				if err := ensureResult(transcript.ToolResultPayload{
					CallID: a.prepared.ToolCallID, Outcome: transcript.ToolResultSuccess,
					Content: "patch recovered: write was already committed before the crash",
				}); err != nil {
					return err
				}
			case diskSHA256 == a.prepared.SourceSHA256:
				// Write didn't happen, source unchanged — auto-retry
				if err := s.retryCommittedActionLocked(runID, reqID, a.prepared, ensureResult); err != nil {
					return err
				}
			default:
				// Disk content matches neither — stale
				if err := s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
					RequestID: reqID, Reason: transcript.AbortStale,
				}); err != nil {
					return err
				}
				if err := ensureResult(transcript.ToolResultPayload{
					CallID: a.prepared.ToolCallID, Outcome: transcript.ToolResultError,
					Content: "patch recovery: file was modified externally, re-prepare and re-approve required",
				}); err != nil {
					return err
				}
			}
			continue
		}
		// prepared (with or without approved) but no committing → cancelled
		if err := s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
			RequestID: reqID, Reason: transcript.AbortCancelled,
		}); err != nil {
			return err
		}
		if err := ensureResult(transcript.ToolResultPayload{
			CallID: a.prepared.ToolCallID, Outcome: transcript.ToolResultCancelled,
			Content: "patch recovery: action was not committed before crash",
		}); err != nil {
			return err
		}
	}
	return nil
}

// retryCommittedActionLocked re-executes a patch whose committing record was
// written but whose execute did not run (or whose result was not recorded).
// The source file is unchanged so the original approval remains valid.
func (s *Session) retryCommittedActionLocked(runID, reqID string, p *transcript.ActionPreparedPayload, ensureResult func(transcript.ToolResultPayload) error) error {
	// Find the original tool call to re-prepare
	var toolCall *transcript.ToolCallPayload
	for _, rec := range s.records {
		if rec.Kind == transcript.KindToolCall {
			var tc transcript.ToolCallPayload
			if err := rec.DecodePayload(&tc); err != nil {
				return err
			}
			if tc.CallID == p.ToolCallID {
				toolCall = &tc
				break
			}
		}
	}
	if toolCall == nil {
		if err := s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
			RequestID: reqID, Reason: transcript.AbortStale,
		}); err != nil {
			return err
		}
		return ensureResult(transcript.ToolResultPayload{
			CallID: p.ToolCallID, Outcome: transcript.ToolResultError,
			Content: "patch recovery: cannot find original tool call to re-execute",
		})
	}

	call := provider.ToolCall{ID: toolCall.CallID, Name: toolCall.Name, Arguments: toolCall.Arguments}
	prepared, err := s.broker.Prepare(context.Background(), call)
	if err != nil {
		if err := s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
			RequestID: reqID, Reason: transcript.AbortStale,
		}); err != nil {
			return err
		}
		return ensureResult(transcript.ToolResultPayload{
			CallID: p.ToolCallID, Outcome: transcript.ToolResultError,
			Content: "patch recovery: re-prepare failed: " + err.Error(),
		})
	}
	// Verify the patch hasn't changed
	if prepared.Patch == nil || prepared.Patch.SourceSHA256 != p.SourceSHA256 || prepared.Patch.ExpectedSHA256 != p.ExpectedSHA256 {
		if err := s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
			RequestID: reqID, Reason: transcript.AbortStale,
		}); err != nil {
			return err
		}
		return ensureResult(transcript.ToolResultPayload{
			CallID: p.ToolCallID, Outcome: transcript.ToolResultError,
			Content: "patch recovery: re-prepared action differs from original",
		})
	}

	// Release lock for execution (ExecutePrepared may do I/O)
	s.mu.Unlock()
	result := s.broker.ExecutePrepared(context.Background(), prepared, p.ToolCallID)
	s.mu.Lock()

	if result.Outcome == transcript.ToolResultSuccess {
		if err := s.appendPayloadLocked(runID, transcript.KindActionCommitted, transcript.ActionCommittedPayload{
			RequestID: reqID, ActualSHA256: prepared.Patch.ExpectedSHA256,
		}); err != nil {
			return err
		}
		result.Content = "patch recovered: auto-retry succeeded (file was unchanged)"
	} else {
		if err := s.appendPayloadLocked(runID, transcript.KindActionAborted, transcript.ActionAbortedPayload{
			RequestID: reqID, Reason: transcript.AbortStale,
		}); err != nil {
			return err
		}
		result.Content = "patch recovery: auto-retry failed: " + result.Content
	}
	return ensureResult(result)
}

func validateToolCalls(calls []provider.ToolCall) error {
	seen := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.ID == "" || call.Name == "" || !jsonValid(call.Arguments) || seen[call.ID] {
			return errors.New("invalid completed tool call")
		}
		seen[call.ID] = true
	}
	return nil
}

func jsonValid(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	return json.Unmarshal(raw, &value) == nil
}

func isTerminal(kind event.Kind) bool {
	return kind == event.KindRunCompleted || kind == event.KindRunFailed || kind == event.KindRunCancelled
}

func failureCause(err error) event.FailureCause {
	if errors.Is(err, store.ErrBudgetExhausted) {
		return event.CauseBudgetExhausted
	}
	if errors.Is(err, prompt.ErrContextLimit) {
		return event.CauseContextLimit
	}
	var f *provider.Failure
	if errors.As(err, &f) {
		switch f.Class {
		case provider.ClassPermanent:
			return event.CauseProviderPermanent
		case provider.ClassProtocol:
			return event.CauseProviderProtocol
		case provider.ClassContextOverflow:
			return event.CauseContextLimit
		case provider.ClassOutputLimit:
			return event.CauseProviderOutput
		case provider.ClassRateLimit, provider.ClassTransient, provider.ClassOverloaded:
			return event.CauseProviderTransient
		}
	}
	return event.CauseEngineInvariant
}
