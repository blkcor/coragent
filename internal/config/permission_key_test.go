package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPermissionFingerprintKeyResultRepresentationsAreRedacted(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef")
	result := newPermissionFingerprintKeyResult(append([]byte(nil), raw...), PermissionFingerprintKeyExisting)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	logger.Info("fingerprint result", "result", result)
	joined := strings.Join([]string{
		fmt.Sprint(result), fmt.Sprintf("%+v", result), fmt.Sprintf("%#v", result), string(encoded), logs.String(),
	}, "\n")
	for _, secret := range []string{string(raw), hex.EncodeToString(raw)} {
		if strings.Contains(joined, secret) {
			t.Fatalf("fingerprint result leaked key material %q: %s", secret, joined)
		}
	}
	if !strings.Contains(strings.ToLower(joined), "redacted") {
		t.Fatalf("redaction is not explicit: %s", joined)
	}
}

func TestPermissionFingerprintKeyCreates0600AndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".coragent", permissionFingerprintKeyFile)
	first, err := loadOrCreatePermissionFingerprintKeyAt(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	second, err := loadOrCreatePermissionFingerprintKeyAt(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if first.Status != PermissionFingerprintKeyFresh || second.Status != PermissionFingerprintKeyExisting || len(first.Material()) != permissionFingerprintKeySize || !bytes.Equal(first.Material(), second.Material()) {
		t.Fatalf("key did not survive reload: first=%+v second=%+v", first, second)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v, want regular 0600", info.Mode())
	}
}

func TestPermissionFingerprintKeyPublicationIsConcurrentAndAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".coragent", permissionFingerprintKeyFile)
	const workers = 16
	results := make([]PermissionFingerprintKeyResult, workers)
	errs := make([]error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index], errs[index] = loadOrCreatePermissionFingerprintKeyAt(path)
		}()
	}
	wait.Wait()
	fresh := 0
	for index := range results {
		if errs[index] != nil {
			t.Fatalf("worker %d: %v", index, errs[index])
		}
		if !bytes.Equal(results[0].Material(), results[index].Material()) {
			t.Fatalf("worker %d observed a different key", index)
		}
		if results[index].Status == PermissionFingerprintKeyFresh {
			fresh++
		}
		if results[index].Status != PermissionFingerprintKeyFresh && results[index].Status != PermissionFingerprintKeyExisting {
			t.Fatalf("worker %d status = %q", index, results[index].Status)
		}
	}
	if fresh != 1 {
		t.Fatalf("fresh publishers = %d, want exactly 1", fresh)
	}
}

func TestPermissionFingerprintKeyRotatesUnsafeFiles(t *testing.T) {
	t.Run("broad mode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), permissionFingerprintKeyFile)
		old := bytes.Repeat([]byte{0x42}, permissionFingerprintKeySize)
		if err := os.WriteFile(path, old, 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := loadOrCreatePermissionFingerprintKeyAt(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if result.Status != PermissionFingerprintKeyRotated || bytes.Equal(result.Material(), old) {
			t.Fatalf("unsafe key was not rotated: %+v", result)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("symlink", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "target")
		if err := os.WriteFile(target, bytes.Repeat([]byte{0x42}, permissionFingerprintKeySize), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, permissionFingerprintKeyFile)
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		result, err := loadOrCreatePermissionFingerprintKeyAt(path)
		if err != nil || result.Status != PermissionFingerprintKeyRotated {
			t.Fatalf("symlink key was not safely rotated: result=%+v err=%v", result, err)
		}
		unchanged, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(unchanged, bytes.Repeat([]byte{0x42}, permissionFingerprintKeySize)) {
			t.Fatal("symlink rotation modified its target")
		}
	})

	t.Run("wrong length", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), permissionFingerprintKeyFile)
		if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := loadOrCreatePermissionFingerprintKeyAt(path)
		if err != nil || result.Status != PermissionFingerprintKeyRotated || len(result.Material()) != permissionFingerprintKeySize {
			t.Fatalf("wrong-length key was not rotated: result=%+v err=%v", result, err)
		}
	})

	t.Run("hard link", func(t *testing.T) {
		directory := t.TempDir()
		path := filepath.Join(directory, permissionFingerprintKeyFile)
		alias := filepath.Join(directory, "old-key-alias")
		old := bytes.Repeat([]byte{0x24}, permissionFingerprintKeySize)
		if err := os.WriteFile(path, old, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, alias); err != nil {
			t.Fatal(err)
		}
		result, err := loadOrCreatePermissionFingerprintKeyAt(path)
		if err != nil || result.Status != PermissionFingerprintKeyRotated || bytes.Equal(result.Material(), old) {
			t.Fatalf("hard-linked key was not rotated: result=%+v err=%v", result, err)
		}
		aliasData, err := os.ReadFile(alias)
		if err != nil || !bytes.Equal(aliasData, old) {
			t.Fatal("rotation modified the old hard-link alias")
		}
		assertNoPermissionKeyBackup(t, directory)
	})
}

func TestPermissionFingerprintKeyRejectsUnsafeParentWithoutReadingKey(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o770); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, permissionFingerprintKeyFile)
	old := bytes.Repeat([]byte{0x31}, permissionFingerprintKeySize)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrCreatePermissionFingerprintKeyAt(path)
	if err == nil || !strings.Contains(err.Error(), "unsafe") || !strings.Contains(err.Error(), "group/other write") {
		t.Fatalf("unsafe parent error = %v", err)
	}
	unchanged, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(unchanged, old) {
		t.Fatal("unsafe-parent failure modified the key")
	}
}

func TestPermissionFingerprintKeyRejectsSymlinkParent(t *testing.T) {
	container := t.TempDir()
	realParent := t.TempDir()
	linkedParent := filepath.Join(container, ".coragent")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(linkedParent, permissionFingerprintKeyFile)
	old := bytes.Repeat([]byte{0x32}, permissionFingerprintKeySize)
	if err := os.WriteFile(filepath.Join(realParent, permissionFingerprintKeyFile), old, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrCreatePermissionFingerprintKeyAt(path)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("symlink parent error = %v", err)
	}
	unchanged, readErr := os.ReadFile(filepath.Join(realParent, permissionFingerprintKeyFile))
	if readErr != nil || !bytes.Equal(unchanged, old) {
		t.Fatal("symlink-parent failure modified the target key")
	}
}

func TestPermissionKeyMetadataValidationRejectsWrongOwnerAndLinks(t *testing.T) {
	base := permissionPathSecurity{regular: true, uid: 501, permissions: 0o600, nlink: 1, size: permissionFingerprintKeySize}
	if err := validatePermissionKeyFile(base, 501); err != nil {
		t.Fatalf("secure metadata rejected: %v", err)
	}
	wrongOwner := base
	wrongOwner.uid = 502
	if err := validatePermissionKeyFile(wrongOwner, 501); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Fatalf("wrong owner error = %v", err)
	}
	extraLink := base
	extraLink.nlink = 2
	if err := validatePermissionKeyFile(extraLink, 501); err == nil || !strings.Contains(err.Error(), "link count") {
		t.Fatalf("extra link error = %v", err)
	}
	parent := permissionPathSecurity{directory: true, uid: 502, permissions: 0o700}
	if err := validatePermissionKeyParent(parent, 501); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Fatalf("wrong parent owner error = %v", err)
	}
}

func TestFreshOrRotatedFingerprintKeyScrubsAllDiskExactRules(t *testing.T) {
	for _, test := range []struct {
		name      string
		unsafeKey bool
		want      PermissionFingerprintKeyStatus
	}{
		{name: "missing key", want: PermissionFingerprintKeyFresh},
		{name: "unsafe existing key", unsafeKey: true, want: PermissionFingerprintKeyRotated},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			project := t.TempDir()
			t.Setenv("HOME", home)
			previous, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(project); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(previous) })
			homeSettings, err := HomeSettingsPath()
			if err != nil {
				t.Fatal(err)
			}
			legacy := "exact-v1:read:sha256:" + strings.Repeat("d", 64)
			keyed := "exact-v2:read:hmac-sha256:" + strings.Repeat("e", 64)
			writeRawSettings(t, homeSettings, 0o640, `{"model":{"api_key":"${PRESERVE_ME}"},"permission":{"allow":["`+legacy+`","`+keyed+`","read:README.md"],"deny":["`+legacy+`"]}}`)
			writeRawSettings(t, ProjectSettingsPath(), 0o600, `{"project_unknown":true,"permission":{"allow":["`+keyed+`"],"deny":["`+legacy+`","command:rm"]}}`)
			keyPath, err := PermissionFingerprintKeyPath()
			if err != nil {
				t.Fatal(err)
			}
			if test.unsafeKey {
				if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x55}, permissionFingerprintKeySize), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previousLogger) })
			result, err := LoadOrCreatePermissionFingerprintKey()
			if err != nil || result.Status != test.want {
				t.Fatalf("key result=%+v err=%v", result, err)
			}
			for _, path := range []string{homeSettings, ProjectSettingsPath()} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				text := string(data)
				if strings.Contains(text, "exact-v1") || strings.Contains(text, "exact-v2") || strings.Contains(text, strings.Repeat("d", 64)) || strings.Contains(text, strings.Repeat("e", 64)) {
					t.Fatalf("invalidated exact rule survived in %s: %s", path, text)
				}
			}
			homeData, _ := os.ReadFile(homeSettings)
			if !bytes.Contains(homeData, []byte("${PRESERVE_ME}")) || !bytes.Contains(homeData, []byte("read:README.md")) {
				t.Fatalf("home raw settings were not preserved: %s", homeData)
			}
			logText := logs.String()
			for _, forbidden := range []string{strings.Repeat("d", 64), strings.Repeat("e", 64), string(bytes.Repeat([]byte{0x55}, permissionFingerprintKeySize))} {
				if strings.Contains(logText, forbidden) {
					t.Fatalf("key lifecycle warning leaked secret material: %s", logText)
				}
			}
			assertNoPermissionKeyBackup(t, filepath.Dir(keyPath))
		})
	}
}

func assertNoPermissionKeyBackup(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, ".bak") || strings.HasSuffix(name, ".tmp") {
			t.Fatalf("unexpected key backup or temporary file %q", name)
		}
	}
}
