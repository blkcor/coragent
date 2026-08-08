//go:build windows

package nop

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/blkcor/coragent/internal/sandbox"
)

// windowsPTYManager implements sandbox.PTYManager using os.Pipe pairs as a
// PTY fallback on Windows. ConPTY support (Windows 10 1809+) is deferred to
// the platform sandbox document (03-windows-sandbox.md).
type windowsPTYManager struct{}

// NewPTYManager creates a pipe-based PTY fallback for Windows.
func NewPTYManager() sandbox.PTYManager {
	return &windowsPTYManager{}
}

func (m *windowsPTYManager) Allocate() (*os.File, *os.File, error) {
	// Create a pipe pair for process output: process writes to slaveW, we
	// read from masterR.
	masterR, slaveW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("pipe: %w", err)
	}
	// Create a pipe pair for process input: we write to masterW, process
	// reads from slaveR. Unused in non-interactive mode but required for
	// a complete PTY abstraction.
	slaveR, masterW, err := os.Pipe()
	if err != nil {
		_ = masterR.Close()
		_ = slaveW.Close()
		return nil, nil, err
	}
	// Combine master read/write into a single *os.File by using the read
	// end as the primary FD. Process stdin is not used in the NOP sandbox;
	// the write end is closed after the process exits.
	_ = masterW.Close()
	_ = slaveR.Close()
	return masterR, slaveW, nil
}

func (m *windowsPTYManager) Resize(master *os.File, rows, cols int) error {
	return nil
}

func (m *windowsPTYManager) ReadLoop(ctx context.Context, master *os.File, buf io.Writer, maxBytes int64) error {
	_, err := io.Copy(buf, io.LimitReader(master, maxBytes))
	if err != nil {
		return err
	}
	return nil
}

var _ sandbox.PTYManager = (*windowsPTYManager)(nil)
