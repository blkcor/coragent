package tools

import (
	"context"
	"encoding/json"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/policy"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

// CommandEffectAnalyzer is the deterministic classification seam used by the
// command Prepare phase.
type CommandEffectAnalyzer interface {
	Classify(string, []string, sandbox.Grants) policy.EffectClassification
}

// CommandPolicy is the pure session-policy seam used by command preparation.
type CommandPolicy interface {
	Decide(context.Context, policy.CommandSpec, policy.EffectClassification, *policy.SessionState) policy.PolicyDecision
}

// CommandToolConfig contains only session-scoped capabilities. It has no
// ambient filesystem, process, environment, or credential authority.
type CommandToolConfig struct {
	WorkspaceRoot string
	FileService   workspace.FileService
	Projector     *dataproj.Projector
	Analyzer      CommandEffectAnalyzer
	Policy        CommandPolicy
	Session       *policy.SessionState
	Sandbox       sandbox.Sandbox
	Grants        sandbox.Grants
	Now           func() time.Time
}

// CommandTool implements the side-effect-free S2.7 Prepare phase. Execution is
// added in S2.8; until then this tool is not part of the production catalog.
type CommandTool struct {
	workspaceRoot string
	fs            workspace.FileService
	projector     *dataproj.Projector
	analyzer      CommandEffectAnalyzer
	policy        CommandPolicy
	session       *policy.SessionState
	runner        sandbox.Sandbox
	grants        sandbox.Grants
	now           func() time.Time
}

// NewCommandTool constructs a command tool with explicit scoped dependencies.
func NewCommandTool(cfg CommandToolConfig) (*CommandTool, error) {
	if cfg.WorkspaceRoot == "" || cfg.FileService == nil {
		return nil, errors.New("command: workspace root and FileService are required")
	}
	if cfg.Analyzer == nil || cfg.Policy == nil || cfg.Sandbox == nil {
		return nil, errors.New("command: analyzer, policy, and sandbox are required")
	}
	root, err := filepath.EvalSymlinks(cfg.WorkspaceRoot)
	if err != nil {
		return nil, errors.New("command: resolve workspace root")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, errors.New("command: canonicalize workspace root")
	}
	projector := cfg.Projector
	if projector == nil {
		projector = dataproj.New()
	}
	if projector.Detector().Contains(root) {
		return nil, errors.New("command: workspace root contains detected credential material")
	}
	scopedFS, clean, err := cfg.FileService.List(".")
	if err != nil {
		return nil, errors.New("command: inspect FileService root")
	}
	scopedInfo, err := iofs.Stat(scopedFS, clean)
	if err != nil {
		return nil, errors.New("command: stat FileService root")
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !os.SameFile(scopedInfo, rootInfo) {
		return nil, errors.New("command: FileService and workspace root differ")
	}
	session := cfg.Session
	if session == nil {
		session = policy.NewSessionState()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	grants := cloneGrants(cfg.Grants)
	if len(grants.AllowedReadPaths) == 0 {
		grants.AllowedReadPaths = []string{root}
	}
	if len(grants.AllowedWritePaths) == 0 {
		grants.AllowedWritePaths = []string{root}
	}
	return &CommandTool{
		workspaceRoot: root, fs: cfg.FileService, projector: projector,
		analyzer: cfg.Analyzer, policy: cfg.Policy, session: session,
		runner: cfg.Sandbox, grants: grants, now: now,
	}, nil
}

func (t *CommandTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Name:        "command",
		Description: "Prepare one structured command for policy review and sandboxed execution. Shell interpreters, pipelines, and redirects are unavailable.",
		Schema:      json.RawMessage(`{"type":"object","additionalProperties":false,"required":["command"],"properties":{"command":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"cwd":{"type":"string"},"env":{"type":"object","additionalProperties":{"type":"string"}},"timeout_ms":{"type":"integer","minimum":0,"maximum":600000},"max_output_bytes":{"type":"integer","minimum":0,"maximum":4194304},"pty":{"type":"boolean"}}}`),
	}
}

func (t *CommandTool) Prepare(ctx context.Context, raw json.RawMessage) (action.Prepared, error) {
	if t == nil {
		return action.Prepared{}, errors.New("command: nil tool")
	}
	return t.prepare(ctx, raw)
}

func (t *CommandTool) Execute(ctx context.Context, prepared action.Prepared) action.Execution {
	if err := ctx.Err(); err != nil {
		return action.Execution{Outcome: transcript.ToolResultCancelled, Content: "command execution cancelled"}
	}
	if prepared.Denied {
		return action.Execution{Outcome: transcript.ToolResultBlocked, Content: prepared.DenyReason}
	}
	return action.Execution{Outcome: transcript.ToolResultError, Content: "command execution is unavailable until S2.8"}
}
