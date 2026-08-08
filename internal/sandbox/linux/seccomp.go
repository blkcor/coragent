//go:build linux

package linux

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

const (
	bpfLdWAbs = 0x20 // BPF_LD | BPF_W | BPF_ABS
	bpfJeqK   = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
	bpfRetK   = 0x06 // BPF_RET | BPF_K
	bpfJa     = 0x05 // BPF_JMP | BPF_JA

	seccompRetKillProcess = 0x80000000
	seccompRetAllow       = 0x7FFF0000

	// seccomp_data.arch offset (32-bit word at byte 4).
	seccompDataArchOffset = 4
	// seccomp_data.nr offset (32-bit word at byte 0).
	seccompDataNrOffset = 0
)

// audit arch constants — the value of seccomp_data.arch on each platform.
const (
	auditArchAMD64 = 0xC000003E
	auditArchARM64 = 0xC00000B7
)

// bpfStmt returns a BPF statement (no jump).
func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: 0, Jf: 0, K: k}
}

// bpfJump returns a BPF conditional jump instruction.
func bpfJump(code uint16, k uint32, jt uint8, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}

// buildSeccompFilter returns a BPF filter program for seccomp SECCOMP_SET_MODE_FILTER.
// networkAllowed controls whether socket-family syscalls are permitted.
func buildSeccompFilter(networkAllowed bool) ([]unix.SockFilter, error) {
	auditArch := uint32(0)
	switch runtime.GOARCH {
	case "amd64":
		auditArch = auditArchAMD64
	case "arm64":
		auditArch = auditArchARM64
	default:
		return nil, fmt.Errorf("seccomp: unsupported architecture %s", runtime.GOARCH)
	}

	allowed := allowedSyscalls(networkAllowed)
	insns := make([]unix.SockFilter, 0, 4+len(allowed)+2)

	// 0: Load architecture and validate.
	insns = append(insns, bpfStmt(bpfLdWAbs, seccompDataArchOffset))
	insns = append(insns, bpfJump(bpfJeqK, auditArch, 1, 0))
	// 2: Wrong architecture — kill.
	insns = append(insns, bpfStmt(bpfRetK, seccompRetKillProcess))

	// 3: Load syscall number.
	insns = append(insns, bpfStmt(bpfLdWAbs, seccompDataNrOffset))

	// 4..N-1: Check each allowed syscall. A matching JEQ falls through to the
	// JA below it, which jumps past the remaining pairs and the KILL
	// instruction to reach ALLOW. A non-matching JEQ skips the JA.
	total := len(allowed)
	for i, nr := range allowed {
		insns = append(insns, bpfJump(bpfJeqK, uint32(nr), 0, 1))
		skipToAllow := 2*(total-i-1) + 1 // remaining pairs + KILL
		insns = append(insns, bpfStmt(bpfJa, uint32(skipToAllow)))
	}

	// N: Kill (default).
	insns = append(insns, bpfStmt(bpfRetK, seccompRetKillProcess))
	// N+1: Allow.
	insns = append(insns, bpfStmt(bpfRetK, seccompRetAllow))

	return insns, nil
}

// allowedSyscalls returns the list of permitted syscall numbers for the
// current architecture. Numbers come from golang.org/x/sys/unix constants,
// so the compiler resolves the correct value for each target architecture.
func allowedSyscalls(networkAllowed bool) []int {
	base := baseAllowed()
	if networkAllowed {
		base = append(base, networkSyscalls()...)
	}
	return base
}

// baseAllowed lists syscalls needed by typical command-line programs. The
// generic table compiles on every supported architecture; syscalls that
// exist only on one architecture come from archExtras in the per-arch files.
func baseAllowed() []int {
	generic := []int{
		unix.SYS_READ,
		unix.SYS_WRITE,
		unix.SYS_CLOSE,
		unix.SYS_FSTAT,
		unix.SYS_LSEEK,
		unix.SYS_MMAP,
		unix.SYS_MPROTECT,
		unix.SYS_MUNMAP,
		unix.SYS_BRK,
		unix.SYS_RT_SIGACTION,
		unix.SYS_RT_SIGPROCMASK,
		unix.SYS_RT_SIGRETURN,
		unix.SYS_IOCTL,
		unix.SYS_PREAD64,
		unix.SYS_PWRITE64,
		unix.SYS_READV,
		unix.SYS_WRITEV,
		unix.SYS_SCHED_YIELD,
		unix.SYS_MREMAP,
		unix.SYS_MADVISE,
		unix.SYS_NANOSLEEP,
		unix.SYS_GETPID,
		unix.SYS_CLONE,
		unix.SYS_EXECVE,
		unix.SYS_EXIT,
		unix.SYS_UNAME,
		unix.SYS_FCNTL,
		unix.SYS_GETCWD,
		unix.SYS_GETUID,
		unix.SYS_GETGID,
		unix.SYS_GETEUID,
		unix.SYS_GETEGID,
		unix.SYS_GETPPID,
		unix.SYS_SIGALTSTACK,
		unix.SYS_GETTID,
		unix.SYS_FUTEX,
		unix.SYS_SCHED_GETAFFINITY,
		unix.SYS_SET_TID_ADDRESS,
		unix.SYS_CLOCK_GETTIME,
		unix.SYS_CLOCK_NANOSLEEP,
		unix.SYS_EXIT_GROUP,
		unix.SYS_TGKILL,
		unix.SYS_OPENAT,
		unix.SYS_GETDENTS64,
		unix.SYS_SET_ROBUST_LIST,
		unix.SYS_PRLIMIT64,
		unix.SYS_GETRANDOM,
		unix.SYS_MEMFD_CREATE,
		unix.SYS_STATX,
		unix.SYS_RSEQ,
		unix.SYS_CLONE3,
		unix.SYS_EXECVEAT,
		unix.SYS_READLINKAT,
		unix.SYS_FACCESSAT,
		unix.SYS_PPOLL,
		unix.SYS_EPOLL_CREATE1,
		unix.SYS_EPOLL_CTL,
		unix.SYS_EPOLL_PWAIT,
		unix.SYS_PIPE2,
		unix.SYS_GETRLIMIT,
		unix.SYS_DUP,
		unix.SYS_DUP3,
		unix.SYS_FADVISE64,
	}
	return append(generic, archExtras()...)
}

// networkSyscalls returns socket-related syscall numbers. They are added to
// the allow list only when the command's grants declare network access.
func networkSyscalls() []int {
	return []int{
		unix.SYS_SOCKET,
		unix.SYS_CONNECT,
		unix.SYS_ACCEPT,
		unix.SYS_SENDTO,
		unix.SYS_RECVFROM,
		unix.SYS_SENDMSG,
		unix.SYS_RECVMSG,
		unix.SYS_SHUTDOWN,
		unix.SYS_BIND,
		unix.SYS_LISTEN,
		unix.SYS_GETSOCKNAME,
		unix.SYS_GETPEERNAME,
		unix.SYS_SOCKETPAIR,
		unix.SYS_SETSOCKOPT,
		unix.SYS_GETSOCKOPT,
	}
}
