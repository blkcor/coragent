// Package agent provides the public SDK surface for the coragent harness.
//
// The agent package exposes the core vocabulary and interfaces needed to build
// LLM-powered coding agents: conversations, tools, tool calls, and the provider
// interface for model backends.
//
// # Core Concepts
//
// Conversation: A sequence of turns between user and assistant.
//
// Turn: A single exchange (user message, assistant reply, or tool interaction).
//
// Tool: A capability the assistant can invoke (read file, run command, etc.).
//
// ToolCall: A request from the assistant to invoke a specific tool with arguments.
//
// ToolResult: The outcome of a tool call, returned to the assistant.
//
// Provider: A backend that streams model replies (text and tool calls).
//
// RunEvent: Typed events streamed during a run (text deltas, tool calls, status).
//
// # Architecture
//
// This package is the public contract. It depends on no internal machinery and
// no frontend. Implementations live in internal/ packages.
//
// # Permission
//
// The permission request and decision shapes are declared here for stability,
// though no Phase 0 component emits or acts on them.
package agent
