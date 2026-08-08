//go:build linux

package linux

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/blkcor/coragent/internal/sandbox"
)

// Landlock syscall numbers. These use the generic syscall table shared by
// x86_64, arm64, and riscv64 (all recent architectures).
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
)

// Landlock filesystem access rights.
const (
	landlockAccessFSWriteFile  uint64 = 1 << 1
	landlockAccessFSRemoveDir  uint64 = 1 << 4
	landlockAccessFSRemoveFile uint64 = 1 << 5
	landlockAccessFSMakeChar   uint64 = 1 << 6
	landlockAccessFSMakeDir    uint64 = 1 << 7
	landlockAccessFSMakeReg    uint64 = 1 << 8
	landlockAccessFSMakeSock   uint64 = 1 << 9
	landlockAccessFSMakeFIFO   uint64 = 1 << 10
	landlockAccessFSMakeBlock  uint64 = 1 << 11
	landlockAccessFSMakeSym    uint64 = 1 << 12
	landlockAccessFSRefer      uint64 = 1 << 13
	landlockAccessFSTruncate   uint64 = 1 << 14
)

const landlockRulePathBeneath = 1

// allWriteAccess covers every write-related Landlock access right. Reads and
// exec are intentionally excluded — like the macOS Seatbelt backend, we allow
// reads globally and restrict only writes.
const allWriteAccess = landlockAccessFSWriteFile |
	landlockAccessFSRemoveDir | landlockAccessFSRemoveFile |
	landlockAccessFSMakeChar | landlockAccessFSMakeDir | landlockAccessFSMakeReg |
	landlockAccessFSMakeSock | landlockAccessFSMakeFIFO | landlockAccessFSMakeBlock |
	landlockAccessFSMakeSym | landlockAccessFSRefer | landlockAccessFSTruncate

type landlockRulesetAttr struct {
	HandledAccessFS uint64
}

type landlockPathBeneathAttr struct {
	AllowedAccess uint64
	ParentFd      int32
	_             uint32
}

func landlockCreateRulesetC(attr *landlockRulesetAttr, flags uint32) (int, error) {
	//nolint:gosec // raw Landlock syscall: no Go wrapper exists in x/sys; attr layout matches the kernel ABI
	r0, _, e1 := syscall.Syscall(sysLandlockCreateRuleset, uintptr(unsafe.Pointer(attr)), unsafe.Sizeof(*attr), uintptr(flags))
	if e1 != 0 {
		return -1, os.NewSyscallError("landlock_create_ruleset", e1)
	}
	return int(r0), nil
}

func landlockAddRuleC(rulesetFd int, ruleType uint32, attr *landlockPathBeneathAttr, flags uint32) error {
	//nolint:gosec // raw Landlock syscall: no Go wrapper exists in x/sys; attr layout matches the kernel ABI
	_, _, e1 := syscall.Syscall6(sysLandlockAddRule, uintptr(rulesetFd), uintptr(ruleType), uintptr(unsafe.Pointer(attr)), uintptr(flags), 0, 0)
	if e1 != 0 {
		return os.NewSyscallError("landlock_add_rule", e1)
	}
	return nil
}

func landlockRestrictSelfC(rulesetFd int, flags uint32) error {
	_, _, e1 := syscall.Syscall(sysLandlockRestrictSelf, uintptr(rulesetFd), uintptr(flags), 0)
	if e1 != 0 {
		return os.NewSyscallError("landlock_restrict_self", e1)
	}
	return nil
}

// buildLandlockRuleset creates a Landlock ruleset file descriptor from the
// spec's grants. The ruleset controls all write-related access types but
// leaves reads and exec unrestricted. The caller must close the FD.
func buildLandlockRuleset(spec sandbox.CommandSpec) (int, error) {
	attr := &landlockRulesetAttr{HandledAccessFS: allWriteAccess}
	rulesetFD, err := landlockCreateRulesetC(attr, 0)
	if err != nil {
		return -1, fmt.Errorf("landlock: create ruleset: %w", err)
	}

	// Allow writes on workspace, tmp, and declared write paths.
	paths := collectWritePaths(spec)
	for _, p := range paths {
		if err := allowPathWrite(rulesetFD, p); err != nil {
			_ = syscall.Close(rulesetFD)
			return -1, fmt.Errorf("landlock: add rule for %s: %w", p, err)
		}
	}

	return rulesetFD, nil
}

// collectWritePaths gathers all paths a process should be allowed to write.
func collectWritePaths(spec sandbox.CommandSpec) []string {
	paths := make([]string, 0, 2+len(spec.Grants.AllowedWritePaths))
	if spec.CWD != "" {
		paths = append(paths, spec.CWD)
	}
	paths = append(paths, os.TempDir())
	paths = append(paths, spec.Grants.AllowedWritePaths...)
	return paths
}

// allowPathWrite adds a Landlock rule granting all write access beneath the
// given directory.
func allowPathWrite(rulesetFD int, path string) error {
	f, err := os.Open(path) //nolint:gosec // grant paths are caller-declared workspace roots; opening them to obtain an FD for Landlock is the intended behavior
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	attr := &landlockPathBeneathAttr{
		AllowedAccess: allWriteAccess,
		ParentFd:      int32(f.Fd()),
	}
	return landlockAddRuleC(rulesetFD, landlockRulePathBeneath, attr, 0)
}

// applyLandlock applies the Landlock ruleset to the current process. Must be
// called in the child process before exec.
func applyLandlock(rulesetFD int) error {
	if err := landlockRestrictSelfC(rulesetFD, 0); err != nil {
		return fmt.Errorf("landlock: restrict self: %w", err)
	}
	_ = syscall.Close(rulesetFD)
	return nil
}

// landlockAvailable probes whether the running kernel supports Landlock.
func landlockAvailable() bool {
	// Create a ruleset handling one real access right. A kernel without
	// Landlock returns ENOSYS; a disabled one returns EOPNOTSUPP. Note that
	// handled_access_fs must be non-zero — a zero ruleset is rejected with
	// EINVAL even on Landlock-capable kernels.
	attr := &landlockRulesetAttr{HandledAccessFS: landlockAccessFSWriteFile}
	fd, err := landlockCreateRulesetC(attr, 0)
	if err != nil {
		return false
	}
	_ = syscall.Close(fd)
	return true
}
