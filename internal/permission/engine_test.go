package permission

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

func testFingerprintKey(t *testing.T, seed string) FingerprintKey {
	t.Helper()
	raw := sha256.Sum256([]byte(seed))
	key, err := NewFingerprintKey(raw[:])
	if err != nil {
		t.Fatalf("NewFingerprintKey: %v", err)
	}
	return key
}

// --- mode and rule parsing --------------------------------------------------

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"default":           ModeDefault,
		"auto-accept-edits": ModeAutoAcceptEdits,
		"plan":              ModePlan,
		"bypass":            ModeBypass,
		"":                  ModeDefault, // empty falls back to default
	}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil {
			t.Errorf("ParseMode(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseMode(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseMode("nonsense"); err == nil {
		t.Error("ParseMode must reject an unknown mode")
	}
}

func TestParseRule(t *testing.T) {
	r, err := ParseRule("command:git status")
	if err != nil {
		t.Fatalf("ParseRule: %v", err)
	}
	if r.Kind != core.ActionCommand || r.Match != "git status" {
		t.Errorf("got %+v", r)
	}
	if _, err := ParseRule("garbage-no-colon"); err == nil {
		t.Error("ParseRule must reject a rule with no kind prefix")
	}
	if _, err := ParseRule("nope:x"); err == nil {
		t.Error("ParseRule must reject an unknown kind")
	}
	if _, err := ParseRule("exact-v3:read:hmac-sha256:" + strings.Repeat("0", 64)); err == nil {
		t.Error("ParseRule must reject an unknown exact-call version")
	}
	if _, err := ParseRule("exact-v2:read:sha256:" + strings.Repeat("0", 64)); err == nil {
		t.Error("ParseRule must reject an unkeyed v2 algorithm")
	}
	if legacy, err := ParseRule("exact-v1:read:sha256:" + strings.Repeat("0", 64)); err != nil || legacy.exactScheme != exactRuleLegacySHA256 {
		t.Fatalf("legacy v1 rule should remain parseable for safe migration: rule=%+v err=%v", legacy, err)
	}
}

func TestExactRuleMatchesCanonicalCallIdentity(t *testing.T) {
	key := testFingerprintKey(t, "canonical-match-key")
	call := core.ToolCall{ToolName: "search_content", Arguments: map[string]interface{}{
		"pattern": "needle", "ignore_case": true,
	}}
	digest, err := exactCallFingerprint(key, core.ActionRead, call)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := ParseRule("exact-v2:read:hmac-sha256:" + digest)
	if err != nil {
		t.Fatalf("ParseRule exact: %v", err)
	}
	equivalent := core.ToolCall{ID: "different-call-id", ToolName: "search_content", Arguments: map[string]interface{}{
		"ignore_case": true, "pattern": "needle",
	}}
	if !rule.matches(key, core.ActionRead, equivalent) {
		t.Fatal("canonical key order and call ID must not change an exact match")
	}
	if rule.Matches(core.ActionRead, equivalent) {
		t.Fatal("keyed exact rule must not match through the keyless compatibility method")
	}
	wrongTool := equivalent
	wrongTool.ToolName = "other_search"
	if rule.matches(key, core.ActionRead, wrongTool) {
		t.Fatal("exact rule matched a different tool name")
	}
	wrongArgs := equivalent
	wrongArgs.Arguments = map[string]interface{}{"pattern": "other", "ignore_case": true}
	if rule.matches(key, core.ActionRead, wrongArgs) {
		t.Fatal("exact rule matched different effective arguments")
	}
	if rule.matches(key, core.ActionCommand, equivalent) {
		t.Fatal("exact rule matched a different action kind")
	}
}

func TestLegacyExactV1RulesFailSafeWithoutMatching(t *testing.T) {
	legacy := "exact-v1:read:sha256:" + strings.Repeat("a", 64)
	parsed, err := ParseRule(legacy)
	if err != nil {
		t.Fatalf("ParseRule legacy: %v", err)
	}
	call := core.ToolCall{ToolName: "search_content", Arguments: map[string]interface{}{"pattern": "a"}}
	if parsed.matches(testFingerprintKey(t, "legacy-skip-key"), core.ActionRead, call) {
		t.Fatal("legacy unkeyed digest must never match")
	}
	rules := ParseRules([]string{legacy}, []string{legacy}, nil)
	if len(rules.Allow) != 0 || len(rules.Deny) != 0 {
		t.Fatalf("legacy rules must be skipped during migration: %+v", rules)
	}
}

func TestExactFingerprintRequiresSecretKeyInsteadOfPlainHashGuessing(t *testing.T) {
	call := core.ToolCall{ToolName: "custom_tool", Arguments: map[string]interface{}{"token": "1234"}}
	keyA := testFingerprintKey(t, "offline-oracle-a")
	keyB := testFingerprintKey(t, "offline-oracle-b")
	fingerprintA, err := exactCallFingerprint(keyA, core.ActionUnknown, call)
	if err != nil {
		t.Fatal(err)
	}
	fingerprintB, err := exactCallFingerprint(keyB, core.ActionUnknown, call)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalExactCall(core.ActionUnknown, call)
	if err != nil {
		t.Fatal(err)
	}
	plain := sha256.Sum256(canonical)
	if fingerprintA == fmt.Sprintf("%x", plain[:]) {
		t.Fatal("exact fingerprint regressed to an offline-verifiable plain SHA-256 digest")
	}
	if fingerprintA == fingerprintB {
		t.Fatal("the same settings guess must produce different fingerprints under different secret keys")
	}
}

// --- command-family matching ------------------------------------------------

func TestCommandFamilyMatching(t *testing.T) {
	rule := Rule{Kind: core.ActionCommand, Match: "git status"}
	cmd := func(s string) core.ToolCall {
		return core.ToolCall{ToolName: "run_command", Arguments: map[string]interface{}{"command": s}}
	}
	if !rule.Matches(core.ActionCommand, cmd("git status")) {
		t.Error("exact command must match")
	}
	if !rule.Matches(core.ActionCommand, cmd("git status --short")) {
		t.Error("argument variation must match")
	}
	if rule.Matches(core.ActionCommand, cmd("git stash")) {
		t.Error("unrelated subcommand must not match")
	}
	if rule.Matches(core.ActionCommand, cmd("git statusfoo")) {
		t.Error("token-boundary: statusfoo must not match status")
	}
	if rule.Matches(core.ActionRead, cmd("git status")) {
		t.Error("kind mismatch must not match")
	}
}

func TestFilePathMatching(t *testing.T) {
	rule := Rule{Kind: core.ActionEdit, Match: "/tmp/a.txt"}
	edit := func(p string) core.ToolCall {
		return core.ToolCall{ToolName: "edit_file", Arguments: map[string]interface{}{"path": p}}
	}
	if !rule.Matches(core.ActionEdit, edit("/tmp/a.txt")) {
		t.Error("exact path must match")
	}
	if rule.Matches(core.ActionEdit, edit("/tmp/b.txt")) {
		t.Error("different path must not match")
	}
}

// --- decision helpers -------------------------------------------------------

// allowEmit answers any permission request with the given decision, sent from a
// goroutine so a buffered reply channel never wedges the engine.
func answerEmit(d core.PermissionDecision) func(core.RunEvent) error {
	return func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			rp := ev.Permission.ReplyPath
			go func() { rp <- d }()
		}
		return nil
	}
}

// silentEmit never answers — used for the fail-safe path.
func silentEmit(core.RunEvent) error { return nil }

func cmdCall(s string) core.ToolCall {
	return core.ToolCall{ID: "c1", ToolName: "run_command", Arguments: map[string]interface{}{"command": s}}
}

// --- three outcomes / ask default -------------------------------------------

func TestUncoveredActionAsksAndAllowOnApprove(t *testing.T) {
	e := New(Config{Mode: ModeDefault})
	asked := 0
	emit := func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			asked++
			if ev.Permission.Reason == "" {
				t.Error("prompt must carry a reason")
			}
			rp := ev.Permission.ReplyPath
			go func() { rp <- core.PermissionDecision{Allow: true} }()
		}
		return nil
	}
	res := e.Decide(context.Background(), cmdCall("ls"), core.ActionCommand, emit)
	if !res.Allow {
		t.Errorf("approve must allow")
	}
	if asked != 1 {
		t.Errorf("uncovered action must prompt exactly once, asked %d", asked)
	}
}

func TestDenyByUserStops(t *testing.T) {
	e := New(Config{Mode: ModeDefault})
	res := e.Decide(context.Background(), cmdCall("ls"), core.ActionCommand,
		answerEmit(core.PermissionDecision{Allow: false}))
	if res.Allow {
		t.Error("user deny must not allow")
	}
	if res.Reason == "" {
		t.Error("denial must carry a reason")
	}
}

// --- modes ------------------------------------------------------------------

func TestBypassAllowsWithoutPromptOrRules(t *testing.T) {
	// A deny rule exists but bypass must ignore rules entirely.
	e := New(Config{Mode: ModeBypass, Rules: RuleSet{Deny: []Rule{{Kind: core.ActionCommand, Match: "ls"}}}})
	prompted := false
	emit := func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			prompted = true
		}
		return nil
	}
	res := e.Decide(context.Background(), cmdCall("ls"), core.ActionCommand, emit)
	if !res.Allow {
		t.Error("bypass must allow")
	}
	if prompted {
		t.Error("bypass must not prompt")
	}
}

func TestPlanModeBlocksMutationAllowsRead(t *testing.T) {
	e := New(Config{Mode: ModePlan})
	for _, kind := range []core.ActionKind{core.ActionEdit, core.ActionCommand, core.ActionUnknown} {
		res := e.Decide(context.Background(), core.ToolCall{ID: "c1", ToolName: "x"}, kind, silentEmit)
		if res.Allow {
			t.Errorf("plan mode must block kind %v", kind)
		}
		if !strings.Contains(res.Reason, "plan mode") {
			t.Errorf("plan-mode block must name plan mode, got %q", res.Reason)
		}
	}
	res := e.Decide(context.Background(), core.ToolCall{ID: "c1", ToolName: "read"}, core.ActionRead, silentEmit)
	if !res.Allow {
		t.Error("plan mode must allow reads")
	}
}

func TestPlanModeNotDefeatedByAllowRule(t *testing.T) {
	e := New(Config{
		Mode:  ModePlan,
		Rules: RuleSet{Allow: []Rule{{Kind: core.ActionEdit, Match: "*"}}},
	})
	res := e.Decide(context.Background(), core.ToolCall{ID: "c1", ToolName: "edit"}, core.ActionEdit, silentEmit)
	if res.Allow {
		t.Error("an allow rule must not defeat a plan-mode block")
	}
}

func TestAutoAcceptEditsAllowsEditsAsksCommands(t *testing.T) {
	e := New(Config{Mode: ModeAutoAcceptEdits})
	// Edit runs with no prompt.
	editRes := e.Decide(context.Background(), core.ToolCall{ID: "c1", ToolName: "edit"}, core.ActionEdit, silentEmit)
	if !editRes.Allow {
		t.Error("auto-accept-edits must allow an edit without asking")
	}
	// Command still asks.
	asked := false
	emit := func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			asked = true
			rp := ev.Permission.ReplyPath
			go func() { rp <- core.PermissionDecision{Allow: true} }()
		}
		return nil
	}
	e.Decide(context.Background(), cmdCall("ls"), core.ActionCommand, emit)
	if !asked {
		t.Error("auto-accept-edits must still ask about commands")
	}
}

func TestAutoAcceptEditsDenyRuleStillBlocks(t *testing.T) {
	e := New(Config{
		Mode:  ModeAutoAcceptEdits,
		Rules: RuleSet{Deny: []Rule{{Kind: core.ActionEdit, Match: "*"}}},
	})
	res := e.Decide(context.Background(), core.ToolCall{ID: "c1", ToolName: "edit"}, core.ActionEdit, silentEmit)
	if res.Allow {
		t.Error("a deny rule must block even in auto-accept-edits mode")
	}
}

func TestSetModeSwitchesBetweenTurns(t *testing.T) {
	e := New(Config{Mode: ModeBypass})
	if res := e.Decide(context.Background(), cmdCall("ls"), core.ActionCommand, silentEmit); !res.Allow {
		t.Fatal("bypass should allow")
	}
	e.SetMode(ModePlan)
	res := e.Decide(context.Background(), core.ToolCall{ID: "c1"}, core.ActionCommand, silentEmit)
	if res.Allow {
		t.Error("after switching to plan, a command must be blocked")
	}
}

func TestLiveModeSwitchLinearizesWithoutResolvingOpenPrompt(t *testing.T) {
	e := New(Config{Mode: ModeDefault})
	call := core.ToolCall{ToolName: "run_command", Arguments: map[string]interface{}{"command": "git status"}}
	requests := make(chan core.ObservedPermissionRequest, 1)
	firstResult := make(chan core.RichPermissionResult, 1)
	go func() {
		firstResult <- e.DecideRich(context.Background(), core.RichPermissionInput{
			CallID: "call-1", Revision: 1, EffectiveCall: call, Action: core.ActionCommand,
			Preview: core.ActionPreview{Kind: core.ActionPreviewText},
		}, func(event core.RichEvent) error {
			requests <- event.Payload.(*core.PermissionRequestedPayload).Request
			return nil
		})
	}()

	request := <-requests
	if request.Mode != "default" {
		t.Fatalf("open request mode = %q", request.Mode)
	}
	e.SetMode(ModeBypass)
	second := e.DecideRich(context.Background(), core.RichPermissionInput{
		CallID: "call-2", Revision: 1, EffectiveCall: call, Action: core.ActionCommand,
	}, func(core.RichEvent) error {
		t.Fatal("decision begun after bypass setter unexpectedly prompted")
		return nil
	})
	if second.Action != core.PermissionReplyAllow {
		t.Fatalf("post-setter decision = %+v", second)
	}
	decision := core.ObservedPermissionDecision{
		RequestID: request.RequestID, CallID: request.CallID, Revision: request.Revision,
		Action: core.PermissionReplyDeny,
	}
	if reply := request.Reply(context.Background(), decision); reply.Status != core.PermissionReplyAccepted {
		t.Fatalf("reply to pre-setter prompt = %+v", reply)
	}
	if first := <-firstResult; first.Action != core.PermissionReplyDeny {
		t.Fatalf("open prompt was retroactively approved: %+v", first)
	}
}

func TestModeAccessIsRaceSafeWithConcurrentDecisions(t *testing.T) {
	e := New(Config{Mode: ModeBypass})
	call := core.ToolCall{ToolName: "read_file", Arguments: map[string]interface{}{"path": "README.md"}}
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		for index := 0; index < 1_000; index++ {
			if index%2 == 0 {
				e.SetMode(ModeBypass)
			} else {
				e.SetMode(ModePlan)
			}
		}
	}()
	for worker := 0; worker < 2; worker++ {
		go func() {
			defer wait.Done()
			for index := 0; index < 1_000; index++ {
				_ = e.Mode()
				_ = e.Decide(context.Background(), call, core.ActionRead, silentEmit)
			}
		}()
	}
	wait.Wait()
}

// --- rules ------------------------------------------------------------------

func TestAllowRuleRunsWithoutAsking(t *testing.T) {
	e := New(Config{Mode: ModeDefault, Rules: RuleSet{Allow: []Rule{{Kind: core.ActionCommand, Match: "git status"}}}})
	res := e.Decide(context.Background(), cmdCall("git status --short"), core.ActionCommand, silentEmit)
	if !res.Allow {
		t.Error("allow rule must run without asking")
	}
}

func TestDenyRuleRefusesWithoutAsking(t *testing.T) {
	e := New(Config{Mode: ModeDefault, Rules: RuleSet{Deny: []Rule{{Kind: core.ActionCommand, Match: "rm"}}}})
	res := e.Decide(context.Background(), cmdCall("rm -rf /"), core.ActionCommand, silentEmit)
	if res.Allow {
		t.Error("deny rule must refuse without asking")
	}
}

func TestDenyWinsOverAllow(t *testing.T) {
	e := New(Config{Mode: ModeDefault, Rules: RuleSet{
		Allow: []Rule{{Kind: core.ActionCommand, Match: "git"}},
		Deny:  []Rule{{Kind: core.ActionCommand, Match: "git push"}},
	}})
	res := e.Decide(context.Background(), cmdCall("git push origin main"), core.ActionCommand, silentEmit)
	if res.Allow {
		t.Error("deny must win when both could match")
	}
}

// --- edited arguments -------------------------------------------------------

func TestEditedArgumentsCarriedThrough(t *testing.T) {
	e := New(Config{Mode: ModeDefault})
	edited := map[string]interface{}{"command": "ls -l"}
	res := e.Decide(context.Background(), cmdCall("ls"), core.ActionCommand,
		answerEmit(core.PermissionDecision{Allow: true, EditedArguments: edited}))
	if !res.Allow {
		t.Fatal("approve must allow")
	}
	if res.EditedArguments["command"] != "ls -l" {
		t.Errorf("edited arguments must be carried through, got %v", res.EditedArguments)
	}
}

// --- fail-safe and one-decision ---------------------------------------------

func TestUnansweredFailsSafeOnCancel(t *testing.T) {
	e := New(Config{Mode: ModeDefault})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := e.Decide(ctx, cmdCall("ls"), core.ActionCommand, silentEmit)
	if res.Allow {
		t.Error("an unanswered prompt under a cancelled context must fail safe (deny)")
	}
	if !strings.Contains(strings.ToLower(res.Reason), "timed out") && !strings.Contains(strings.ToLower(res.Reason), "cancel") {
		t.Errorf("denial reason must name the timeout/cancellation, got %q", res.Reason)
	}
}

func TestSecondAnswerIgnored(t *testing.T) {
	e := New(Config{Mode: ModeDefault})
	emit := func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			rp := ev.Permission.ReplyPath
			go func() {
				rp <- core.PermissionDecision{Allow: true}  // honored
				rp <- core.PermissionDecision{Allow: false} // ignored
			}()
		}
		return nil
	}
	res := e.Decide(context.Background(), cmdCall("ls"), core.ActionCommand, emit)
	if !res.Allow {
		t.Error("the first decision (allow) must be honored, the second ignored")
	}
}

// --- remember ---------------------------------------------------------------

func TestRememberAddsRuleImmediatelyAndPersists(t *testing.T) {
	var saved []string
	e := New(Config{
		Mode: ModeDefault,
		Save: func(allow bool, rule string) error {
			if !allow {
				t.Error("remembered approval must persist as an allow rule")
			}
			saved = append(saved, rule)
			return nil
		},
	})
	// First call: approve and remember.
	res := e.Decide(context.Background(), cmdCall("git status"), core.ActionCommand,
		answerEmit(core.PermissionDecision{Allow: true, Remember: true}))
	if !res.Allow {
		t.Fatal("approve must allow")
	}
	if len(saved) != 1 || !strings.Contains(saved[0], "git status") {
		t.Errorf("remembered rule must be persisted, got %v", saved)
	}
	// Second call of the same family: now covered, must not prompt.
	prompted := false
	res2 := e.Decide(context.Background(), cmdCall("git status --short"), core.ActionCommand,
		func(ev core.RunEvent) error {
			if ev.Type == core.PermissionRequestedEvent {
				prompted = true
			}
			return nil
		})
	if !res2.Allow || prompted {
		t.Errorf("remembered rule must silence the next matching action immediately (allow=%v prompted=%v)", res2.Allow, prompted)
	}
}

func TestExactRememberFallbackCoversCallsWithoutFamilyScope(t *testing.T) {
	tests := []struct {
		name string
		kind core.ActionKind
		call core.ToolCall
	}{
		{name: "search without path", kind: core.ActionRead, call: core.ToolCall{ToolName: "search_content", Arguments: map[string]interface{}{"pattern": "TOP-SECRET-SEARCH"}}},
		{name: "find root argument", kind: core.ActionRead, call: core.ToolCall{ToolName: "find_files", Arguments: map[string]interface{}{"pattern": "*TOP-SECRET-FIND*", "root": "."}}},
		{name: "task instruction", kind: core.ActionRead, call: core.ToolCall{ToolName: "task", Arguments: map[string]interface{}{"label": "audit", "instruction": "TOP-SECRET-INSTRUCTION"}}},
		{name: "compound command", kind: core.ActionCommand, call: core.ToolCall{ToolName: "run_command", Arguments: map[string]interface{}{"command": "echo TOP-SECRET-COMMAND && pwd"}}},
		{name: "unknown custom", kind: core.ActionUnknown, call: core.ToolCall{ToolName: "custom_tool", Arguments: map[string]interface{}{"token": "TOP-SECRET-TOKEN"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := testFingerprintKey(t, "persisted-"+test.name)
			var saved string
			e := New(Config{Mode: ModeDefault, FingerprintKey: key, Save: func(_ bool, rule string) error {
				saved = rule
				return nil
			}})
			result := e.Decide(context.Background(), test.call, test.kind, answerEmit(core.PermissionDecision{Allow: true, Remember: true}))
			if !result.Allow {
				t.Fatal("remembered exact call was denied")
			}
			if !strings.HasPrefix(saved, "exact-v2:"+kindLabel(test.kind)+":hmac-sha256:") || strings.Contains(saved, "TOP-SECRET") {
				t.Fatalf("persisted exact rule leaked call content: %q", saved)
			}
			prompted := false
			repeat := e.Decide(context.Background(), test.call, test.kind, func(event core.RunEvent) error {
				if event.Type == core.PermissionRequestedEvent {
					prompted = true
				}
				return nil
			})
			if !repeat.Allow || prompted {
				t.Fatalf("same exact call was not remembered: allow=%v prompted=%v", repeat.Allow, prompted)
			}
			reloaded := New(Config{Mode: ModeDefault, FingerprintKey: key, Rules: ParseRules([]string{saved}, nil, nil)})
			persistedPrompted := false
			persisted := reloaded.Decide(context.Background(), test.call, test.kind, func(event core.RunEvent) error {
				if event.Type == core.PermissionRequestedEvent {
					persistedPrompted = true
					reply := event.Permission.ReplyPath
					go func() { reply <- core.PermissionDecision{Allow: false} }()
				}
				return nil
			})
			if !persisted.Allow || persistedPrompted {
				t.Fatalf("persisted exact rule after reload: result=%+v prompted=%v", persisted, persistedPrompted)
			}
			different := test.call
			different.Arguments = make(map[string]interface{}, len(test.call.Arguments)+1)
			for key, value := range test.call.Arguments {
				different.Arguments[key] = value
			}
			different.Arguments["different"] = true
			differentPrompted := false
			_ = reloaded.Decide(context.Background(), different, test.kind, func(event core.RunEvent) error {
				if event.Type == core.PermissionRequestedEvent {
					differentPrompted = true
					reply := event.Permission.ReplyPath
					go func() { reply <- core.PermissionDecision{Allow: false} }()
				}
				return nil
			})
			if !differentPrompted {
				t.Fatal("different effective arguments matched exact rule")
			}

			wrongKey := New(Config{
				Mode: ModeDefault, FingerprintKey: testFingerprintKey(t, "wrong-"+test.name),
				Rules: ParseRules([]string{saved}, nil, nil),
			})
			wrongKeyPrompted := false
			_ = wrongKey.Decide(context.Background(), test.call, test.kind, func(event core.RunEvent) error {
				if event.Type == core.PermissionRequestedEvent {
					wrongKeyPrompted = true
					go func() { event.Permission.ReplyPath <- core.PermissionDecision{Allow: false} }()
				}
				return nil
			})
			if !wrongKeyPrompted {
				t.Fatal("settings fingerprint matched without the originating secret key")
			}
		})
	}
}

func TestEphemeralExactRememberWorksInSessionButIsNotPersisted(t *testing.T) {
	call := core.ToolCall{ToolName: "custom_tool", Arguments: map[string]interface{}{"token": "session-only"}}
	var saved []string
	e := New(Config{Mode: ModeDefault, Save: func(_ bool, rule string) error {
		saved = append(saved, rule)
		return nil
	}})
	result := e.Decide(context.Background(), call, core.ActionUnknown, answerEmit(core.PermissionDecision{Allow: true, Remember: true}))
	if !result.Allow {
		t.Fatal("ephemeral exact remember approval was denied")
	}
	prompted := false
	repeat := e.Decide(context.Background(), call, core.ActionUnknown, func(event core.RunEvent) error {
		prompted = event.Type == core.PermissionRequestedEvent
		return nil
	})
	if !repeat.Allow || prompted {
		t.Fatalf("ephemeral exact remember did not apply immediately: result=%+v prompted=%v", repeat, prompted)
	}
	if len(saved) != 0 {
		t.Fatalf("ephemeral exact fingerprint must not be persisted: %v", saved)
	}
}

func TestRichExactScopeIsSafeAndRememberable(t *testing.T) {
	call := core.ToolCall{ToolName: "search_content", Arguments: map[string]interface{}{"pattern": "TOP-SECRET-RICH"}}
	var saved string
	e := New(Config{
		Mode: ModeDefault, FingerprintKey: testFingerprintKey(t, "rich-exact-key"),
		Save: func(_ bool, rule string) error { saved = rule; return nil },
	})
	result := e.DecideRich(context.Background(), core.RichPermissionInput{
		CallID: "call-1", Revision: 1, EffectiveCall: call, Action: core.ActionRead,
		Preview: core.ActionPreview{Kind: core.ActionPreviewMetadata},
	}, func(event core.RichEvent) error {
		request := event.Payload.(*core.PermissionRequestedPayload).Request
		if !request.Capabilities.Remember || request.RememberedScope == nil || request.RememberedScope.ScopeKind != core.RememberedRuleScopeExact {
			t.Fatalf("exact remember capability = %+v", request)
		}
		if request.RememberedScope.ToolName != "search_content" || strings.Contains(request.RememberedScope.Match, "TOP-SECRET") || strings.Contains(request.RememberedScope.Display, "sha256") {
			t.Fatalf("unsafe exact scope = %+v", request.RememberedScope)
		}
		decision := core.ObservedPermissionDecision{
			RequestID: request.RequestID, CallID: request.CallID, Revision: request.Revision,
			Action: core.PermissionReplyAllow, Remember: true,
		}
		if reply := request.Reply(context.Background(), decision); reply.Status != core.PermissionReplyAccepted {
			t.Fatalf("reply = %+v", reply)
		}
		return nil
	})
	if result.Action != core.PermissionReplyAllow || !strings.HasPrefix(saved, "exact-v2:read:hmac-sha256:") || strings.Contains(saved, "TOP-SECRET") {
		t.Fatalf("result=%+v saved=%q", result, saved)
	}
}

func TestExactDenyStillWinsOverExactAllow(t *testing.T) {
	call := core.ToolCall{ToolName: "task", Arguments: map[string]interface{}{"label": "x", "instruction": "same"}}
	key := testFingerprintKey(t, "exact-deny-key")
	rule, _, ok := rememberedRule(key, core.ActionRead, call)
	if !ok || rule.ExactDigest == "" {
		t.Fatalf("exact rule = %+v ok=%v", rule, ok)
	}
	e := New(Config{Mode: ModeDefault, FingerprintKey: key, Rules: RuleSet{Allow: []Rule{rule}, Deny: []Rule{rule}}})
	if result := e.Decide(context.Background(), call, core.ActionRead, silentEmit); result.Allow || result.Reason != "denied by rule" {
		t.Fatalf("deny did not win: %+v", result)
	}
}

func TestRememberedDenialAddsDenyRuleImmediatelyAndPersists(t *testing.T) {
	var saved []string
	var savedAllow []bool
	e := New(Config{
		Mode: ModeDefault,
		Save: func(allow bool, rule string) error {
			savedAllow = append(savedAllow, allow)
			saved = append(saved, rule)
			return nil
		},
	})
	res := e.Decide(context.Background(), cmdCall("rm -rf /tmp/demo"), core.ActionCommand,
		answerEmit(core.PermissionDecision{Allow: false, Remember: true}))
	if res.Allow {
		t.Fatal("denied action must not be allowed")
	}
	if len(saved) != 1 || savedAllow[0] || saved[0] != "command:rm" {
		t.Fatalf("remembered denial must persist as a deny rule, saved=%v allow=%v", saved, savedAllow)
	}

	prompted := false
	res2 := e.Decide(context.Background(), cmdCall("rm -rf /tmp/other"), core.ActionCommand,
		func(ev core.RunEvent) error {
			if ev.Type == core.PermissionRequestedEvent {
				prompted = true
			}
			return nil
		})
	if res2.Allow || prompted {
		t.Errorf("remembered deny rule must refuse the next matching action without prompting (allow=%v prompted=%v)", res2.Allow, prompted)
	}
}

func TestRememberedApprovalUsesEditedArguments(t *testing.T) {
	var saved []string
	e := New(Config{
		Mode: ModeDefault,
		Save: func(_ bool, rule string) error {
			saved = append(saved, rule)
			return nil
		},
	})
	res := e.Decide(context.Background(), cmdCall("git stash"),
		core.ActionCommand,
		answerEmit(core.PermissionDecision{
			Allow:           true,
			Remember:        true,
			EditedArguments: map[string]interface{}{"command": "git status --short"},
		}))
	if !res.Allow {
		t.Fatal("approved action must be allowed")
	}
	if len(saved) != 1 || saved[0] != "command:git status" {
		t.Fatalf("remembered approval must use edited arguments, saved=%v", saved)
	}

	prompted := false
	res2 := e.Decide(context.Background(), cmdCall("git status --porcelain"),
		core.ActionCommand,
		func(ev core.RunEvent) error {
			if ev.Type == core.PermissionRequestedEvent {
				prompted = true
			}
			return nil
		})
	if !res2.Allow || prompted {
		t.Errorf("edited remembered rule must cover the edited command family (allow=%v prompted=%v)", res2.Allow, prompted)
	}
}

func TestRememberSaveFailureDoesNotBlock(t *testing.T) {
	e := New(Config{
		Mode: ModeDefault,
		Save: func(bool, string) error { return errors.New("disk full") },
	})
	res := e.Decide(context.Background(), cmdCall("git status"), core.ActionCommand,
		answerEmit(core.PermissionDecision{Allow: true, Remember: true}))
	if !res.Allow {
		t.Error("a failed save must not block the approved action")
	}
}

// --- remembered-rule preview on the prompt ----------------------------------

func TestPermissionRequestCarriesRememberedRule(t *testing.T) {
	// The prompt must advertise the exact rule an "always"/"never" would persist,
	// so the human sees what they are about to save.
	e := New(Config{Mode: ModeDefault})
	var previewed string
	emit := func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			previewed = ev.Permission.RememberedRule
			rp := ev.Permission.ReplyPath
			go func() { rp <- core.PermissionDecision{Allow: true} }()
		}
		return nil
	}
	e.Decide(context.Background(), cmdCall("mkdir -p /home/user"), core.ActionCommand, emit)
	if previewed != "command:mkdir" {
		t.Errorf("request must preview the rule a remember would save, got %q", previewed)
	}
}

func TestPermissionRequestCarriesExactRuleForCompound(t *testing.T) {
	// A compound command has no honest family generalization, so the preview uses
	// a secret-free exact-call fingerprint instead.
	e := New(Config{Mode: ModeDefault})
	previewed := "unset"
	emit := func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			previewed = ev.Permission.RememberedRule
			rp := ev.Permission.ReplyPath
			go func() { rp <- core.PermissionDecision{Allow: true} }()
		}
		return nil
	}
	e.Decide(context.Background(), cmdCall("pwd && ls"), core.ActionCommand, emit)
	if !strings.HasPrefix(previewed, "exact-v2:command:hmac-sha256:") || strings.Contains(previewed, "pwd") || strings.Contains(previewed, "ls") {
		t.Errorf("compound command exact rule = %q", previewed)
	}
}

// Engine satisfies the core.Permission seam.
var _ core.Permission = (*Engine)(nil)

// --- compound-command safety ------------------------------------------------

func TestCompoundCommandNotAutoAllowedBySpecificRule(t *testing.T) {
	// A specific allow for a simple program must not silently cover a compound
	// command that merely starts with it — chaining past the approved prefix
	// (rustc … && rm -rf /) must fall through to a prompt.
	e := New(Config{Mode: ModeDefault, Rules: RuleSet{
		Allow: []Rule{{Kind: core.ActionCommand, Match: "rustc"}},
	}})
	asked := false
	emit := func(ev core.RunEvent) error {
		if ev.Type == core.PermissionRequestedEvent {
			asked = true
			rp := ev.Permission.ReplyPath
			go func() { rp <- core.PermissionDecision{Allow: false} }()
		}
		return nil
	}
	res := e.Decide(context.Background(), cmdCall("rustc fibonacci.rs && rm -rf /"), core.ActionCommand, emit)
	if res.Allow {
		t.Error("a specific allow rule must not auto-allow a chained (compound) command")
	}
	if !asked {
		t.Error("a compound command not otherwise covered must ask the human")
	}
}

func TestBlanketAllowStillCoversCompoundCommand(t *testing.T) {
	// An explicit "*" is a deliberate blanket and still covers compound commands.
	e := New(Config{Mode: ModeDefault, Rules: RuleSet{
		Allow: []Rule{{Kind: core.ActionCommand, Match: "*"}},
	}})
	res := e.Decide(context.Background(), cmdCall("echo hi && echo bye"), core.ActionCommand, silentEmit)
	if !res.Allow {
		t.Error(`a "*" blanket allow must still cover a compound command`)
	}
}

func TestDenyStillCatchesCompoundLeadingCommand(t *testing.T) {
	// Deny must remain sticky: a denied program at the head of a compound command
	// is still refused.
	e := New(Config{Mode: ModeDefault, Rules: RuleSet{
		Deny: []Rule{{Kind: core.ActionCommand, Match: "rm"}},
	}})
	res := e.Decide(context.Background(), cmdCall("rm -rf / && echo done"), core.ActionCommand, silentEmit)
	if res.Allow {
		t.Error("a deny rule must still catch a denied program heading a compound command")
	}
}

// --- honest remembered-rule derivation --------------------------------------

func TestRememberDerivesHonestRule(t *testing.T) {
	cases := []struct {
		name, cmd, want string
		exact           bool
	}{
		{name: "flag not captured as subcommand", cmd: "mkdir -p /home/user", want: "command:mkdir"},
		{name: "path-like arg not captured", cmd: "rustc fibonacci.rs -o out", want: "command:rustc"},
		{name: "bareword subcommand kept", cmd: "git status --short", want: "command:git status"},
		{name: "second bareword kept", cmd: "which cargo rustc", want: "command:which cargo"},
		{name: "single program", cmd: "ls", want: "command:ls"},
		{name: "chained gets exact fallback", cmd: "pwd && ls", exact: true},
		{name: "piped gets exact fallback", cmd: "cat a | grep b", exact: true},
		{name: "redirect gets exact fallback", cmd: "echo hi > f", exact: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var saved []string
			e := New(Config{
				Mode: ModeDefault, FingerprintKey: testFingerprintKey(t, "honest-rule-"+tc.name),
				Save: func(_ bool, r string) error { saved = append(saved, r); return nil },
			})
			res := e.Decide(context.Background(), cmdCall(tc.cmd), core.ActionCommand,
				answerEmit(core.PermissionDecision{Allow: true, Remember: true}))
			if !res.Allow {
				t.Fatalf("approve must allow %q", tc.cmd)
			}
			if tc.exact {
				if len(saved) != 1 || !strings.HasPrefix(saved[0], "exact-v2:command:hmac-sha256:") || strings.Contains(saved[0], tc.cmd) {
					t.Errorf("%q exact rule = %v", tc.cmd, saved)
				}
				return
			}
			if len(saved) != 1 || saved[0] != tc.want {
				t.Errorf("%q → saved %v, want [%s]", tc.cmd, saved, tc.want)
			}
		})
	}
}

// --- path form independence -------------------------------------------------

func TestEditRuleMatchesRegardlessOfPathForm(t *testing.T) {
	// A rule stored as an absolute path matches a call giving the equivalent
	// relative path, so a cwd-relative form neither slips nor duplicates the gate.
	abs, err := filepath.Abs("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	rule := Rule{Kind: core.ActionEdit, Match: abs}
	call := core.ToolCall{ToolName: "edit_file", Arguments: map[string]interface{}{"path": "notes.txt"}}
	if !rule.Matches(core.ActionEdit, call) {
		t.Error("an absolute rule must match the equivalent relative call path")
	}
}

func TestRememberedEditRuleIsAbsolute(t *testing.T) {
	abs, err := filepath.Abs("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	var saved []string
	e := New(Config{Mode: ModeDefault, Save: func(_ bool, r string) error { saved = append(saved, r); return nil }})
	e.Decide(context.Background(),
		core.ToolCall{ID: "c1", ToolName: "edit_file", Arguments: map[string]interface{}{"path": "notes.txt"}},
		core.ActionEdit,
		answerEmit(core.PermissionDecision{Allow: true, Remember: true}))
	if len(saved) != 1 || saved[0] != "edit:"+abs {
		t.Errorf("remembered edit rule must be absolute, got %v (want edit:%s)", saved, abs)
	}
}
