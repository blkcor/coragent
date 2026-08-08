// Package policy provides command effect classification and execution policy.
// The EffectAnalyzer performs deterministic, hard-coded classification of
// commands into three risk tiers for the Policy Engine (S2.6).
package policy

import (
	"github.com/blkcor/coragent/internal/sandbox"
)

// EffectClassification grades the expected side-effect risk of a command.
type EffectClassification int

const (
	// EffectUnknown is the fail-closed zero value. Missing or uninitialized
	// classification must never become an automatic allow decision.
	EffectUnknown EffectClassification = iota
	// EffectSafe indicates a read-only command with no side effects.
	EffectSafe
	// EffectWorkspace indicates a command that may mutate the workspace.
	EffectWorkspace
	// EffectDangerous indicates a high-risk command that M2 policy always denies.
	// This classification cannot be overridden by model hints or session memory.
	EffectDangerous
)

func (e EffectClassification) String() string {
	switch e {
	case EffectUnknown:
		return "unknown"
	case EffectSafe:
		return "safe"
	case EffectWorkspace:
		return "workspace"
	case EffectDangerous:
		return "dangerous"
	default:
		return "unknown"
	}
}

// EffectAnalyzer classifies commands by expected side effects using hard-coded
// pattern rules. Classification is deterministic — rules are compile-time
// constants, not read from configuration files, to prevent model or user
// tampering.
//
// Pipeline and redirect parsing are NOT handled here; the caller (command tool
// prepare phase) is responsible for splitting pipelines and checking redirect
// targets. For a pipeline, the caller should classify each segment and take the
// maximum risk level.
type EffectAnalyzer struct{}

// NewEffectAnalyzer returns a ready-to-use analyzer.
func NewEffectAnalyzer() *EffectAnalyzer {
	return &EffectAnalyzer{}
}

// Classify determines the effect classification of a single command.
// Rules are checked in priority order: dangerous > workspace > safe.
// Commands that match no rule default to EffectWorkspace (conservative:
// when uncertain, treat as mutation requiring approval).
//
// The grants parameter is reserved for future use (e.g. cross-referencing
// redirect targets against allowed paths). It is not consulted by the
// current rule-based classifier.
func (a *EffectAnalyzer) Classify(cmd string, args []string, grants sandbox.Grants) EffectClassification {
	for _, r := range allRules {
		if matchRule(cmd, args, r) {
			return r.Classification
		}
	}
	return EffectWorkspace
}
