//go:build windows

package windows

import (
	"context"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"github.com/blkcor/coragent/internal/sandbox"
	"golang.org/x/sys/windows"
)

var _ sandbox.PTYManager = (*ptyManager)(nil)

type ptyManager struct {
	useConPTY bool
	hpc       syscall.Handle
}

func NewPTYManager() sandbox.PTYManager {
	return &ptyManager{useConPTY: supportsConPTY()}
}

func (m *ptyManager) Allocate() (*os.File, *os.File, error) {
	if m.useConPTY {
		return m.allocateConPTY()
	}
	return m.allocatePipe()
}

func (m *ptyManager) allocatePipe() (*os.File, *os.File, error) {
	masterR, slaveW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("windows pty: pipe: %w", err)
	}
	slaveR, masterW, err := os.Pipe()
	if err != nil {
		_ = masterR.Close()
		_ = slaveW.Close()
		return nil, nil, fmt.Errorf("windows pty: pipe: %w", err)
	}
	_ = masterW.Close()
	_ = slaveR.Close()
	return masterR, slaveW, nil
}

func (m *ptyManager) allocateConPTY() (*os.File, *os.File, error) {
	coninR, coninW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("windows pty: conin pipe: %w", err)
	}
	conoutR, conoutW, err := os.Pipe()
	if err != nil {
		_ = coninR.Close()
		_ = coninW.Close()
		return nil, nil, fmt.Errorf("windows pty: conout pipe: %w", err)
	}

	var hpc syscall.Handle
	if err := createPseudoConsole(
		conptyCoord{X: 80, Y: 24},
		syscall.Handle(coninR.Fd()),
		syscall.Handle(conoutW.Fd()),
		0,
		&hpc,
	); err != nil {
		_ = coninR.Close()
		_ = coninW.Close()
		_ = conoutR.Close()
		_ = conoutW.Close()
		return nil, nil, fmt.Errorf("windows pty: CreatePseudoConsole: %w", err)
	}
	m.hpc = hpc

	// coninR and conoutW handles are now owned by the ConPTY. Close the Go
	// *os.File wrappers without closing the underlying handles.
	_ = coninR.Close()
	_ = conoutW.Close()

	return conoutR, coninW, nil
}

func (m *ptyManager) Resize(master *os.File, rows, cols int) error {
	if !m.useConPTY {
		return nil
	}
	return resizePseudoConsole(m.hpc, conptyCoord{X: int16(cols), Y: int16(rows)})
}

func (m *ptyManager) ReadLoop(ctx context.Context, master *os.File, buf io.Writer, maxBytes int64) error {
	handle := syscall.Handle(master.Fd())
	readBuf := make([]byte, 4096)
	written := int64(0)

	for written < maxBytes {
		done := make(chan struct{})
		var n uint32
		var readErr error

		go func() {
			defer close(done)
			n, readErr = readFileChunk(handle, readBuf)
		}()

		select {
		case <-done:
			if readErr != nil {
				if readErr == io.EOF {
					return nil
				}
				return readErr
			}
			if n == 0 {
				return nil
			}
			toWrite := int64(n)
			if written+toWrite > maxBytes {
				toWrite = maxBytes - written
			}
			if _, err := buf.Write(readBuf[:toWrite]); err != nil {
				return err
			}
			written += toWrite
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// readFileChunk reads from the ConPTY output pipe, translating Windows error
// codes to Go sentinel errors.
func readFileChunk(handle syscall.Handle, buf []byte) (uint32, error) {
	var n uint32
	err := windows.ReadFile(windows.Handle(handle), buf, &n, nil)
	if err != nil {
		// ERROR_BROKEN_PIPE (109) and ERROR_NO_DATA (232) signal EOF.
		if errno, ok := err.(syscall.Errno); ok {
			if errno == 109 || errno == 232 { // ERROR_BROKEN_PIPE, ERROR_NO_DATA
				return 0, io.EOF
			}
		}
		return 0, err
	}
	return n, nil
}

// buildProcThreadAttribute creates a ProcThreadAttributeList containing the
// ConPTY HPCON for use with STARTUPINFOEX in CreateProcess. Returns nil on
// the pipe fallback path.
func (m *ptyManager) buildProcThreadAttribute() (*windows.ProcThreadAttributeListContainer, error) {
	if !m.useConPTY {
		return nil, nil
	}
	attr, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("windows pty: NewProcThreadAttributeList: %w", err)
	}
	hpc := m.hpc
	//nolint:gosec // unsafe.Pointer required for Windows ProcThreadAttributeList API
	if err := attr.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(&hpc), unsafe.Sizeof(hpc)); err != nil {
		attr.Delete()
		return nil, fmt.Errorf("windows pty: Update(PSEUDOCONSOLE): %w", err)
	}
	return attr, nil
}

func (m *ptyManager) closeHPCON() {
	if m.hpc != 0 {
		closePseudoConsole(m.hpc)
		m.hpc = 0
	}
}

// ---- ConPTY syscalls (not in golang.org/x/sys/windows) ----

type conptyCoord struct {
	X int16
	Y int16
}

var (
	modKernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreatePseudoConsole = modKernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole = modKernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole  = modKernel32.NewProc("ClosePseudoConsole")
)

//nolint:gosec // unsafe.Pointer required for COORD-to-uint32 packing in Windows API
func createPseudoConsole(size conptyCoord, hInput, hOutput syscall.Handle, flags uint32, hpc *syscall.Handle) error {
	// COORD is 4 bytes (two int16), packed into a uint32 by value.
	r0, _, _ := procCreatePseudoConsole.Call(
		uintptr(*(*uint32)(unsafe.Pointer(&size))),
		uintptr(hInput),
		uintptr(hOutput),
		uintptr(flags),
		uintptr(unsafe.Pointer(hpc)),
	)
	if r0 != 0 {
		return fmt.Errorf("HRESULT 0x%x", r0)
	}
	return nil
}

func resizePseudoConsole(hpc syscall.Handle, size conptyCoord) error {
	//nolint:gosec // unsafe.Pointer required for COORD-to-uint32 packing in Windows API
	r0, _, _ := procResizePseudoConsole.Call(
		uintptr(hpc),
		uintptr(*(*uint32)(unsafe.Pointer(&size))),
	)
	if r0 != 0 {
		return fmt.Errorf("HRESULT 0x%x", r0)
	}
	return nil
}

func closePseudoConsole(hpc syscall.Handle) {
	_, _, _ = procClosePseudoConsole.Call(uintptr(hpc))
}
