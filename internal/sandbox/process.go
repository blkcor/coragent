package sandbox

import "os"

// Process represents a running command inside a sandbox. The caller must read
// from Done() to be notified of completion, then call Result() exactly once.
type Process interface {
	// PID returns the OS process ID of the sandboxed process.
	PID() int

	// Done returns a channel that closes when the process exits. The caller
	// must not close or write to this channel.
	Done() <-chan struct{}

	// Result returns the final process state. It must only be called after
	// Done() has closed.
	Result() ProcessResult

	// Signal sends an OS signal to the process or its process group.
	Signal(os.Signal) error

	// ResizePTY resizes the PTY window. It is a no-op for non-PTY processes.
	ResizePTY(rows, cols int) error
}
