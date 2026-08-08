// Package nop provides a Sandbox implementation that uses os/exec with
// process-group management and optional PTY allocation. It does not enforce
// filesystem or network isolation — callers must check ConfinementLevel()
// before executing untrusted commands.
package nop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/blkcor/coragent/internal/sandbox"
)

var _ sandbox.Sandbox = (*Sandbox)(nil)

// Sandbox is a NOP sandbox that provides process-group isolation and I/O
// control without kernel-level confinement.
type Sandbox struct {
	pty sandbox.PTYManager
}

// New creates a NOP sandbox with the given PTYManager. If pty is nil, PTY
// allocation is disabled and commands use pipe-based I/O.
func New(pty sandbox.PTYManager) *Sandbox {
	return &Sandbox{pty: pty}
}

// ConfinementLevel returns ConfinementProcess.
func (s *Sandbox) ConfinementLevel() sandbox.ConfinementLevel {
	return sandbox.ConfinementProcess
}

// Start launches a command asynchronously. It validates the spec, builds an
// os/exec command with a clean environment, optionally allocates a PTY, and
// starts the process. On timeout or context cancellation, the entire process
// group receives SIGKILL.
func (s *Sandbox) Start(ctx context.Context, spec sandbox.CommandSpec) (sandbox.Process, error) {
	if spec.Command == "" {
		return nil, errors.New("nop sandbox: command is required")
	}
	if spec.Timeout <= 0 {
		return nil, errors.New("nop sandbox: timeout must be positive")
	}
	if spec.MaxOutputBytes <= 0 {
		spec.MaxOutputBytes = 64 * 1024
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Use plain exec.Command — we manage cancellation and timeouts ourselves
	// via process groups, avoiding interference from CommandContext's internal
	// kill goroutine.
	//
	//nolint:gosec // command execution from user-supplied spec is the sandbox's intended purpose
	execCmd := exec.Command(spec.Command, spec.Args...)
	execCmd.Dir = spec.CWD
	execCmd.Env = spec.Env
	execCmd.SysProcAttr = sysProcAttr()

	var ptyMaster *os.File
	var stdoutReader, stderrReader io.ReadCloser

	if spec.PTY && s.pty != nil {
		master, slave, err := s.pty.Allocate()
		if err != nil {
			return nil, fmt.Errorf("nop sandbox: pty allocate: %w", err)
		}
		ptyMaster = master
		execCmd.Stdin = slave
		execCmd.Stdout = slave
		execCmd.Stderr = slave
		execCmd.SysProcAttr = ptySysProcAttr()
		defer func() { _ = slave.Close() }()
	} else {
		var err error
		stdoutReader, err = execCmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("nop sandbox: stdout pipe: %w", err)
		}
		stderrReader, err = execCmd.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("nop sandbox: stderr pipe: %w", err)
		}
	}

	if err := execCmd.Start(); err != nil {
		if ptyMaster != nil {
			_ = ptyMaster.Close()
		}
		return nil, fmt.Errorf("nop sandbox: start %s: %w", spec.Command, err)
	}

	p := &process{
		cmd:       execCmd,
		spec:      spec,
		ptyMaster: ptyMaster,
		ptyMgr:    s.pty,
		doneCh:    make(chan struct{}),
	}

	if ptyMaster != nil {
		go p.readPTY(ctx)
	} else {
		go p.readPipes(stdoutReader, stderrReader)
	}
	go p.watchTimeout(ctx)

	return p, nil
}

// process implements sandbox.Process for an os/exec command.
type process struct {
	cmd       *exec.Cmd
	spec      sandbox.CommandSpec
	ptyMaster *os.File
	ptyMgr    sandbox.PTYManager

	mu     sync.Mutex
	result sandbox.ProcessResult
	doneCh chan struct{}
}

func (p *process) PID() int {
	if p.cmd.Process == nil {
		return -1
	}
	return p.cmd.Process.Pid
}

func (p *process) Done() <-chan struct{} {
	return p.doneCh
}

func (p *process) Result() sandbox.ProcessResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}

func (p *process) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("nop sandbox: process not started")
	}
	return p.cmd.Process.Signal(sig)
}

func (p *process) ResizePTY(rows, cols int) error {
	if p.ptyMaster == nil || p.ptyMgr == nil {
		return nil
	}
	return p.ptyMgr.Resize(p.ptyMaster, rows, cols)
}

func (p *process) readPipes(stdoutPipe, stderrPipe io.ReadCloser) {
	defer close(p.doneCh)

	var (
		stdoutBuf bytes.Buffer
		stderrBuf bytes.Buffer
		wg        sync.WaitGroup
	)
	wg.Add(2)

	go func() {
		defer wg.Done()
		copyBounded(&stdoutBuf, stdoutPipe, p.spec.MaxOutputBytes/2)
	}()
	go func() {
		defer wg.Done()
		copyBounded(&stderrBuf, stderrPipe, p.spec.MaxOutputBytes/2)
	}()

	wg.Wait()
	waitErr := p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Merge stdout + stderr.
	merged := make([]byte, 0, stdoutBuf.Len()+stderrBuf.Len())
	merged = append(merged, stdoutBuf.Bytes()...)
	merged = append(merged, stderrBuf.Bytes()...)
	if int64(len(merged)) > p.spec.MaxOutputBytes {
		merged = merged[:p.spec.MaxOutputBytes]
	}
	p.result.Stdout = merged
	p.result.PID = p.PID()

	if p.cmd.ProcessState != nil {
		p.result.ExitCode = p.cmd.ProcessState.ExitCode()
	}
	if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
		p.result.Error = waitErr
	}
}

func (p *process) readPTY(ctx context.Context) {
	defer close(p.doneCh)
	defer func() { _ = p.ptyMaster.Close() }()

	var output bytes.Buffer

	done := make(chan struct{})
	var readErr error
	go func() {
		defer close(done)
		readErr = p.ptyMgr.ReadLoop(ctx, p.ptyMaster, &output, p.spec.MaxOutputBytes)
	}()

	// Wait for either the ReadLoop to finish or the process to exit.
	select {
	case <-done:
	case <-ctx.Done():
	}

	_ = p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	outputBytes := output.Bytes()
	if int64(len(outputBytes)) > p.spec.MaxOutputBytes {
		outputBytes = outputBytes[:p.spec.MaxOutputBytes]
	}
	p.result.Stdout = outputBytes
	p.result.PID = p.PID()

	if p.cmd.ProcessState != nil {
		p.result.ExitCode = p.cmd.ProcessState.ExitCode()
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, context.Canceled) {
		p.result.Error = readErr
	}
}

func (p *process) watchTimeout(ctx context.Context) {
	select {
	case <-ctx.Done():
		p.killProcessGroup()
	case <-time.After(p.spec.Timeout):
		p.killProcessGroup()
	case <-p.doneCh:
		return
	}
}

func (p *process) killProcessGroup() {
	pid := p.PID()
	if pid <= 0 {
		return
	}
	signalProcessGroup(pid, p.cmd.Process)

	p.mu.Lock()
	p.result.Signaled = true
	p.mu.Unlock()
}

func copyBounded(dst io.Writer, src io.Reader, maxBytes int64) {
	if src == nil {
		return
	}
	_, _ = io.Copy(dst, io.LimitReader(src, maxBytes))
}
