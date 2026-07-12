//go:build !darwin && !linux

package config

import "fmt"

func platformLoadOrCreatePermissionFingerprintKey(string, func() error) (PermissionFingerprintKeyResult, error) {
	return PermissionFingerprintKeyResult{}, fmt.Errorf("secure permission fingerprint key validation is unsupported on this platform")
}
