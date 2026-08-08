//go:build windows

package windows

import (
	"sync"
	"syscall"
	"unsafe"
)

// build1809 is the minimum Windows build for ConPTY support (Windows 10 1809).
const build1809 = 17763

var (
	versionOnce    sync.Once
	cachedBuild    uint32
	cachedBuildErr error
)

type osVersionInfoExW struct {
	size             uint32
	majorVersion     uint32
	minorVersion     uint32
	buildNumber      uint32
	platformID       uint32
	csdVersion       [128]uint16
	servicePackMajor uint16
	servicePackMinor uint16
	suitMask         uint16
	productType      byte
	reserved         byte
}

// buildNumber returns the Windows build number via RtlGetVersion. The result is
// cached for the lifetime of the process since the OS version cannot change.
func buildNumber() (uint32, error) {
	versionOnce.Do(func() {
		ntdll := syscall.NewLazyDLL("ntdll.dll")
		rtlGetVersion := ntdll.NewProc("RtlGetVersion")

		info := osVersionInfoExW{size: uint32(unsafe.Sizeof(osVersionInfoExW{}))}
		//nolint:gosec // unsafe.Pointer required for RtlGetVersion Windows API
		r0, _, err := rtlGetVersion.Call(uintptr(unsafe.Pointer(&info)))
		// RtlGetVersion returns NTSTATUS. STATUS_SUCCESS (0) is the only success code.
		const statusSuccess = 0x00000000
		if r0 != statusSuccess {
			cachedBuildErr = err
			return
		}
		cachedBuild = info.buildNumber
	})
	return cachedBuild, cachedBuildErr
}

// supportsConPTY reports whether the current Windows build supports the
// ConPTY API (CreatePseudoConsole, available since Windows 10 1809 / build 17763).
func supportsConPTY() bool {
	build, err := buildNumber()
	if err != nil {
		return false
	}
	return build >= build1809
}
