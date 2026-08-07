// Package workspace supplies the scoped filesystem capability and the FileService
// abstraction used by all file-access tools.
package workspace

import (
	"io/fs"
	"os"
)

// FileService is the unified file-access interface for tools. Every method
// enforces workspace confinement, symlink rejection, and TOCTOU protection.
// Tools depend on this interface instead of a concrete filesystem type.
type FileService interface {
	// Clean validates a workspace-relative slash path and returns its clean form.
	// It rejects absolute paths, ".." traversal, and NUL bytes.
	Clean(name string) (string, error)

	// Read opens a workspace file for reading with TOCTOU and symlink protection.
	// The caller must close the returned file.
	Read(name string) (*os.File, string, error)

	// List validates that name exists and returns the scoped filesystem for
	// directory walking along with the clean path.
	List(name string) (fs.FS, string, error)

	// Search validates that name exists and returns the scoped filesystem for
	// file walking along with the clean path. In M1 this is identical to List;
	// future milestones may add search-specific optimizations.
	Search(name string) (fs.FS, string, error)

	// Write writes data to a workspace file, rejecting path escapes and
	// symlinks. expectedSHA256, when non-empty, must match the SHA256 of data;
	// a mismatch fails before any write. Returns the SHA256 of the written
	// content.
	Write(name string, data []byte, expectedSHA256 string) (string, error)

	// Identity returns the SHA256 hex digest of a file's content.
	Identity(name string) (string, error)
}
