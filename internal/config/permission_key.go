package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	permissionFingerprintKeyFile = "permission-fingerprint.key"
	permissionFingerprintKeySize = 32
)

type PermissionFingerprintKeyStatus string

const (
	PermissionFingerprintKeyExisting PermissionFingerprintKeyStatus = "existing"
	PermissionFingerprintKeyFresh    PermissionFingerprintKeyStatus = "fresh"
	PermissionFingerprintKeyRotated  PermissionFingerprintKeyStatus = "rotated"
)

type PermissionFingerprintKeyResult struct {
	key    []byte
	Status PermissionFingerprintKeyStatus
}

func newPermissionFingerprintKeyResult(key []byte, status PermissionFingerprintKeyStatus) PermissionFingerprintKeyResult {
	return PermissionFingerprintKeyResult{key: key, Status: status}
}

func (result PermissionFingerprintKeyResult) Material() []byte {
	return append([]byte(nil), result.key...)
}

// ConsumeMaterial transfers the internal key bytes to the caller and clears the
// result. The caller must clear the returned slice after constructing its own
// redacting key value.
func (result *PermissionFingerprintKeyResult) ConsumeMaterial() []byte {
	material := result.key
	result.key = nil
	return material
}

func (result PermissionFingerprintKeyResult) String() string {
	return fmt.Sprintf("PermissionFingerprintKeyResult{status:%q key:redacted}", result.Status)
}

func (result PermissionFingerprintKeyResult) GoString() string { return result.String() }

func (result PermissionFingerprintKeyResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status PermissionFingerprintKeyStatus `json:"status"`
		Key    string                         `json:"key"`
	}{Status: result.Status, Key: "[REDACTED]"})
}

func (result PermissionFingerprintKeyResult) LogValue() slog.Value {
	return slog.GroupValue(slog.String("status", string(result.Status)), slog.String("key", "[REDACTED]"))
}

func (result PermissionFingerprintKeyResult) InvalidatesExactRules() bool {
	return result.Status == PermissionFingerprintKeyFresh || result.Status == PermissionFingerprintKeyRotated
}

type permissionPathSecurity struct {
	regular     bool
	directory   bool
	uid         uint32
	permissions uint32
	nlink       uint64
	size        int64
	extendedACL bool
}

func validatePermissionKeyParent(security permissionPathSecurity, effectiveUID uint32) error {
	switch {
	case !security.directory:
		return fmt.Errorf("not a directory")
	case security.uid != effectiveUID:
		return fmt.Errorf("owned by uid %d instead of current uid %d", security.uid, effectiveUID)
	case security.permissions&0o022 != 0:
		return fmt.Errorf("group or other write bits are set (%04o)", security.permissions)
	case security.extendedACL:
		return fmt.Errorf("extended ACL is present")
	default:
		return nil
	}
}

func validatePermissionKeyFile(security permissionPathSecurity, effectiveUID uint32) error {
	switch {
	case !security.regular:
		return fmt.Errorf("not a regular file")
	case security.uid != effectiveUID:
		return fmt.Errorf("owned by uid %d instead of current uid %d", security.uid, effectiveUID)
	case security.permissions != 0o600:
		return fmt.Errorf("mode is %04o instead of 0600", security.permissions)
	case security.nlink != 1:
		return fmt.Errorf("link count is %d instead of 1", security.nlink)
	case security.size != permissionFingerprintKeySize:
		return fmt.Errorf("size is %d instead of %d bytes", security.size, permissionFingerprintKeySize)
	case security.extendedACL:
		return fmt.Errorf("extended ACL is present")
	default:
		return nil
	}
}

// PermissionFingerprintKeyPath returns the user-private secret path used by the
// standard bootstrap. It is deliberately independent from home/project
// settings.json files, which may be shared or inspected as ordinary config.
func PermissionFingerprintKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for permission fingerprint key: %w", err)
	}
	return filepath.Join(home, ".coragent", permissionFingerprintKeyFile), nil
}

// LoadOrCreatePermissionFingerprintKey returns stable per-user secret material
// plus the lifecycle status needed to invalidate in-memory exact selectors. A
// fresh or rotated key is published only after stale disk selectors are scrubbed.
func LoadOrCreatePermissionFingerprintKey() (PermissionFingerprintKeyResult, error) {
	path, err := PermissionFingerprintKeyPath()
	if err != nil {
		return PermissionFingerprintKeyResult{}, err
	}
	result, err := loadOrCreatePermissionFingerprintKeyAtWithReset(path, ScrubAllExactPermissionRules)
	if err != nil {
		return PermissionFingerprintKeyResult{}, err
	}
	if result.Status == PermissionFingerprintKeyRotated {
		slog.Warn(
			"rotated an unsafe permission fingerprint key; rotate credentials that may have appeared in remembered exact calls",
			"path", path,
			"status", result.Status,
		)
	}
	return result, nil
}

func loadOrCreatePermissionFingerprintKeyAt(path string) (PermissionFingerprintKeyResult, error) {
	return loadOrCreatePermissionFingerprintKeyAtWithReset(path, nil)
}

func loadOrCreatePermissionFingerprintKeyAtWithReset(path string, beforePublish func() error) (PermissionFingerprintKeyResult, error) {
	return platformLoadOrCreatePermissionFingerprintKey(path, beforePublish)
}
