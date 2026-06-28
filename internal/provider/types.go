package provider

import (
	"encoding/json"
)

// OpenAI-compatible request and response types

// ChatCompletionRequest is the request sent to the OpenAI-compatible endpoint
type ChatCompletionRequest struct {
	Model       string           `json:"model"`
	Messages    []ChatMessage    `json:"messages"`
	Tools       []ChatTool       `json:"tools,omitempty"`
	Stream      bool             `json:"stream"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   *int             `json:"max_tokens,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// StreamOptions configures streaming behavior
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatMessage represents a message in the conversation
type ChatMessage struct {
	Role      string         `json:"role"`
	Content   *string        `json:"content,omitempty"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

// ChatTool describes an available tool
type ChatTool struct {
	Type     string              `json:"type"`
	Function ChatFunctionDef     `json:"function"`
}

// ChatFunctionDef describes a function tool
type ChatFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatToolCall represents a tool call in a message
type ChatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ChatFunctionCall `json:"function"`
}

// ChatFunctionCall represents a function invocation
type ChatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Usage reports token consumption for a request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionResponse is the non-streaming response (not used in streaming mode)
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents one completion choice
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatCompletionChunk is a streaming chunk (SSE data)
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice represents a choice in a streaming chunk
type ChunkChoice struct {
	Index        int              `json:"index"`
	Delta        ChatMessageDelta `json:"delta"`
	FinishReason *string          `json:"finish_reason,omitempty"`
}

// ChatMessageDelta represents incremental changes to a message
type ChatMessageDelta struct {
	Role      string              `json:"role,omitempty"`
	Content   *string             `json:"content,omitempty"`
	ToolCalls []ChatToolCallDelta `json:"tool_calls,omitempty"`
}

// ChatToolCallDelta represents incremental tool call fragments
type ChatToolCallDelta struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id,omitempty"`
	Type     string                `json:"type,omitempty"`
	Function *ChatFunctionCallDelta `json:"function,omitempty"`
}

// ChatFunctionCallDelta represents incremental function call fragments
type ChatFunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
