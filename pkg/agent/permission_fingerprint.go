package agent

import "github.com/blkcor/coragent/internal/permission"

// PermissionFingerprintKey is secret material used to produce reloadable,
// domain-separated exact-call permission fingerprints. Its String, GoString,
// JSON, and structured-log forms are always redacted.
type PermissionFingerprintKey = permission.FingerprintKey

// PermissionFingerprintKeySize is the exact byte length accepted by
// NewPermissionFingerprintKey.
const PermissionFingerprintKeySize = permission.FingerprintKeySize

// NewPermissionFingerprintKey copies caller-owned secret material into a
// redacting value suitable for SessionConfig or BootstrapOptions.
func NewPermissionFingerprintKey(raw []byte) (PermissionFingerprintKey, error) {
	return permission.NewFingerprintKey(raw)
}
