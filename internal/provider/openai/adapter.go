// Package openai implements the single M1 OpenAI-compatible streaming adapter.
// Wire request and response types are deliberately private to this package.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/blkcor/coragent/internal/credential"
	"github.com/blkcor/coragent/internal/provider"
)

const WireProtocolVersion = "openai-chat-completions-sse-v1"

const (
	maxStreamBytes = 32 << 20
	maxToolCalls   = 128
	maxToolCallID  = 1024
	maxToolName    = 256
)

// Config fixes transport authority and Provider capabilities for one adapter.
type Config struct {
	Endpoint        string
	Model           string
	ContextWindow   int
	MaxOutputTokens int
	Temperature     *float64
	Seed            *int64
	ToolChoice      string
	Credential      credential.Source
	HTTPClient      *http.Client
}

type Adapter struct {
	endpoint               string
	model                  string
	contextWindow          int
	maxOutputTokens        int
	temperature            *float64
	seed                   *int64
	toolChoice             string
	credential             credential.Source
	credentialSourceSHA256 string
	client                 *http.Client
}

func New(cfg Config) (*Adapter, error) {
	if cfg.ContextWindow <= 0 {
		return nil, errors.New("openai provider: explicit context-window limit is required")
	}
	if cfg.MaxOutputTokens <= 0 {
		return nil, errors.New("openai provider: explicit output limit is required")
	}
	if cfg.Model == "" || cfg.Endpoint == "" || cfg.Credential == nil {
		return nil, errors.New("openai provider: endpoint, model, and credential source are required")
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("openai provider: endpoint must be an http(s) URL without user information")
	}
	if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
		return nil, errors.New("openai provider: plaintext HTTP is allowed only for a loopback endpoint")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	credentialDigest := sha256.Sum256([]byte(cfg.Credential.SourceIdentity()))
	choice := cfg.ToolChoice
	if choice == "" {
		choice = "auto"
	}
	return &Adapter{
		endpoint: cfg.Endpoint, model: cfg.Model, contextWindow: cfg.ContextWindow,
		maxOutputTokens: cfg.MaxOutputTokens, temperature: cloneFloat(cfg.Temperature),
		seed: cloneInt64(cfg.Seed), toolChoice: choice, credential: cfg.Credential,
		credentialSourceSHA256: hex.EncodeToString(credentialDigest[:]), client: &clientCopy,
	}, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *Adapter) ContextWindow() int { return a.contextWindow }

func (a *Adapter) Identity() provider.Identity {
	digest := sha256.Sum256([]byte(a.endpoint))
	return provider.Identity{
		Adapter: "openai-chat-completions", WireProtocol: WireProtocolVersion,
		EndpointSHA256: hex.EncodeToString(digest[:]), CredentialSourceSHA256: a.credentialSourceSHA256, Model: a.model,
		ContextWindow: a.contextWindow, MaxOutputTokens: a.maxOutputTokens,
		Temperature: cloneFloat(a.temperature), Seed: cloneInt64(a.seed), ToolChoice: a.toolChoice,
	}
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (a *Adapter) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	return a.Stream(ctx, req, nil)
}

func (a *Adapter) Stream(ctx context.Context, req provider.Request, onText func(string) error) (provider.Response, error) {
	wreq, err := a.buildRequest(req)
	if err != nil {
		return provider.Response{}, &provider.Failure{Class: provider.ClassPermanent, Message: "invalid internal request"}
	}
	body, err := json.Marshal(wreq)
	if err != nil {
		return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "encode request"}
	}
	token, err := a.credential.Credential(ctx)
	if err != nil {
		return provider.Response{}, &provider.Failure{Class: provider.ClassPermanent, Message: "provider credential unavailable"}
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return provider.Response{}, &provider.Failure{Class: provider.ClassPermanent, Message: "build transport request"}
	}
	hreq.Header.Set("Authorization", "Bearer "+token)
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	resp, err := a.client.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return provider.Response{}, ctx.Err()
		}
		return provider.Response{}, &provider.Failure{Class: provider.ClassTransient, Message: "transport request failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		return provider.Response{}, classifyHTTP(resp.StatusCode, resp.Header.Get("Retry-After"), time.Now(), errorBody)
	}
	if media := resp.Header.Get("Content-Type"); media != "" && !strings.Contains(strings.ToLower(media), "text/event-stream") {
		_ = resp.Body.Close()
		return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "unexpected response content type"}
	}
	response, streamErr := decodeStream(ctx, resp.Body, onText, responseByteLimit(a.maxOutputTokens))
	closeErr := resp.Body.Close()
	if streamErr != nil {
		return provider.Response{}, streamErr
	}
	if closeErr != nil {
		return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "close provider response"}
	}
	return response, nil
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

type wireFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireRequest struct {
	Model      string        `json:"model"`
	Messages   []wireMessage `json:"messages"`
	Tools      []wireTool    `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
	Stream     bool          `json:"stream"`
	StreamOpts *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature *float64 `json:"temperature,omitempty"`
	Seed        *int64   `json:"seed,omitempty"`
}

func (a *Adapter) buildRequest(req provider.Request) (wireRequest, error) {
	w := wireRequest{
		Model: a.model, Stream: true, MaxTokens: a.maxOutputTokens,
		Temperature: a.temperature, Seed: a.seed,
	}
	w.StreamOpts = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}
	if req.MaxOutput > 0 && req.MaxOutput < w.MaxTokens {
		w.MaxTokens = req.MaxOutput
	}
	if req.StablePrompt != "" {
		w.Messages = append(w.Messages, wireMessage{Role: "system", Content: req.StablePrompt})
	}
	if req.DynamicPrompt != "" {
		w.Messages = append(w.Messages, wireMessage{Role: "system", Content: req.DynamicPrompt})
	}
	if len(req.Messages) == 0 && req.Prompt != "" {
		w.Messages = append(w.Messages, wireMessage{Role: "user", Content: req.Prompt})
	}
	for _, msg := range req.Messages {
		wm := wireMessage{Role: string(msg.Role), Content: msg.Content, ToolCallID: msg.ToolCallID}
		for _, call := range msg.ToolCalls {
			if call.ID == "" || call.Name == "" || !json.Valid(call.Arguments) {
				return wireRequest{}, errors.New("invalid tool call context")
			}
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{ID: call.ID, Type: "function", Function: wireFunction{Name: call.Name, Arguments: call.Arguments}})
		}
		w.Messages = append(w.Messages, wm)
	}
	for _, tool := range req.Tools {
		if tool.Name == "" || !json.Valid(tool.Schema) {
			return wireRequest{}, errors.New("invalid tool definition")
		}
		wt := wireTool{Type: "function"}
		wt.Function.Name = tool.Name
		wt.Function.Description = tool.Description
		wt.Function.Parameters = tool.Schema
		w.Tools = append(w.Tools, wt)
	}
	if len(w.Tools) > 0 {
		w.ToolChoice = a.toolChoice
	}
	if len(w.Messages) == 0 {
		return wireRequest{}, errors.New("empty message list")
	}
	return w, nil
}

type wireChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     uint64 `json:"prompt_tokens"`
		CompletionTokens uint64 `json:"completion_tokens"`
	} `json:"usage"`
}

type callBuilder struct {
	id, name, arguments string
}

func decodeStream(ctx context.Context, body io.Reader, onText func(string) error, outputByteLimit int) (provider.Response, error) {
	if outputByteLimit <= 0 {
		return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "invalid response limit"}
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var out provider.Response
	builders := make(map[int]*callBuilder)
	sawData := false
	totalStreamBytes := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return provider.Response{}, err
		}
		line := scanner.Text()
		totalStreamBytes += len(line) + 1
		if totalStreamBytes > streamByteLimit(outputByteLimit) {
			return provider.Response{}, &provider.Failure{Class: provider.ClassOutputLimit, Message: "provider stream exceeded the bounded response size"}
		}
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "malformed SSE field"}
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawData = true
			break
		}
		var chunk wireChunk
		dec := json.NewDecoder(strings.NewReader(data))
		if err := dec.Decode(&chunk); err != nil {
			return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "malformed stream JSON"}
		}
		sawData = true
		if chunk.Usage != nil {
			out.Usage = provider.Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if len(out.Text)+len(choice.Delta.Content) > outputByteLimit {
					return provider.Response{}, &provider.Failure{Class: provider.ClassOutputLimit, Message: "provider text exceeded the bounded output size"}
				}
				out.Text += choice.Delta.Content
				if onText != nil {
					if err := onText(choice.Delta.Content); err != nil {
						return provider.Response{}, err
					}
				}
			}
			for _, part := range choice.Delta.ToolCalls {
				if part.Index < 0 || part.Index >= maxToolCalls {
					return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "tool call index exceeded the bounded call count"}
				}
				b := builders[part.Index]
				if b == nil {
					if len(builders) >= maxToolCalls {
						return provider.Response{}, &provider.Failure{Class: provider.ClassOutputLimit, Message: "provider emitted too many tool calls"}
					}
					b = &callBuilder{}
					builders[part.Index] = b
				}
				if part.ID != "" {
					if b.id != "" && b.id != part.ID {
						return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "tool call ID changed during stream"}
					}
					b.id = part.ID
				}
				if len(b.id) > maxToolCallID || len(b.name)+len(part.Function.Name) > maxToolName || len(b.arguments)+len(part.Function.Arguments) > outputByteLimit {
					return provider.Response{}, &provider.Failure{Class: provider.ClassOutputLimit, Message: "provider tool call exceeded the bounded output size"}
				}
				b.name += part.Function.Name
				b.arguments += part.Function.Arguments
			}
			if choice.FinishReason != nil {
				out.Reason = mapReason(*choice.FinishReason)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return provider.Response{}, ctx.Err()
		}
		return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "stream read failed"}
	}
	if !sawData {
		return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "empty provider stream"}
	}
	for i := 0; i < len(builders); i++ {
		b, ok := builders[i]
		if !ok || b.id == "" || b.name == "" || !json.Valid([]byte(b.arguments)) {
			return provider.Response{}, &provider.Failure{Class: provider.ClassProtocol, Message: "incomplete tool call"}
		}
		out.ToolCalls = append(out.ToolCalls, provider.ToolCall{ID: b.id, Name: b.name, Arguments: json.RawMessage(b.arguments)})
	}
	return out, nil
}

func responseByteLimit(maxOutputTokens int) int {
	const bytesPerTokenUpperBound = 8
	if maxOutputTokens > maxStreamBytes/bytesPerTokenUpperBound {
		return maxStreamBytes
	}
	return maxOutputTokens * bytesPerTokenUpperBound
}

func streamByteLimit(outputLimit int) int {
	limit := outputLimit*4 + (1 << 20)
	if limit > maxStreamBytes || limit < outputLimit {
		return maxStreamBytes
	}
	return limit
}

func mapReason(reason string) provider.TerminalReason {
	switch reason {
	case "stop":
		return provider.ReasonStop
	case "tool_calls", "function_call":
		return provider.ReasonToolCalls
	case "length":
		return provider.ReasonLength
	default:
		return provider.ReasonOther
	}
}

func classifyHTTP(status int, retryAfter string, now time.Time, body []byte) *provider.Failure {
	f := &provider.Failure{Message: fmt.Sprintf("provider HTTP status %d", status)}
	if status == http.StatusBadRequest && contextLimitError(body) {
		f.Class = provider.ClassContextOverflow
		return f
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		f.Class = provider.ClassPermanent
	case http.StatusTooManyRequests:
		f.Class = provider.ClassRateLimit
	case http.StatusServiceUnavailable:
		f.Class = provider.ClassOverloaded
	default:
		if status >= 500 {
			f.Class = provider.ClassTransient
		} else {
			f.Class = provider.ClassPermanent
		}
	}
	f.RetryAfter = parseRetryAfter(retryAfter, now)
	return f
}

func contextLimitError(body []byte) bool {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	code := strings.ToLower(envelope.Error.Code + " " + envelope.Error.Type)
	return strings.Contains(code, "context_length") || strings.Contains(code, "context_window")
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
