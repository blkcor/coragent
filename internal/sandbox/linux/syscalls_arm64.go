//go:build linux && arm64

package linux

import "golang.org/x/sys/unix"

// archExtras returns arm64-specific syscall names whose numbering or naming
// differs from the generic table entries used on amd64.
func archExtras() []int {
	return []int{
		unix.SYS_FSTATAT,
		unix.SYS_STATFS,
		unix.SYS_FSTATFS,
	}
}
