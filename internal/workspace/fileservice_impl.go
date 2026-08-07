package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

type fileServiceImpl struct {
	w *FS
}

// NewFileService wraps a workspace FS as a FileService for tool consumption.
func NewFileService(w *FS) FileService {
	return &fileServiceImpl{w: w}
}

func (s *fileServiceImpl) Clean(name string) (string, error) {
	return s.w.Clean(name)
}

func (s *fileServiceImpl) Read(name string) (*os.File, string, error) {
	return s.w.OpenFile(name)
}

func (s *fileServiceImpl) List(name string) (fs.FS, string, error) {
	_, clean, err := s.w.Stat(name)
	if err != nil {
		return nil, clean, translateStatError(err)
	}
	return s.w.GoFS(), clean, nil
}

func (s *fileServiceImpl) Search(name string) (fs.FS, string, error) {
	_, clean, err := s.w.Stat(name)
	if err != nil {
		return nil, clean, translateStatError(err)
	}
	return s.w.GoFS(), clean, nil
}

func (s *fileServiceImpl) Write(name string, data []byte, expectedSHA256 string) (string, error) {
	clean, err := s.w.Clean(name)
	if err != nil {
		return "", err
	}
	if err := s.w.RejectSymlinks(clean); err != nil {
		return "", err
	}
	actual := sha256Hex(data)
	if expectedSHA256 != "" && actual != expectedSHA256 {
		return "", fmt.Errorf("workspace: write SHA256 mismatch for %s: expected %s, got %s", clean, expectedSHA256, actual)
	}
	if err := s.w.WriteFile(clean, data); err != nil {
		return "", err
	}
	return actual, nil
}

func sha256Hex(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (s *fileServiceImpl) Identity(name string) (string, error) {
	f, _, err := s.w.OpenFile(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("workspace: hash %s: %w", name, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func translateStatError(err error) error {
	if errors.Is(err, ErrEscape) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return err
	}
	if strings.Contains(err.Error(), "path escapes") {
		return ErrEscape
	}
	return err
}
