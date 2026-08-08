//go:build linux && amd64

package linux

import "golang.org/x/sys/unix"

// archExtras returns amd64-only syscalls that glibc still uses on top of the
// generic table (pre-openat legacy calls and arch-specific prctl).
func archExtras() []int {
	return []int{
		unix.SYS_OPEN,
		unix.SYS_STAT,
		unix.SYS_LSTAT,
		unix.SYS_POLL,
		unix.SYS_SELECT,
		unix.SYS_FORK,
		unix.SYS_PIPE,
		unix.SYS_ACCESS,
		unix.SYS_READLINK,
		unix.SYS_GETDENTS,
		unix.SYS_NEWFSTATAT,
		unix.SYS_STATFS,
		unix.SYS_FSTATFS,
		unix.SYS_TIME,
		unix.SYS_ARCH_PRCTL,
		unix.SYS_DUP2,
	}
}
