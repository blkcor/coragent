//go:build darwin || linux

package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const permissionFingerprintLockFile = ".permission-fingerprint.lock"

type permissionKeyInspection uint8

const (
	permissionKeyMissing permissionKeyInspection = iota
	permissionKeySecure
	permissionKeyUnsafe
)

func platformLoadOrCreatePermissionFingerprintKey(path string, beforePublish func() error) (PermissionFingerprintKeyResult, error) {
	directory := filepath.Dir(path)
	if err := os.Mkdir(directory, 0o700); err != nil && !os.IsExist(err) {
		return PermissionFingerprintKeyResult{}, fmt.Errorf("create permission fingerprint key directory: %w", err)
	}
	parentFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return PermissionFingerprintKeyResult{}, unsafePermissionKeyParentError(directory, err)
	}
	defer unix.Close(parentFD)
	if err := validatePermissionParentFD(parentFD); err != nil {
		return PermissionFingerprintKeyResult{}, unsafePermissionKeyParentError(directory, err)
	}

	lockFD, err := unix.Openat(parentFD, permissionFingerprintLockFile, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.EEXIST) {
		lockFD, err = unix.Openat(parentFD, permissionFingerprintLockFile, unix.O_RDWR|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return PermissionFingerprintKeyResult{}, fmt.Errorf("open permission fingerprint key lock: %w", err)
	}
	defer unix.Close(lockFD)
	if err := validatePermissionLockFD(lockFD); err != nil {
		return PermissionFingerprintKeyResult{}, fmt.Errorf("permission fingerprint key lock is unsafe: %w", err)
	}
	if err := unix.Flock(lockFD, unix.LOCK_EX); err != nil {
		return PermissionFingerprintKeyResult{}, fmt.Errorf("lock permission fingerprint key lifecycle: %w", err)
	}
	defer unix.Flock(lockFD, unix.LOCK_UN)

	name := filepath.Base(path)
	inspection, key, err := inspectPermissionKeyAt(parentFD, name)
	if err != nil {
		return PermissionFingerprintKeyResult{}, err
	}
	if inspection == permissionKeySecure {
		return newPermissionFingerprintKeyResult(key, PermissionFingerprintKeyExisting), nil
	}

	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return PermissionFingerprintKeyResult{}, fmt.Errorf("scrub invalidated exact permission rules before key publication: %w", err)
		}
	}
	generated := make([]byte, permissionFingerprintKeySize)
	if _, err := rand.Read(generated); err != nil {
		return PermissionFingerprintKeyResult{}, fmt.Errorf("generate permission fingerprint key: %w", err)
	}
	if err := publishPermissionKeyAt(parentFD, name, generated); err != nil {
		clear(generated)
		return PermissionFingerprintKeyResult{}, err
	}
	status := PermissionFingerprintKeyFresh
	if inspection == permissionKeyUnsafe {
		status = PermissionFingerprintKeyRotated
	}
	return newPermissionFingerprintKeyResult(generated, status), nil
}

func validatePermissionParentFD(fd int) error {
	stat, err := permissionFstat(fd)
	if err != nil {
		return err
	}
	acl, err := permissionFDHasExtendedACL(fd, true)
	if err != nil {
		return fmt.Errorf("inspect parent extended ACL: %w", err)
	}
	return validatePermissionKeyParent(permissionSecurityFromStat(stat, acl), uint32(unix.Geteuid()))
}

func validatePermissionLockFD(fd int) error {
	stat, err := permissionFstat(fd)
	if err != nil {
		return err
	}
	acl, err := permissionFDHasExtendedACL(fd, false)
	if err != nil {
		return fmt.Errorf("inspect lock extended ACL: %w", err)
	}
	security := permissionSecurityFromStat(stat, acl)
	switch {
	case !security.regular:
		return fmt.Errorf("not a regular file")
	case security.uid != uint32(unix.Geteuid()):
		return fmt.Errorf("owned by a different uid")
	case security.permissions != 0o600:
		return fmt.Errorf("mode is %04o instead of 0600", security.permissions)
	case security.nlink != 1:
		return fmt.Errorf("link count is %d instead of 1", security.nlink)
	case security.extendedACL:
		return fmt.Errorf("extended ACL is present")
	default:
		return nil
	}
}

func inspectPermissionKeyAt(parentFD int, name string) (permissionKeyInspection, []byte, error) {
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return permissionKeyMissing, nil, nil
		}
		var pathStat unix.Stat_t
		if statErr := unix.Fstatat(parentFD, name, &pathStat, unix.AT_SYMLINK_NOFOLLOW); statErr == nil {
			return permissionKeyUnsafe, nil, nil
		}
		return permissionKeyMissing, nil, fmt.Errorf("open permission fingerprint key without following links: %w", err)
	}
	defer unix.Close(fd)
	stat, err := permissionFstat(fd)
	if err != nil {
		return permissionKeyUnsafe, nil, nil
	}
	acl, err := permissionFDHasExtendedACL(fd, false)
	if err != nil {
		return permissionKeyUnsafe, nil, nil
	}
	if err := validatePermissionKeyFile(permissionSecurityFromStat(stat, acl), uint32(unix.Geteuid())); err != nil {
		return permissionKeyUnsafe, nil, nil
	}
	key := make([]byte, permissionFingerprintKeySize)
	offset := 0
	for offset < len(key) {
		read, readErr := unix.Pread(fd, key[offset:], int64(offset))
		offset += read
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			clear(key)
			return permissionKeyUnsafe, nil, nil
		}
		if read == 0 {
			clear(key)
			return permissionKeyUnsafe, nil, nil
		}
	}
	return permissionKeySecure, key, nil
}

func publishPermissionKeyAt(parentFD int, name string, key []byte) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("generate temporary permission fingerprint key name: %w", err)
	}
	temporaryName := ".permission-fingerprint-key-" + hex.EncodeToString(random[:]) + ".tmp"
	fd, err := unix.Openat(parentFD, temporaryName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary permission fingerprint key: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = unix.Close(fd)
		}
		_ = unix.Unlinkat(parentFD, temporaryName, 0)
	}()
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return fmt.Errorf("secure temporary permission fingerprint key mode: %w", err)
	}
	offset := 0
	for offset < len(key) {
		written, writeErr := unix.Write(fd, key[offset:])
		offset += written
		if writeErr != nil {
			return fmt.Errorf("write temporary permission fingerprint key: %w", writeErr)
		}
		if written == 0 {
			return fmt.Errorf("write temporary permission fingerprint key: short write")
		}
	}
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync temporary permission fingerprint key: %w", err)
	}
	fdStat, err := permissionFstat(fd)
	if err != nil {
		return fmt.Errorf("inspect temporary permission fingerprint key: %w", err)
	}
	acl, err := permissionFDHasExtendedACL(fd, false)
	if err != nil {
		return fmt.Errorf("inspect temporary permission fingerprint key ACL: %w", err)
	}
	if err := validatePermissionKeyFile(permissionSecurityFromStat(fdStat, acl), uint32(unix.Geteuid())); err != nil {
		return fmt.Errorf("temporary permission fingerprint key is unsafe: %w", err)
	}
	var pathStat unix.Stat_t
	if err := unix.Fstatat(parentFD, temporaryName, &pathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil || pathStat.Dev != fdStat.Dev || pathStat.Ino != fdStat.Ino {
		return fmt.Errorf("temporary permission fingerprint key identity changed before publication")
	}
	if err := unix.Renameat(parentFD, temporaryName, parentFD, name); err != nil {
		return fmt.Errorf("atomically publish permission fingerprint key: %w", err)
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync permission fingerprint key directory: %w", err)
	}
	if err := unix.Close(fd); err != nil {
		return fmt.Errorf("close published permission fingerprint key: %w", err)
	}
	closed = true
	return nil
}

func permissionSecurityFromStat(stat unix.Stat_t, extendedACL bool) permissionPathSecurity {
	mode := uint32(stat.Mode)
	return permissionPathSecurity{
		regular: mode&unix.S_IFMT == unix.S_IFREG, directory: mode&unix.S_IFMT == unix.S_IFDIR,
		uid: stat.Uid, permissions: mode & 0o7777, nlink: uint64(stat.Nlink), size: stat.Size,
		extendedACL: extendedACL,
	}
}

func permissionFstat(fd int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	err := unix.Fstat(fd, &stat)
	return stat, err
}

func unsafePermissionKeyParentError(path string, cause error) error {
	return fmt.Errorf(
		"permission fingerprint key parent %s is unsafe: %v; ensure it is owned by the current user, remove extended ACLs, and remove group/other write permission before retrying",
		path, cause,
	)
}
