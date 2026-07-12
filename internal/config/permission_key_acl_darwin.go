//go:build darwin

package config

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func permissionFDHasExtendedACL(fd int, _ bool) (bool, error) {
	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_EXTENDED_SECURITY,
	}
	buffer := make([]byte, 8192)
	// x/sys has no fgetattrlist wrapper; the raw fd-based call avoids following a
	// path after the key or parent has been validated.
	//nolint:staticcheck // SYS_FGETATTRLIST is the available fd-based Darwin API.
	_, _, callErr := unix.Syscall6(
		unix.SYS_FGETATTRLIST,
		uintptr(fd),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(unix.FSOPT_REPORT_FULLSIZE),
		0,
	)
	if callErr != 0 {
		return false, callErr
	}
	if len(buffer) < 12 {
		return false, fmt.Errorf("extended security response is too short")
	}
	total := int(binary.LittleEndian.Uint32(buffer[:4]))
	if total < 12 || total > len(buffer) {
		return false, fmt.Errorf("extended security response length %d is invalid", total)
	}
	referenceOffset := int(int32(binary.LittleEndian.Uint32(buffer[4:8])))
	securityLength := int(binary.LittleEndian.Uint32(buffer[8:12]))
	securityStart := 4 + referenceOffset
	if referenceOffset < 8 || securityLength < 0 || securityStart < 12 || securityStart > total || securityLength > total-securityStart {
		return false, fmt.Errorf("extended security reference is invalid")
	}
	return securityLength > 0, nil
}
