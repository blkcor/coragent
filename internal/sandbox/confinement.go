package sandbox

// ConfinementLevel grades the isolation strength of the active sandbox backend.
// Every command tool path checks this level before execution and may refuse to
// run certain command classes when the level is below the required threshold.
type ConfinementLevel int

const (
	// ConfinementNone means no isolation of any kind. Only safe for read-only
	// diagnostic commands in fully trusted environments.
	ConfinementNone ConfinementLevel = iota

	// ConfinementProcess provides process-group management, environment cleanup,
	// and bounded output buffering. It does not enforce filesystem or network
	// restrictions. This is the NOP sandbox level.
	ConfinementProcess

	// ConfinementKernel enforces OS-level mandatory isolation (macOS Seatbelt,
	// Linux Landlock+seccomp). This is the minimum level required for untrusted
	// command execution.
	ConfinementKernel
)

func (c ConfinementLevel) String() string {
	switch c {
	case ConfinementNone:
		return "none"
	case ConfinementProcess:
		return "process"
	case ConfinementKernel:
		return "kernel"
	default:
		return "unknown"
	}
}
