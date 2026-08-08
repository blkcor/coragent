//go:build linux

package linux

import (
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

// sandboxInitMarker is the first argument when /proc/self/exe is re-exec'd
// to apply Landlock + seccomp in a child process. It is exported so the CLI
// entry point can check for it.
const sandboxInitMarker = "__coragent_sandbox_linux_init"

// HandleInit checks whether the process was invoked as a sandbox init
// wrapper. If so, it applies Landlock and seccomp, then execs the target
// command (and never returns). It reports false when the process is not in
// sandbox-init mode, so the caller can proceed with normal startup.
func HandleInit(args []string) bool {
	if len(args) < 1 || args[0] != sandboxInitMarker {
		return false
	}

	// args layout:
	//   [0] sandboxInitMarker
	//   [1] Landlock FD (decimal string)
	//   [2] seccomp filter (base64-encoded)
	//   [3] target command
	//   [4..] target args

	if len(args) < 4 {
		os.Exit(126)
	}

	landlockFD, err := strconv.Atoi(args[1])
	if err != nil {
		os.Exit(126)
	}

	seccompFilter, err := decodeSeccompFilter(args[2])
	if err != nil {
		os.Exit(126)
	}

	// Apply no-new-privs first — required before seccomp.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		os.Exit(126)
	}

	// Apply Landlock filesystem restrictions.
	if err := applyLandlock(landlockFD); err != nil {
		os.Exit(126)
	}

	// Apply seccomp syscall filter.
	if err := applySeccompFilter(seccompFilter); err != nil {
		os.Exit(126)
	}

	// Exec the real target. unix.Exec does not return on success.
	targetCmd := args[3]
	targetArgs := args[4:]
	_ = unix.Exec(targetCmd, append([]string{targetCmd}, targetArgs...), os.Environ())
	os.Exit(126)
	panic("unreachable")
}

// applySeccompFilter loads a BPF filter into the kernel via prctl.
func applySeccompFilter(filter []unix.SockFilter) error {
	prog := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}
	//nolint:gosec // installing a BPF filter requires passing its address to prctl; prog references pinned Go memory for the duration of the call
	return unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&prog)), 0, 0)
}
