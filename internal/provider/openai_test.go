package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

func TestOpenAIProvider_StreamText(t *testing.T) {
	// Create test server that streams text deltas
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Stream text chunks
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "gpt-4")
	conv := core.Conversation{
		Turns: []core.Turn{
			{Role: "user", Content: "Hi"},
		},
	}

	events := provider.StreamReply(context.Background(), conv, nil, core.StreamOptions{})

	// Collect text deltas
	var text string
	for event := range events {
		if event.Type == core.TextDelta {
			text += event.TextDelta
		}
	}

	if text != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", text)
	}
}

func TestOpenAIProvider_StreamToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Stream tool call fragments
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"\"}}]}}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\"\"}}]}}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\":\\\"test.txt\\\"}\"}}]}}]}\n\n")
		finishReason := "tool_calls"
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"%s\"}]}\n\n", finishReason)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "gpt-4")
	conv := core.Conversation{
		Turns: []core.Turn{
			{Role: "user", Content: "Read test.txt"},
		},
	}

	events := provider.StreamReply(context.Background(), conv, nil, core.StreamOptions{})

	// Collect tool calls
	var toolCalls []core.ToolCall
	for event := range events {
		if event.Type == core.ToolCallEvent && event.ToolCall != nil {
			toolCalls = append(toolCalls, *event.ToolCall)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc.ToolName != "read_file" {
		t.Errorf("expected tool name read_file, got %s", tc.ToolName)
	}

	if tc.Arguments["path"] != "test.txt" {
		t.Errorf("expected path test.txt, got %v", tc.Arguments["path"])
	}
}

func TestOpenAIProvider_MultipleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Two tool calls
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"tool_a\",\"arguments\":\"{}\"}}]}}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_2\",\"type\":\"function\",\"function\":{\"name\":\"tool_b\",\"arguments\":\"{}\"}}]}}]}\n\n")
		finishReason := "tool_calls"
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"%s\"}]}\n\n", finishReason)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "gpt-4")
	conv := core.Conversation{
		Turns: []core.Turn{
			{Role: "user", Content: "Do both"},
		},
	}

	events := provider.StreamReply(context.Background(), conv, nil, core.StreamOptions{})

	// Collect tool calls in order
	var toolCalls []core.ToolCall
	for event := range events {
		if event.Type == core.ToolCallEvent && event.ToolCall != nil {
			toolCalls = append(toolCalls, *event.ToolCall)
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}

	// Check order preserved
	if toolCalls[0].ToolName != "tool_a" {
		t.Errorf("expected first tool tool_a, got %s", toolCalls[0].ToolName)
	}
	if toolCalls[1].ToolName != "tool_b" {
		t.Errorf("expected second tool tool_b, got %s", toolCalls[1].ToolName)
	}
}

func TestOpenAIProvider_CancelContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Stream one chunk and flush so the client receives it before we block
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "gpt-4")
	conv := core.Conversation{
		Turns: []core.Turn{
			{Role: "user", Content: "Hi"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := provider.StreamReply(ctx, conv, nil, core.StreamOptions{})

	// Read first event
	event := <-events
	if event.Type != core.TextDelta {
		t.Errorf("expected text delta, got %v", event.Type)
	}

	// Cancel
	cancel()

	// Should get error event
	for event := range events {
		if event.Type == core.ErrorEvent {
			return // Success
		}
	}

	t.Error("expected error event after cancellation")
}

func TestOpenAIProvider_PermanentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error": "invalid api key"}`)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "bad-key", "gpt-4")
	conv := core.Conversation{
		Turns: []core.Turn{
			{Role: "user", Content: "Hi"},
		},
	}

	events := provider.StreamReply(context.Background(), conv, nil, core.StreamOptions{})

	// Should get error event immediately (no retry)
	event := <-events
	if event.Type != core.ErrorEvent {
		t.Errorf("expected error event, got %v", event.Type)
	}

	if event.Error == nil {
		t.Error("expected error")
	}

	// Should be permanent error
	if _, ok := event.Error.(*PermanentError); !ok {
		t.Errorf("expected PermanentError, got %T", event.Error)
	}
}

func TestOpenAIProvider_ReplyEndReason(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		expected     core.ReplyEndReason
	}{
		{"finished", "stop", core.Finished},
		{"tool_calls", "tool_calls", core.StoppedToCallTools},
		{"cut_off", "length", core.CutOff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"%s\"}]}\n\n", tt.finishReason)
			}))
			defer server.Close()

			provider := NewOpenAIProvider(server.URL, "test-key", "gpt-4")
			conv := core.Conversation{
				Turns: []core.Turn{
					{Role: "user", Content: "Hi"},
				},
			}

			events := provider.StreamReply(context.Background(), conv, nil, core.StreamOptions{})

			var replyEnded *core.ReplyEnded
			for event := range events {
				if event.Type == core.ReplyEndedEvent {
					replyEnded = event.ReplyEnded
				}
			}

			if replyEnded == nil {
				t.Fatal("expected reply ended event")
			}

			if replyEnded.Reason != tt.expected {
				t.Errorf("expected reason %v, got %v", tt.expected, replyEnded.Reason)
			}
		})
	}
}

func TestOpenAIProvider_PerRequestOptions(t *testing.T) {
	var receivedRequest ChatCompletionRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&receivedRequest); err != nil {
			t.Errorf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "test-key", "default-model")
	conv := core.Conversation{
		Turns: []core.Turn{
			{Role: "user", Content: "Hi"},
		},
	}

	temp := 0.5
	maxTok := 100
	opts := core.StreamOptions{
		Model:       "custom-model",
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}

	events := provider.StreamReply(context.Background(), conv, nil, opts)
	for range events {
	}

	if receivedRequest.Model != "custom-model" {
		t.Errorf("expected model custom-model, got %s", receivedRequest.Model)
	}
	if receivedRequest.Temperature == nil || *receivedRequest.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5")
	}
	if receivedRequest.MaxTokens == nil || *receivedRequest.MaxTokens != 100 {
		t.Errorf("expected max tokens 100")
	}
}

func TestOpenAIProviderRichStreamPreservesSafeSummaryUsageAndReason(t *testing.T) {
	var received ChatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_summary\":\"checked safely\",\"reasoning\":\"hidden raw\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"length\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":4,\"total_tokens\":15,\"prompt_tokens_details\":{\"cached_tokens\":3},\"completion_tokens_details\":{\"reasoning_tokens\":2},\"context_window\":100}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := NewOpenAIProvider(server.URL, "secret", "model")
	var events []core.RichProviderEvent
	for event := range provider.StreamRichReply(context.Background(), core.Conversation{}, nil, core.StreamOptions{}) {
		events = append(events, event)
	}
	if received.StreamOptions == nil || !received.StreamOptions.IncludeUsage {
		t.Fatal("rich request did not request streaming usage")
	}
	if len(events) != 4 {
		t.Fatalf("rich event count = %d, want 4: %+v", len(events), events)
	}
	if events[0].Type != core.RichProviderReasoningSummaryDelta || events[0].ReasoningSummaryDelta != "checked safely" {
		t.Fatalf("safe summary event = %+v", events[0])
	}
	for _, event := range events {
		if event.ReasoningSummaryDelta == "hidden raw" || event.TextDelta == "hidden raw" {
			t.Fatal("raw reasoning leaked through explicit summary adapter")
		}
	}
	if events[1].Type != core.RichProviderTextDelta || events[1].TextDelta != "answer" {
		t.Fatalf("text event = %+v", events[1])
	}
	usage := events[2].Usage
	if events[2].Type != core.RichProviderUsage || usage == nil || !usage.PromptTokens.Known || usage.PromptTokens.Value != 11 || !usage.CachedPromptTokens.Known || usage.CachedPromptTokens.Value != 3 || !usage.ReasoningCompletionTokens.Known || usage.ReasoningCompletionTokens.Value != 2 || !usage.ContextWindowTokens.Known || usage.ContextWindowTokens.Value != 100 {
		t.Fatalf("usage event = %+v", events[2])
	}
	if events[3].Type != core.RichProviderReplyEnded || events[3].ReplyEnded == nil || events[3].ReplyEnded.Reason != core.ProviderTerminationLength {
		t.Fatalf("ending event = %+v", events[3])
	}
}

func TestOpenAIProviderRichFinishReasonsStayDistinct(t *testing.T) {
	tests := []struct {
		reason string
		want   core.ProviderTerminationReason
	}{
		{"stop", core.ProviderTerminationStop},
		{"tool_calls", core.ProviderTerminationToolCalls},
		{"length", core.ProviderTerminationLength},
		{"content_filter", core.ProviderTerminationContentFilter},
		{"vendor_limit", core.ProviderTerminationProviderSpecific},
	}
	for _, test := range tests {
		t.Run(test.reason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", test.reason)
			}))
			defer server.Close()
			provider := NewOpenAIProvider(server.URL, "", "model")
			var ending *core.RichReplyEnded
			for event := range provider.StreamRichReply(context.Background(), core.Conversation{}, nil, core.StreamOptions{}) {
				if event.Type == core.RichProviderReplyEnded {
					ending = event.ReplyEnded
				}
			}
			if ending == nil || ending.Reason != test.want || ending.ProviderReasonCode != test.reason {
				t.Fatalf("ending = %+v, want %q", ending, test.want)
			}
		})
	}
}

func TestOpenAIProviderRichMalformedUsageBecomesWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":-1}}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	provider := NewOpenAIProvider(server.URL, "", "model")
	warnings, usage := 0, 0
	for event := range provider.StreamRichReply(context.Background(), core.Conversation{}, nil, core.StreamOptions{}) {
		switch event.Type {
		case core.RichProviderWarning:
			warnings++
		case core.RichProviderUsage:
			usage++
		}
	}
	if warnings != 1 || usage != 0 {
		t.Fatalf("warnings=%d usage=%d, want 1/0", warnings, usage)
	}
}
