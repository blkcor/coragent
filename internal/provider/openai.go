package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/blkcor/coragent/pkg/agent"
)

// OpenAIProvider implements the Provider interface for OpenAI-compatible endpoints
type OpenAIProvider struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	// Default settings
	defaultModel       string
	defaultTemperature *float64
	defaultMaxTokens   *int
	retryMax           int
	retryBackoff       time.Duration
}

// NewOpenAIProvider creates a new OpenAI-compatible provider
func NewOpenAIProvider(baseURL, apiKey, defaultModel string) *OpenAIProvider {
	temperature := 0.7
	retryMax := 3
	retryBackoff := 1000 * time.Millisecond

	return &OpenAIProvider{
		baseURL:            baseURL,
		apiKey:             apiKey,
		httpClient:         &http.Client{},
		defaultModel:       defaultModel,
		defaultTemperature: &temperature,
		retryMax:           retryMax,
		retryBackoff:       retryBackoff,
	}
}

// StreamReply implements the Provider interface
func (p *OpenAIProvider) StreamReply(ctx context.Context, conv agent.Conversation, tools []agent.Tool, opts agent.StreamOptions) <-chan agent.RunEvent {
	events := make(chan agent.RunEvent, 10)

	go func() {
		defer close(events)

		// Build request
		req := p.buildRequest(conv, tools, opts)

		// Execute with retry
		var lastErr error
		for attempt := 0; attempt <= p.retryMax; attempt++ {
			if attempt > 0 {
				backoff := p.retryBackoff * time.Duration(1<<uint(attempt-1))
				timer := time.NewTimer(backoff)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					events <- agent.RunEvent{Type: agent.ErrorEvent, Error: ctx.Err()}
					return
				}
			}

			emitted, err := p.streamOnce(ctx, req, events)
			if err == nil {
				return // Success
			}

			lastErr = err
			if emitted || !isTransient(err) {
				// Can't retry after partial emission; also don't retry permanent failures
				events <- agent.RunEvent{Type: agent.ErrorEvent, Error: err}
				return
			}
		}

		// Retry exhausted
		events <- agent.RunEvent{Type: agent.ErrorEvent, Error: fmt.Errorf("retry exhausted: %w", lastErr)}
	}()

	return events
}

// buildRequest constructs the ChatCompletionRequest from conversation and tools
func (p *OpenAIProvider) buildRequest(conv agent.Conversation, tools []agent.Tool, opts agent.StreamOptions) *ChatCompletionRequest {
	// Convert conversation to messages
	var messages []ChatMessage
	for _, turn := range conv.Turns {
		switch turn.Role {
		case "assistant":
			msg := ChatMessage{Role: turn.Role}
			if turn.Content != "" {
				msg.Content = &turn.Content
			}
			for _, tc := range turn.ToolCalls {
				argsJSON := []byte("{}")
				if tc.Arguments != nil {
					argsJSON, _ = json.Marshal(tc.Arguments)
				}
				msg.ToolCalls = append(msg.ToolCalls, ChatToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: ChatFunctionCall{
						Name:      tc.ToolName,
						Arguments: string(argsJSON),
					},
				})
			}
			messages = append(messages, msg)
		case "tool":
			for _, tr := range turn.ToolResults {
				content := tr.Result
				messages = append(messages, ChatMessage{
					Role:       "tool",
					Content:    &content,
					ToolCallID: tr.ToolCallID,
				})
			}
		default:
			messages = append(messages, ChatMessage{
				Role:    turn.Role,
				Content: &turn.Content,
			})
		}
	}

	// Convert tools
	var chatTools []ChatTool
	for _, tool := range tools {
		chatTools = append(chatTools, ChatTool{
			Type: "function",
			Function: ChatFunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}

	// Apply options with fallback to defaults
	model := p.defaultModel
	if opts.Model != "" {
		model = opts.Model
	}

	temperature := p.defaultTemperature
	if opts.Temperature != nil {
		temperature = opts.Temperature
	}

	maxTokens := p.defaultMaxTokens
	if opts.MaxTokens != nil {
		maxTokens = opts.MaxTokens
	}

	return &ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Tools:       chatTools,
		Stream:      true,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
}

// streamOnce performs a single streaming request.
// Returns (emitted, err) where emitted indicates whether any events were
// sent to the channel before the error occurred.
func (p *OpenAIProvider) streamOnce(ctx context.Context, req *ChatCompletionRequest, events chan<- agent.RunEvent) (bool, error) {
	// Marshal request body
	body, err := json.Marshal(req)
	if err != nil {
		return false, fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Execute request
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return false, p.handleErrorResponse(resp)
	}

	// Stream response
	return p.streamResponse(ctx, resp.Body, events)
}

// streamResponse reads and parses the SSE stream.
// Returns (emitted, err) where emitted indicates whether any events were
// written to the channel.
func (p *OpenAIProvider) streamResponse(ctx context.Context, body io.Reader, events chan<- agent.RunEvent) (bool, error) {
	scanner := bufio.NewScanner(body)
	toolCallBuffer := make(map[int]*toolCallAccumulator)
	var toolCallOrder []int
	emitted := false

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return emitted, ctx.Err()
		default:
		}

		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Check for end of stream
		if data == "[DONE]" {
			// Emit accumulated tool calls in order
			for _, idx := range toolCallOrder {
				if acc, ok := toolCallBuffer[idx]; ok {
					toolCall, err := acc.complete()
					if err != nil {
						events <- agent.RunEvent{Type: agent.ErrorEvent, Error: err}
						return true, err
					}
					events <- agent.RunEvent{Type: agent.ToolCallEvent, ToolCall: toolCall}
				}
			}

			// Use correct end reason based on whether tool calls were accumulated
			reason := agent.Finished
			if len(toolCallOrder) > 0 {
				reason = agent.StoppedToCallTools
			}

			events <- agent.RunEvent{
				Type: agent.ReplyEndedEvent,
				ReplyEnded: &agent.ReplyEnded{
					Reason: reason,
				},
			}
			return true, nil
		}

		// Parse chunk
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return emitted, fmt.Errorf("parse chunk: %w", err)
		}

		// Process choices
		for _, choice := range chunk.Choices {
			// Emit text deltas
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				events <- agent.RunEvent{
					Type:      agent.TextDelta,
					TextDelta: *choice.Delta.Content,
				}
				emitted = true
			}

			// Accumulate tool call fragments
			for _, tcDelta := range choice.Delta.ToolCalls {
				acc, exists := toolCallBuffer[tcDelta.Index]
				if !exists {
					acc = &toolCallAccumulator{
						id:        tcDelta.ID,
						toolType:  tcDelta.Type,
						firstSeen: len(toolCallOrder),
					}
					toolCallBuffer[tcDelta.Index] = acc
					toolCallOrder = append(toolCallOrder, tcDelta.Index)
				}

				if tcDelta.Function != nil {
					if tcDelta.Function.Name != "" {
						acc.name = tcDelta.Function.Name
					}
					acc.arguments += tcDelta.Function.Arguments
				}
			}

			// Check finish reason
			if choice.FinishReason != nil {
				switch *choice.FinishReason {
				case "tool_calls":
					// Emit tool calls
					for _, idx := range toolCallOrder {
						if acc, ok := toolCallBuffer[idx]; ok {
							toolCall, err := acc.complete()
							if err != nil {
								events <- agent.RunEvent{Type: agent.ErrorEvent, Error: err}
								return true, err
							}
							events <- agent.RunEvent{Type: agent.ToolCallEvent, ToolCall: toolCall}
						}
					}
					events <- agent.RunEvent{
						Type: agent.ReplyEndedEvent,
						ReplyEnded: &agent.ReplyEnded{
							Reason: agent.StoppedToCallTools,
						},
					}
					return true, nil
				case "length":
					events <- agent.RunEvent{
						Type: agent.ReplyEndedEvent,
						ReplyEnded: &agent.ReplyEnded{
							Reason: agent.CutOff,
						},
					}
					return true, nil
				case "stop":
					events <- agent.RunEvent{
						Type: agent.ReplyEndedEvent,
						ReplyEnded: &agent.ReplyEnded{
							Reason: agent.Finished,
						},
					}
					return true, nil
				case "content_filter":
					events <- agent.RunEvent{
						Type: agent.ReplyEndedEvent,
						ReplyEnded: &agent.ReplyEnded{
							Reason: agent.CutOff,
						},
					}
					return true, nil
				default:
					// Unknown finish reason — treat as normal completion
					events <- agent.RunEvent{
						Type: agent.ReplyEndedEvent,
						ReplyEnded: &agent.ReplyEnded{
							Reason: agent.Finished,
						},
					}
					return true, nil
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return emitted, fmt.Errorf("stream error: %w", err)
	}

	// Stream ended without [DONE] or finish_reason — emit a terminal event
	// so consumers always see a ReplyEndedEvent before the channel closes.
	reason := agent.Finished
	if len(toolCallOrder) > 0 {
		reason = agent.StoppedToCallTools
	}
	events <- agent.RunEvent{
		Type: agent.ReplyEndedEvent,
		ReplyEnded: &agent.ReplyEnded{
			Reason: reason,
		},
	}
	return true, nil
}

// handleErrorResponse handles non-200 HTTP responses
func (p *OpenAIProvider) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &PermanentError{Message: "unauthorized: invalid API key"}
	case http.StatusBadRequest:
		return &PermanentError{Message: fmt.Sprintf("bad request: %s", string(body))}
	case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return &TransientError{Message: fmt.Sprintf("transient error %d: %s", resp.StatusCode, string(body))}
	default:
		return &PermanentError{Message: fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, string(body))}
	}
}

// toolCallAccumulator buffers tool call fragments
type toolCallAccumulator struct {
	id        string
	toolType  string
	name      string
	arguments string
	firstSeen int
}

// complete parses the accumulated fragments into a ToolCall
func (a *toolCallAccumulator) complete() (*agent.ToolCall, error) {
	if a.name == "" {
		return nil, fmt.Errorf("tool call missing name")
	}

	var args map[string]interface{}
	if a.arguments != "" {
		if err := json.Unmarshal([]byte(a.arguments), &args); err != nil {
			return nil, fmt.Errorf("parse tool arguments: %w", err)
		}
	}

	return &agent.ToolCall{
		ID:        a.id,
		ToolName:  a.name,
		Arguments: args,
	}, nil
}

// isTransient checks if an error is transient (retryable)
func isTransient(err error) bool {
	_, ok := err.(*TransientError)
	return ok
}

// TransientError represents a retryable error
type TransientError struct {
	Message string
}

func (e *TransientError) Error() string {
	return e.Message
}

// PermanentError represents a non-retryable error
type PermanentError struct {
	Message string
}

func (e *PermanentError) Error() string {
	return e.Message
}
