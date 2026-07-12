//go:build darwin

package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPermissionFingerprintKeyRotatesFileWithExtendedACL(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, permissionFingerprintKeyFile)
	old := bytes.Repeat([]byte{0x61}, permissionFingerprintKeySize)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/chmod", "+a", "everyone allow read", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("add key ACL: %v: %s", err, output)
	}
	result, err := loadOrCreatePermissionFingerprintKeyAt(path)
	if err != nil || result.Status != PermissionFingerprintKeyRotated || bytes.Equal(result.Material(), old) {
		t.Fatalf("ACL-bearing key was not rotated: result=%+v err=%v", result, err)
	}
	assertNoPermissionKeyBackup(t, directory)
}

func TestPermissionFingerprintKeyRejectsParentWithExtendedACL(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, permissionFingerprintKeyFile)
	old := bytes.Repeat([]byte{0x62}, permissionFingerprintKeySize)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/chmod", "+a", "everyone allow read", directory)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("add parent ACL: %v: %s", err, output)
	}
	_, err := loadOrCreatePermissionFingerprintKeyAt(path)
	if err == nil || !strings.Contains(err.Error(), "extended ACL") || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("unsafe parent ACL error = %v", err)
	}
	unchanged, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(unchanged, old) {
		t.Fatal("unsafe-parent ACL failure modified the key")
	}
}
