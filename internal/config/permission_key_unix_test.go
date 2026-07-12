//go:build darwin || linux

package config

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPermissionFingerprintKeyRotatesSpecialFileWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), permissionFingerprintKeyFile)
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := loadOrCreatePermissionFingerprintKeyAt(path)
	if err != nil || result.Status != PermissionFingerprintKeyRotated {
		t.Fatalf("special key path was not rotated: result=%+v err=%v", result, err)
	}
}
