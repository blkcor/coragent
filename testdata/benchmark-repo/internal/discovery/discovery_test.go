package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoveryExclusionOrdering(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"main.go", ".root/visible.go", ".tmp/ignored.go", "vendor/lib/ignored.go",
		"pkg/.hidden/ignored.go", "pkg/report.json", "pkg/REPORT.JSON",
	} {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Discover(root, []string{".go", ".json"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".root/visible.go", "main.go", "pkg/report.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}
