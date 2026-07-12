//go:build !darwin

package tools

import "context"

var identityPrimitiveAvailable = func() bool { return false }

func readFileSnapshot(context.Context, string, bool) ([]byte, platformFileSnapshot, error) {
	return nil, platformFileSnapshot{}, ErrIdentityCommitUnsupported
}

func commitFileCandidate(context.Context, *preparedFileToken) error {
	return ErrIdentityCommitUnsupported
}
