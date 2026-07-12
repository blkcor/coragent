// Package agent is Coragent's public, frontend-neutral SDK.
//
// A Session owns one Conversation, a tool registry, and the canonical
// execution chain. Session.Run preserves the original RunEvent contract;
// Session.RunObserved is an opt-in, versioned stream for richer frontends. The
// two entry points execute the same work and share one in-flight guard.
//
// Core public concepts include Conversation, Tool, ToolCall, ToolResult,
// Provider, Session, RunEvent, and ObservedEvent. Optional rich providers can
// add display-safe reasoning summaries, structured usage, context-window
// metadata, and distinct termination reasons without changing the required
// Provider interface.
//
// LoadSettings and Bootstrap provide the validated first-party construction
// path. Settings is intentionally opaque: its String, logging, and JSON forms
// expose only a positive allowlist and never credentials, resolved environment
// values, hook command arguments, or system prompts. SDK embedders may continue
// to construct SessionConfig directly.
//
// Session.Describe returns a recursively independent, secret-free snapshot of
// model identity, permission ownership, sandbox posture, context-window
// knowledge, provider features, and effective capability inventory. Descriptor
// visibility is descriptive only and never grants tool authority.
//
// Permission requests carried by the observed stream bind their own
// exactly-once reply operation. Rich replies support allow, deny, non-approving
// argument revision, remembered action scope, and ephemeral one-call sandbox
// grants. Permission mode can be changed through typed APIs only between runs.
//
// This package is the only supported boundary for frontends and external SDK
// clients. Its implementation may use module-internal packages, but callers do
// not import those packages and the harness never imports a frontend.
package agent
