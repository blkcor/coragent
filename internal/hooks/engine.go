// Package hooks implements the hard, unconditional hook engine.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/blkcor/coragent/internal/core"
)

const (
	defaultTimeout     = 5 * time.Second
	defaultOutputLimit = 64 * 1024
)

// Options tunes hook engine resource limits.
type Options struct {
	ExternalOutputLimit int
}

// Engine evaluates in-process and external hooks in deterministic order.
type Engine struct {
	hooks       []compiledHook
	outputLimit int
}

type hookKind int

const (
	inProcessHook hookKind = iota
	externalHook
)

type compiledHook struct {
	name    string
	moment  core.HookMoment
	scope   core.HookScope
	pattern *regexp.Regexp
	timeout time.Duration
	kind    hookKind
	handler core.HookFunc
	command []string
}

// New builds a hook engine. In-process hooks are ordered before external hooks;
// definitions are validated up front so bad patterns and impossible commands are
// discovered before a run reaches them.
func New(inProcess []core.HookRegistration, external []core.ExternalHook, opts Options) (*Engine, error) {
	limit := opts.ExternalOutputLimit
	if limit <= 0 {
		limit = defaultOutputLimit
	}

	compiled := make([]compiledHook, 0, len(inProcess)+len(external))
	for i, h := range inProcess {
		c, err := compileInProcess(h, i)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, c)
	}
	for i, h := range external {
		c, err := compileExternal(h, i)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, c)
	}

	return &Engine{hooks: compiled, outputLimit: limit}, nil
}

// Empty returns true when no hooks are configured.
func (e *Engine) Empty() bool {
	return e == nil || len(e.hooks) == 0
}

func compileInProcess(h core.HookRegistration, i int) (compiledHook, error) {
	name := h.Name
	if name == "" {
		name = fmt.Sprintf("in-process-%d", i+1)
	}
	if h.Handler == nil {
		return compiledHook{}, fmt.Errorf("hooks: hook %q has no handler", name)
	}
	pattern, err := validate(name, h.Moment, h.Scope, h.Timeout)
	if err != nil {
		return compiledHook{}, err
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return compiledHook{
		name: name, moment: h.Moment, scope: h.Scope, pattern: pattern,
		timeout: timeout, kind: inProcessHook, handler: h.Handler,
	}, nil
}

func compileExternal(h core.ExternalHook, i int) (compiledHook, error) {
	name := h.Name
	if name == "" {
		name = fmt.Sprintf("external-%d", i+1)
	}
	pattern, err := validate(name, h.Moment, h.Scope, h.Timeout)
	if err != nil {
		return compiledHook{}, err
	}
	if len(h.Command) == 0 || h.Command[0] == "" {
		return compiledHook{}, fmt.Errorf("hooks: hook %q has empty command", name)
	}
	if err := validateCommand(name, h.Command[0]); err != nil {
		return compiledHook{}, err
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	cmd := append([]string(nil), h.Command...)
	return compiledHook{
		name: name, moment: h.Moment, scope: h.Scope, pattern: pattern,
		timeout: timeout, kind: externalHook, command: cmd,
	}, nil
}

func validate(name string, moment core.HookMoment, scope core.HookScope, timeout time.Duration) (*regexp.Regexp, error) {
	switch moment {
	case core.HookSessionStart, core.HookPromptSubmit, core.HookBeforeTool, core.HookAfterTool, core.HookRunFinished, core.HookSessionStop:
	default:
		return nil, fmt.Errorf("hooks: hook %q has invalid moment %q", name, moment)
	}
	if timeout < 0 {
		return nil, fmt.Errorf("hooks: hook %q has invalid timeout", name)
	}
	if scope.Pattern == "" {
		return nil, nil
	}
	pattern, err := regexp.Compile(scope.Pattern)
	if err != nil {
		return nil, fmt.Errorf("hooks: hook %q has invalid pattern: %w", name, err)
	}
	return pattern, nil
}

func validateCommand(name, program string) error {
	if strings.ContainsRune(program, filepath.Separator) {
		if _, err := os.Stat(program); err != nil {
			return fmt.Errorf("hooks: hook %q command %q is unavailable: %w", name, program, err)
		}
		return nil
	}
	if _, err := exec.LookPath(program); err != nil {
		return fmt.Errorf("hooks: hook %q command %q is unavailable: %w", name, program, err)
	}
	return nil
}

// PreCheck adapts before-tool hooks to the executor's hard pre-check slot.
func (e *Engine) PreCheck(ctx context.Context, call core.ToolCall) core.StageDecision {
	return e.PreCheckWithEmit(ctx, call, nil)
}

// PreCheckWithEmit adapts before-tool hooks and emits every observable hook
// outcome as it happens.
func (e *Engine) PreCheckWithEmit(ctx context.Context, call core.ToolCall, emit func(core.RunEvent) error) core.StageDecision {
	ev := core.HookEvent{Moment: core.HookBeforeTool, ToolCall: &call, Detail: detailForCall(call)}
	out := e.run(ctx, ev, emit)
	d := core.StageDecision{
		Block:           out.Block,
		Reason:          out.Reason,
		EditedArguments: out.Arguments,
	}
	if emit == nil {
		d.Outcome = outcomeFromRun(out)
		if d.Outcome != nil && d.Outcome.Action == "" {
			d.Outcome.Action = core.HookReplaced
		}
	}
	return d
}

// PostCheck adapts after-tool hooks to the executor's hard post-check slot.
func (e *Engine) PostCheck(ctx context.Context, call core.ToolCall, result core.ToolResult) core.StageDecision {
	return e.PostCheckWithEmit(ctx, call, result, nil)
}

// PostCheckWithEmit adapts after-tool hooks and emits every observable hook
// outcome as it happens.
func (e *Engine) PostCheckWithEmit(ctx context.Context, call core.ToolCall, result core.ToolResult, emit func(core.RunEvent) error) core.StageDecision {
	ev := core.HookEvent{Moment: core.HookAfterTool, ToolCall: &call, ToolResult: &result, Detail: result.Result}
	out := e.run(ctx, ev, emit)
	d := core.StageDecision{Block: out.Block, Reason: out.Reason}
	if emit == nil {
		d.Outcome = outcomeFromRun(out)
	}
	if out.Result != nil {
		repl := result
		repl.Result = *out.Result
		d.ReplacementResult = &repl
		if emit == nil && d.Outcome == nil {
			d.Outcome = &core.HookOutcome{HookName: out.HookName, Moment: ev.Moment, Action: core.HookReplaced, Reason: out.Reason}
		}
	}
	return d
}

// SessionStart evaluates session-start hooks.
func (e *Engine) SessionStart(ctx context.Context, conv core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	ev := core.HookEvent{Moment: core.HookSessionStart, Conversation: conv}
	out := e.run(ctx, ev, emit)
	return core.HookLifecycleResult{Block: out.Block, Reason: out.Reason, InjectedContext: out.InjectedContext}
}

// PromptSubmit evaluates prompt-submit hooks.
func (e *Engine) PromptSubmit(ctx context.Context, prompt string, conv core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	ev := core.HookEvent{Moment: core.HookPromptSubmit, Prompt: prompt, Conversation: conv, Detail: prompt}
	out := e.run(ctx, ev, emit)
	return core.HookLifecycleResult{Block: out.Block, Reason: out.Reason, InjectedContext: out.InjectedContext}
}

// RunFinished evaluates hooks after a run has reached its terminal outcome and
// before the caller emits the terminal event.
func (e *Engine) RunFinished(ctx context.Context, fin core.RunFinished, conv core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	ev := core.HookEvent{Moment: core.HookRunFinished, RunFinished: &fin, Conversation: conv, Detail: runFinishedDetail(fin)}
	out := e.run(ctx, ev, emit)
	return core.HookLifecycleResult{Block: out.Block, Reason: out.Reason, InjectedContext: out.InjectedContext}
}

// SessionStop evaluates session-stop hooks.
func (e *Engine) SessionStop(ctx context.Context, conv core.Conversation, emit func(core.RunEvent) error) core.HookLifecycleResult {
	ev := core.HookEvent{Moment: core.HookSessionStop, Conversation: conv}
	out := e.run(ctx, ev, emit)
	return core.HookLifecycleResult{Block: out.Block, Reason: out.Reason, InjectedContext: out.InjectedContext}
}

type runOutcome struct {
	Block           bool
	Reason          string
	Arguments       map[string]interface{}
	Result          *string
	InjectedContext []string
	HookName        string
	Action          core.HookAction
}

func (e *Engine) run(ctx context.Context, ev core.HookEvent, emit func(core.RunEvent) error) runOutcome {
	if e.Empty() {
		return runOutcome{}
	}
	current := ev
	var out runOutcome
	for i := range e.hooks {
		h := e.hooks[i]
		if !h.matches(current) {
			continue
		}
		verdict := e.invoke(ctx, h, current)
		if verdict.Block {
			return e.record(emit, current.Moment, h.name, runOutcome{
				Block: true, Reason: nonEmpty(verdict.Reason, "blocked by hook "+h.name),
				HookName: h.name, Action: core.HookBlocked,
			})
		}
		if verdict.Arguments != nil {
			out.Arguments = verdict.Arguments
			out.HookName = h.name
			out.Action = core.HookReplaced
			if current.ToolCall != nil {
				current.ToolCall.Arguments = verdict.Arguments
				current.Detail = detailForCall(*current.ToolCall)
			}
			e.record(emit, current.Moment, h.name, runOutcome{HookName: h.name, Action: core.HookReplaced, Reason: verdict.Reason})
		}
		if verdict.Result != nil {
			out.Result = verdict.Result
			out.HookName = h.name
			out.Action = core.HookReplaced
			if current.ToolResult != nil {
				current.ToolResult.Result = *verdict.Result
				current.Detail = *verdict.Result
			}
			e.record(emit, current.Moment, h.name, runOutcome{HookName: h.name, Action: core.HookReplaced, Reason: verdict.Reason})
		}
		if len(verdict.InjectedContext) > 0 {
			out.InjectedContext = append(out.InjectedContext, verdict.InjectedContext...)
			out.HookName = h.name
			out.Action = core.HookInjected
			e.record(emit, current.Moment, h.name, runOutcome{HookName: h.name, Action: core.HookInjected, Reason: verdict.Reason})
		}
	}
	return out
}

func (e *Engine) invoke(ctx context.Context, h compiledHook, ev core.HookEvent) (verdict core.HookVerdict) {
	if h.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.timeout)
		defer cancel()
	}
	switch h.kind {
	case inProcessHook:
		defer func() {
			if r := recover(); r != nil {
				verdict = core.HookVerdict{Block: true, Reason: fmt.Sprintf("hook %s panicked: %v", h.name, r)}
			}
		}()
		return h.handler(ctx, ev)
	case externalHook:
		return e.invokeExternal(ctx, h, ev)
	default:
		return core.HookVerdict{Block: true, Reason: "unknown hook kind"}
	}
}

func (e *Engine) invokeExternal(ctx context.Context, h compiledHook, ev core.HookEvent) core.HookVerdict {
	payload, err := json.Marshal(ev)
	if err != nil {
		return core.HookVerdict{Block: true, Reason: "marshal hook event: " + err.Error()}
	}

	cmd := exec.CommandContext(ctx, h.command[0], h.command[1:]...)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return core.HookVerdict{Block: true, Reason: "prepare hook stdout: " + err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return core.HookVerdict{Block: true, Reason: "prepare hook stderr: " + err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return core.HookVerdict{Block: true, Reason: "start hook: " + err.Error()}
	}

	var out, errOut limitedBuffer
	out.limit = e.outputLimit
	errOut.limit = e.outputLimit
	outDone := make(chan error, 1)
	errDone := make(chan error, 1)
	go func() { _, err := io.Copy(&out, stdout); outDone <- err }()
	go func() { _, err := io.Copy(&errOut, stderr); errDone <- err }()

	waitErr := cmd.Wait()
	copyErr := errors.Join(<-outDone, <-errDone)
	if ctx.Err() != nil {
		killProcessGroup(cmd.Process)
		return core.HookVerdict{Block: true, Reason: "hook timed out or was cancelled: " + ctx.Err().Error()}
	}
	if copyErr != nil && !errors.Is(copyErr, errOutputLimit) {
		return core.HookVerdict{Block: true, Reason: "read hook output: " + copyErr.Error()}
	}
	if out.exceeded || errOut.exceeded {
		return core.HookVerdict{Block: true, Reason: "hook output exceeded limit"}
	}

	raw := strings.TrimSpace(out.String())
	if raw != "" {
		if e.outputLimit > 0 && len(out.Bytes()) >= e.outputLimit {
			return core.HookVerdict{Block: true, Reason: "hook output exceeded limit"}
		}
		var verdict core.HookVerdict
		if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
			return core.HookVerdict{Block: true, Reason: "malformed hook verdict: " + err.Error()}
		}
		return verdict
	}
	if waitErr != nil {
		reason := strings.TrimSpace(errOut.String())
		if reason == "" {
			reason = waitErr.Error()
		}
		return core.HookVerdict{Block: true, Reason: reason}
	}
	return core.HookVerdict{}
}

func (h compiledHook) matches(ev core.HookEvent) bool {
	if h.moment != ev.Moment {
		return false
	}
	if h.scope.ToolName != "" {
		if ev.ToolCall == nil || ev.ToolCall.ToolName != h.scope.ToolName {
			return false
		}
	}
	if h.pattern != nil && !h.pattern.MatchString(ev.Detail) {
		return false
	}
	return true
}

func (e *Engine) record(emit func(core.RunEvent) error, moment core.HookMoment, name string, out runOutcome) runOutcome {
	if out.HookName == "" {
		out.HookName = name
	}
	if emit != nil && out.Action != "" {
		_ = emit(core.RunEvent{Type: core.HookOutcomeEvent, HookOutcome: &core.HookOutcome{
			HookName: out.HookName,
			Moment:   moment,
			Action:   out.Action,
			Reason:   out.Reason,
		}})
	}
	return out
}

func outcomeFromRun(out runOutcome) *core.HookOutcome {
	if out.Action == "" {
		return nil
	}
	return &core.HookOutcome{HookName: out.HookName, Action: out.Action, Reason: out.Reason}
}

func detailForCall(call core.ToolCall) string {
	if len(call.Arguments) == 0 {
		return call.ToolName
	}
	b, err := json.Marshal(call.Arguments)
	if err != nil {
		return call.ToolName
	}
	return call.ToolName + " " + string(b)
}

func runFinishedDetail(fin core.RunFinished) string {
	if fin.Err != nil {
		return fmt.Sprintf("%s: %v", stopReasonDetail(fin.Reason), fin.Err)
	}
	return stopReasonDetail(fin.Reason)
}

func stopReasonDetail(reason core.StopReason) string {
	switch reason {
	case core.StopCompleted:
		return "completed"
	case core.StopReachedStepLimit:
		return "reached-step-limit"
	case core.StopCancelled:
		return "cancelled"
	case core.StopFailed:
		return "failed"
	default:
		return "unknown"
	}
}

func nonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

var errOutputLimit = errors.New("hook output limit exceeded")

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.exceeded = true
		return 0, errOutputLimit
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.exceeded = true
		return remaining, errOutputLimit
	}
	return b.Buffer.Write(p)
}

func killProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}

var (
	_ core.PreToolCheck   = (*Engine)(nil)
	_ core.PostToolCheck  = (*Engine)(nil)
	_ core.LifecycleHooks = (*Engine)(nil)
)
