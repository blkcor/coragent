package policy

import "github.com/blkcor/coragent/internal/sandbox"

// PolicyVersion participates in command approval identity. Changing policy
// semantics invalidates identities prepared under the previous version.
const PolicyVersion = "m2-s2.6-v1"

// CommandSpec is the effective command evaluated by the Policy Engine. Keep
// the execution contract owned by sandbox while giving policy callers the
// product-vocabulary name used by the decision API.
type CommandSpec = sandbox.CommandSpec

// PolicyDecisionKind is the action selected by the Policy Engine.
type PolicyDecisionKind string

const (
	PolicyAllow   PolicyDecisionKind = "allow"
	PolicyApprove PolicyDecisionKind = "approve"
	PolicyDeny    PolicyDecisionKind = "deny"
)

// PolicyDecision combines the machine-readable decision with the reason that
// is safe to expose through Events and the durable transcript.
type PolicyDecision struct {
	Kind           PolicyDecisionKind
	Reason         string
	Prefix         string
	IdentityDigest string

	// memory binds an approval decision to the SessionMemory consulted by
	// Decide. It is intentionally process-local and never serialized.
	memory *SessionMemory
}

// Is reports whether the decision has the requested kind.
func (d PolicyDecision) Is(kind PolicyDecisionKind) bool {
	return d.Kind == kind
}
