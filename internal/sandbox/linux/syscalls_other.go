//go:build linux && !amd64 && !arm64

package linux

// archExtras returns no extra syscalls on architectures without a curated
// table. buildSeccompFilter rejects these architectures at filter build time.
func archExtras() []int {
	return nil
}
