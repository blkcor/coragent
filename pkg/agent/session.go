package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/blkcor/coragent/internal/config"
	convo "github.com/blkcor/coragent/internal/context"
	"github.com/blkcor/coragent/internal/executor"
	"github.com/blkcor/coragent/internal/hooks"
	"github.com/blkcor/coragent/internal/loop"
	"github.com/blkcor/coragent/internal/permission"
	sandboxpkg "github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/tools"
)

// defaultMaxRounds bounds how many model rounds a run may take before a normal
// step-limit stop, guarding against a model stuck requesting tools forever.
const defaultMaxRounds = 25

// ErrRunInFlight is returned when a second run is started on a session that
// already has one in flight. The in-flight run is unaffected.
var ErrRunInFlight = errors.New("agent: a run is already in flight on this session")

// ErrSessionClosed is returned when a run is started after Close.
var ErrSessionClosed = errors.New("agent: session is closed")

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
	// (the ordered chain with a real permission gate) seeded with the built-in tools.
	Dispatcher Dispatcher

	// Hooks are in-process hooks registered through the SDK.
	Hooks []HookRegistration

	// ExternalHooks are operator-configured external command hooks.
	ExternalHooks []ExternalHook

	// HookOutputLimit bounds external hook stdout/stderr. Zero uses a default.
	HookOutputLimit int

	// PermissionMode is the starting permission posture for the default executor:
	// "default", "auto-accept-edits", "plan", or "bypass". Empty means default.
	// Ignored when a custom Dispatcher is supplied.
	PermissionMode string

	// PermissionAllow and PermissionDeny are the starting allow/deny rule lists,
	// each entry a "<kind>:<match>" string (e.g. "command:git status"). Ignored
	// when a custom Dispatcher is supplied.
	PermissionAllow []string
	PermissionDeny  []string

	// PersistRememberedRules, when set, durably writes a remembered decision to the
	// project settings file so it survives a restart. Ignored when a custom
	// Dispatcher is supplied.
	PersistRememberedRules bool

	// WorkingDirectory is the project root used to derive the sandbox policy.
	// Empty uses the current process working directory. Ignored when a custom
	// Dispatcher is supplied.
	WorkingDirectory string

	// SandboxScratchRoot is the writable scratch root for sandboxed commands.
	// Empty uses the OS temporary directory. Ignored when a custom Dispatcher is
	// supplied.
	SandboxScratchRoot string

	// SandboxExtraReadRoots and SandboxExtraWriteRoots are additive policy grants
	// for sandboxed commands. Ignored when a custom Dispatcher is supplied.
	SandboxExtraReadRoots  []string
	SandboxExtraWriteRoots []string

	// SandboxNetwork grants outbound network access to sandboxed commands. The
	// default is false, so network is denied. Ignored when a custom Dispatcher is
	// supplied.
	SandboxNetwork bool
}

// Session is one agent interaction lifecycle. It owns the conversation and runs
// the agent loop, exposing a single run entry point and a read-only snapshot of
// history. One run is in flight at a time; a second concurrent start is refused.
type Session struct {
	provider   Provider
	convo      *convo.Manager
	dispatcher Dispatcher
	permission *permission.Engine
	tools      []Tool
	maxRounds  int
	budget     int
	opts       StreamOptions
	hooks      LifecycleHooks
	sandbox    SandboxStatus
	startupErr error

	inFlight atomic.Bool
	closed   atomic.Bool
}

// NewSession creates a Session from the given configuration.
func NewSession(cfg SessionConfig) *Session {
	s, _ := newSession(cfg, false)
	return s
}

// NewSessionWithError creates a Session and reports hook construction/startup
// failures instead of deferring them to Run.
func NewSessionWithError(cfg SessionConfig) (*Session, error) {
	return newSession(cfg, true)
}

func newSession(cfg SessionConfig, strict bool) (*Session, error) {
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultMaxRounds
	}

	manager := convo.New(cfg.SystemPrompt)
	hookEngine, hookErr := hooks.New(cfg.Hooks, cfg.ExternalHooks, hooks.Options{ExternalOutputLimit: cfg.HookOutputLimit})
	var startupErr error
	if hookErr != nil {
		if strict {
			return nil, hookErr
		}
		startupErr = hookErr
	}
	if hookErr == nil && !hookEngine.Empty() {
		start := hookEngine.SessionStart(context.Background(), manager.Snapshot(), nil)
		if start.Block {
			startupErr = fmt.Errorf("session-start hook blocked startup: %s", start.Reason)
			if strict {
				return nil, startupErr
			}
		}
		for _, injected := range start.InjectedContext {
			manager.AppendSystem(injected)
		}
	}

	d, eng, advertised, sandboxStatus, dispatchErr := resolveDispatcher(cfg, hookEngine)
	if dispatchErr != nil {
		if strict {
			return nil, dispatchErr
		}
		if startupErr == nil {
			startupErr = dispatchErr
		}
	}

	return &Session{
		provider:   cfg.Provider,
		convo:      manager,
		dispatcher: d,
		permission: eng,
		tools:      advertised,
		maxRounds:  maxRounds,
		budget:     cfg.ContextBudgetTokens,
		opts:       cfg.StreamOptions,
		hooks:      hookEngine,
		sandbox:    sandboxStatus,
		startupErr: startupErr,
	}, nil
}

// resolveDispatcher picks the tool-dispatch seam, the permission engine, and the
// tool list advertised to the model. A caller-supplied Dispatcher is used as-is
// with the caller's Tools and no engine. Otherwise the default executor is built
// over the built-ins plus any custom handlers, with hooks and permission wired
// into the same ordered chain.
func resolveDispatcher(cfg SessionConfig, hookEngine *hooks.Engine) (Dispatcher, *permission.Engine, []Tool, SandboxStatus, error) {
	if cfg.Dispatcher != nil {
		return cfg.Dispatcher, nil, cfg.Tools, SandboxStatus{}, nil
	}

	catalog := tools.NewDefaultCatalog()
	for _, h := range cfg.ToolHandlers {
		catalog.MustRegister(h)
	}
	advertised := cfg.Tools
	if advertised == nil {
		advertised = catalog.Advertise()
	}

	eng := buildEngine(cfg)
	stages := executor.InertStages()
	if hookEngine != nil && !hookEngine.Empty() {
		stages.Pre = hookEngine
		stages.Post = hookEngine
	}
	stages.Permission = eng
	sbox, err := buildSandbox(cfg)
	if err != nil {
		return nil, nil, nil, SandboxStatus{}, err
	}
	stages.Sandbox = sbox
	return executor.New(catalog, stages, 0), eng, advertised, sbox.Status(), nil
}

// buildEngine constructs the permission engine from the config: starting mode,
// rule lists, and (optionally) persistence of remembered rules to the project
// settings file. An invalid mode string falls back to the default posture.
func buildEngine(cfg SessionConfig) *permission.Engine {
	mode, _ := permission.ParseMode(cfg.PermissionMode)
	pcfg := permission.Config{
		Mode:  mode,
		Rules: permission.ParseRules(cfg.PermissionAllow, cfg.PermissionDeny, nil),
	}
	if cfg.PersistRememberedRules {
		pcfg.Save = func(allow bool, rule string) error {
			return config.AppendPermissionRule(config.ProjectSettingsPath(), allow, rule)
		}
	}
	return permission.New(pcfg)
}

func buildSandbox(cfg SessionConfig) (*sandboxpkg.Sandbox, error) {
	wd := cfg.WorkingDirectory
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("agent: derive sandbox policy: %w", err)
		}
	}
	inputs := sandboxpkg.PolicyInputs{
		WorkingDirectory: wd,
		ScratchRoot:      cfg.SandboxScratchRoot,
		Settings: sandboxpkg.Grants{
			ExtraReadRoots:  cfg.SandboxExtraReadRoots,
			ExtraWriteRoots: cfg.SandboxExtraWriteRoots,
			Network:         cfg.SandboxNetwork,
		},
	}
	sbox, err := sandboxpkg.NewFromInputs(inputs)
	if err != nil {
		return nil, fmt.Errorf("agent: derive sandbox policy: %w", err)
	}
	return sbox, nil
}

// SetPermissionMode changes the permission posture for subsequent turns. It is
// intended to be called between turns. It errors on an unknown mode or when the
// session was built with a custom Dispatcher (which owns its own permission).
func (s *Session) SetPermissionMode(mode string) error {
	if s.permission == nil {
		return fmt.Errorf("agent: session has no default permission engine (custom Dispatcher in use)")
	}
	m, err := permission.ParseMode(mode)
	if err != nil {
		return err
	}
	s.permission.SetMode(m)
	return nil
}

// SandboxStatus reports the active command confinement level for sessions using
// the default executor. A session with a custom Dispatcher returns the zero value
// because the caller owns command execution.
func (s *Session) SandboxStatus() SandboxStatus {
	return s.sandbox
}

// Run starts a run from the user's input and returns one live, read-only event
// stream the caller drains to completion. A second concurrent run on the same
// session is refused with ErrRunInFlight, leaving the first run and history
// untouched.
func (s *Session) Run(ctx context.Context, input string) (<-chan RunEvent, error) {
	if s.startupErr != nil {
		return nil, s.startupErr
	}
	if s.closed.Load() {
		return nil, ErrSessionClosed
	}
	if !s.inFlight.CompareAndSwap(false, true) {
		return nil, ErrRunInFlight
	}

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

		var transientContext []string
		if s.hooks != nil {
			prompt := s.hooks.PromptSubmit(ctx, input, s.convo.Snapshot(), emit)
			if prompt.Block {
				_ = emit(RunEvent{Type: ErrorEvent, Error: fmt.Errorf("prompt-submit hook blocked turn: %s", prompt.Reason)})
				_ = emit(RunEvent{Type: StatusChange, Status: StatusIdle})
				fin := RunFinished{Reason: StopFailed, Err: fmt.Errorf("%s", prompt.Reason)}
				_ = s.hooks.RunFinished(ctx, fin, s.convo.Snapshot(), emit)
				emitTerminal(ctx, ch, RunEvent{Type: RunFinishedEvent, RunFinished: &fin})
				return
			}
			transientContext = append(transientContext, prompt.InjectedContext...)
		}

		s.convo.AppendUser(input)

		fin := loop.Run(ctx, loop.Deps{
			Provider:            s.provider,
			Context:             s.convo,
			Dispatcher:          s.dispatcher,
			Tools:               s.tools,
			MaxRounds:           s.maxRounds,
			ContextBudgetTokens: s.budget,
			StreamOptions:       s.opts,
			TransientContext:    transientContext,
		}, emit)

		if s.hooks != nil {
			_ = s.hooks.RunFinished(ctx, fin, s.convo.Snapshot(), emit)
		}

		emitTerminal(ctx, ch, RunEvent{Type: RunFinishedEvent, RunFinished: &fin})
	}()

	return ch, nil
}

// Conversation returns a deep-copied snapshot of the conversation. Callers can
// inspect, log, or render it but cannot mutate the live conversation.
func (s *Session) Conversation() Conversation {
	if s.convo == nil {
		return Conversation{}
	}
	return s.convo.Snapshot()
}

// Close stops the session and runs session-stop hooks. A blocking or failing
// session-stop hook is returned to the caller, but the session is still closed.
func (s *Session) Close(ctx context.Context) error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.hooks == nil || s.convo == nil {
		return nil
	}
	stop := s.hooks.SessionStop(ctx, s.convo.Snapshot(), nil)
	if stop.Block {
		return fmt.Errorf("session-stop hook failed: %s", stop.Reason)
	}
	return nil
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
