package policy

import (
	"context"
	"fmt"
)

// PolicyEngine maps deterministic effect classifications and process-local
// Session state to allow, approve, or deny decisions. It performs no I/O.
type PolicyEngine struct {
	state *SessionState
}

// NewPolicyEngine constructs a Policy Engine. Passing no state creates fresh
// session-local memory; callers may pass one state to make ownership explicit.
func NewPolicyEngine(states ...*SessionState) *PolicyEngine {
	state := NewSessionState()
	if len(states) > 0 && states[0] != nil {
		state = states[0]
		_ = state.memory()
	}
	return &PolicyEngine{state: state}
}

// Decide applies the fixed M2 policy matrix. The optional session argument
// takes precedence over the engine's owned state and lets tests and runtimes
// prove that approval memory does not cross Session boundaries.
func (e *PolicyEngine) Decide(ctx context.Context, cmd CommandSpec, effect EffectClassification, session *SessionState) PolicyDecision {
	if err := ctx.Err(); err != nil {
		return PolicyDecision{Kind: PolicyDeny, Reason: "policy decision cancelled"}
	}
	memory := e.sessionMemory(session)
	prefix := ApprovalPrefix(cmd.Command, cmd.Args)
	identityDigest := CommandIdentityDigest(cmd)
	switch effect {
	case EffectDangerous:
		return PolicyDecision{
			Kind:   PolicyDeny,
			Reason: fmt.Sprintf("dangerous command: %q is denied by policy", cmd.Command),
			Prefix: prefix, IdentityDigest: identityDigest, memory: memory,
		}
	case EffectSafe:
		return PolicyDecision{
			Kind:   PolicyAllow,
			Reason: "safe read-only command",
			Prefix: prefix, IdentityDigest: identityDigest, memory: memory,
		}
	case EffectWorkspace:
		if memory != nil && memory.isApprovedSpec(cmd) {
			return PolicyDecision{
				Kind:   PolicyAllow,
				Reason: fmt.Sprintf("previously approved workspace command: %q", prefix),
				Prefix: prefix, IdentityDigest: identityDigest, memory: memory,
			}
		}
		return PolicyDecision{
			Kind:   PolicyApprove,
			Reason: "workspace mutation requires approval",
			Prefix: prefix, IdentityDigest: identityDigest, memory: memory,
		}
	default:
		return PolicyDecision{
			Kind:   PolicyApprove,
			Reason: "unrecognized command, requires approval",
			Prefix: prefix, IdentityDigest: identityDigest, memory: memory,
		}
	}
}

// RecordApproval remembers only an actual approve decision and only in the
// SessionMemory that produced it. Dangerous commands remain denied and safe
// commands never create memory entries.
func (e *PolicyEngine) RecordApproval(cmd CommandSpec, decision PolicyDecision) {
	if decision.Kind != PolicyApprove {
		return
	}
	prefix := ApprovalPrefix(cmd.Command, cmd.Args)
	if prefix == "" || (decision.Prefix != "" && decision.Prefix != prefix) {
		return
	}
	identityDigest := CommandIdentityDigest(cmd)
	if decision.IdentityDigest == "" || decision.IdentityDigest != identityDigest {
		return
	}
	memory := decision.memory
	if memory == nil {
		memory = e.sessionMemory(nil)
	}
	if memory != nil {
		memory.markApproved(cmd, identityDigest)
	}
}

func (e *PolicyEngine) sessionMemory(session *SessionState) *SessionMemory {
	if session != nil {
		return session.memory()
	}
	if e == nil {
		return nil
	}
	return e.state.memory()
}
