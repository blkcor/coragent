//go:build !windows

package nop

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/creack/pty"
)

// unixPTYManager implements sandbox.PTYManager using posix_openpt via the
// creack/pty library.
type unixPTYManager struct{}

// NewPTYManager creates a Unix PTY manager backed by posix_openpt.
func NewPTYManager() sandbox.PTYManager {
	return &unixPTYManager{}
}

func (m *unixPTYManager) Allocate() (*os.File, *os.File, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("pty open: %w", err)
	}
	return master, slave, nil
}

func (m *unixPTYManager) Resize(master *os.File, rows, cols int) error {
	if rows <= 0 || cols <= 0 {
		return nil
	}
	win := pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}
	if err := pty.Setsize(master, &win); err != nil {
		return fmt.Errorf("pty setsize: %w", err)
	}
	return nil
}

func (m *unixPTYManager) ReadLoop(ctx context.Context, master *os.File, buf io.Writer, maxBytes int64) error {
	_, err := io.Copy(buf, io.LimitReader(master, maxBytes))
	if err != nil {
		return err
	}
	return nil
}

var _ sandbox.PTYManager = (*unixPTYManager)(nil)
