package sandbox

import "time"

// Grants declares the filesystem paths and network access a command is allowed.
// NOP sandbox does not enforce grants; platform sandboxes (macOS/Linux) use
// them to construct kernel-level restrictions.
type Grants struct {
	AllowedReadPaths  []string
	AllowedWritePaths []string
	// Network enables outbound network access. Default (false) blocks all
	// socket syscalls in kernel-level sandboxes (seccomp on Linux, Seatbelt
	// on macOS). Set to true when the command legitimately needs network.
	Network bool
}

// CommandSpec carries all parameters needed to execute a command in a sandbox.
// Env is an explicit minimal set — the caller is responsible for building it.
// The sandbox never inherits the host environment or ambient credentials.
type CommandSpec struct {
	Command        string
	Args           []string
	CWD            string
	Env            []string
	Timeout        time.Duration
	MaxOutputBytes int64
	PTY            bool
	Grants         Grants
}

// ProcessResult is the final state of a completed process.
type ProcessResult struct {
	PID      int
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	Signaled bool
	Error    error
}
