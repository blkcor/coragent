//go:build linux

package linux

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/sandbox/nop"
	"golang.org/x/sys/unix"
)

var _ sandbox.Sandbox = (*Sandbox)(nil)

// Sandbox implements sandbox.Sandbox using Linux Landlock + seccomp for
// kernel-level (ConfinementKernel) process isolation. It wraps the NOP
// sandbox for process-group management and I/O control, and adds Landlock
// filesystem restrictions and seccomp syscall filtering.
type Sandbox struct {
	nop *nop.Sandbox
}

// New creates a Linux Landlock+seccomp-backed sandbox. pty may be nil to
// disable PTY allocation.
func New(pty sandbox.PTYManager) *Sandbox {
	return &Sandbox{nop: nop.New(pty)}
}

// ConfinementLevel returns ConfinementKernel.
func (s *Sandbox) ConfinementLevel() sandbox.ConfinementLevel {
	return sandbox.ConfinementKernel
}

// isAvailable checks whether both Landlock and seccomp are supported by the
// running kernel. Both must be present for ConfinementKernel.
func isAvailable() bool {
	return landlockAvailable() && seccompAvailable()
}

// Start launches a command under Landlock + seccomp confinement. It builds a
// Landlock ruleset and seccomp filter, then re-execs /proc/self/exe as a
// sandbox-init wrapper — the same structural pattern as the macOS backend
// (which wraps commands via sandbox-exec). The re-exec'd process applies
// kernel restrictions in the child, between fork and exec.
func (s *Sandbox) Start(ctx context.Context, spec sandbox.CommandSpec) (sandbox.Process, error) {
	if !isAvailable() {
		return nil, fmt.Errorf("linux sandbox: Landlock and/or seccomp not available on this kernel")
	}

	landlockFD, err := buildLandlockRuleset(spec)
	if err != nil {
		return nil, fmt.Errorf("linux sandbox: %w", err)
	}

	seccompFilter, err := buildSeccompFilter(spec.Grants.Network)
	if err != nil {
		_ = unix.Close(landlockFD)
		return nil, fmt.Errorf("linux sandbox: %w", err)
	}
	seccompEnc := encodeSeccompFilter(seccompFilter)

	// Clear FD_CLOEXEC so the Landlock ruleset survives execve into the
	// re-exec'd /proc/self/exe child. The FD is created with O_CLOEXEC by
	// the kernel; without this step the child would see a closed FD.
	if _, err := unix.FcntlInt(uintptr(landlockFD), unix.F_SETFD, 0); err != nil {
		return nil, fmt.Errorf("linux sandbox: clear cloexec: %w", err)
	}

	// Re-exec /proc/self/exe with the init marker. The child (our own
	// binary in init mode) applies Landlock + seccomp, then execves the
	// real command. This is the pure-Go equivalent of macOS's sandbox-exec
	// wrapper pattern.
	wrappedSpec := spec
	wrappedSpec.Command = "/proc/self/exe"
	wrappedSpec.Args = append([]string{
		sandboxInitMarker,
		strconv.Itoa(landlockFD),
		seccompEnc,
		spec.Command,
	}, spec.Args...)

	// The Landlock FD is inherited across the re-exec of /proc/self/exe
	// (FD_CLOEXEC was cleared above); its number is passed to the child as an
	// init argument. The child's HandleInit closes its copy after applying
	// the ruleset.
	proc, err := s.nop.Start(ctx, wrappedSpec)
	if err != nil {
		_ = unix.Close(landlockFD)
		return nil, err
	}

	// The child's HandleInit closes the Landlock FD after applying it. The
	// parent closes its copy once the child is running.
	_ = unix.Close(landlockFD)

	return proc, nil
}

// encodeSeccompFilter packs a SockFilter slice into a compact wire format:
// each instruction is 8 bytes (Code:2, Jt:1, Jf:1, K:4), base64-encoded.
func encodeSeccompFilter(filter []unix.SockFilter) string {
	buf := make([]byte, 8*len(filter))
	for i, f := range filter {
		off := i * 8
		binary.LittleEndian.PutUint16(buf[off:], f.Code)
		buf[off+2] = f.Jt
		buf[off+3] = f.Jf
		binary.LittleEndian.PutUint32(buf[off+4:], f.K)
	}
	return base64.RawStdEncoding.EncodeToString(buf)
}

// decodeSeccompFilter reverses encodeSeccompFilter.
func decodeSeccompFilter(encoded string) ([]unix.SockFilter, error) {
	buf, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("seccomp: decode filter: %w", err)
	}
	if len(buf)%8 != 0 {
		return nil, fmt.Errorf("seccomp: filter data not aligned (len=%d)", len(buf))
	}
	filter := make([]unix.SockFilter, len(buf)/8)
	for i := range filter {
		off := i * 8
		filter[i].Code = binary.LittleEndian.Uint16(buf[off:])
		filter[i].Jt = buf[off+2]
		filter[i].Jf = buf[off+3]
		filter[i].K = binary.LittleEndian.Uint32(buf[off+4:])
	}
	return filter, nil
}

// seccompAvailable probes whether the running kernel supports seccomp
// filter mode (SECCOMP_SET_MODE_FILTER). Kernel >= 3.17 is required; this
// is effectively always available on any supported Linux system.
func seccompAvailable() bool {
	// Install a null filter without PR_SET_NO_NEW_PRIVS. A kernel with
	// filter support rejects this with EACCES (no_new_privs not set and no
	// CAP_SYS_ADMIN) or EFAULT (bad filter pointer); ENOSYS or EINVAL mean
	// filter mode is unsupported.
	err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, 0, 0, 0)
	return err == nil || (!isENOSYS(err) && !errors.Is(err, unix.EINVAL))
}

// isENOSYS reports whether err is an ENOSYS syscall error.
func isENOSYS(err error) bool {
	if errno, ok := err.(unix.Errno); ok {
		return errno == unix.ENOSYS
	}
	return false
}
