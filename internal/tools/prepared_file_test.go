package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/blkcor/coragent/internal/core"
)

func TestPreparedWriteCreateIsSideEffectFreeAndCommitsCandidate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing", "nested", "new.txt")
	prepared, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{
		"path": path, "content": "one\ntwo", "create_parents": true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preparation mutated parents: %v", err)
	}
	if prepared.Operation != core.ActionOperationCreate || prepared.Preview.Kind != core.ActionPreviewFileDiff || prepared.Preview.FileDiff == nil || prepared.Preview.FileDiff.AddedLines.Value != 2 {
		t.Fatalf("create preview = %+v", prepared.Preview)
	}
	if _, err := (WriteFile{}).ExecutePrepared(context.Background(), prepared); err != nil {
		t.Fatalf("ExecutePrepared: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "one\ntwo" {
		t.Fatalf("candidate content=%q err=%v", content, err)
	}
}

func TestPreparedWriteCancellationStopsBeforeFilesystemObservation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cancelled.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (WriteFile{}).Prepare(ctx, map[string]interface{}{"path": target, "content": "candidate"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Prepare error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled preparation touched target: %v", statErr)
	}
}

func TestPreparedEditProducesMultipleHunksAndExactCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi.txt")
	before := "first old\n" + strings.Repeat("middle\n", 10) + "last old\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := (EditFile{}).Prepare(context.Background(), map[string]interface{}{
		"path": path, "old_string": "old", "new_string": "new", "replace_all": true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	diff := prepared.Preview.FileDiff
	if diff == nil || len(diff.Hunks) != 2 || diff.ChangedRegions.Value != 2 || diff.AddedLines.Value != 2 || diff.RemovedLines.Value != 2 {
		t.Fatalf("multi-hunk preview = %+v", diff)
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != before {
		t.Fatal("preparation changed the target")
	}
	if _, err := (EditFile{}).ExecutePrepared(context.Background(), prepared); err != nil {
		t.Fatalf("ExecutePrepared: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != strings.ReplaceAll(before, "old", "new") {
		t.Fatalf("committed candidate = %q", after)
	}
}

func TestPreparedFilePreviewEmptyBinaryAndInvalidEncoding(t *testing.T) {
	t.Run("empty create", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.txt")
		prepared, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": ""})
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Preview.FileDiff == nil || prepared.Preview.FileDiff.AddedLines.Value != 0 {
			t.Fatalf("empty preview = %+v", prepared.Preview)
		}
	})

	tests := []struct {
		name      string
		before    []byte
		candidate string
	}{
		{"binary before", []byte{'a', 0, 'b'}, "text"},
		{"invalid candidate", []byte("text"), string([]byte{0xff, 0xfe})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "value.bin")
			if err := os.WriteFile(path, test.before, 0o644); err != nil {
				t.Fatal(err)
			}
			prepared, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": test.candidate})
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Preview.Kind != core.ActionPreviewMetadata || prepared.Preview.FileDiff == nil || !prepared.Preview.FileDiff.NonText || prepared.Preview.Text != "" {
				t.Fatalf("non-text preview = %+v", prepared.Preview)
			}
		})
	}
}

func TestPreparedPreviewBoundsPreserveAggregateCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	var before, after strings.Builder
	for index := 0; index < 1_000; index++ {
		fmt.Fprintf(&before, "old-%04d\n", index)
		fmt.Fprintf(&after, "new-%04d\n", index)
	}
	if err := os.WriteFile(path, []byte(before.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": after.String()})
	if err != nil {
		t.Fatal(err)
	}
	preview, diff := prepared.Preview, prepared.Preview.FileDiff
	if preview.Omission == nil || preview.Omission.Kind != core.OmissionPreviewBudget || preview.Omission.Recoverability != core.RecoverabilityUnrecoverable {
		t.Fatalf("missing preview omission: %+v", preview.Omission)
	}
	if len(preview.Text) > previewByteLimit || logicalLineCountForTest(preview.Text) > previewLineLimit || !utf8.ValidString(preview.Text) {
		t.Fatalf("retained preview exceeds bound: bytes=%d lines=%d", len(preview.Text), logicalLineCountForTest(preview.Text))
	}
	if diff.AddedLines.Value != 1_000 || diff.RemovedLines.Value != 1_000 {
		t.Fatalf("aggregate counts were bounded: %+v", diff)
	}
}

func TestPreparedFileBoundsCandidateSnapshotAndDiffWork(t *testing.T) {
	t.Run("oversized candidate", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "candidate.txt")
		_, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{
			"path": path, "content": strings.Repeat("x", preparedFileByteLimit+1),
		})
		if !errors.Is(err, ErrPreparedFileTooLarge) {
			t.Fatalf("Prepare error=%v, want size refusal", err)
		}
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("oversized candidate touched target: %v", statErr)
		}
	})

	t.Run("oversized sparse target", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sparse.txt")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(preparedFileByteLimit + 1); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": "small"})
		if !errors.Is(err, ErrPreparedFileTooLarge) {
			t.Fatalf("Prepare error=%v, want size refusal", err)
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() != preparedFileByteLimit+1 {
			t.Fatalf("sparse target changed: info=%v err=%v", info, statErr)
		}
	})

	t.Run("diff computation fallback", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "large-diff.txt")
		before := strings.Repeat("old line\n", diffComputationByteLimit/16)
		candidate := strings.Repeat("new line\n", diffComputationByteLimit/16)
		if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": candidate})
		if err != nil {
			t.Fatal(err)
		}
		preview, diff := prepared.Preview, prepared.Preview.FileDiff
		if preview.Kind != core.ActionPreviewMetadata || preview.Metadata["diff"] != "omitted_over_computation_budget" || preview.Omission == nil || preview.Omission.Kind != core.OmissionPreviewBudget {
			t.Fatalf("computation fallback = %+v", preview)
		}
		if preview.Text != "" || diff == nil || len(diff.Hunks) != 0 || !diff.BeforeBytes.Known || diff.BeforeBytes.Value != uint64(len(before)) || !diff.CandidateBytes.Known || diff.CandidateBytes.Value != uint64(len(candidate)) {
			t.Fatalf("fallback metadata = preview=%+v diff=%+v", preview, diff)
		}
		if diff.AddedLines.Known || diff.RemovedLines.Known || diff.ChangedRegions.Known {
			t.Fatalf("fallback fabricated aggregate diff counts: %+v", diff)
		}
	})
}

func TestPreparedFileCancellationBeforeAtomicReplaceLeavesTargetsIntact(t *testing.T) {
	tests := []struct {
		name       string
		before     string
		targetWant string
		exists     bool
	}{
		{name: "existing", before: "before", targetWant: "before", exists: true},
		{name: "new", exists: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "target.txt")
			if test.exists {
				if err := os.WriteFile(path, []byte(test.before), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			prepared := mustPrepareWrite(t, path, "candidate")
			ctx, cancel := context.WithCancel(context.Background())
			originalHook := beforePreparedFileReplace
			beforePreparedFileReplace = cancel
			defer func() { beforePreparedFileReplace = originalHook }()

			_, err := (WriteFile{}).ExecutePrepared(ctx, prepared)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("ExecutePrepared error=%v, want cancellation", err)
			}
			if test.exists {
				assertFile(t, path, test.targetWant)
			} else if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("cancelled create published target: %v", statErr)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".coragent-") {
					t.Fatalf("temporary candidate leaked: %s", entry.Name())
				}
			}
		})
	}
}

func TestPreparedFileCommitRejectsIdentityAndPathRaces(t *testing.T) {
	t.Run("same-byte inode replacement", func(t *testing.T) {
		path := writeFixture(t, "before")
		prepared := mustPrepareWrite(t, path, "after")
		replacement := filepath.Join(filepath.Dir(path), "replacement")
		if err := os.WriteFile(replacement, []byte("before"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
		assertStaleCommit(t, prepared)
		assertFile(t, path, "before")
	})

	t.Run("stale preimage", func(t *testing.T) {
		path := writeFixture(t, "before")
		prepared := mustPrepareWrite(t, path, "after")
		if err := os.WriteFile(path, []byte("newer"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertStaleCommit(t, prepared)
		assertFile(t, path, "newer")
	})

	t.Run("symlink retarget", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "target")
		if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared := mustPrepareWrite(t, path, "after")
		other := filepath.Join(root, "other")
		if err := os.WriteFile(other, []byte("other"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(other, path); err != nil {
			t.Fatal(err)
		}
		assertStaleCommit(t, prepared)
		assertFile(t, other, "other")
	})

	t.Run("parent rename swap", func(t *testing.T) {
		root := t.TempDir()
		parent := filepath.Join(root, "parent")
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(parent, "target")
		if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
			t.Fatal(err)
		}
		prepared := mustPrepareWrite(t, path, "after")
		moved := filepath.Join(root, "moved")
		if err := os.Rename(parent, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("replacement"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertStaleCommit(t, prepared)
		assertFile(t, path, "replacement")
		assertFile(t, filepath.Join(moved, "target"), "before")
	})

	t.Run("target appears before create", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "new.txt")
		prepared := mustPrepareWrite(t, path, "candidate")
		if err := os.WriteFile(path, []byte("racer"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertStaleCommit(t, prepared)
		assertFile(t, path, "racer")
	})
}

func TestPreparedFileRejectsHardLinksAndUnsupportedIdentity(t *testing.T) {
	t.Run("preexisting out-of-directory alias", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "target")
		if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
			t.Fatal(err)
		}
		aliasRoot := t.TempDir()
		if err := os.Link(path, filepath.Join(aliasRoot, "alias")); err != nil {
			t.Fatal(err)
		}
		_, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": "after"})
		if !errors.Is(err, ErrHardLinkAliasUnsupported) {
			t.Fatalf("Prepare error=%v, want hard-link refusal", err)
		}
		assertFile(t, path, "before")
	})

	t.Run("hard link added before commit", func(t *testing.T) {
		path := writeFixture(t, "before")
		prepared := mustPrepareWrite(t, path, "after")
		alias := filepath.Join(filepath.Dir(path), "alias")
		if err := os.Link(path, alias); err != nil {
			t.Fatal(err)
		}
		assertStaleCommit(t, prepared)
		assertFile(t, path, "before")
		assertFile(t, alias, "before")
	})

	t.Run("unsupported primitive", func(t *testing.T) {
		original := identityPrimitiveAvailable
		identityPrimitiveAvailable = func() bool { return false }
		defer func() { identityPrimitiveAvailable = original }()
		path := filepath.Join(t.TempDir(), "target")
		_, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": "value"})
		if !errors.Is(err, ErrIdentityCommitUnsupported) {
			t.Fatalf("Prepare error=%v, want unsupported", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unsupported preparation mutated target: %v", err)
		}
	})
}

func TestPreparedFileRefusesSymlinkTargetAtPreparation(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(real, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": link, "content": "after"}); err == nil {
		t.Fatal("symlink target was prepared")
	}
	assertFile(t, real, "before")
}

func mustPrepareWrite(t *testing.T, path, content string) core.PreparedAction {
	t.Helper()
	prepared, err := (WriteFile{}).Prepare(context.Background(), map[string]interface{}{"path": path, "content": content})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return prepared
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertStaleCommit(t *testing.T, prepared core.PreparedAction) {
	t.Helper()
	_, err := (WriteFile{}).ExecutePrepared(context.Background(), prepared)
	if !errors.Is(err, ErrStalePreparedAction) {
		t.Fatalf("ExecutePrepared error=%v, want stale", err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != want {
		t.Fatalf("%s content=%q err=%v, want %q", path, content, err, want)
	}
}

func logicalLineCountForTest(value string) int {
	if value == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(value, "\n"), "\n") + 1
}
