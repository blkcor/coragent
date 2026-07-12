//go:build darwin

package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPreparedNewFileRespectsProcessUmask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	prepared := mustPrepareWrite(t, path, "secret")
	originalMask := unix.Umask(0o077)
	defer unix.Umask(originalMask)

	if _, err := (WriteFile{}).ExecutePrepared(context.Background(), prepared); err != nil {
		t.Fatalf("ExecutePrepared: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("created mode = %04o, want umask-derived 0600", got)
	}
}

func TestPreparedExistingFilePreservesSecurityMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "protected.txt")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	const attribute = "com.coragent.prepared-file-test"
	if err := unix.Setxattr(path, attribute, []byte("metadata"), 0); err != nil {
		t.Fatalf("Setxattr: %v", err)
	}
	if err := unix.Chflags(path, unix.UF_HIDDEN); err != nil {
		t.Fatalf("Chflags: %v", err)
	}
	command := exec.Command("/bin/chmod", "+a", "everyone allow read", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("add ACL: %v: %s", err, output)
	}
	var before unix.Stat_t
	if err := unix.Stat(path, &before); err != nil {
		t.Fatal(err)
	}

	prepared := mustPrepareWrite(t, path, "after")
	if _, err := (WriteFile{}).ExecutePrepared(context.Background(), prepared); err != nil {
		t.Fatalf("ExecutePrepared: %v", err)
	}
	assertFile(t, path, "after")

	var after unix.Stat_t
	if err := unix.Stat(path, &after); err != nil {
		t.Fatal(err)
	}
	if !sameSecurityMetadata(before, after) {
		t.Fatalf("security stat changed: before=%+v after=%+v", before, after)
	}
	value := make([]byte, 64)
	size, err := unix.Getxattr(path, attribute, value)
	if err != nil || string(value[:size]) != "metadata" {
		t.Fatalf("xattr value=%q size=%d err=%v", value[:max(size, 0)], size, err)
	}
	acl := exec.Command("/bin/ls", "-lde", path)
	output, err := acl.CombinedOutput()
	if err != nil {
		t.Fatalf("read ACL: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "group:everyone allow read") {
		t.Fatalf("file-specific ACL was not preserved: %s", output)
	}
}

func TestPreparedExistingFileRollsBackFinalIdentityRace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared := mustPrepareWrite(t, path, "candidate")
	racer := filepath.Join(root, "racer.txt")
	if err := os.WriteFile(racer, []byte("racer"), 0o644); err != nil {
		t.Fatal(err)
	}
	var racerStat unix.Stat_t
	if err := unix.Stat(racer, &racerStat); err != nil {
		t.Fatal(err)
	}
	originalHook := beforePreparedFileExchange
	beforePreparedFileExchange = func() {
		if err := os.Rename(racer, path); err != nil {
			t.Errorf("install racer: %v", err)
		}
	}
	defer func() { beforePreparedFileExchange = originalHook }()

	_, err := (WriteFile{}).ExecutePrepared(context.Background(), prepared)
	if !errors.Is(err, ErrStalePreparedAction) {
		t.Fatalf("ExecutePrepared error=%v, want stale", err)
	}
	assertFile(t, path, "racer")
	var targetStat unix.Stat_t
	if err := unix.Stat(path, &targetStat); err != nil {
		t.Fatal(err)
	}
	if !identityOf(targetStat).equal(identityOf(racerStat)) {
		t.Fatalf("rollback did not restore racer identity: got=%s want=%s", identityOf(targetStat).safeString(), identityOf(racerStat).safeString())
	}
	assertNoPreparedTemps(t, root)
}

func TestPreparedExistingFileRollsBackFinalMetadataRace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared := mustPrepareWrite(t, path, "candidate")
	const attribute = "com.coragent.prepared-file-race"
	var hookErr error
	originalHook := beforePreparedFileExchange
	beforePreparedFileExchange = func() {
		hookErr = unix.Setxattr(path, attribute, []byte("newer metadata"), 0)
	}
	defer func() { beforePreparedFileExchange = originalHook }()

	_, err := (WriteFile{}).ExecutePrepared(context.Background(), prepared)
	if hookErr != nil {
		t.Fatalf("install metadata race: %v", hookErr)
	}
	if !errors.Is(err, ErrStalePreparedAction) {
		t.Fatalf("ExecutePrepared error=%v, want stale", err)
	}
	assertFile(t, path, "before")
	value := make([]byte, 32)
	size, getErr := unix.Getxattr(path, attribute, value)
	if getErr != nil || string(value[:size]) != "newer metadata" {
		t.Fatalf("racing metadata was not restored: value=%q err=%v", value[:max(size, 0)], getErr)
	}
	assertNoPreparedTemps(t, root)
}

func TestPreparedExistingFileRefusesInheritedACLExpansion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/chmod", "+a", "everyone allow read,file_inherit", root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("add parent inheritable ACL: %v: %s", err, output)
	}
	prepared := mustPrepareWrite(t, path, "candidate")
	_, err := (WriteFile{}).ExecutePrepared(context.Background(), prepared)
	if !errors.Is(err, ErrIdentityCommitUnsupported) {
		t.Fatalf("ExecutePrepared error=%v, want ACL-preservation refusal", err)
	}
	assertFile(t, path, "before")
	acl := exec.Command("/bin/ls", "-lde", path)
	output, listErr := acl.CombinedOutput()
	if listErr != nil {
		t.Fatalf("read source ACL: %v: %s", listErr, output)
	}
	if strings.Contains(string(output), "inherited allow read") {
		t.Fatalf("source ACL was expanded: %s", output)
	}
	assertNoPreparedTemps(t, root)
}

func assertNoPreparedTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".coragent-") {
			t.Fatalf("temporary candidate leaked: %s", entry.Name())
		}
	}
}
