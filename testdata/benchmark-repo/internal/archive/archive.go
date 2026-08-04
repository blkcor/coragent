package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Extract(zipPath, destination string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, entry := range reader.File {
		target, err := secureDestination(root, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive symlink entry %q is not allowed", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		// Re-check after parent creation and immediately before commit. An
		// existing or raced parent symlink must not redirect the write.
		target, err = secureDestination(root, entry.Name)
		if err != nil {
			return err
		}
		if err := writeEntry(entry, target); err != nil {
			return err
		}
	}
	return nil
}

func secureDestination(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) {
		return "", errors.New("archive entry must be a relative path")
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes destination", name)
	}
	target := filepath.Join(root, clean)
	parent := filepath.Dir(target)
	existingParent := parent
	for {
		_, statErr := os.Lstat(existingParent)
		if statErr == nil {
			break
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		next := filepath.Dir(existingParent)
		if next == existingParent {
			return "", errors.New("archive destination has no existing parent")
		}
		existingParent = next
	}
	resolvedParent, err := filepath.EvalSymlinks(existingParent)
	if err != nil {
		return "", fmt.Errorf("resolve archive parent: %w", err)
	}
	resolvedParent, err = filepath.Abs(resolvedParent)
	if err != nil {
		return "", err
	}
	if resolvedParent != root && !strings.HasPrefix(resolvedParent, root+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q crosses a parent symlink", name)
	}
	return target, nil
}

func writeEntry(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, source)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
