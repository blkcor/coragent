//go:build !windows

package nop

import (
	"os"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func ptySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func signalProcessGroup(pid int, proc *os.Process) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = proc.Kill()
}
