// Package workspace supplies the scoped filesystem capability used by every
// M1 file tool and instruction discovery. It holds an os.Root descriptor so
// path traversal, symlink replacement, and symlink escape fail closed at open.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/blkcor/coragent/internal/platform/fileid"
)

var ErrEscape = errors.New("workspace: path escapes workspace")

type FS struct {
	name string
	root *os.Root
}

func Open(name string) (*FS, error) {
	if name == "" {
		return nil, errors.New("workspace: root path is required")
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve root: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, fmt.Errorf("workspace: clean root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("workspace: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("workspace: root is not a directory")
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("workspace: open scoped root: %w", err)
	}
	return &FS{name: resolved, root: root}, nil
}

func (w *FS) Close() error { return w.root.Close() }
func (w *FS) Name() string { return w.name }
func (w *FS) GoFS() fs.FS  { return w.root.FS() }

func (w *FS) Identity() (string, error) {
	info, err := w.root.Stat(".")
	if err != nil {
		return "", fmt.Errorf("workspace: stat root identity: %w", err)
	}
	return fileid.FromInfo(info), nil
}

// Clean validates a model-provided workspace-relative slash path.
func (w *FS) Clean(name string) (string, error) {
	if strings.IndexByte(name, 0) >= 0 || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", ErrEscape
	}
	name = filepath.ToSlash(name)
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "" {
		clean = "."
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", ErrEscape
	}
	return clean, nil
}

func (w *FS) OpenFile(name string) (*os.File, string, error) {
	clean, err := w.Clean(name)
	if err != nil {
		return nil, "", err
	}
	if err := w.rejectSymlinkComponents(clean); err != nil {
		return nil, clean, err
	}
	f, err := w.root.Open(clean)
	if err != nil {
		if strings.Contains(err.Error(), "path escapes") {
			return nil, clean, fmt.Errorf("%w: %s", ErrEscape, clean)
		}
		return nil, clean, err
	}
	openedInfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, clean, err
	}
	pathInfo, err := w.root.Lstat(clean)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, pathInfo) {
		_ = f.Close()
		return nil, clean, fmt.Errorf("%w: symlink or replaced path %s", ErrEscape, clean)
	}
	return f, clean, nil
}

func (w *FS) Stat(name string) (fs.FileInfo, string, error) {
	clean, err := w.Clean(name)
	if err != nil {
		return nil, "", err
	}
	if err := w.rejectSymlinkComponents(clean); err != nil {
		return nil, clean, err
	}
	info, err := w.root.Stat(clean)
	if err != nil {
		if strings.Contains(err.Error(), "path escapes") {
			return nil, clean, fmt.Errorf("%w: %s", ErrEscape, clean)
		}
		return nil, clean, err
	}
	pathInfo, err := w.root.Lstat(clean)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
		return nil, clean, fmt.Errorf("%w: symlink or replaced path %s", ErrEscape, clean)
	}
	return info, clean, nil
}

// rejectSymlinkComponents makes in-workspace aliases fail closed too. Merely
// containing the resolved target inside the workspace is insufficient: path
// classification (for example .env protection) must apply to the exact path
// that is opened, and an alias must not hide the target's sensitive name.
func (w *FS) rejectSymlinkComponents(clean string) error {
	if clean == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(filepath.ToSlash(clean), "/") {
		if current == "" {
			current = component
		} else {
			current += "/" + component
		}
		info, err := w.root.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink path %s", ErrEscape, clean)
		}
	}
	return nil
}
