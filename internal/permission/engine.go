// Package permission implements the soft, human-in-the-loop permission gate.
//
// The Engine is the real core.Permission: it resolves every tool call to allow,
// deny, or ask, applying four modes (default, auto-accept-edits, plan, bypass)
// and an allow/deny rule set in a fixed order that yields the user-visible
// promises — bypass overrides everything soft, a plan-mode block beats any allow,
// and a deny beats an allow. Permission is advisory and bypassable; it is never a
// security boundary (the hard guardrails are hooks and the sandbox).
package permission

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blkcor/coragent/internal/core"
)

// Mode is the permission posture governing a turn.
type Mode int

const (
	// ModeDefault consults rules and asks about anything uncovered.
	ModeDefault Mode = iota

	// ModeAutoAcceptEdits allows file edits without asking; other actions still ask.
	ModeAutoAcceptEdits

	// ModePlan blocks every state-changing action; only reads proceed.
	ModePlan

	// ModeBypass trusts everything with no prompts and no rule consultation.
	ModeBypass
)

// ParseMode maps a settings string to a Mode. An empty string is ModeDefault.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", "default":
		return ModeDefault, nil
	case "auto-accept-edits":
		return ModeAutoAcceptEdits, nil
	case "plan":
		return ModePlan, nil
	case "bypass":
		return ModeBypass, nil
	default:
		return ModeDefault, fmt.Errorf("permission: unknown mode %q", s)
	}
}

// Rule is a single allow/deny rule for one action kind. Match is a command prefix
// (for ActionCommand) or a file path (for edits/reads); "*" matches any call of
// the rule's kind.
type Rule struct {
	Kind  core.ActionKind
	Match string
}

// RuleSet is the merged allow and deny lists. Deny is consulted before allow so a
// deny always wins.
type RuleSet struct {
	Allow []Rule
	Deny  []Rule
}

// ParseRule parses a "<kind>:<match>" settings entry, e.g. "command:git status".
func ParseRule(s string) (Rule, error) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return Rule{}, fmt.Errorf("permission: rule %q must be \"<kind>:<match>\"", s)
	}
	kind, err := parseKind(s[:i])
	if err != nil {
		return Rule{}, err
	}
	match := strings.TrimSpace(s[i+1:])
	if match == "" {
		return Rule{}, fmt.Errorf("permission: rule %q has an empty match", s)
	}
	return Rule{Kind: kind, Match: match}, nil
}

func parseKind(s string) (core.ActionKind, error) {
	switch strings.TrimSpace(s) {
	case "read":
		return core.ActionRead, nil
	case "edit":
		return core.ActionEdit, nil
	case "command":
		return core.ActionCommand, nil
	default:
		return core.ActionUnknown, fmt.Errorf("permission: unknown rule kind %q", s)
	}
}

// kindLabel is the inverse of parseKind, for persisting a remembered rule.
func kindLabel(k core.ActionKind) string {
	switch k {
	case core.ActionRead:
		return "read"
	case core.ActionEdit:
		return "edit"
	case core.ActionCommand:
		return "command"
	default:
		return "unknown"
	}
}

// ParseRules turns the home-then-project merged settings lists into a RuleSet,
// skipping (and logging) any malformed entry rather than failing the whole load.
func ParseRules(allow, deny []string, logger *slog.Logger) RuleSet {
	parse := func(list []string) []Rule {
		out := make([]Rule, 0, len(list))
		for _, s := range list {
			r, err := ParseRule(s)
			if err != nil {
				if logger != nil {
					logger.Warn("skipping malformed permission rule", "rule", s, "err", err)
				}
				continue
			}
			out = append(out, r)
		}
		return out
	}
	return RuleSet{Allow: parse(allow), Deny: parse(deny)}
}

// Matches reports whether the rule covers a call of the given kind. Command rules
// match when the rule's tokens are a prefix of the call's command tokens at token
// boundaries; other kinds match on exact path equality. "*" matches any call of
// the kind.
func (r Rule) Matches(kind core.ActionKind, call core.ToolCall) bool {
	if r.Kind != kind {
		return false
	}
	if r.Match == "*" {
		return true
	}
	switch kind {
	case core.ActionCommand:
		return tokenPrefix(r.Match, stringArg(call.Arguments, "command"))
	default:
		return absPath(r.Match) == absPath(stringArg(call.Arguments, "path"))
	}
}

// hasShellOperators reports whether s carries a shell metacharacter that chains,
// substitutes, or redirects (`&& || | ; $() “ > <` …) — the marks of a compound
// command the gate must not silently generalize across.
func hasShellOperators(s string) bool {
	return strings.ContainsAny(s, "&|;<>()$`\n")
}

// absPath resolves p against the process working directory so a rule and a call
// compare on the same footing regardless of relative/absolute form, closing the
// gap where a cwd-relative path would neither match an absolute rule nor persist
// stably. It returns p unchanged if resolution fails.
func absPath(p string) string {
	if p == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// tokenPrefix reports whether ruleStr's whitespace tokens are a leading prefix of
// cmdStr's tokens. "git status" covers "git status --short" but not "git stash"
// (token mismatch) nor "git statusfoo" (token mismatch).
func tokenPrefix(ruleStr, cmdStr string) bool {
	rt := strings.Fields(ruleStr)
	ct := strings.Fields(cmdStr)
	if len(rt) == 0 || len(ct) < len(rt) {
		return false
	}
	for i := range rt {
		if rt[i] != ct[i] {
			return false
		}
	}
	return true
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// Config constructs an Engine.
type Config struct {
	// Mode is the starting mode.
	Mode Mode

	// Rules is the merged allow/deny rule set.
	Rules RuleSet

	// Save persists a remembered rule (allow reports allow vs deny; rule is the
	// "<kind>:<match>" string). Nil disables persistence. A returned error is
	// logged and swallowed — durability is lost but the action still runs.
	Save func(allow bool, rule string) error

	// Logger receives advisory messages. Nil uses slog.Default.
	Logger *slog.Logger
}

// Engine is the real human-in-the-loop permission gate.
type Engine struct {
	mu     sync.Mutex
	mode   Mode
	rules  RuleSet
	save   func(allow bool, rule string) error
	logger *slog.Logger
}

// New builds an Engine from the given Config.
func New(cfg Config) *Engine {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		mode:   cfg.Mode,
		rules:  cfg.Rules,
		save:   cfg.Save,
		logger: logger,
	}
}

// SetMode changes the governing mode. It is intended to be called between turns.
func (e *Engine) SetMode(m Mode) {
	e.mu.Lock()
	e.mode = m
	e.mu.Unlock()
}

// Decide resolves one call. The resolution order is fixed: bypass → plan-mode
// block → deny rule → allow rule → auto-accept-edits → ask the human.
func (e *Engine) Decide(ctx context.Context, call core.ToolCall, kind core.ActionKind, emit func(core.RunEvent) error) core.PermissionResult {
	e.mu.Lock()
	mode := e.mode
	rules := e.rules
	e.mu.Unlock()

	// 1. Bypass: allow everything soft, never consult rules.
	if mode == ModeBypass {
		return core.PermissionResult{Allow: true}
	}

	// 2. Plan mode: block every mutating action with a reason and let reads proceed
	//    normally. An unknown kind is treated as mutating, erring safe. The block is
	//    not defeatable by rules because this precedes rule checks.
	if mode == ModePlan {
		if kind == core.ActionRead {
			return core.PermissionResult{Allow: true}
		}
		return core.PermissionResult{Allow: false, Reason: "plan mode: changes are disabled"}
	}

	// 3. Deny rule wins over allow.
	if matchesAny(rules.Deny, kind, call) {
		return core.PermissionResult{Allow: false, Reason: "denied by rule"}
	}

	// 4. Allow rule. A specific rule never auto-allows a compound command; only an
	//    explicit "*" blanket does. (Deny above still uses prefix matching, so a
	//    denied program heading a compound command stays refused.)
	if matchesAllow(rules.Allow, kind, call) {
		return core.PermissionResult{Allow: true}
	}

	// 5. Auto-accept-edits: edits run without asking.
	if mode == ModeAutoAcceptEdits && kind == core.ActionEdit {
		return core.PermissionResult{Allow: true}
	}

	// 6. Ask the human.
	return e.ask(ctx, call, kind, emit)
}

func matchesAny(rules []Rule, kind core.ActionKind, call core.ToolCall) bool {
	for _, r := range rules {
		if r.Matches(kind, call) {
			return true
		}
	}
	return false
}

// matchesAllow reports whether an allow rule covers the call. A specific
// (non-"*") command rule never covers a compound command (one carrying shell
// operators): that is not the simple family the human vetted, so it falls through
// to a prompt. An explicit "*" blanket still covers everything of its kind.
func matchesAllow(rules []Rule, kind core.ActionKind, call core.ToolCall) bool {
	compound := kind == core.ActionCommand && hasShellOperators(stringArg(call.Arguments, "command"))
	for _, r := range rules {
		if r.Kind != kind {
			continue
		}
		if r.Match == "*" {
			return true
		}
		if compound {
			continue
		}
		if r.Matches(kind, call) {
			return true
		}
	}
	return false
}

// ask emits a permission request and blocks on the reply or the context. The
// reply channel is buffered (cap 1) so a frontend that answers more than once is
// tolerated: the first decision is consumed here, the rest land in the freed
// buffer slot and are never read.
func (e *Engine) ask(ctx context.Context, call core.ToolCall, kind core.ActionKind, emit func(core.RunEvent) error) core.PermissionResult {
	reply := make(chan core.PermissionDecision, 1)
	req := &core.PermissionRequest{
		ToolCall:       call,
		Reason:         askReason(call, kind),
		RememberedRule: rememberedRuleString(kind, call),
		ReplyPath:      reply,
	}
	if err := emit(core.RunEvent{Type: core.PermissionRequestedEvent, Permission: req}); err != nil {
		return core.PermissionResult{Allow: false, Reason: "permission timed out: " + err.Error()}
	}

	select {
	case d := <-reply:
		effectiveCall := call
		if d.Allow && d.EditedArguments != nil {
			effectiveCall.Arguments = d.EditedArguments
		}
		if d.Remember {
			e.remember(kind, effectiveCall, d)
		}
		if !d.Allow {
			return core.PermissionResult{Allow: false, Reason: "denied by user"}
		}
		return core.PermissionResult{Allow: true, EditedArguments: d.EditedArguments}
	case <-ctx.Done():
		return core.PermissionResult{Allow: false, Reason: "permission timed out: " + ctx.Err().Error()}
	}
}

// askReason states what the action is and why approval is needed, for the prompt.
func askReason(call core.ToolCall, kind core.ActionKind) string {
	subject := call.ToolName
	switch kind {
	case core.ActionCommand:
		subject = "command: " + stringArg(call.Arguments, "command")
	case core.ActionEdit:
		subject = "edit of " + stringArg(call.Arguments, "path")
	}
	return fmt.Sprintf("the agent wants to run %s — approval is required because it is not covered by a rule", subject)
}

// remember turns an approved/denied decision into a durable rule: it is added to
// the in-memory set immediately (so the next matching action benefits) and then
// persisted. A persistence failure is logged and swallowed.
func (e *Engine) remember(kind core.ActionKind, call core.ToolCall, d core.PermissionDecision) {
	match := deriveMatch(kind, call)
	if match == "" {
		return
	}
	rule := Rule{Kind: kind, Match: match}

	e.mu.Lock()
	if d.Allow {
		e.rules.Allow = append(e.rules.Allow, rule)
	} else {
		e.rules.Deny = append(e.rules.Deny, rule)
	}
	save := e.save
	e.mu.Unlock()

	if save == nil {
		return
	}
	ruleStr := kindLabel(kind) + ":" + match
	if err := save(d.Allow, ruleStr); err != nil {
		e.logger.Warn("failed to persist remembered permission rule", "rule", ruleStr, "err", err)
	}
}

// deriveMatch picks a remembered rule's match, generalizing honestly. For a
// command nothing is remembered when it carries shell operators (a compound line
// was approved as-is, not as a family); otherwise it is the program plus a
// following bare-word subcommand only — a flag ("-p") or a path-like argument
// ("fibonacci.rs") is never captured, so "mkdir -p /x" remembers "mkdir", not
// "mkdir -p". For an edit/read it is the absolute file path.
func deriveMatch(kind core.ActionKind, call core.ToolCall) string {
	switch kind {
	case core.ActionCommand:
		cmd := stringArg(call.Arguments, "command")
		if hasShellOperators(cmd) {
			return ""
		}
		toks := strings.Fields(cmd)
		if len(toks) == 0 {
			return ""
		}
		if len(toks) >= 2 && isSubcommandToken(toks[1]) {
			return toks[0] + " " + toks[1]
		}
		return toks[0]
	default:
		return absPath(stringArg(call.Arguments, "path"))
	}
}

// isSubcommandToken reports whether a command's second token reads as a
// subcommand (like "status" in "git status") rather than a flag ("-p") or a
// path/filename ("fibonacci.rs", "./x") — only a bare word narrows the rule.
func isSubcommandToken(t string) bool {
	if t == "" || strings.HasPrefix(t, "-") {
		return false
	}
	return !strings.ContainsAny(t, "/.")
}

// rememberedRuleString is the "<kind>:<match>" rule a Remember decision would
// persist for this call, or "" when the action cannot be safely generalized. It
// is what the prompt previews so the human sees what "remember" will save.
func rememberedRuleString(kind core.ActionKind, call core.ToolCall) string {
	match := deriveMatch(kind, call)
	if match == "" {
		return ""
	}
	return kindLabel(kind) + ":" + match
}

// Engine satisfies the soft-permission seam.
var _ core.Permission = (*Engine)(nil)
