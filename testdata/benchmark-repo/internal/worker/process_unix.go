//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package worker

import (
	"os/exec"
	"syscall"
)

type commandProcess struct{ cmd *exec.Cmd }

func newCommandProcess(cmd *exec.Cmd) process {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &commandProcess{cmd: cmd}
}

func (p *commandProcess) Start() error       { return p.cmd.Start() }
func (p *commandProcess) Wait() error        { return p.cmd.Wait() }
func (p *commandProcess) KillProcess() error { return p.cmd.Process.Kill() }
func (p *commandProcess) KillGroup() error   { return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) }
