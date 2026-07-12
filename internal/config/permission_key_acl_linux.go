//go:build linux

package config

import (
	"errors"

	"golang.org/x/sys/unix"
)

func permissionFDHasExtendedACL(fd int, directory bool) (bool, error) {
	names := []string{"system.posix_acl_access"}
	if directory {
		names = append(names, "system.posix_acl_default")
	}
	for _, name := range names {
		size, err := unix.Fgetxattr(fd, name, nil)
		switch {
		case err == nil && size > 0:
			return true, nil
		case err == nil:
			continue
		case errors.Is(err, unix.ENODATA):
			continue
		default:
			return false, err
		}
	}
	return false, nil
}
