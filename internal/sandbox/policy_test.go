package sandbox

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDerivePolicyBaseline(t *testing.T) {
	wd := t.TempDir()
	scratch := t.TempDir()

	p, err := DerivePolicy(PolicyInputs{WorkingDirectory: wd, ScratchRoot: scratch})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if !p.CanWrite(filepath.Join(wd, "out.txt")) {
		t.Fatalf("project should be writable: %+v", p)
	}
	if !p.CanWrite(filepath.Join(scratch, "tmp.txt")) {
		t.Fatalf("scratch should be writable: %+v", p)
	}
	if p.CanWrite(filepath.Join(t.TempDir(), "outside.txt")) {
		t.Fatalf("outside path should not be writable: %+v", p)
	}
	if p.Network != NetworkDenied {
		t.Fatalf("network should be denied by default, got %s", p.Network)
	}
	if len(p.ReadRoots) <= len(p.WriteRoots)-1 {
		t.Fatalf("reads should be broader than writes, read=%v write=%v", p.ReadRoots, p.WriteRoots)
	}
}

func TestDerivePolicyDeterministicAndDeduplicated(t *testing.T) {
	wd := t.TempDir()
	extra := filepath.Join(wd, "shared")
	input := PolicyInputs{
		WorkingDirectory: wd,
		ScratchRoot:      t.TempDir(),
		Settings:         Grants{ExtraReadRoots: []string{extra, extra}, ExtraWriteRoots: []string{extra}},
		Permission:       Grants{ExtraReadRoots: []string{extra}, Network: true},
	}

	first, err := DerivePolicy(input)
	if err != nil {
		t.Fatalf("derive first: %v", err)
	}
	second, err := DerivePolicy(input)
	if err != nil {
		t.Fatalf("derive second: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("policy derivation must be deterministic:\n%+v\n%+v", first, second)
	}
	if first.Network != NetworkAllowed {
		t.Fatalf("network grant should allow network, got %s", first.Network)
	}
	canonExtra, err := canonicalPath(extra)
	if err != nil {
		t.Fatalf("canonical extra: %v", err)
	}
	if count(first.ReadRoots, canonExtra) != 1 {
		t.Fatalf("read roots should be deduplicated, got %v", first.ReadRoots)
	}
}

func TestReadDoesNotImplyWrite(t *testing.T) {
	wd := t.TempDir()
	readOnly := t.TempDir()
	p, err := DerivePolicy(PolicyInputs{
		WorkingDirectory: wd,
		ScratchRoot:      t.TempDir(),
		Settings:         Grants{ExtraReadRoots: []string{readOnly}},
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	target := filepath.Join(readOnly, "data.txt")
	if !p.CanRead(target) {
		t.Fatalf("extra read root should be readable")
	}
	if p.CanWrite(target) {
		t.Fatalf("extra read root must not be writable")
	}
}

func TestDerivePolicyAllowsActiveGoToolchain(t *testing.T) {
	p, err := DerivePolicy(PolicyInputs{
		WorkingDirectory: t.TempDir(),
		ScratchRoot:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go executable unavailable")
	}
	paths := []string{goPath}
	if goroot := discoverGoRoot(); goroot != "" {
		paths = append(paths, goroot)
	}
	for _, path := range paths {
		if !p.CanRead(path) {
			t.Fatalf("active Go tooling path should be readable by default: %s", path)
		}
	}
}

func count(xs []string, want string) int {
	var n int
	for _, x := range xs {
		if x == want {
			n++
		}
	}
	return n
}
