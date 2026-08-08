// Package sandbox defines the platform-independent command execution interface.
// Implementations range from NOP (process-group only) to kernel-level isolation
// (macOS Seatbelt, Linux Landlock+seccomp).
package sandbox

import "context"

// Sandbox is the platform-independent command execution interface. Every
// command tool invocation goes through a sandbox backend. The caller must
// check ConfinementLevel() to decide whether the current isolation level
// meets the policy requirements for a given command class.
type Sandbox interface {
	// Start launches a command asynchronously. The returned Process is
	// running when this call returns without error.
	Start(ctx context.Context, spec CommandSpec) (Process, error)

	// ConfinementLevel reports the isolation strength of this sandbox.
	ConfinementLevel() ConfinementLevel
}
