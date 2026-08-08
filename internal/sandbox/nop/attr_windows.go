//go:build windows

package nop

import (
	"os"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func ptySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func signalProcessGroup(_ int, proc *os.Process) {
	_ = proc.Kill()
}
