// Package benchmark implements the deterministic Mercury fixture and M1
// investigation scorer. Goldens, seeds, and triggers remain outside every
// model-visible attempt workspace.
package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const BaseVersion = "mercury-v1"
const TaskPackVersion = "m1-taskpack-v1"

type Manifest struct {
	BaseVersion string `json:"base_version"`
	SHA256      string `json:"sha256"`
}

func LoadManifest(name string) (Manifest, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.BaseVersion == "" || manifest.SHA256 == "" {
		return Manifest{}, errors.New("benchmark: incomplete fixture manifest")
	}
	return manifest, nil
}

// DigestTree hashes each relative path, separator, bytes, and separator in
// lexical order. Any base file or name change changes the result.
func DigestTree(root string) (string, error) {
	var names []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func CopyFixture(source, destination string) error {
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("benchmark: fixture contains symlink %s", relative)
		}
		if entry.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		sourceFile, err := os.Open(name)
		if err != nil {
			return err
		}
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = sourceFile.Close()
			return err
		}
		_, copyErr := io.Copy(targetFile, sourceFile)
		sourceCloseErr := sourceFile.Close()
		targetCloseErr := targetFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if sourceCloseErr != nil {
			return sourceCloseErr
		}
		return targetCloseErr
	})
}

func ValidateFrozenBase(fixtureRoot, manifestPath string) error {
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	if manifest.BaseVersion != BaseVersion {
		return fmt.Errorf("benchmark: base version %q, want %q", manifest.BaseVersion, BaseVersion)
	}
	digest, err := DigestTree(fixtureRoot)
	if err != nil {
		return err
	}
	if !strings.EqualFold(digest, manifest.SHA256) {
		return fmt.Errorf("benchmark: fixture digest %s, manifest has %s", digest, manifest.SHA256)
	}
	return nil
}
