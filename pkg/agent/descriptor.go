package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/blkcor/coragent/internal/subagent"
	"github.com/blkcor/coragent/internal/tools"
)

// SessionID is a stable opaque identifier for one Session lifetime.
type SessionID string

// PermissionOwnership identifies who controls permission decisions.
type PermissionOwnership string

const (
	PermissionOwnershipUnknown  PermissionOwnership = "unknown"
	PermissionOwnershipEngine   PermissionOwnership = "engine"
	PermissionOwnershipExternal PermissionOwnership = "external"
)

// SandboxPosture is the frontend-safe strength of command confinement.
type SandboxPosture string

const (
	SandboxPostureUnknown        SandboxPosture = "unknown"
	SandboxPostureOSEnforced     SandboxPosture = "os-enforced"
	SandboxPosturePolicyFallback SandboxPosture = "policy-fallback"
	SandboxPostureExternal       SandboxPosture = "external"
)

// ProviderFeatures reports only facts supported by the configured provider
// contract. Unsupported and unknown remain distinct.
type ProviderFeatures struct {
	ReasoningSummary CapabilitySupport
	Usage            CapabilitySupport
	ContextWindow    CapabilitySupport
}

// PermissionDescription reports standard-engine mode only when Coragent owns
// permission control.
type PermissionDescription struct {
	Ownership PermissionOwnership
	Mode      PermissionMode
}

// SandboxDescription is a secret-free snapshot of confinement strength.
type SandboxDescription struct {
	Posture SandboxPosture
	Reason  string
}

// ContextWindowDescription never guesses from a model name. Known is false
// until configuration or a provider supplies trustworthy metadata.
type ContextWindowDescription struct {
	Known  bool
	Tokens uint64
}

// SessionDescription is an immutable-by-value, frontend-neutral snapshot of
// effective session facts. Describe returns a recursively independent copy.
type SessionDescription struct {
	SessionID        SessionID
	RootAgentID      AgentID
	Model            string
	Provider         string
	ProviderFeatures ProviderFeatures
	WorkingDirectory string
	Permission       PermissionDescription
	Sandbox          SandboxDescription
	ContextWindow    ContextWindowDescription
	Capabilities     []CapabilityCategory
}

// Clone returns an independent descriptor inventory.
func (d SessionDescription) Clone() SessionDescription {
	out := d
	out.Capabilities = make([]CapabilityCategory, len(d.Capabilities))
	for i, category := range d.Capabilities {
		out.Capabilities[i] = category.Clone()
	}
	return out
}

// CapabilityReporter optionally describes external categories such as skills
// or MCP. Reporting is descriptive only and never registers or advertises a
// model tool.
type CapabilityReporter interface {
	CapabilityCategories() []CapabilityCategory
}

// Describe returns a fresh, secret-free snapshot. The standard permission mode
// is read at call time so a between-run change is reflected without mutating a
// previously returned description.
func (s *Session) Describe() SessionDescription {
	description := s.description.Clone()
	if s.permission != nil {
		description.Permission = PermissionDescription{
			Ownership: PermissionOwnershipEngine,
			Mode:      publicPermissionMode(s.permission.Mode()),
		}
	}
	return description
}

func buildSessionDescription(cfg SessionConfig, advertised []Tool, sandboxStatus SandboxStatus) SessionDescription {
	providerName, providerModel := safeProviderIdentityFromProvider(cfg.Provider)
	if cfg.StreamOptions.Model != "" {
		providerModel = cfg.StreamOptions.Model
	}
	if providerModel == "" {
		providerModel = "unknown"
	}

	workingDirectory := cfg.WorkingDirectory
	if workingDirectory == "" {
		workingDirectory, _ = os.Getwd()
	}
	if absolute, err := filepath.Abs(workingDirectory); err == nil {
		workingDirectory = filepath.Clean(absolute)
	}

	description := SessionDescription{
		SessionID:        SessionID(newOpaqueID("session")),
		RootAgentID:      AgentID(newOpaqueID("agent")),
		Model:            providerModel,
		Provider:         providerName,
		ProviderFeatures: safeProviderFeatures(cfg.Provider),
		WorkingDirectory: workingDirectory,
		Permission: PermissionDescription{
			Ownership: PermissionOwnershipExternal,
		},
		Sandbox: SandboxDescription{
			Posture: SandboxPostureExternal,
			Reason:  "command execution is owned by the caller-supplied Dispatcher",
		},
		Capabilities: buildCapabilityInventory(cfg, advertised, sandboxStatus),
	}
	if cfg.Dispatcher == nil {
		description.Permission = PermissionDescription{
			Ownership: PermissionOwnershipEngine,
			Mode:      permissionModeFromConfig(cfg.PermissionMode),
		}
		switch sandboxStatus.Level {
		case ConfinementOSEnforced:
			description.Sandbox = SandboxDescription{Posture: SandboxPostureOSEnforced}
		case ConfinementPolicyFallback:
			description.Sandbox = SandboxDescription{Posture: SandboxPosturePolicyFallback, Reason: sandboxStatus.Reason}
		default:
			description.Sandbox = SandboxDescription{Posture: SandboxPostureUnknown, Reason: sandboxStatus.Reason}
		}
	}
	return description
}

func permissionModeFromConfig(value string) PermissionMode {
	switch value {
	case string(PermissionModeAutoAcceptEdits):
		return PermissionModeAutoAcceptEdits
	case string(PermissionModePlan):
		return PermissionModePlan
	case string(PermissionModeBypass):
		return PermissionModeBypass
	default:
		return PermissionModeDefault
	}
}

type providerIdentity interface {
	ProviderIdentity() (provider, model string)
}

type providerFeatureReporter interface {
	ProviderFeatureSupport() (reasoningSummary, usage, contextWindow bool)
}

func safeProviderIdentityFromProvider(provider Provider) (string, string) {
	if reporter, ok := provider.(providerIdentity); ok {
		name, model := reporter.ProviderIdentity()
		return safeLabel(name, "provider"), safeLabel(model, "")
	}
	return "caller", ""
}

func safeProviderFeatures(provider Provider) ProviderFeatures {
	if reporter, ok := provider.(providerFeatureReporter); ok {
		reasoning, usage, window := reporter.ProviderFeatureSupport()
		return ProviderFeatures{
			ReasoningSummary: supportFromBool(reasoning),
			Usage:            supportFromBool(usage),
			ContextWindow:    supportFromBool(window),
		}
	}
	return ProviderFeatures{
		ReasoningSummary: CapabilitySupportUnknown,
		Usage:            CapabilitySupportUnknown,
		ContextWindow:    CapabilitySupportUnknown,
	}
}

func supportFromBool(value bool) CapabilitySupport {
	if value {
		return CapabilitySupportSupported
	}
	return CapabilitySupportUnsupported
}

func buildCapabilityInventory(cfg SessionConfig, advertised []Tool, sandboxStatus SandboxStatus) []CapabilityCategory {
	categories := []CapabilityCategory{
		buildToolCategory(cfg, advertised),
		buildHookCategory(cfg),
		buildSandboxCategory(cfg, sandboxStatus),
		buildSubagentCategory(cfg, advertised),
	}

	for _, kind := range []CapabilityKind{CapabilityKindSkill, CapabilityKindMCP} {
		if category, ok := reportedCategory(kind, cfg.Provider, cfg.Dispatcher); ok {
			categories = append(categories, category)
		} else {
			categories = append(categories, CapabilityCategory{Kind: kind, Support: CapabilitySupportUnsupported})
		}
	}
	return categories
}

func buildToolCategory(cfg SessionConfig, advertised []Tool) CapabilityCategory {
	category := CapabilityCategory{Kind: CapabilityKindTool, Support: CapabilitySupportSupported, Source: "coragent"}
	if cfg.Dispatcher != nil {
		category.Support = CapabilitySupportUnknown
		category.Source = "caller"
		for _, descriptor := range advertised {
			category.Items = append(category.Items, Capability{
				Kind: CapabilityKindTool, Name: descriptor.Name, Source: "caller",
				Availability: CapabilityAvailabilityUnknown,
				Detail:       "advertised; execution is owned by the caller-supplied Dispatcher",
			})
		}
		return category
	}

	registeredOrder, sources := registeredToolFacts(cfg)
	registered := make(map[string]struct{}, len(registeredOrder))
	for _, name := range registeredOrder {
		registered[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(advertised))
	for _, descriptor := range advertised {
		if _, duplicate := seen[descriptor.Name]; duplicate {
			continue
		}
		seen[descriptor.Name] = struct{}{}
		availability := CapabilityAvailabilityUnavailable
		detail := "advertised without an executable handler"
		if _, ok := registered[descriptor.Name]; ok {
			availability = CapabilityAvailabilityAvailable
			detail = "advertised and executable"
		}
		category.Items = append(category.Items, Capability{
			Kind: CapabilityKindTool, Name: descriptor.Name, Source: sources[descriptor.Name],
			Availability: availability, Detail: detail,
		})
	}
	for _, name := range registeredOrder {
		if _, advertised := seen[name]; advertised {
			continue
		}
		category.Items = append(category.Items, Capability{
			Kind: CapabilityKindTool, Name: name, Source: sources[name],
			Availability: CapabilityAvailabilityUnavailable,
			Detail:       "registered but not advertised to the model",
		})
	}
	return category
}

func registeredToolFacts(cfg SessionConfig) ([]string, map[string]string) {
	var order []string
	sources := make(map[string]string)
	for _, handler := range tools.Builtins() {
		name := handler.Descriptor().Name
		order = append(order, name)
		sources[name] = "builtin"
	}
	hasTask := false
	for _, handler := range cfg.ToolHandlers {
		name := handler.Descriptor().Name
		order = append(order, name)
		sources[name] = "caller"
		if name == subagent.ToolName {
			hasTask = true
		}
	}
	if !hasTask {
		order = append(order, subagent.ToolName)
		sources[subagent.ToolName] = "subagent"
	}
	return order, sources
}

func buildHookCategory(cfg SessionConfig) CapabilityCategory {
	category := CapabilityCategory{Kind: CapabilityKindHook, Support: CapabilitySupportSupported, Source: "coragent"}
	for _, hook := range cfg.Hooks {
		category.Items = append(category.Items, Capability{
			Kind: CapabilityKindHook, Name: hook.Name, Source: "sdk",
			Availability: CapabilityAvailabilityAvailable, Detail: string(hook.Moment),
		})
	}
	for _, hook := range cfg.ExternalHooks {
		category.Items = append(category.Items, Capability{
			Kind: CapabilityKindHook, Name: hook.Name, Source: "settings",
			Availability: CapabilityAvailabilityAvailable, Detail: string(hook.Moment),
		})
	}
	return category
}

func buildSandboxCategory(cfg SessionConfig, status SandboxStatus) CapabilityCategory {
	if cfg.Dispatcher != nil {
		return CapabilityCategory{
			Kind: CapabilityKindSandbox, Support: CapabilitySupportUnknown, Source: "caller",
			Items: []Capability{{Kind: CapabilityKindSandbox, Name: "command sandbox", Source: "caller", Availability: CapabilityAvailabilityUnknown, Detail: "externally owned"}},
		}
	}
	detail := string(status.Level)
	if status.Reason != "" {
		detail += ": " + status.Reason
	}
	return CapabilityCategory{
		Kind: CapabilityKindSandbox, Support: CapabilitySupportSupported, Source: "coragent",
		Items: []Capability{{Kind: CapabilityKindSandbox, Name: "command sandbox", Source: "coragent", Availability: CapabilityAvailabilityAvailable, Detail: detail}},
	}
}

func buildSubagentCategory(cfg SessionConfig, advertised []Tool) CapabilityCategory {
	if cfg.Dispatcher != nil {
		return CapabilityCategory{Kind: CapabilityKindSubagent, Support: CapabilitySupportUnknown, Source: "caller"}
	}
	for _, handler := range cfg.ToolHandlers {
		if handler.Descriptor().Name == subagent.ToolName {
			return CapabilityCategory{
				Kind:    CapabilityKindSubagent,
				Support: CapabilitySupportUnknown,
				Source:  "caller",
				Items: []Capability{{
					Kind:         CapabilityKindSubagent,
					Name:         subagent.ToolName,
					Source:       "caller",
					Availability: CapabilityAvailabilityUnknown,
					Detail:       "a caller-owned task-shaped tool is registered; subagent semantics are not reported",
				}},
			}
		}
	}
	available := false
	for _, descriptor := range advertised {
		if descriptor.Name == subagent.ToolName {
			available = true
			break
		}
	}
	availability := CapabilityAvailabilityUnavailable
	detail := "task handler is not advertised"
	if available {
		availability = CapabilityAvailabilityAvailable
		detail = "task delegation is advertised and executable"
	}
	return CapabilityCategory{
		Kind: CapabilityKindSubagent, Support: CapabilitySupportSupported, Source: "coragent",
		Items: []Capability{{Kind: CapabilityKindSubagent, Name: "task", Source: "subagent", Availability: availability, Detail: detail}},
	}
}

func reportedCategory(kind CapabilityKind, candidates ...any) (CapabilityCategory, bool) {
	for _, candidate := range candidates {
		reporter, ok := candidate.(CapabilityReporter)
		if !ok {
			continue
		}
		for _, category := range reporter.CapabilityCategories() {
			if category.Kind == kind {
				return category.Clone(), true
			}
		}
	}
	return CapabilityCategory{}, false
}

func safeLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

var opaqueIDFallback atomic.Uint64

func newOpaqueID(prefix string) string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%s-%d", prefix, opaqueIDFallback.Add(1))
}
