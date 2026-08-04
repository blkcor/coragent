package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/credential"
	"github.com/blkcor/coragent/internal/provider"
)

func testCredential() string {
	return strings.Join([]string{"runtime", "only", "credential"}, "-")
}

func writeTest(w io.Writer, values ...any) {
	_, _ = fmt.Fprint(w, values...)
}

func writeTestLine(w io.Writer, values ...any) {
	_, _ = fmt.Fprintln(w, values...)
}

func newTestAdapter(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a, err := New(Config{
		Endpoint: server.URL, Model: "immutable-test-model", ContextWindow: 32000,
		MaxOutputTokens: 8000, Credential: credential.Static{Value: testCredential()},
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestStreamTextToolCallUsageAndReason(t *testing.T) {
	var requestBody []byte
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testCredential() {
			t.Errorf("authorization header = %q", got)
		}
		requestBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestLine(w, `data: {"choices":[{"delta":{"content":"I will "},"finish_reason":null}]}`)
		writeTestLine(w)
		writeTestLine(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"sea","arguments":"{\"pattern\":"}}]},"finish_reason":null}]}`)
		writeTestLine(w)
		writeTestLine(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"rch","arguments":"\"TODO\"}"}}]},"finish_reason":"tool_calls"}]}`)
		writeTestLine(w)
		writeTestLine(w, `data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`)
		writeTestLine(w)
		writeTestLine(w, "data: [DONE]")
	})
	var deltas []string
	resp, err := a.Stream(context.Background(), provider.Request{
		StablePrompt: "stable", DynamicPrompt: "dynamic",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "question"}},
		Tools:    []provider.ToolDefinition{{Name: "search", Description: "search files", Schema: json.RawMessage(`{"type":"object"}`)}},
	}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.Text != "I will " || len(deltas) != 1 || deltas[0] != "I will " {
		t.Fatalf("text = %q, deltas = %#v", resp.Text, deltas)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call-1" || resp.ToolCalls[0].Name != "search" || string(resp.ToolCalls[0].Arguments) != `{"pattern":"TODO"}` {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 || resp.Reason != provider.ReasonToolCalls {
		t.Fatalf("metadata = %+v", resp)
	}
	if strings.Contains(string(requestBody), testCredential()) {
		t.Fatal("runtime credential entered model request body")
	}
}

func TestHTTPFailuresAreClassified(t *testing.T) {
	cases := []struct {
		status int
		class  provider.FailureClass
	}{
		{http.StatusUnauthorized, provider.ClassPermanent},
		{http.StatusBadRequest, provider.ClassPermanent},
		{http.StatusTooManyRequests, provider.ClassRateLimit},
		{http.StatusServiceUnavailable, provider.ClassOverloaded},
		{http.StatusBadGateway, provider.ClassTransient},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			a := newTestAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				if tc.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "4")
				}
				w.WriteHeader(tc.status)
				writeTest(w, "provider response must not enter the failure")
			})
			_, err := a.Complete(context.Background(), provider.Request{Prompt: "hello"})
			var fail *provider.Failure
			if !errors.As(err, &fail) || fail.Class != tc.class {
				t.Fatalf("error = %v", err)
			}
			if tc.status == http.StatusTooManyRequests && fail.RetryAfter != 4*time.Second {
				t.Fatalf("RetryAfter = %v", fail.RetryAfter)
			}
			if strings.Contains(fail.Message, "provider response") {
				t.Fatal("response body leaked into failure")
			}
		})
	}
}

func TestContextOverflowIsTypedWithoutLeakingBody(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeTest(w, `{"error":{"code":"context_length_exceeded","message":"private echoed prompt"}}`)
	})
	_, err := a.Complete(context.Background(), provider.Request{Prompt: "hello"})
	var failure *provider.Failure
	if !errors.As(err, &failure) || failure.Class != provider.ClassContextOverflow {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(failure.Message, "private echoed prompt") {
		t.Fatal("Provider error body leaked into classified failure")
	}
}

func TestMalformedStreamIsProtocolFailure(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestLine(w, "data: not-json")
	})
	_, err := a.Complete(context.Background(), provider.Request{Prompt: "hello"})
	var fail *provider.Failure
	if !errors.As(err, &fail) || fail.Class != provider.ClassProtocol {
		t.Fatalf("error = %v", err)
	}
}

func TestCancellationClosesRequest(t *testing.T) {
	started := make(chan struct{})
	serverCancelled := make(chan struct{})
	var once atomic.Bool
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestLine(w, `data: {"choices":[{"delta":{"content":"partial "},"finish_reason":null}]}`)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(started)
		<-r.Context().Done()
		if once.CompareAndSwap(false, true) {
			close(serverCancelled)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.Complete(ctx, provider.Request{Prompt: "wait"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Complete error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Complete did not return after cancellation")
	}
	select {
	case <-serverCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("server request context remained active")
	}
}

func TestRequiresExplicitCapabilities(t *testing.T) {
	_, err := New(Config{Endpoint: "https://example.test", Model: "m", MaxOutputTokens: 1, Credential: credential.Static{Value: "x"}})
	if err == nil || !strings.Contains(err.Error(), "context-window") {
		t.Fatalf("New missing context window = %v", err)
	}
}

func TestRejectsPlaintextCredentialTransportOutsideLoopback(t *testing.T) {
	base := Config{
		Model: "immutable-test-model", ContextWindow: 32000, MaxOutputTokens: 8000,
		Credential: credential.Static{Value: testCredential()},
	}
	for _, endpoint := range []string{"http://provider.example/v1", "http://192.0.2.10/v1"} {
		cfg := base
		cfg.Endpoint = endpoint
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Fatalf("New(%q) = %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"https://provider.example/v1", "http://localhost:8080/v1", "http://127.0.0.1:8080/v1", "http://[::1]:8080/v1"} {
		cfg := base
		cfg.Endpoint = endpoint
		if _, err := New(cfg); err != nil {
			t.Fatalf("New(%q) = %v", endpoint, err)
		}
	}
}

func TestProviderRedirectCannotEscapeFixedEndpoint(t *testing.T) {
	var targetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalled.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestLine(w, "data: [DONE]")
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	adapter, err := New(Config{
		Endpoint: source.URL, Model: "immutable-test-model", ContextWindow: 32000, MaxOutputTokens: 8000,
		Credential: credential.Static{Value: testCredential()}, HTTPClient: source.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Complete(context.Background(), provider.Request{Prompt: "hello"})
	var failure *provider.Failure
	if !errors.As(err, &failure) || failure.Class != provider.ClassPermanent {
		t.Fatalf("redirect error = %v", err)
	}
	if targetCalled.Load() {
		t.Fatal("Provider client followed a redirect outside the fixed endpoint")
	}
}

func TestProviderStreamAndToolCallsAreCumulativelyBounded(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		a := newTestAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			for range 20 {
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": strings.Repeat("x", 4096)}}}})
				writeTestLine(w, "data: ", string(chunk))
			}
		})
		_, err := a.Complete(context.Background(), provider.Request{Prompt: "hello"})
		var failure *provider.Failure
		if !errors.As(err, &failure) || failure.Class != provider.ClassOutputLimit {
			t.Fatalf("oversized text error = %v", err)
		}
	})

	t.Run("tool call count", func(t *testing.T) {
		a := newTestAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			parts := make([]any, 0, maxToolCalls+1)
			for index := 0; index <= maxToolCalls; index++ {
				parts = append(parts, map[string]any{"index": index, "id": fmt.Sprintf("call-%d", index), "function": map[string]any{"name": "read", "arguments": `{}`}})
			}
			chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": parts}}}})
			writeTestLine(w, "data: ", string(chunk))
		})
		_, err := a.Complete(context.Background(), provider.Request{Prompt: "hello"})
		var failure *provider.Failure
		if !errors.As(err, &failure) || (failure.Class != provider.ClassOutputLimit && failure.Class != provider.ClassProtocol) {
			t.Fatalf("tool call count error = %v", err)
		}
	})
}
