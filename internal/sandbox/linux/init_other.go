//go:build !linux

package linux

// HandleInit is a no-op on non-Linux platforms.
func HandleInit(args []string) bool { return false }
