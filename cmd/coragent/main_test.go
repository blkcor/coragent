package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/event"
)

func TestCLI_CreateListResumeClose(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CORAGENT_CLI_TEST_KEY", "runtime-test-value")
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n\nfunc Alpha() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer runtime-test-value" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if strings.Contains(fmt.Sprint(body), "runtime-test-value") {
			t.Error("runtime credential entered model request")
		}
		answer := "Alpha is declared at main.go:3-3."
		if requests.Add(1) == 2 {
			answer = "The follow-up still resolves to main.go:3-3."
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answer}, "finish_reason": "stop"}}})
		writeFormatted(w, "data: %s\n\n", chunk)
		writeLine(w, "data: [DONE]")
	}))
	defer server.Close()
	settingsDir := filepath.Join(home, ".coragent")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settingsBody, _ := json.Marshal(map[string]any{"provider": map[string]any{
		"endpoint": server.URL, "model": "immutable-test-snapshot", "context_window": 32000,
		"max_output_tokens": 8000, "api_key_env": "CORAGENT_CLI_TEST_KEY",
	}})
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), settingsBody, 0o600); err != nil {
		t.Fatal(err)
	}
	interrupt := make(chan os.Signal)
	var out, errOut bytes.Buffer
	if code := run([]string{"-C", workspace, "--prompt", "Where is Alpha?"}, strings.NewReader(""), &out, &errOut, interrupt); code != 0 {
		t.Fatalf("create code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "main.go:3-3") {
		t.Fatalf("create output = %s", out.String())
	}
	match := regexp.MustCompile(`session (sess-[0-9a-f]+)`).FindStringSubmatch(out.String())
	if len(match) != 2 {
		t.Fatalf("session ID absent: %s", out.String())
	}
	sessionID := match[1]

	out.Reset()
	errOut.Reset()
	if code := run([]string{"sessions"}, strings.NewReader(""), &out, &errOut, interrupt); code != 0 || !strings.Contains(out.String(), sessionID+"\topen") {
		t.Fatalf("sessions code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"resume", sessionID, "--prompt", "And the follow-up?"}, strings.NewReader(""), &out, &errOut, interrupt); code != 0 {
		t.Fatalf("resume code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "> Where is Alpha?") || !strings.Contains(out.String(), "follow-up still resolves") {
		t.Fatalf("resume output = %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"close", sessionID}, strings.NewReader(""), &out, &errOut, interrupt); code != 0 || !strings.Contains(out.String(), "closed") {
		t.Fatalf("close code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"resume", sessionID}, strings.NewReader(""), &out, &errOut, interrupt); code == 0 || !strings.Contains(errOut.String(), "session is closed") || !strings.Contains(out.String(), "> Where is Alpha?") {
		t.Fatalf("closed resume code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if requests.Load() != 2 {
		t.Fatalf("provider requests = %d", requests.Load())
	}
}

func TestCLI_HelpIsLineOriented(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errOut, make(chan os.Signal)); code != 0 {
		t.Fatalf("help code = %d", code)
	}
	if strings.Contains(strings.ToLower(out.String()), "tui") || !strings.Contains(out.String(), "coragent sessions") {
		t.Fatalf("help = %s", out.String())
	}
}

// This covers the version output and exit code contract independently of the
// installed-runtime smoke check performed by the official benchmark driver.
func TestCLI_VersionOutputAndExitCodes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, strings.NewReader(""), &out, &errOut, make(chan os.Signal)); code != 0 || strings.TrimSpace(out.String()) != version || errOut.Len() != 0 {
		t.Fatalf("version code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"unexpected"}, strings.NewReader(""), &out, &errOut, make(chan os.Signal)); code != 2 || !strings.Contains(errOut.String(), "unexpected argument") {
		t.Fatalf("invalid argument code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestTerminalRendererNeutralizesControlAndBidiSequences(t *testing.T) {
	malicious := "safe\x1b]0;owned\a\x1b[31m red \u202ereversed"
	var out bytes.Buffer
	renderer := eventRenderer{out: &out}
	ev, err := event.New("sess-safe", "run-safe", 1, time.Now(), event.KindAssistantText, event.AssistantTextPayload{Text: malicious})
	if err != nil {
		t.Fatal(err)
	}
	renderer.event(ev)
	got := out.String()
	if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\a') || strings.ContainsRune(got, '\u202e') {
		t.Fatalf("terminal control sequence survived rendering: %q", got)
	}
	if !strings.Contains(got, "safe") || !strings.Contains(got, "red") || !strings.ContainsRune(got, '\ufffd') {
		t.Fatalf("sanitized terminal text lost ordinary content: %q", got)
	}
}

func TestCLI_ActualProcessExitResumeAndFollowUpReads(t *testing.T) {
	home := t.TempDir()
	workspace, err := filepath.Abs("../../testdata/benchmark-repo")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer subprocess-runtime-value" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		encoded := fmt.Sprint(body)
		if strings.Contains(encoded, "subprocess-runtime-value") {
			t.Error("runtime credential entered model request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch request := requests.Add(1); request {
		case 1:
			assertContainsAll(t, encoded, "Trace a job", "list")
			writeToolCall(w, "call-list", "list", map[string]any{"path": ".", "recursive": true})
		case 2:
			assertContainsAll(t, encoded, "call-list", "cmd/mercury/main.go")
			writeToolCall(w, "call-search", "search", map[string]any{
				"path": ".", "pattern": "runSubmit|ValidateSubmit|FindByRequestID|NewID|Save",
			})
		case 3:
			assertContainsAll(t, encoded, "call-search", "internal/jobs/service.go")
			writeToolCall(w, "call-read-service", "read", map[string]any{
				"path": "internal/jobs/service.go", "start_line": 26, "end_line": 39,
			})
		case 4:
			assertContainsAll(t, encoded, "call-read-service", "26: func (s *Service) Submit")
			writeAssistantText(w, "runSubmit reaches validation and Service.Submit; duplicate request IDs are rejected before NewID and Save (cmd/mercury/main.go:48-65; internal/jobs/model.go:22-30; internal/jobs/service.go:26-39; internal/jobs/storage.go:44-65).")
		case 5:
			assertContainsAll(t, encoded, "runSubmit reaches validation", "Inspect storage conversion details")
			writeToolCall(w, "call-read-storage", "read", map[string]any{
				"path": "internal/jobs/storage.go", "start_line": 44, "end_line": 65,
			})
		case 6:
			assertContainsAll(t, encoded, "call-read-storage", "internal/jobs/storage.go")
			writeAssistantText(w, "The follow-up read confirms the storage conversion path at internal/jobs/storage.go:44-65.")
		default:
			t.Errorf("unexpected provider request %d", request)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	writeCLISettings(t, home, server.URL)

	firstPrompt := "Trace a job from the CLI submit command through validation, service logic, and storage.\n"
	stdout, stderr, runErr := runCLIProcess(t, home, firstPrompt, "-C", workspace)
	if runErr != nil {
		t.Fatalf("initial process: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
	}
	if !strings.Contains(stdout, "internal/jobs/service.go:26-39") {
		t.Fatalf("initial process output = %s", stdout)
	}
	match := regexp.MustCompile(`session (sess-[0-9a-f]+)`).FindStringSubmatch(stdout)
	if len(match) != 2 {
		t.Fatalf("session ID absent: %s", stdout)
	}
	sessionID := match[1]

	stdout, stderr, runErr = runCLIProcess(t, home, "", "sessions")
	if runErr != nil || !strings.Contains(stdout, sessionID+"\topen") {
		t.Fatalf("list process: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
	}

	stdout, stderr, runErr = runCLIProcess(t, home, "Inspect storage conversion details.\n", "resume", sessionID)
	if runErr != nil {
		t.Fatalf("resume process: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
	}
	assertContainsAll(t, stdout, "> Trace a job", "runSubmit reaches validation", "follow-up read confirms")

	stdout, stderr, runErr = runCLIProcess(t, home, "", "close", sessionID)
	if runErr != nil || !strings.Contains(stdout, "closed") {
		t.Fatalf("close process: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
	}
	stdout, stderr, runErr = runCLIProcess(t, home, "new turn\n", "resume", sessionID)
	if runErr == nil || !strings.Contains(stderr, "session is closed") || !strings.Contains(stdout, "> Trace a job") {
		t.Fatalf("closed resume: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
	}
	if requests.Load() != 6 {
		t.Fatalf("provider requests = %d, want 6", requests.Load())
	}
}

func TestCLI_OperatingSystemInterruptCancelsAndReturnsToIdle(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	providerCancelled := make(chan struct{})
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer subprocess-runtime-value" {
			t.Errorf("Authorization = %q", got)
		}
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("test response writer cannot flush")
				return
			}
			_, _ = fmt.Fprint(w, ": waiting\n\n")
			flusher.Flush()
			close(started)
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			defer close(providerCancelled)
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if _, err := fmt.Fprint(w, ": waiting\n\n"); err != nil {
						return
					}
					flusher.Flush()
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeAssistantText(w, "The session accepted a follow-up after cancellation at main.go:1-1.")
	}))
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
	})
	writeCLISettings(t, home, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd, stdout, stderr := newCLIProcess(ctx, home, "Keep investigating until interrupted.\n", "-C", workspace)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("Provider request did not start: %v", ctx.Err())
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("signal CLI: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("interrupted CLI exit: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	select {
	case <-providerCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Provider request remained active after CLI exit")
	}
	if !strings.Contains(stdout.String(), "[cancelled]") {
		t.Fatalf("cancel outcome not visible: %s", stdout.String())
	}
	match := regexp.MustCompile(`session (sess-[0-9a-f]+)`).FindStringSubmatch(stdout.String())
	if len(match) != 2 {
		t.Fatalf("session ID absent: %s", stdout.String())
	}

	resumedOut, resumedErr, runErr := runCLIProcess(t, home, "Continue now.\n", "resume", match[1])
	if runErr != nil {
		t.Fatalf("follow-up process: %v\nstdout=%s\nstderr=%s", runErr, resumedOut, resumedErr)
	}
	assertContainsAll(t, resumedOut, "[cancelled]", "accepted a follow-up after cancellation")
	if requests.Load() != 2 {
		t.Fatalf("provider requests = %d, want 2", requests.Load())
	}
}

// TestCLI_CtrlDWhileIdleExitsWithoutClosingSession proves an EOF (Ctrl-D) at
// the idle prompt exits the CLI cleanly and leaves the created session open.
func TestCLI_CtrlDWhileIdleExitsWithoutClosingSession(t *testing.T) {
	home := t.TempDir()
	workspace, err := filepath.Abs("../../testdata/benchmark-repo")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no Provider request is expected while exiting idle")
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	writeCLISettings(t, home, server.URL)

	// Empty stdin is an immediate EOF at the idle prompt (Ctrl-D).
	stdout, stderr, runErr := runCLIProcess(t, home, "", "-C", workspace)
	if runErr != nil {
		t.Fatalf("idle exit: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
	}
	match := regexp.MustCompile(`session (sess-[0-9a-f]+)`).FindStringSubmatch(stdout)
	if len(match) != 2 {
		t.Fatalf("session ID absent from idle output: %s", stdout)
	}
	sessionID := match[1]

	// The idle exit must not close the session; it stays open for resume.
	stdout, stderr, runErr = runCLIProcess(t, home, "", "sessions")
	if runErr != nil || !strings.Contains(stdout, sessionID+"\topen") {
		t.Fatalf("sessions after idle exit: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
	}
}

// newApprovalScenario builds a workspace with one patchable file and a
// scripted Provider whose first turn emits a patch tool call and whose later
// turns answer with plain text.
func newApprovalScenario(t *testing.T, fileContent, replacement string) (home, workspace string) {
	t.Helper()
	home = t.TempDir()
	workspace = t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte(fileContent), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			writeToolCall(w, "call-patch", "patch", map[string]any{
				"path": "note.txt", "target": "L2", "replacement": replacement,
			})
			return
		}
		writeAssistantText(w, "Patch turn resolved.")
	}))
	t.Cleanup(server.Close)
	writeCLISettings(t, home, server.URL)
	t.Setenv("HOME", home)
	t.Setenv("CORAGENT_CLI_SUBPROCESS_KEY", "subprocess-runtime-value")
	return home, workspace
}

func runApprovalPrompt(t *testing.T, home, workspace, input string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run([]string{"-C", workspace, "--prompt", "patch line 2"}, strings.NewReader(input), &out, &errOut, make(chan os.Signal)); code != 0 {
		t.Fatalf("approval run code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	t.Logf("CLI session transcript (stdin %q):\n%s", input, out.String())
	return out.String()
}

// TestCLIApprovalApprovePath proves typing "a" at the approval prompt sends an
// approve SessionCommand and the prepared patch is executed.
func TestCLIApprovalApprovePath(t *testing.T) {
	home, workspace := newApprovalScenario(t, "alpha\nbeta\ngamma\n", "beta patched")
	out := runApprovalPrompt(t, home, workspace, "a\n")
	assertContainsAll(t, out,
		"--- Approval Required ---", "Path: note.txt",
		"--- note.txt", "+++ note.txt", "@@ -2,1 +2,1 @@",
		"-beta", "+beta patched",
		"[a] Approve  [d] Deny", "[a/d] > ",
		"[success call-patch]", "Patch turn resolved.")
	content, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "alpha\nbeta patched\ngamma\n" {
		t.Fatalf("file content = %q", content)
	}
}

// TestCLIApprovalDenyPath proves typing "d" sends a deny SessionCommand, the
// tool result is blocked, and the file stays untouched.
func TestCLIApprovalDenyPath(t *testing.T) {
	home, workspace := newApprovalScenario(t, "alpha\nbeta\ngamma\n", "beta patched")
	out := runApprovalPrompt(t, home, workspace, "d\n")
	assertContainsAll(t, out,
		"--- Approval Required ---", "[a] Approve  [d] Deny",
		"[blocked call-patch]", "Patch turn resolved.")
	content, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "alpha\nbeta\ngamma\n" {
		t.Fatalf("file modified despite denial: %q", content)
	}
}

// TestCLIApprovalInvalidInputRePrompts proves unrecognized input re-renders
// the option line without sending a command, and that the approve letter is
// case-insensitive.
func TestCLIApprovalInvalidInputRePrompts(t *testing.T) {
	home, workspace := newApprovalScenario(t, "alpha\nbeta\ngamma\n", "beta patched")
	out := runApprovalPrompt(t, home, workspace, "x\nA\n")
	assertContainsAll(t, out,
		"Press 'a' to approve or 'd' to deny", "[success call-patch]")
	if got := strings.Count(out, "[a/d] > "); got < 2 {
		t.Fatalf("option line rendered %d times, want at least 2: %s", got, out)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "alpha\nbeta patched\ngamma\n" {
		t.Fatalf("uppercase approve did not execute patch: %q", content)
	}
}

// TestCLIApprovalSensitiveDiffBlocked proves a patch whose source file carries
// detected credential material renders the blocked message instead of the
// diff, never leaks the credential, and still honors the deny option.
func TestCLIApprovalSensitiveDiffBlocked(t *testing.T) {
	const credential = "AKIA0123456789ABCDEF"
	home, workspace := newApprovalScenario(t, "alpha\nbeta\n"+credential+"\n", "beta patched")
	out := runApprovalPrompt(t, home, workspace, "d\n")
	assertContainsAll(t, out,
		"[BLOCKED: credential detected in patch]", "[a] Approve  [d] Deny",
		"[blocked call-patch]")
	if strings.Contains(out, credential) {
		t.Fatalf("credential leaked into CLI output: %s", out)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "alpha\nbeta\n"+credential+"\n" {
		t.Fatalf("sensitive patch applied despite denial: %q", content)
	}
}

// lockedBuffer serializes writes from the CLI subprocess with reads from the
// test goroutine waiting on rendered output.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestCLIApprovalInterruptCancelsRun proves Ctrl+C while the approval prompt
// waits cancels the run: no patch is applied, the outcome is persisted, and a
// resumed session replays the cancellation and accepts follow-up turns.
func TestCLIApprovalInterruptCancelsRun(t *testing.T) {
	home, workspace := newApprovalScenario(t, "alpha\nbeta\ngamma\n", "beta patched")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0],
		"-test.run=^TestCLIProcessHelper$", "--", "-C", workspace, "--prompt", "patch line 2")
	cmd.Env = []string{
		"HOME=" + home,
		"CORAGENT_CLI_PROCESS_HELPER=1",
		"CORAGENT_CLI_SUBPROCESS_KEY=subprocess-runtime-value",
		"NO_PROXY=127.0.0.1,localhost,[::1]",
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := &lockedBuffer{}
	stderr := &lockedBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(stdout.String(), "[a/d] > ") {
		if time.Now().After(deadline) {
			t.Fatalf("approval prompt never rendered: stdout=%s stderr=%s", stdout.String(), stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("signal CLI: %v", err)
	}
	_ = cmd.Wait()
	_ = stdin.Close()
	t.Logf("CLI session transcript (Ctrl+C at approval prompt):\n%s", stdout.String())

	assertContainsAll(t, stdout.String(), "--- Approval Required ---", "[a] Approve  [d] Deny")
	if !strings.Contains(stderr.String(), "interrupted") {
		t.Fatalf("interrupt error absent from stderr: %s", stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "alpha\nbeta\ngamma\n" {
		t.Fatalf("file modified despite cancellation: %q", content)
	}
	match := regexp.MustCompile(`session (sess-[0-9a-f]+)`).FindStringSubmatch(stdout.String())
	if len(match) != 2 {
		t.Fatalf("session ID absent: %s", stdout.String())
	}

	resumedOut, resumedErr, runErr := runCLIProcess(t, home, "Continue now.\n", "resume", match[1])
	if runErr != nil {
		t.Fatalf("resume process: %v\nstdout=%s\nstderr=%s", runErr, resumedOut, resumedErr)
	}
	assertContainsAll(t, resumedOut, "[cancelled]", "Patch turn resolved.")
}

// TestCLIProcessHelper is launched by TestCLI_ActualProcessExitResumeAndFollowUpReads
// so every lifecycle transition crosses a real process boundary.
func TestCLIProcessHelper(t *testing.T) {
	if os.Getenv("CORAGENT_CLI_PROCESS_HELPER") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		os.Exit(2)
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	code := run(os.Args[separator+1:], os.Stdin, os.Stdout, os.Stderr, interrupt)
	signal.Stop(interrupt)
	os.Exit(code)
}

func runCLIProcess(t *testing.T, home, input string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd, stdout, stderr := newCLIProcess(ctx, home, input, args...)
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI subprocess timed out: %v", ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

func newCLIProcess(ctx context.Context, home, input string, args ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	childArgs := append([]string{"-test.run=^TestCLIProcessHelper$", "--"}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], childArgs...)
	cmd.Env = []string{
		"HOME=" + home,
		"CORAGENT_CLI_PROCESS_HELPER=1",
		"CORAGENT_CLI_SUBPROCESS_KEY=subprocess-runtime-value",
		"NO_PROXY=127.0.0.1,localhost,[::1]",
	}
	cmd.Stdin = strings.NewReader(input)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd, stdout, stderr
}

func writeCLISettings(t *testing.T, home, endpoint string) {
	t.Helper()
	dir := filepath.Join(home, ".coragent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"provider": map[string]any{
		"endpoint": endpoint, "model": "immutable-test-snapshot", "context_window": 32000,
		"max_output_tokens": 8000, "api_key_env": "CORAGENT_CLI_SUBPROCESS_KEY",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeToolCall(w http.ResponseWriter, callID, name string, arguments map[string]any) {
	argumentData, _ := json.Marshal(arguments)
	writeSSEChunk(w, map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": callID, "function": map[string]any{"name": name, "arguments": string(argumentData)},
		}}},
		"finish_reason": "tool_calls",
	}}})
}

func writeAssistantText(w http.ResponseWriter, text string) {
	writeSSEChunk(w, map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"content": text}, "finish_reason": "stop",
	}}})
}

func writeSSEChunk(w http.ResponseWriter, value any) {
	data, _ := json.Marshal(value)
	writeFormatted(w, "data: %s\n\n", data)
	writeLine(w, "data: [DONE]")
}

func assertContainsAll(t *testing.T, value string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			t.Errorf("value does not contain %q: %s", needle, value)
		}
	}
}
