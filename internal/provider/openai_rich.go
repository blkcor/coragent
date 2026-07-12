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

	"github.com/blkcor/coragent/internal/core"
)

// StreamRichReply implements the optional rich-provider extension. It performs
// one request, explicitly asks for streaming usage, exposes only the documented
// reasoning_summary field, and retains detailed finish reasons.
func (p *OpenAIProvider) StreamRichReply(ctx context.Context, conv core.Conversation, tools []core.Tool, opts core.StreamOptions) <-chan core.RichProviderEvent {
	events := make(chan core.RichProviderEvent, 10)
	go func() {
		defer close(events)
		request := p.buildRequest(conv, tools, opts)
		request.StreamOptions = &StreamOptions{IncludeUsage: true}

		var lastErr error
		for attempt := 0; attempt <= p.retryMax; attempt++ {
			if attempt > 0 {
				timer := time.NewTimer(p.retryBackoff * time.Duration(1<<uint(attempt-1)))
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					events <- core.RichProviderEvent{Type: core.RichProviderError, Error: ctx.Err()}
					return
				}
			}

			emitted, err := p.streamRichOnce(ctx, request, events)
			if err == nil {
				return
			}
			lastErr = err
			if emitted || !isTransient(err) {
				events <- core.RichProviderEvent{Type: core.RichProviderError, Error: err}
				return
			}
		}
		events <- core.RichProviderEvent{Type: core.RichProviderError, Error: fmt.Errorf("retry exhausted: %w", lastErr)}
	}()
	return events
}

func (p *OpenAIProvider) streamRichOnce(ctx context.Context, request *ChatCompletionRequest, events chan<- core.RichProviderEvent) (bool, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return false, fmt.Errorf("marshal request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.httpClient.Do(httpRequest)
	if err != nil {
		return false, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return false, p.handleErrorResponse(response)
	}
	return p.streamRichResponse(ctx, response.Body, events)
}

func (p *OpenAIProvider) streamRichResponse(ctx context.Context, body io.Reader, events chan<- core.RichProviderEvent) (bool, error) {
	scanner := bufio.NewScanner(body)
	toolCalls := make(map[int]*toolCallAccumulator)
	var toolOrder []int
	var ending *core.RichReplyEnded
	emitted := false

	finish := func(inferred bool) (bool, error) {
		completed := make([]*core.ToolCall, 0, len(toolOrder))
		for _, index := range toolOrder {
			call, err := toolCalls[index].complete()
			if err != nil {
				return emitted, err
			}
			completed = append(completed, call)
		}
		for _, call := range completed {
			events <- core.RichProviderEvent{Type: core.RichProviderToolCall, ToolCall: call}
			emitted = true
		}
		if ending == nil {
			reason := core.ProviderTerminationStop
			if len(completed) > 0 {
				reason = core.ProviderTerminationToolCalls
			}
			ending = &core.RichReplyEnded{Reason: reason}
			if !inferred {
				ending.ProviderReasonCode = "done"
			}
		}
		events <- core.RichProviderEvent{Type: core.RichProviderReplyEnded, ReplyEnded: ending}
		return true, nil
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return emitted, ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return finish(false)
		}

		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return emitted, fmt.Errorf("parse chunk: %w", err)
		}
		if chunk.Usage != nil {
			usage, err := richUsage(chunk.Usage)
			if err != nil {
				events <- core.RichProviderEvent{
					Type: core.RichProviderWarning, WarningCode: "invalid_provider_usage",
					Warning: "provider usage was ignored because it was malformed",
				}
				emitted = true
			} else {
				events <- core.RichProviderEvent{Type: core.RichProviderUsage, Usage: &usage}
				emitted = true
			}
		}

		for _, choice := range chunk.Choices {
			if ending != nil && hasPostFinishContent(choice.Delta) {
				return emitted, fmt.Errorf("provider emitted content after finish reason")
			}
			if choice.Delta.ReasoningSummary != nil && *choice.Delta.ReasoningSummary != "" {
				events <- core.RichProviderEvent{Type: core.RichProviderReasoningSummaryDelta, ReasoningSummaryDelta: *choice.Delta.ReasoningSummary}
				emitted = true
			}
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				events <- core.RichProviderEvent{Type: core.RichProviderTextDelta, TextDelta: *choice.Delta.Content}
				emitted = true
			}
			for _, delta := range choice.Delta.ToolCalls {
				accumulator, exists := toolCalls[delta.Index]
				if !exists {
					accumulator = &toolCallAccumulator{id: delta.ID, toolType: delta.Type, firstSeen: len(toolOrder)}
					toolCalls[delta.Index] = accumulator
					toolOrder = append(toolOrder, delta.Index)
				}
				if delta.Function != nil {
					if delta.Function.Name != "" {
						accumulator.name = delta.Function.Name
					}
					accumulator.arguments += delta.Function.Arguments
				}
			}
			if choice.FinishReason != nil {
				if ending != nil {
					return emitted, fmt.Errorf("provider emitted more than one finish reason")
				}
				ending = richEnding(*choice.FinishReason)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return emitted, fmt.Errorf("stream error: %w", err)
	}
	if ending == nil {
		return emitted, fmt.Errorf("provider stream closed without finish reason or [DONE]")
	}
	return finish(true)
}

func richEnding(reason string) *core.RichReplyEnded {
	ending := &core.RichReplyEnded{ProviderReasonCode: reason}
	switch reason {
	case "stop":
		ending.Reason = core.ProviderTerminationStop
	case "tool_calls":
		ending.Reason = core.ProviderTerminationToolCalls
	case "length":
		ending.Reason = core.ProviderTerminationLength
	case "content_filter":
		ending.Reason = core.ProviderTerminationContentFilter
	default:
		ending.Reason = core.ProviderTerminationProviderSpecific
	}
	return ending
}

func richUsage(value *Usage) (core.ProviderUsage, error) {
	if negativeCount(value.PromptTokens) || negativeCount(value.CompletionTokens) || negativeCount(value.TotalTokens) || negativeCount(value.ContextWindowTokens) ||
		value.PromptTokensDetails != nil && negativeCount(value.PromptTokensDetails.CachedTokens) ||
		value.CompletionTokensDetails != nil && negativeCount(value.CompletionTokensDetails.ReasoningTokens) {
		return core.ProviderUsage{}, fmt.Errorf("provider usage count is negative")
	}
	usage := core.ProviderUsage{
		PromptTokens:        optionalCount(value.PromptTokens),
		CompletionTokens:    optionalCount(value.CompletionTokens),
		TotalTokens:         optionalCount(value.TotalTokens),
		ContextWindowTokens: optionalCount(value.ContextWindowTokens),
	}
	if value.PromptTokensDetails != nil {
		usage.CachedPromptTokens = optionalCount(value.PromptTokensDetails.CachedTokens)
	}
	if value.CompletionTokensDetails != nil {
		usage.ReasoningCompletionTokens = optionalCount(value.CompletionTokensDetails.ReasoningTokens)
	}
	if usage.TotalTokens.Known && usage.PromptTokens.Known && usage.CompletionTokens.Known && usage.TotalTokens.Value < usage.PromptTokens.Value+usage.CompletionTokens.Value {
		return core.ProviderUsage{}, fmt.Errorf("provider total usage is smaller than its components")
	}
	return usage, nil
}

func negativeCount(value *int) bool { return value != nil && *value < 0 }

func optionalCount(value *int) core.OptionalUint64 {
	if value == nil || *value < 0 {
		return core.OptionalUint64{}
	}
	return core.OptionalUint64{Known: true, Value: uint64(*value)}
}

func hasPostFinishContent(delta ChatMessageDelta) bool {
	return delta.Content != nil && *delta.Content != "" ||
		delta.ReasoningSummary != nil && *delta.ReasoningSummary != "" || len(delta.ToolCalls) > 0
}

var _ core.RichProvider = (*OpenAIProvider)(nil)
