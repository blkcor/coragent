package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/prompt"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

type EngineConfig struct {
	StoreRoot       string
	Provider        provider.Provider
	ContextWindow   int
	MaxOutputTokens int
	UserPreferences string
	Logger          *slog.Logger
	Now             func() time.Time
	Sleep           SleepFunc
	Jitter          JitterFunc
}

// Engine owns process-wide adapters and creates or loads independent sessions.
type Engine struct {
	cfg     EngineConfig
	binding store.ProviderBinding
}

// StoredSession is the frontend-safe lifecycle view of durable state.
type StoredSession struct {
	SessionID    string
	Workspace    string
	LastActivity time.Time
	Closed       bool
}

func DefaultStoreRoot() (string, error) { return store.DefaultRoot() }

func InspectStoredSession(root, sessionID string) (StoredSession, error) {
	durable, err := store.Open(root, sessionID)
	if err != nil {
		return StoredSession{}, err
	}
	manifest := durable.Manifest()
	return StoredSession{SessionID: manifest.SessionID, Workspace: manifest.Workspace, LastActivity: manifest.UpdatedAt, Closed: manifest.Closed}, nil
}

// LoadStoredTranscript returns durable semantic history without constructing a
// Provider runtime. Frontends use it for replay, including closed sessions
// whose history remains available but cannot accept another submit command.
func LoadStoredTranscript(root, sessionID string) ([]transcript.Record, error) {
	durable, err := store.Open(root, sessionID)
	if err != nil {
		return nil, err
	}
	return durable.Transcript(), nil
}

func ListStoredSessions(root string) ([]StoredSession, error) {
	summaries, err := store.List(root)
	if err != nil {
		return nil, err
	}
	out := make([]StoredSession, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, StoredSession{SessionID: summary.SessionID, Workspace: summary.Workspace, LastActivity: summary.LastActivity, Closed: summary.Closed})
	}
	return out, nil
}

// CloseStoredSession is a Provider-free lifecycle command. It uses the same
// Session state machine and durable command/event path as an active runtime,
// but cannot run a model turn or execute an action.
func CloseStoredSession(ctx context.Context, root, sessionID, commandID string) error {
	durable, err := store.Open(root, sessionID)
	if err != nil {
		return err
	}
	if durable.Manifest().Closed {
		return nil
	}
	session, err := NewSession(sessionID, Config{Provider: provider.NewScripted(), Durable: durable})
	if err != nil {
		return err
	}
	command, err := sessioncommand.NewClose(commandID)
	if err != nil {
		return err
	}
	return session.Apply(ctx, command.ForSession(sessionID))
}

func New(cfg EngineConfig) (*Engine, error) {
	if cfg.StoreRoot == "" {
		return nil, errors.New("engine: store root is required")
	}
	if cfg.Provider == nil {
		return nil, errors.New("engine: Provider is required")
	}
	if cfg.ContextWindow <= 0 || cfg.MaxOutputTokens <= 0 {
		return nil, errors.New("engine: explicit context and output limits are required")
	}
	identityProvider, ok := cfg.Provider.(provider.IdentityProvider)
	if !ok {
		return nil, errors.New("engine: Provider does not expose an immutable session identity")
	}
	identity := identityProvider.Identity()
	if identity.Adapter == "" || identity.WireProtocol == "" || identity.EndpointSHA256 == "" || identity.CredentialSourceSHA256 == "" || identity.Model == "" {
		return nil, errors.New("engine: Provider identity is incomplete")
	}
	if (identity.ContextWindow != 0 && identity.ContextWindow != cfg.ContextWindow) || (identity.MaxOutputTokens != 0 && identity.MaxOutputTokens != cfg.MaxOutputTokens) {
		return nil, errors.New("engine: Provider capability limits differ from Engine configuration")
	}
	preferencesDigest := sha256.Sum256([]byte(cfg.UserPreferences))
	binding := store.ProviderBinding{
		Adapter: identity.Adapter, WireProtocol: identity.WireProtocol,
		EndpointSHA256: identity.EndpointSHA256, CredentialSourceSHA256: identity.CredentialSourceSHA256, Model: identity.Model,
		ContextWindow: cfg.ContextWindow, MaxOutputTokens: cfg.MaxOutputTokens,
		Temperature: identity.Temperature, Seed: identity.Seed, ToolChoice: identity.ToolChoice,
		UserPreferencesSHA256: hex.EncodeToString(preferencesDigest[:]), PromptVersion: prompt.PromptVersion,
	}
	binding.Digest = binding.ComputeDigest()
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	return &Engine{cfg: cfg, binding: binding}, nil
}

func (e *Engine) Create(ctx context.Context, workspacePath string) (*Session, error) {
	return e.createOrLoad(ctx, workspacePath, "", nil)
}

func (e *Engine) Load(ctx context.Context, sessionID string) (*Session, error) {
	durable, err := store.Open(e.cfg.StoreRoot, sessionID)
	if err != nil {
		return nil, err
	}
	return e.createOrLoad(ctx, durable.Manifest().Workspace, sessionID, durable)
}

func (e *Engine) List() ([]store.Summary, error) { return store.List(e.cfg.StoreRoot) }

// CloseSession loads and closes without deleting history. It does not contact
// the Provider.
func (e *Engine) CloseSession(ctx context.Context, sessionID, commandID string) error {
	s, err := e.Load(ctx, sessionID)
	if err != nil {
		return err
	}
	cmd, err := sessioncommand.NewClose(commandID)
	if err != nil {
		return err
	}
	return s.Apply(ctx, cmd.ForSession(sessionID))
}

func (e *Engine) createOrLoad(ctx context.Context, workspacePath, sessionID string, durable *store.Session) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w, err := workspace.Open(workspacePath)
	if err != nil {
		return nil, err
	}
	if durable != nil {
		manifest := durable.Manifest()
		if w.Name() != manifest.Workspace {
			_ = w.Close()
			return nil, errors.New("engine: durable workspace identity changed")
		}
		identity, err := w.Identity()
		if err != nil {
			_ = w.Close()
			return nil, err
		}
		if identity != manifest.WorkspaceIdentity {
			_ = w.Close()
			return nil, errors.New("engine: durable workspace filesystem identity changed")
		}
		if manifest.ProjectionVersion != dataproj.ProjectionVersion {
			_ = w.Close()
			return nil, errors.New("engine: durable session data-projection version differs from this runtime")
		}
		if manifest.ProviderBinding.Digest != e.binding.Digest {
			_ = w.Close()
			return nil, errors.New("engine: durable session Provider binding differs from current runtime settings")
		}
	}
	projector := dataproj.New()
	if err := projector.ValidatePrompt(w.Name()); err != nil {
		_ = w.Close()
		return nil, errors.New("engine: workspace path contains detected credential material")
	}
	if e.cfg.UserPreferences != "" {
		if err := projector.ValidatePrompt(e.cfg.UserPreferences); err != nil {
			_ = w.Close()
			return nil, errors.New("engine: user preferences contain detected credential material")
		}
	}
	docs, err := prompt.DiscoverInstructions(ctx, w, ".", projector)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	broker, err := action.NewBrokerWithProjector(projector, tools.NewCatalog(workspace.NewFileService(w), projector)...)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	assembler, err := prompt.NewAssembler(prompt.Config{
		Workspace: w.Name(), ActivePath: ".", ContextWindow: e.cfg.ContextWindow,
		MaxOutputTokens: e.cfg.MaxOutputTokens, UserPreferences: e.cfg.UserPreferences,
	})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	if durable == nil {
		now := time.Now()
		if e.cfg.Now != nil {
			now = e.cfg.Now()
		}
		for attempts := 0; attempts < 8; attempts++ {
			sessionID, err = newSessionID()
			if err != nil {
				_ = w.Close()
				return nil, err
			}
			durable, err = store.Create(e.cfg.StoreRoot, sessionID, w.Name(), dataproj.ProjectionVersion, e.binding, now)
			if errors.Is(err, store.ErrExists) {
				continue
			}
			if err != nil {
				_ = w.Close()
				return nil, err
			}
			break
		}
		if durable == nil {
			_ = w.Close()
			return nil, errors.New("engine: could not allocate a unique session ID")
		}
	}
	s, err := NewSession(sessionID, Config{
		Provider: e.cfg.Provider, Durable: durable, Broker: broker, Assembler: assembler,
		Instructions: docs, InstructionSource: func(refreshCtx context.Context) ([]prompt.Instruction, error) {
			return prompt.DiscoverInstructions(refreshCtx, w, ".", projector)
		}, Projector: projector, Logger: e.cfg.Logger, Now: e.cfg.Now, Resource: w,
		Sleep: e.cfg.Sleep, Jitter: e.cfg.Jitter,
	})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	return s, nil
}

func newSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("engine: generate session ID: %w", err)
	}
	return "sess-" + hex.EncodeToString(raw[:]), nil
}
