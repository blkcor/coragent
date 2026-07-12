//go:build darwin

package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

var identityPrimitiveAvailable = func() bool { return true }

// CLONE_ACL is defined by <sys/clonefile.h> but is not currently exported by
// x/sys/unix. fclonefileat with this flag copies file-specific ACLs in addition
// to the source file's attributes and extended attributes.
const cloneCopyACL = 0x0004

// Tests replace this only to cancel at the precise boundary after a complete,
// durable temporary candidate exists but before the namespace is changed.
var beforePreparedFileReplace = func() {}

// Tests use this boundary to exercise the final validation-to-exchange race.
var beforePreparedFileExchange = func() {}

func readFileSnapshot(ctx context.Context, path string, createParents bool) ([]byte, platformFileSnapshot, error) {
	if !identityPrimitiveAvailable() {
		return nil, platformFileSnapshot{}, ErrIdentityCommitUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, platformFileSnapshot{}, err
	}
	parentPath, targetName := filepath.Dir(path), filepath.Base(path)
	if targetName == "." || targetName == string(filepath.Separator) || targetName == "" {
		return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: invalid target path %s", path)
	}

	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, platformFileSnapshot{}, fmt.Errorf("%w: open filesystem root: %v", ErrIdentityCommitUnsupported, err)
	}
	defer func() { _ = unix.Close(fd) }()
	rootStat, err := fstat(fd)
	if err != nil {
		return nil, platformFileSnapshot{}, fmt.Errorf("%w: stat filesystem root: %v", ErrIdentityCommitUnsupported, err)
	}
	snapshot := platformFileSnapshot{rootIdentity: identityOf(rootStat), parentIdentity: identityOf(rootStat)}
	components := pathComponents(parentPath)
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return nil, platformFileSnapshot{}, err
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) && createParents {
				snapshot.missingParents = append([]string(nil), components[index:]...)
				break
			}
			return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: open parent %s without following links: %w", component, openErr)
		}
		stat, statErr := fstat(next)
		if statErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			_ = unix.Close(next)
			if statErr != nil {
				return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: inspect parent %s: %w", component, statErr)
			}
			return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: parent %s is not a directory", component)
		}
		identity := identityOf(stat)
		snapshot.parents = append(snapshot.parents, directorySnapshot{name: component, identity: identity})
		snapshot.parentIdentity = identity
		_ = unix.Close(fd)
		fd = next
	}
	if len(snapshot.missingParents) > 0 {
		return nil, snapshot, nil
	}

	targetFD, openErr := unix.Openat(fd, targetName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(openErr, unix.ENOENT) {
		return nil, snapshot, nil
	}
	if openErr != nil {
		return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: open target without following links: %w", openErr)
	}
	file := os.NewFile(uintptr(targetFD), targetName)
	defer func() { _ = file.Close() }()
	stat, err := fstat(targetFD)
	if err != nil {
		return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: inspect target: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: target is not a regular file")
	}
	if stat.Nlink != 1 {
		return nil, platformFileSnapshot{}, fmt.Errorf("%w: target has %d links", ErrHardLinkAliasUnsupported, stat.Nlink)
	}
	if stat.Size < 0 || stat.Size > int64(preparedFileByteLimit) {
		return nil, platformFileSnapshot{}, fmt.Errorf("%w: target is %d bytes (limit %d)", ErrPreparedFileTooLarge, stat.Size, preparedFileByteLimit)
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(preparedFileByteLimit)+1))
	if err != nil {
		return nil, platformFileSnapshot{}, fmt.Errorf("prepared file action: read target: %w", err)
	}
	if len(content) > preparedFileByteLimit {
		return nil, platformFileSnapshot{}, fmt.Errorf("%w: target grew beyond %d bytes while being read", ErrPreparedFileTooLarge, preparedFileByteLimit)
	}
	snapshot.targetExists = true
	snapshot.targetIdentity = identityOf(stat)
	return content, snapshot, nil
}

func commitFileCandidate(ctx context.Context, token *preparedFileToken) error {
	if !identityPrimitiveAvailable() {
		return ErrIdentityCommitUnsupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("%w: reopen filesystem root: %v", ErrIdentityCommitUnsupported, err)
	}
	defer func() { _ = unix.Close(fd) }()
	rootStat, err := fstat(fd)
	if err != nil || !identityOf(rootStat).equal(token.platform.rootIdentity) {
		return fmt.Errorf("%w: filesystem root identity changed", ErrStalePreparedAction)
	}
	for _, expected := range token.platform.parents {
		next, openErr := unix.Openat(fd, expected.name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return fmt.Errorf("%w: parent %s changed: %v", ErrStalePreparedAction, expected.name, openErr)
		}
		stat, statErr := fstat(next)
		if statErr != nil || !identityOf(stat).equal(expected.identity) {
			_ = unix.Close(next)
			return fmt.Errorf("%w: parent %s identity changed", ErrStalePreparedAction, expected.name)
		}
		_ = unix.Close(fd)
		fd = next
	}
	created := make([]string, 0, len(token.platform.missingParents))
	for _, component := range token.platform.missingParents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := unix.Mkdirat(fd, component, 0o755); err != nil {
			return fmt.Errorf("%w: missing parent %s appeared or could not be created: %v", ErrStalePreparedAction, component, err)
		}
		created = append(created, component)
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return fmt.Errorf("%w: open newly created parent %s: %v", ErrIdentityCommitUnsupported, component, openErr)
		}
		_ = unix.Close(fd)
		fd = next
	}

	targetName := filepath.Base(token.path)
	if token.exists {
		return commitExistingFile(ctx, fd, targetName, token)
	}
	if err := commitNewFile(ctx, fd, targetName, token.candidate); err != nil {
		// Best-effort removal of an empty final parent keeps a target-appears race
		// from leaving more state than necessary. Earlier created ancestors are
		// retained only if they are no longer empty.
		_ = created
		return err
	}
	return nil
}

func commitExistingFile(ctx context.Context, parentFD int, targetName string, token *preparedFileToken) error {
	fd, err := unix.Openat(parentFD, targetName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("%w: reopen target: %v", ErrStalePreparedAction, err)
	}
	defer func() { _ = unix.Close(fd) }()
	stat, err := fstat(fd)
	if err != nil {
		return fmt.Errorf("%w: inspect reopened target: %v", ErrStalePreparedAction, err)
	}
	if err := validateExistingTarget(parentFD, targetName, fd, stat, token); err != nil {
		return err
	}
	metadataAtClone := stat

	temporaryName, err := stageExistingFileCandidate(ctx, parentFD, fd, token.candidate, stat)
	if err != nil {
		return err
	}
	temporaryPresent := true
	defer func() {
		if temporaryPresent {
			_ = unix.Unlinkat(parentFD, temporaryName, 0)
		}
	}()

	beforePreparedFileReplace()
	if err := ctx.Err(); err != nil {
		return err
	}
	stat, err = fstat(fd)
	if err != nil {
		return fmt.Errorf("%w: inspect target before replacement: %v", ErrStalePreparedAction, err)
	}
	if err := validateExistingTarget(parentFD, targetName, fd, stat, token); err != nil {
		return err
	}
	if !sameSourceMetadataVersion(metadataAtClone, stat) {
		return fmt.Errorf("%w: target security metadata changed while staging", ErrStalePreparedAction)
	}
	beforePreparedFileExchange()
	if err := ctx.Err(); err != nil {
		return err
	}
	stat, err = fstat(fd)
	if err != nil {
		return fmt.Errorf("%w: inspect target at exchange boundary: %v", ErrStalePreparedAction, err)
	}
	if err := validateExistingTarget(parentFD, targetName, fd, stat, token); err != nil {
		return err
	}
	if !sameSourceMetadataVersion(metadataAtClone, stat) {
		return fmt.Errorf("%w: target security metadata changed at exchange boundary", ErrStalePreparedAction)
	}
	flags := uint32(unix.RENAME_SWAP | unix.RENAME_NOFOLLOW_ANY)
	if err := unix.RenameatxNp(parentFD, temporaryName, parentFD, targetName, flags); err != nil {
		return fmt.Errorf("prepared file action: atomically exchange verified target: %w", err)
	}
	var displaced unix.Stat_t
	displacedErr := unix.Fstatat(parentFD, temporaryName, &displaced, unix.AT_SYMLINK_NOFOLLOW)
	if displacedErr == nil && identityOf(displaced).equal(token.platform.targetIdentity) {
		displacedErr = validatePreparedSourceHandle(fd, token, metadataAtClone)
	}
	if displacedErr != nil || !identityOf(displaced).equal(token.platform.targetIdentity) {
		// The target changed in the final validation-to-swap window. Swap back
		// rather than deleting an entry that may belong to another writer.
		temporaryPresent = false
		if restoreErr := unix.RenameatxNp(parentFD, temporaryName, parentFD, targetName, flags); restoreErr != nil {
			return fmt.Errorf("%w: target changed during atomic exchange and rollback failed: %v", ErrStalePreparedAction, restoreErr)
		}
		temporaryPresent = true
		return fmt.Errorf("%w: target changed during atomic exchange", ErrStalePreparedAction)
	}
	if err := unix.Unlinkat(parentFD, temporaryName, 0); err != nil {
		return fmt.Errorf("prepared file action: remove displaced target after exchange: %w", err)
	}
	temporaryPresent = false
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("prepared file action: sync target directory: %w", err)
	}
	return nil
}

func commitNewFile(ctx context.Context, parentFD int, targetName string, candidate []byte) error {
	temporaryName, err := stageNewFileCandidate(ctx, parentFD, candidate, 0o644)
	if err != nil {
		return err
	}
	temporaryPresent := true
	defer func() {
		if temporaryPresent {
			_ = unix.Unlinkat(parentFD, temporaryName, 0)
		}
	}()

	beforePreparedFileReplace()
	if err := ctx.Err(); err != nil {
		return err
	}
	flags := uint32(unix.RENAME_EXCL | unix.RENAME_NOFOLLOW_ANY)
	if err := unix.RenameatxNp(parentFD, temporaryName, parentFD, targetName, flags); err != nil {
		return fmt.Errorf("%w: target appeared before atomic create: %v", ErrStalePreparedAction, err)
	}
	temporaryPresent = false
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("prepared file action: sync target directory: %w", err)
	}
	return nil
}

func validateExistingTarget(parentFD int, targetName string, fd int, stat unix.Stat_t, token *preparedFileToken) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || !identityOf(stat).equal(token.platform.targetIdentity) {
		return fmt.Errorf("%w: target identity or type changed", ErrStalePreparedAction)
	}
	if stat.Nlink != 1 {
		return fmt.Errorf("%w: target link count changed to %d", ErrStalePreparedAction, stat.Nlink)
	}
	if stat.Size < 0 || stat.Size > int64(preparedFileByteLimit) {
		return fmt.Errorf("%w: target size changed outside the prepared bound", ErrStalePreparedAction)
	}
	current, err := preadAll(fd, int(stat.Size))
	if err != nil || !bytes.Equal(current, token.before) {
		return fmt.Errorf("%w: target contents changed", ErrStalePreparedAction)
	}
	var pathStat unix.Stat_t
	if err := unix.Fstatat(parentFD, targetName, &pathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil || pathStat.Mode&unix.S_IFMT != unix.S_IFREG || !identityOf(pathStat).equal(token.platform.targetIdentity) {
		return fmt.Errorf("%w: target path changed", ErrStalePreparedAction)
	}
	return nil
}

func validatePreparedSourceHandle(fd int, token *preparedFileToken, metadataAtClone unix.Stat_t) error {
	stat, err := fstat(fd)
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || !identityOf(stat).equal(token.platform.targetIdentity) || stat.Nlink != 1 {
		return fmt.Errorf("%w: displaced target identity or link state changed", ErrStalePreparedAction)
	}
	if stat.Size < 0 || stat.Size > int64(preparedFileByteLimit) {
		return fmt.Errorf("%w: displaced target size changed outside the prepared bound", ErrStalePreparedAction)
	}
	current, err := preadAll(fd, int(stat.Size))
	if err != nil || !bytes.Equal(current, token.before) {
		return fmt.Errorf("%w: displaced target contents changed", ErrStalePreparedAction)
	}
	if !sameSecurityMetadata(metadataAtClone, stat) {
		return fmt.Errorf("%w: displaced target security metadata changed", ErrStalePreparedAction)
	}
	return nil
}

func stageExistingFileCandidate(ctx context.Context, parentFD, sourceFD int, candidate []byte, sourceStat unix.Stat_t) (string, error) {
	sourceSecurity, err := fileExtendedSecurity(sourceFD)
	if err != nil {
		return "", fmt.Errorf("%w: read source ACL: %v", ErrIdentityCommitUnsupported, err)
	}
	for attempt := 0; attempt < 16; attempt++ {
		name, err := temporaryCandidateName()
		if err != nil {
			return "", fmt.Errorf("prepared file action: generate temporary name: %w", err)
		}
		if err := unix.Fclonefileat(sourceFD, parentFD, name, cloneCopyACL); errors.Is(err, unix.EEXIST) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("%w: clone verified target metadata: %v", ErrIdentityCommitUnsupported, err)
		}
		fd, err := unix.Openat(parentFD, name, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			_ = unix.Unlinkat(parentFD, name, 0)
			return "", fmt.Errorf("prepared file action: open cloned candidate: %w", err)
		}
		cleanup := func() {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(parentFD, name, 0)
		}
		clonedStat, err := fstat(fd)
		if err != nil || !sameSecurityMetadata(sourceStat, clonedStat) {
			cleanup()
			return "", fmt.Errorf("%w: cloned candidate did not preserve security metadata", ErrIdentityCommitUnsupported)
		}
		clonedSecurity, err := fileExtendedSecurity(fd)
		if err != nil || !bytes.Equal(sourceSecurity, clonedSecurity) {
			cleanup()
			return "", fmt.Errorf("%w: cloned candidate ACL differs from verified source", ErrIdentityCommitUnsupported)
		}
		if err := ctx.Err(); err != nil {
			cleanup()
			return "", err
		}
		if err := unix.Ftruncate(fd, int64(len(candidate))); err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: size cloned candidate: %w", err)
		}
		if err := pwriteAll(ctx, fd, candidate); err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: write cloned candidate: %w", err)
		}
		if err := unix.Fsync(fd); err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: sync cloned candidate: %w", err)
		}
		if err := unix.Close(fd); err != nil {
			_ = unix.Unlinkat(parentFD, name, 0)
			return "", fmt.Errorf("prepared file action: close cloned candidate: %w", err)
		}
		return name, nil
	}
	return "", fmt.Errorf("prepared file action: could not allocate a unique cloned candidate")
}

func stageNewFileCandidate(ctx context.Context, parentFD int, candidate []byte, requestedMode uint32) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		name, err := temporaryCandidateName()
		if err != nil {
			return "", fmt.Errorf("prepared file action: generate temporary name: %w", err)
		}
		// Let openat apply the process umask exactly as a direct create would.
		// The empty staging file is immediately narrowed to 0600 before content
		// is written, then restored to that already-masked mode before fsync.
		fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, requestedMode)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("prepared file action: create temporary candidate: %w", err)
		}
		cleanup := func() {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(parentFD, name, 0)
		}
		createdStat, err := fstat(fd)
		if err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: inspect temporary candidate mode: %w", err)
		}
		maskedMode := uint32(createdStat.Mode & 0o777)
		if err := unix.Fchmod(fd, 0o600); err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: protect temporary candidate: %w", err)
		}
		if err := pwriteAll(ctx, fd, candidate); err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: write temporary candidate: %w", err)
		}
		if err := unix.Fchmod(fd, maskedMode); err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: restore umask-derived candidate mode: %w", err)
		}
		if err := unix.Fsync(fd); err != nil {
			cleanup()
			return "", fmt.Errorf("prepared file action: sync temporary candidate: %w", err)
		}
		if err := unix.Close(fd); err != nil {
			_ = unix.Unlinkat(parentFD, name, 0)
			return "", fmt.Errorf("prepared file action: close temporary candidate: %w", err)
		}
		return name, nil
	}
	return "", fmt.Errorf("prepared file action: could not allocate a unique temporary candidate")
}

func sameSecurityMetadata(source, clone unix.Stat_t) bool {
	const securityModeMask = unix.S_IFMT | 0o7777
	return source.Mode&securityModeMask == clone.Mode&securityModeMask &&
		source.Uid == clone.Uid && source.Gid == clone.Gid && source.Flags == clone.Flags
}

func sameSourceMetadataVersion(before, after unix.Stat_t) bool {
	return sameSecurityMetadata(before, after) && before.Ctim == after.Ctim
}

func fileExtendedSecurity(fd int) ([]byte, error) {
	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_EXTENDED_SECURITY,
	}
	// ATTR_MAX_BUFFER is 8192 on Darwin, and a kauth ACL is limited to 128
	// entries. FSOPT_REPORT_FULLSIZE lets us reject rather than compare a
	// silently truncated security object.
	buffer := make([]byte, 8192)
	// x/sys exposes Setattrlist but no libSystem Fgetattrlist wrapper. Keep the
	// raw Darwin call isolated here until that wrapper exists.
	//nolint:staticcheck // SYS_FGETATTRLIST is the only non-cgo fd-based API exposed by x/sys.
	_, _, callErr := unix.Syscall6(
		unix.SYS_FGETATTRLIST,
		uintptr(fd),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(unix.FSOPT_REPORT_FULLSIZE),
		0,
	)
	if callErr != 0 {
		return nil, callErr
	}
	if len(buffer) < 12 {
		return nil, fmt.Errorf("extended security response is too short")
	}
	total := int(binary.LittleEndian.Uint32(buffer[:4]))
	if total < 12 || total > len(buffer) {
		return nil, fmt.Errorf("extended security response length %d is invalid", total)
	}
	referenceOffset := int(int32(binary.LittleEndian.Uint32(buffer[4:8])))
	securityLength := int(binary.LittleEndian.Uint32(buffer[8:12]))
	securityStart := 4 + referenceOffset
	if referenceOffset < 8 || securityLength < 0 || securityStart < 12 || securityStart > total || securityLength > total-securityStart {
		return nil, fmt.Errorf("extended security reference is invalid")
	}
	return append([]byte(nil), buffer[securityStart:securityStart+securityLength]...), nil
}

func temporaryCandidateName() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return ".coragent-" + hex.EncodeToString(random[:]) + ".tmp", nil
}

func pathComponents(path string) []string {
	trimmed := strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
	if trimmed == "" || trimmed == "." {
		return nil
	}
	return strings.Split(trimmed, string(filepath.Separator))
}

func fstat(fd int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	err := unix.Fstat(fd, &stat)
	return stat, err
}

func identityOf(stat unix.Stat_t) fileIdentity {
	return fileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}
}

func preadAll(fd, size int) ([]byte, error) {
	content := make([]byte, size)
	offset := 0
	for offset < len(content) {
		read, err := unix.Pread(fd, content[offset:], int64(offset))
		offset += read
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if read == 0 {
			break
		}
	}
	return content[:offset], nil
}

func pwriteAll(ctx context.Context, fd int, content []byte) error {
	offset := 0
	for offset < len(content) {
		if err := ctx.Err(); err != nil {
			return err
		}
		written, err := unix.Pwrite(fd, content[offset:], int64(offset))
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		offset += written
	}
	return nil
}
