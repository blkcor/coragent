package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/blkcor/coragent/internal/credential"
	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/event"
	provideropenai "github.com/blkcor/coragent/internal/provider/openai"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/settings"
	"github.com/blkcor/coragent/internal/transcript"
)

var version = "m1-dev"

func main() {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, interrupt))
}

func run(args []string, in io.Reader, out, errOut io.Writer, interrupt <-chan os.Signal) int {
	if len(args) > 0 {
		switch args[0] {
		case "sessions":
			return runSessions(args[1:], out, errOut)
		case "resume":
			return runResume(args[1:], in, out, errOut, interrupt)
		case "close":
			return runClose(args[1:], out, errOut)
		case "version", "--version":
			writeLine(out, version)
			return 0
		case "help", "--help", "-h":
			printUsage(out)
			return 0
		}
	}
	fs := flag.NewFlagSet("coragent", flag.ContinueOnError)
	fs.SetOutput(errOut)
	workspace := fs.String("C", ".", "workspace root")
	onePrompt := fs.String("prompt", "", "submit one prompt and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		writeFormatted(errOut, "coragent: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	return startNew(*workspace, *onePrompt, in, out, errOut, interrupt)
}

func startNew(workspacePath, onePrompt string, in io.Reader, out, errOut io.Writer, interrupt <-chan os.Signal) int {
	runtime, err := newRuntime(workspacePath)
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	s, err := runtime.Create(context.Background(), workspacePath)
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	defer shutdown(s)
	writeFormatted(out, "session %s\n", s.ID())
	return interact(s, onePrompt, false, in, out, errOut, interrupt)
}

func runResume(args []string, in io.Reader, out, errOut io.Writer, interrupt <-chan os.Signal) int {
	if len(args) == 0 {
		writeLine(errOut, "usage: coragent resume <session-id> [--prompt text]")
		return 2
	}
	sessionID := args[0]
	fs := flag.NewFlagSet("coragent resume", flag.ContinueOnError)
	fs.SetOutput(errOut)
	onePrompt := fs.String("prompt", "", "submit one prompt and exit")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return 2
	}
	root, err := engine.DefaultStoreRoot()
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	stored, err := engine.InspectStoredSession(root, sessionID)
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	records, err := engine.LoadStoredTranscript(root, sessionID)
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	replay(out, records)
	if stored.Closed {
		writeLine(errOut, engine.ErrSessionClosed)
		return 1
	}
	runtime, err := newRuntime(stored.Workspace)
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	s, err := runtime.Load(context.Background(), sessionID)
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	defer shutdown(s)
	cmd, _ := sessioncommand.NewResume(newCommandID())
	if err := s.Apply(context.Background(), cmd.ForSession(sessionID)); err != nil {
		writeLine(errOut, err)
		return 1
	}
	writeFormatted(out, "session %s resumed\n", s.ID())
	return interact(s, *onePrompt, true, in, out, errOut, interrupt)
}

func runSessions(args []string, out, errOut io.Writer) int {
	if len(args) != 0 {
		writeLine(errOut, "usage: coragent sessions")
		return 2
	}
	root, err := engine.DefaultStoreRoot()
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	sessions, err := engine.ListStoredSessions(root)
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	for _, session := range sessions {
		state := "open"
		if session.Closed {
			state = "closed"
		}
		writeFormatted(out, "%s\t%s\t%s\t%s\n", terminalSafe(session.SessionID), state, session.LastActivity.Format(time.RFC3339), terminalSafe(session.Workspace))
	}
	return 0
}

func runClose(args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		writeLine(errOut, "usage: coragent close <session-id>")
		return 2
	}
	root, err := engine.DefaultStoreRoot()
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	stored, err := engine.InspectStoredSession(root, args[0])
	if err != nil {
		writeLine(errOut, err)
		return 1
	}
	if stored.Closed {
		writeFormatted(out, "session %s already closed\n", args[0])
		return 0
	}
	if err := engine.CloseStoredSession(context.Background(), root, args[0], newCommandID()); err != nil {
		writeLine(errOut, err)
		return 1
	}
	writeFormatted(out, "session %s closed\n", args[0])
	return 0
}

func newRuntime(workspacePath string) (*engine.Engine, error) {
	cfg, err := settings.Load(workspacePath)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	adapter, err := provideropenai.New(provideropenai.Config{
		Endpoint: cfg.Provider.Endpoint, Model: cfg.Provider.Model,
		ContextWindow: cfg.Provider.ContextWindow, MaxOutputTokens: cfg.Provider.MaxOutputTokens,
		Temperature: cfg.Provider.Temperature, Seed: cfg.Provider.Seed, ToolChoice: cfg.Provider.ToolChoice,
		Credential: credential.EnvSource{Name: cfg.Provider.APIKeyEnv},
	})
	if err != nil {
		return nil, err
	}
	root, err := engine.DefaultStoreRoot()
	if err != nil {
		return nil, err
	}
	return engine.New(engine.EngineConfig{
		StoreRoot: root, Provider: adapter, ContextWindow: cfg.Provider.ContextWindow,
		MaxOutputTokens: cfg.Provider.MaxOutputTokens, UserPreferences: cfg.UserPreferences,
	})
}

func interact(s *engine.Session, onePrompt string, resumed bool, in io.Reader, out, errOut io.Writer, interrupt <-chan os.Signal) int {
	if onePrompt != "" {
		if err := runTurn(s, onePrompt, in, out, interrupt); err != nil {
			writeLine(errOut, err)
			return 1
		}
		return 0
	}
	scanner := bufio.NewScanner(in)
	for {
		writeText(out, "> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				writeLine(errOut, err)
				return 1
			}
			return 0
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := runTurn(s, line, in, out, interrupt); err != nil {
			writeLine(errOut, err)
		}
	}
}

func runTurn(s *engine.Session, text string, in io.Reader, out io.Writer, interrupt <-chan os.Signal) error {
	observation, unsubscribe := s.Observe(s.HighWaterMark())
	defer unsubscribe()
	cmd, err := sessioncommand.NewSubmit(newCommandID(), text)
	if err != nil {
		return err
	}
	if err := s.Apply(context.Background(), cmd.ForSession(s.ID())); err != nil {
		return err
	}
	render := eventRenderer{out: out}
	for {
		select {
		case <-interrupt:
			cancel, _ := sessioncommand.NewCancel(newCommandID())
			_ = s.Apply(context.Background(), cancel.ForSession(s.ID()))
		case ev, ok := <-observation.Events:
			if !ok {
				return errors.New("coragent: event observation closed before run termination")
			}
			if ev.Kind == event.KindApprovalRequired {
				var approval event.ApprovalRequiredPayload
				if ev.DecodePayload(&approval) == nil {
					renderApprovalPrompt(out, approval)
					for {
						writeText(out, "\n[a] Approve  [d] Deny\n[a/d] > ")
						cmd, err := readApprovalInput(in, interrupt)
						if err != nil {
							cancel, _ := sessioncommand.NewCancel(newCommandID())
							_ = s.Apply(context.Background(), cancel.ForSession(s.ID()))
							return err
						}
						switch strings.ToLower(cmd) {
						case "a":
							c, cerr := sessioncommand.NewApprove(newCommandID(), approval.RequestID)
							if cerr == nil {
								_ = s.Apply(context.Background(), c.ForSession(s.ID()))
							}
						case "d":
							c, cerr := sessioncommand.NewDeny(newCommandID(), approval.RequestID)
							if cerr == nil {
								_ = s.Apply(context.Background(), c.ForSession(s.ID()))
							}
						default:
							writeLine(out, "Press 'a' to approve or 'd' to deny")
							continue
						}
						break
					}
				}
				continue
			}
			render.event(ev)
			if ev.Kind == event.KindRunCompleted {
				return nil
			}
			if ev.Kind == event.KindRunCancelled {
				return nil
			}
			if ev.Kind == event.KindRunFailed {
				var failed event.RunFailedPayload
				_ = ev.DecodePayload(&failed)
				return fmt.Errorf("run failed: %s", failed.Cause)
			}
		}
	}
}

type eventRenderer struct {
	out      io.Writer
	hadDelta bool
}

func (r *eventRenderer) event(ev event.Event) {
	switch ev.Kind {
	case event.KindAssistantDelta:
		var payload event.AssistantDeltaPayload
		if ev.DecodePayload(&payload) == nil {
			writeText(r.out, terminalSafe(payload.Text))
			r.hadDelta = true
		}
	case event.KindAssistantText:
		if !r.hadDelta {
			var payload event.AssistantTextPayload
			if ev.DecodePayload(&payload) == nil {
				writeText(r.out, terminalSafe(payload.Text))
			}
		}
		r.hadDelta = false
	case event.KindToolStarted:
		var payload event.ToolStartedPayload
		if ev.DecodePayload(&payload) == nil {
			writeFormatted(r.out, "\n[%s %s]\n", terminalSafe(payload.Name), terminalSafe(payload.CallID))
		}
	case event.KindToolFinished:
		var payload event.ToolFinishedPayload
		if ev.DecodePayload(&payload) == nil {
			writeFormatted(r.out, "[%s %s]\n", terminalSafe(payload.Outcome), terminalSafe(payload.CallID))
		}
	case event.KindRetryScheduled:
		var payload event.RetryScheduledPayload
		if ev.DecodePayload(&payload) == nil {
			writeFormatted(r.out, "\n[retry %d in %dms: %s]\n", payload.Attempt, payload.DelayMillis, terminalSafe(payload.Class))
		}
	case event.KindRunCompleted:
		writeLine(r.out)
	case event.KindRunCancelled:
		writeLine(r.out, "\n[cancelled]")
	case event.KindRunFailed:
		var payload event.RunFailedPayload
		_ = ev.DecodePayload(&payload)
		writeFormatted(r.out, "\n[failed: %s]\n", terminalSafe(string(payload.Cause)))
	}
}

func renderApprovalPrompt(out io.Writer, approval event.ApprovalRequiredPayload) {
	writeFormatted(out, "\n--- Approval Required ---\n")
	writeFormatted(out, "Path: %s\n", approval.Path)
	if approval.IsSensitive {
		writeLine(out, "[BLOCKED: credential detected in patch]")
	} else {
		renderDiff(out, approval.Diff)
	}
}

func renderDiff(out io.Writer, diff string) {
	const (
		red    = "\033[31m"
		green  = "\033[32m"
		cyan   = "\033[36m"
		bold   = "\033[1m"
		reset  = "\033[0m"
	)
	for _, line := range strings.Split(diff, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			writeFormatted(out, "%s%s%s\n", bold, terminalSafe(line), reset)
		case strings.HasPrefix(line, "@@"):
			writeFormatted(out, "%s%s%s\n", cyan, terminalSafe(line), reset)
		case strings.HasPrefix(line, "-"):
			writeFormatted(out, "%s%s%s\n", red, terminalSafe(line), reset)
		case strings.HasPrefix(line, "+"):
			writeFormatted(out, "%s%s%s\n", green, terminalSafe(line), reset)
		default:
			writeLine(out, terminalSafe(line))
		}
	}
}

func readApprovalInput(in io.Reader, interrupt <-chan os.Signal) (string, error) {
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(in)
		text, err := reader.ReadString('\n')
		ch <- result{strings.TrimSpace(text), err}
	}()
	select {
	case <-interrupt:
		return "", errors.New("interrupted")
	case r := <-ch:
		return r.text, r.err
	}
}

func replay(out io.Writer, records []transcript.Record) {
	for _, rec := range records {
		switch rec.Kind {
		case transcript.KindUserMessage:
			var payload transcript.UserMessagePayload
			if rec.DecodePayload(&payload) == nil {
				writeFormatted(out, "> %s\n", terminalSafe(payload.Text))
			}
		case transcript.KindAssistantBlock:
			var payload transcript.AssistantBlockPayload
			if rec.DecodePayload(&payload) == nil {
				writeLine(out, terminalSafe(payload.Text))
			}
		case transcript.KindRunOutcome:
			var payload transcript.RunOutcomePayload
			if rec.DecodePayload(&payload) == nil && payload.Outcome != transcript.RunOutcomeCompleted {
				writeFormatted(out, "[%s]\n", terminalSafe(string(payload.Outcome)))
			}
		}
	}
}

func shutdown(s *engine.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}

func newCommandID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("cmd-%d", time.Now().UnixNano())
	}
	return "cmd-" + hex.EncodeToString(raw[:])
}

func printUsage(out io.Writer) {
	writeLine(out, "usage:")
	writeLine(out, "  coragent -C <workspace> [--prompt text]")
	writeLine(out, "  coragent sessions")
	writeLine(out, "  coragent resume <session-id> [--prompt text]")
	writeLine(out, "  coragent close <session-id>")
}

// terminalSafe removes terminal control and directional-override characters
// from every value influenced by a model, repository, durable session, or
// Provider. Newline and tab remain available for ordinary line-oriented text.
func terminalSafe(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r == '\n' || r == '\t':
			out.WriteRune(r)
		case unicode.IsControl(r), r == '\u061c', r == '\u200e', r == '\u200f', r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
			out.WriteRune('\ufffd')
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Terminal output is best-effort. Runtime correctness and durability do not
// depend on a frontend writer remaining available (for example after a broken
// pipe), so every intentionally ignored write error is explicit here.
func writeText(w io.Writer, values ...any) {
	_, _ = fmt.Fprint(w, terminalValues(values)...)
}

func writeLine(w io.Writer, values ...any) {
	_, _ = fmt.Fprintln(w, terminalValues(values)...)
}

func writeFormatted(w io.Writer, format string, values ...any) {
	_, _ = fmt.Fprintf(w, format, terminalValues(values)...)
}

func terminalValues(values []any) []any {
	for index, value := range values {
		switch typed := value.(type) {
		case string:
			values[index] = terminalSafe(typed)
		case error:
			values[index] = terminalSafe(typed.Error())
		}
	}
	return values
}
