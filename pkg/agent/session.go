package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/blkcor/coragent/internal/config"
	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/executor"
	"github.com/blkcor/coragent/internal/hooks"
	"github.com/blkcor/coragent/internal/permission"
	sandboxpkg "github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/sessionrun"
	"github.com/blkcor/coragent/internal/subagent"
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

	// Tools are the capabilities offered to the model. When nil and no custom
	// Dispatcher is set, the built-in tools are advertised automatically. A
	// non-nil slice, including an empty one, is authoritative.
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

	// PermissionAllow and PermissionDeny are the starting allow/deny rule lists.
	// Each entry is either a family rule in "<kind>:<match>" form (for example,
	// "command:git status") or an exact rule in
	// "exact-v2:<kind>:hmac-sha256:<64-lowercase-hex>" form. Unsafe legacy
	// exact-v1 entries remain parseable for migration but never auto-match; the
	// standard disk-loading path scrubs them before resolving settings. Ignored
	// when a custom Dispatcher is supplied.
	PermissionAllow []string
	PermissionDeny  []string

	// PermissionFingerprintKey injects stable secret material for reloadable
	// exact-call rules. The zero value uses a session-ephemeral key unless
	// PersistRememberedRules is set, in which case the standard constructor
	// securely loads or creates the user's independent 0600 key file.
	PermissionFingerprintKey PermissionFingerprintKey

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
	runtime     *sessionrun.Runtime
	permission  *permission.Engine
	sandbox     SandboxStatus
	description SessionDescription
	startupErr  error

	stateMu  sync.Mutex
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

	hookEngine, hookErr := hooks.New(cfg.Hooks, cfg.ExternalHooks, hooks.Options{ExternalOutputLimit: cfg.HookOutputLimit})
	if hookErr != nil && strict {
		return nil, hookErr
	}

	runtime := sessionrun.New(sessionrun.Config{
		Provider:            cfg.Provider,
		SystemPrompt:        cfg.SystemPrompt,
		MaxRounds:           maxRounds,
		ContextBudgetTokens: cfg.ContextBudgetTokens,
		StreamOptions:       cfg.StreamOptions,
		Hooks:               hookEngine,
	})

	var startErr error
	if hookErr == nil {
		startErr = runtime.Start(context.Background(), nil)
	}
	if startErr != nil && strict {
		return nil, startErr
	}

	d, eng, advertised, sandboxStatus, dispatchErr := resolveDispatcher(cfg, hookEngine, maxRounds)
	runtime.BindExecution(d, advertised)
	if dispatchErr != nil && strict {
		return nil, dispatchErr
	}

	startupErr := hookErr
	if startupErr == nil {
		startupErr = startErr
	}
	if startupErr == nil {
		startupErr = dispatchErr
	}

	return &Session{
		runtime:     runtime,
		permission:  eng,
		sandbox:     sandboxStatus,
		description: buildSessionDescription(cfg, advertised, sandboxStatus),
		startupErr:  startupErr,
	}, nil
}

// resolveDispatcher picks the tool-dispatch seam, the permission engine, and the
// tool list advertised to the model. A caller-supplied Dispatcher is used as-is
// with the caller's Tools and no engine. Otherwise the default executor is built
// over the built-ins plus any custom handlers, with hooks and permission wired
// into the same ordered chain.
func resolveDispatcher(cfg SessionConfig, hookEngine *hooks.Engine, maxRounds int) (Dispatcher, *permission.Engine, []Tool, SandboxStatus, error) {
	if cfg.Dispatcher != nil {
		return cfg.Dispatcher, nil, cfg.Tools, SandboxStatus{}, nil
	}
	if cfg.PersistRememberedRules {
		if err := config.ScrubLegacyExactPermissionRules(); err != nil {
			return nil, nil, nil, SandboxStatus{}, fmt.Errorf("agent: migrate legacy exact permission rules: %w", err)
		}
	}
	if cfg.PersistRememberedRules && !cfg.PermissionFingerprintKey.Valid() {
		loaded, err := config.LoadOrCreatePermissionFingerprintKey()
		if err != nil {
			return nil, nil, nil, SandboxStatus{}, fmt.Errorf("agent: load permission fingerprint key: %w", err)
		}
		material := loaded.ConsumeMaterial()
		key, err := NewPermissionFingerprintKey(material)
		clear(material)
		if err != nil {
			return nil, nil, nil, SandboxStatus{}, err
		}
		cfg.PermissionFingerprintKey = key
		if loaded.InvalidatesExactRules() {
			cfg = filterPermissionRulesAfterKeyReset(cfg)
		}
	}

	catalog := tools.NewDefaultCatalog()
	for _, h := range cfg.ToolHandlers {
		catalog.MustRegister(h)
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

	advertised := cfg.Tools
	if _, callerOwnsTask := catalog.Lookup(subagent.ToolName); !callerOwnsTask {
		ordinaryAdvertised := advertised
		if ordinaryAdvertised == nil {
			ordinaryAdvertised = catalog.Advertise()
		}
		blueprint := subagent.NewBlueprint(subagent.BlueprintConfig{
			Provider:            cfg.Provider,
			Catalog:             catalog,
			Advertised:          ordinaryAdvertised,
			Stages:              stages,
			Hooks:               hookEngine,
			MaxRounds:           maxRounds,
			ContextBudgetTokens: cfg.ContextBudgetTokens,
			StreamOptions:       cfg.StreamOptions,
		})
		catalog.MustRegister(subagent.NewTaskHandler(blueprint))
	}
	if advertised == nil {
		advertised = catalog.Advertise()
	}
	return executor.New(catalog, stages, 0), eng, advertised, sbox.Status(), nil
}

func filterPermissionRulesAfterKeyReset(cfg SessionConfig) SessionConfig {
	cfg.PermissionAllow = config.FilterPermissionRulesAfterKeyReset(cfg.PermissionAllow)
	cfg.PermissionDeny = config.FilterPermissionRulesAfterKeyReset(cfg.PermissionDeny)
	return cfg
}

// buildEngine constructs the permission engine from the config: starting mode,
// rule lists, and (optionally) persistence of remembered rules to the project
// settings file. An invalid mode string falls back to the default posture.
func buildEngine(cfg SessionConfig) *permission.Engine {
	mode, _ := permission.ParseMode(cfg.PermissionMode)
	pcfg := permission.Config{
		Mode:           mode,
		Rules:          permission.ParseRules(cfg.PermissionAllow, cfg.PermissionDeny, nil),
		FingerprintKey: cfg.PermissionFingerprintKey,
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

// SetPermissionMode changes the permission posture for subsequent permission
// decisions, including during an active run. A request already open keeps the
// mode it snapshotted. It errors on an unknown mode or when the session was built
// with a custom Dispatcher (which owns its own permission).
func (s *Session) SetPermissionMode(mode string) error {
	m, err := permission.ParseMode(mode)
	if err != nil {
		return err
	}
	return s.SetPermissionModeTyped(publicPermissionMode(m))
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

	// Buffered by one so the single terminal event always has room to enqueue,
	// even when the context is already cancelled (guaranteed terminal delivery).
	ch := make(chan RunEvent, 1)

	go func() {
		defer close(ch)
		defer s.inFlight.Store(false)

		_ = s.runtime.RunRich(ctx, input, sessionrun.RichRunOptions{
			RunID:  RunID(newOpaqueID("run")),
			Origin: Origin{AgentID: s.description.RootAgentID},
		}, func(event core.RichEvent) error {
			if event.Legacy == nil {
				return nil
			}
			if event.Kind == ObservedKindRunFinished {
				emitTerminal(ctx, ch, *event.Legacy)
				return nil
			}
			select {
			case ch <- *event.Legacy:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	return ch, nil
}

// Conversation returns a deep-copied snapshot of the conversation. Callers can
// inspect, log, or render it but cannot mutate the live conversation.
func (s *Session) Conversation() Conversation {
	if s.runtime == nil {
		return Conversation{}
	}
	return s.runtime.Conversation()
}

// Close stops the session and runs session-stop hooks. A blocking or failing
// session-stop hook is returned to the caller, but the session is still closed.
func (s *Session) Close(ctx context.Context) error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.runtime == nil {
		return nil
	}
	return s.runtime.Stop(ctx, nil)
}

// emitTerminal delivers the single terminal RunFinishedEvent. During ordinary
// completion it preserves backpressure and waits for the reader. After
// cancellation, at most one non-terminal event can occupy the stream's one-slot
// buffer; that stale pending fact is discarded to reserve the slot for the
// authoritative terminal. This keeps an abandoned reader from wedging the run
// while ensuring a later reader never sees a non-terminal event without the
// terminal that settles it.
func emitTerminal(ctx context.Context, ch chan RunEvent, ev RunEvent) {
	select {
	case ch <- ev:
		return
	default:
	}

	if ctx.Err() == nil {
		ch <- ev
		return
	}

	// Cancellation can leave one advisory event buffered after the reader stops.
	// Drop only that pending non-terminal event; no other producer writes ch.
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- ev:
	default:
		// A concurrent live reader may have raced with the drain. Retry as a
		// blocking send: the buffer is now guaranteed to make progress.
		ch <- ev
	}
}
