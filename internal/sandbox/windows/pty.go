//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/blkcor/coragent/internal/sandbox"
	"golang.org/x/sys/windows"
)

var _ sandbox.PTYManager = (*ptyManager)(nil)

type ptyManager struct {
	useConPTY bool
	mu        sync.Mutex
	states    map[uintptr]*conPTYState
}

type conPTYState struct {
	hpc         syscall.Handle
	inputRead   *os.File
	outputWrite *os.File
}

func NewPTYManager() sandbox.PTYManager {
	return &ptyManager{useConPTY: supportsConPTY(), states: make(map[uintptr]*conPTYState)}
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
	m.mu.Lock()
	if m.states == nil {
		m.states = make(map[uintptr]*conPTYState)
	}
	m.states[conoutR.Fd()] = &conPTYState{
		hpc:         hpc,
		inputRead:   coninR,
		outputWrite: conoutW,
	}
	m.mu.Unlock()

	return conoutR, coninW, nil
}

func (m *ptyManager) Resize(master *os.File, rows, cols int) error {
	if !m.useConPTY {
		return nil
	}
	state, err := m.stateFor(master)
	if err != nil {
		return err
	}
	return resizePseudoConsole(state.hpc, conptyCoord{X: int16(cols), Y: int16(rows)})
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
func (m *ptyManager) buildProcThreadAttribute(master *os.File) (*windows.ProcThreadAttributeListContainer, error) {
	if !m.useConPTY {
		return nil, nil
	}
	state, err := m.stateFor(master)
	if err != nil {
		return nil, err
	}
	attr, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("windows pty: NewProcThreadAttributeList: %w", err)
	}
	hpc := state.hpc
	if err := attr.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, pseudoConsoleAttributeValue(hpc), unsafe.Sizeof(hpc)); err != nil {
		attr.Delete()
		return nil, fmt.Errorf("windows pty: Update(PSEUDOCONSOLE): %w", err)
	}
	return attr, nil
}

//go:nocheckptr
func pseudoConsoleAttributeValue(hpc syscall.Handle) unsafe.Pointer {
	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE takes the HPCON value itself as
	// lpValue, unlike attributes whose value is stored behind a pointer.
	return unsafe.Pointer(hpc)
}

func (m *ptyManager) releasePseudoConsolePipes(master *os.File) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[master.Fd()]
	if state == nil {
		return
	}
	if state.inputRead != nil {
		_ = state.inputRead.Close()
		state.inputRead = nil
	}
	if state.outputWrite != nil {
		_ = state.outputWrite.Close()
		state.outputWrite = nil
	}
}

func (m *ptyManager) closeHPCON(master *os.File) {
	m.mu.Lock()
	state := m.states[master.Fd()]
	delete(m.states, master.Fd())
	m.mu.Unlock()
	if state == nil {
		return
	}
	if state.inputRead != nil {
		_ = state.inputRead.Close()
	}
	if state.outputWrite != nil {
		_ = state.outputWrite.Close()
	}
	if state.hpc != 0 {
		closePseudoConsole(state.hpc)
	}
}

func (m *ptyManager) stateFor(master *os.File) (*conPTYState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[master.Fd()]
	if state == nil {
		return nil, errors.New("windows pty: unknown ConPTY allocation")
	}
	return state, nil
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
