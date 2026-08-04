package fileid

import (
	"os"
	"testing"
)

func TestIdentityIsStableForOneFilesystemObject(t *testing.T) {
	dir := t.TempDir()
	first, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if firstID, secondID := FromInfo(first), FromInfo(second); firstID == "" || firstID != secondID {
		t.Fatalf("filesystem identity changed: %q != %q", firstID, secondID)
	}
}

func TestRecreatedDirectoryHasDifferentIdentityWhenPlatformSupportsBirthTime(t *testing.T) {
	parent := t.TempDir()
	dir := parent + "/workspace"
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	firstID := FromInfo(first)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	secondID := FromInfo(second)
	if !HasBirthTime(first) {
		t.Skip("platform file metadata does not expose birth time")
	}
	if firstID == secondID {
		t.Fatalf("recreated directory reused identity %q", firstID)
	}
}
