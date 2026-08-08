//go:build windows

package windows

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/blkcor/coragent/internal/sandbox"
	"golang.org/x/sys/windows"
)

var _ sandbox.Sandbox = (*Sandbox)(nil)

// Sandbox implements sandbox.Sandbox on Windows with Job Object process-group
// management and optional ConPTY support.
type Sandbox struct {
	pty *ptyManager
}

func New(pty sandbox.PTYManager) *Sandbox {
	pm, _ := pty.(*ptyManager)
	return &Sandbox{pty: pm}
}

func (s *Sandbox) ConfinementLevel() sandbox.ConfinementLevel {
	return sandbox.ConfinementProcess
}

func (s *Sandbox) Start(ctx context.Context, spec sandbox.CommandSpec) (sandbox.Process, error) {
	if spec.Command == "" {
		return nil, errors.New("windows sandbox: command is required")
	}
	if spec.Timeout <= 0 {
		return nil, errors.New("windows sandbox: timeout must be positive")
	}
	if spec.MaxOutputBytes <= 0 {
		spec.MaxOutputBytes = 64 * 1024
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if spec.PTY && s.pty != nil && s.pty.useConPTY {
		return s.startWithConPTY(ctx, spec)
	}
	return s.startWithExec(ctx, spec)
}

// startWithExec uses os/exec.Cmd with Job Object management. This path handles
// non-PTY commands and the pipe-fallback PTY path on older Windows builds.
func (s *Sandbox) startWithExec(ctx context.Context, spec sandbox.CommandSpec) (sandbox.Process, error) {
	//nolint:gosec
	execCmd := exec.Command(spec.Command, spec.Args...)
	execCmd.Dir = spec.CWD
	execCmd.Env = spec.Env
	execCmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	var ptyMaster *os.File
	var stdoutReader, stderrReader io.ReadCloser

	if spec.PTY && s.pty != nil {
		master, slave, err := s.pty.Allocate()
		if err != nil {
			return nil, fmt.Errorf("windows sandbox: pty allocate: %w", err)
		}
		ptyMaster = master
		execCmd.Stdin = slave
		execCmd.Stdout = slave
		execCmd.Stderr = slave
		defer func() { _ = slave.Close() }()
	} else {
		var err error
		stdoutReader, err = execCmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("windows sandbox: stdout pipe: %w", err)
		}
		stderrReader, err = execCmd.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("windows sandbox: stderr pipe: %w", err)
		}
	}

	jobObject, err := createKillOnCloseJob()
	if err != nil {
		if ptyMaster != nil {
			_ = ptyMaster.Close()
		}
		return nil, err
	}

	if err := execCmd.Start(); err != nil {
		_ = windows.CloseHandle(jobObject)
		if ptyMaster != nil {
			_ = ptyMaster.Close()
		}
		return nil, fmt.Errorf("windows sandbox: start %s: %w", spec.Command, err)
	}

	procHandle, err := openProcessHandle(execCmd.Process.Pid)
	if err != nil {
		_ = windows.TerminateJobObject(jobObject, 1)
		_ = windows.CloseHandle(jobObject)
		_ = execCmd.Process.Kill()
		_ = execCmd.Wait()
		if ptyMaster != nil {
			_ = ptyMaster.Close()
		}
		return nil, fmt.Errorf("windows sandbox: OpenProcess: %w", err)
	}
	if err := windows.AssignProcessToJobObject(jobObject, procHandle); err != nil {
		_ = windows.CloseHandle(procHandle)
		_ = windows.TerminateJobObject(jobObject, 1)
		_ = windows.CloseHandle(jobObject)
		_ = execCmd.Process.Kill()
		_ = execCmd.Wait()
		if ptyMaster != nil {
			_ = ptyMaster.Close()
		}
		return nil, fmt.Errorf("windows sandbox: AssignProcessToJobObject: %w", err)
	}
	_ = windows.CloseHandle(procHandle)

	p := &process{
		cmd:       execCmd,
		spec:      spec,
		ptyMaster: ptyMaster,
		ptyMgr:    s.pty,
		jobObject: jobObject,
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

// startWithConPTY creates the child process via windows.CreateProcess so the
// ConPTY handles can be passed through STARTUPINFOEX.
func (s *Sandbox) startWithConPTY(ctx context.Context, spec sandbox.CommandSpec) (sandbox.Process, error) {
	master, slave, err := s.pty.Allocate()
	if err != nil {
		return nil, fmt.Errorf("windows sandbox: pty allocate: %w", err)
	}
	defer func() { _ = slave.Close() }()

	attr, err := s.pty.buildProcThreadAttribute()
	if err != nil {
		_ = master.Close()
		return nil, fmt.Errorf("windows sandbox: proc thread attribute: %w", err)
	}
	defer attr.Delete()

	jobObject, err := createKillOnCloseJob()
	if err != nil {
		_ = master.Close()
		return nil, err
	}

	cmdLine, err := windows.UTF16PtrFromString(buildCommandLine(spec.Command, spec.Args))
	if err != nil {
		_ = master.Close()
		_ = windows.CloseHandle(jobObject)
		return nil, fmt.Errorf("windows sandbox: command line: %w", err)
	}

	envBlock := buildEnvBlock(spec.Env)

	var cwdPtr *uint16
	if spec.CWD != "" {
		cwdPtr, err = windows.UTF16PtrFromString(spec.CWD)
		if err != nil {
			_ = master.Close()
			_ = windows.CloseHandle(jobObject)
			return nil, fmt.Errorf("windows sandbox: cwd: %w", err)
		}
	}

	startupInfo := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  windows.Handle(slave.Fd()),
			StdOutput: windows.Handle(slave.Fd()),
			StdErr:    windows.Handle(slave.Fd()),
		},
		ProcThreadAttributeList: attr.List(),
	}

	var procInfo windows.ProcessInformation
	err = windows.CreateProcess(
		nil,
		cmdLine,
		nil,
		nil,
		false,
		windows.CREATE_NEW_PROCESS_GROUP|windows.EXTENDED_STARTUPINFO_PRESENT,
		envBlock,
		cwdPtr,
		&startupInfo.StartupInfo,
		&procInfo,
	)
	if err != nil {
		_ = master.Close()
		_ = windows.CloseHandle(jobObject)
		return nil, fmt.Errorf("windows sandbox: CreateProcess %s: %w", spec.Command, err)
	}

	_ = windows.CloseHandle(procInfo.Thread)

	if err := windows.AssignProcessToJobObject(jobObject, procInfo.Process); err != nil {
		_ = windows.TerminateJobObject(jobObject, 1)
		_ = windows.CloseHandle(jobObject)
		_ = windows.CloseHandle(procInfo.Process)
		_ = master.Close()
		return nil, fmt.Errorf("windows sandbox: AssignProcessToJobObject: %w", err)
	}

	proc := osProcessFromHandle(procInfo.Process, int(procInfo.ProcessId))

	p := &process{
		osProcess: proc,
		spec:      spec,
		ptyMaster: master,
		ptyMgr:    s.pty,
		jobObject: jobObject,
		doneCh:    make(chan struct{}),
	}

	go p.readPTY(ctx)
	go p.watchTimeout(ctx)

	return p, nil
}

// ---- process ----

type process struct {
	cmd       *exec.Cmd   // set on os/exec path
	osProcess *os.Process // set on CreateProcess path (ConPTY)
	spec      sandbox.CommandSpec
	ptyMaster *os.File
	ptyMgr    *ptyManager
	jobObject windows.Handle

	mu     sync.Mutex
	result sandbox.ProcessResult
	doneCh chan struct{}
}

func (p *process) PID() int {
	if p.osProcess != nil {
		return p.osProcess.Pid
	}
	if p.cmd.Process == nil {
		return -1
	}
	return p.cmd.Process.Pid
}

func (p *process) Done() <-chan struct{} { return p.doneCh }

func (p *process) Result() sandbox.ProcessResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}

func (p *process) Signal(sig os.Signal) error {
	if p.osProcess != nil {
		return p.osProcess.Signal(sig)
	}
	if p.cmd.Process == nil {
		return errors.New("windows sandbox: process not started")
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
	defer func() { _ = windows.CloseHandle(p.jobObject) }()

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

	var waitErr error
	if p.cmd != nil {
		waitErr = p.cmd.Wait()
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	merged := make([]byte, 0, stdoutBuf.Len()+stderrBuf.Len())
	merged = append(merged, stdoutBuf.Bytes()...)
	merged = append(merged, stderrBuf.Bytes()...)
	if int64(len(merged)) > p.spec.MaxOutputBytes {
		merged = merged[:p.spec.MaxOutputBytes]
	}
	p.result.Stdout = merged
	p.result.PID = p.PID()

	if p.cmd != nil && p.cmd.ProcessState != nil {
		p.result.ExitCode = p.cmd.ProcessState.ExitCode()
	}
	if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
		p.result.Error = waitErr
	}
}

func (p *process) readPTY(ctx context.Context) {
	defer close(p.doneCh)
	defer func() {
		_ = p.ptyMaster.Close()
		_ = windows.CloseHandle(p.jobObject)
		if p.ptyMgr != nil {
			p.ptyMgr.closeHPCON()
		}
	}()

	var output bytes.Buffer

	done := make(chan struct{})
	var readErr error
	go func() {
		defer close(done)
		readErr = p.ptyMgr.ReadLoop(ctx, p.ptyMaster, &output, p.spec.MaxOutputBytes)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}

	// Wait for the process on both paths.
	if p.cmd != nil {
		_ = p.cmd.Wait()
	} else if p.osProcess != nil {
		_, _ = p.osProcess.Wait()
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	outputBytes := output.Bytes()
	if int64(len(outputBytes)) > p.spec.MaxOutputBytes {
		outputBytes = outputBytes[:p.spec.MaxOutputBytes]
	}
	p.result.Stdout = outputBytes
	p.result.PID = p.PID()

	if p.cmd != nil && p.cmd.ProcessState != nil {
		p.result.ExitCode = p.cmd.ProcessState.ExitCode()
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, context.Canceled) {
		p.result.Error = readErr
	}
}

func (p *process) watchTimeout(ctx context.Context) {
	select {
	case <-ctx.Done():
		p.killJobObject()
	case <-time.After(p.spec.Timeout):
		p.killJobObject()
	case <-p.doneCh:
		return
	}
}

func (p *process) killJobObject() {
	_ = windows.TerminateJobObject(p.jobObject, 1)

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

// ---- helpers ----

func createKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("windows sandbox: CreateJobObject: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	//nolint:gosec // unsafe.Pointer required for Windows Job Object API
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("windows sandbox: SetInformationJobObject: %w", err)
	}
	return job, nil
}

func openProcessHandle(pid int) (windows.Handle, error) {
	const processSetQuery = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE | windows.PROCESS_QUERY_INFORMATION | windows.PROCESS_DUP_HANDLE
	h, err := windows.OpenProcess(processSetQuery, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	return h, nil
}

func osProcessFromHandle(handle windows.Handle, pid int) *os.Process {
	// os.FindProcess works via PID — the CreateProcess handle is redundant
	// after the process is assigned to the job object.
	_ = windows.CloseHandle(handle)
	proc, _ := os.FindProcess(pid)
	return proc
}

// buildCommandLine joins a command and args into a Windows command line string.
// The executable is quoted if it contains spaces; args are appended as-is.
func buildCommandLine(command string, args []string) string {
	if len(args) == 0 {
		return windowsEscapeArg(command)
	}
	var b strings.Builder
	b.WriteString(windowsEscapeArg(command))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(windowsEscapeArg(a))
	}
	return b.String()
}

func windowsEscapeArg(s string) string {
	if s == "" {
		return `""`
	}
	hasSpace := false
	hasQuote := false
	for _, c := range s {
		switch c {
		case ' ':
			hasSpace = true
		case '"':
			hasQuote = true
		}
	}
	if !hasSpace && !hasQuote {
		return s
	}
	escaped := make([]byte, 0, len(s)+2)
	escaped = append(escaped, '"')
	for i := 0; i < len(s); i++ {
		// Count consecutive backslashes.
		n := 0
		for i+n < len(s) && s[i+n] == '\\' {
			n++
		}
		if i+n >= len(s) || s[i+n] != '"' {
			escaped = append(escaped, s[i:i+n]...)
			i += n - 1
		} else {
			escaped = append(escaped, s[i:i+2*n]...)
			i += n
		}
	}
	escaped = append(escaped, '"')
	return string(escaped)
}

// buildEnvBlock converts a []string of "KEY=VALUE" pairs into the
// null-separated, double-null-terminated block that CreateProcess expects.
func buildEnvBlock(env []string) *uint16 {
	if len(env) == 0 {
		return nil
	}
	var block []uint16
	for _, e := range env {
		block = append(block, windows.StringToUTF16(e)...)
	}
	block = append(block, 0) // second null for double termination
	return &block[0]
}
