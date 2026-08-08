package sandbox

import (
	"context"
	"io"
	"os"
)

// PTYManager abstracts platform-specific pseudo-terminal allocation and I/O.
// Unix uses posix_openpt (via creack/pty); Windows ≥1809 uses ConPTY; older
// Windows falls back to pipes. Callers only see master/slave file descriptors.
type PTYManager interface {
	// Allocate creates a new PTY pair. The caller writes to master and the
	// sandboxed process reads from slave.
	Allocate() (master *os.File, slave *os.File, err error)

	// Resize sets the window size of the PTY identified by master.
	Resize(master *os.File, rows, cols int) error

	// ReadLoop copies from master to buf until the process exits or the
	// context is cancelled. It stops when maxBytes is exceeded, returning
	// no error.
	ReadLoop(ctx context.Context, master *os.File, buf io.Writer, maxBytes int64) error
}
