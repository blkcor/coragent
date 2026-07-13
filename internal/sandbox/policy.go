package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
)

// NetworkMode describes whether sandboxed commands can reach the network.
type NetworkMode string

const (
	NetworkDenied  NetworkMode = "denied"
	NetworkAllowed NetworkMode = "allowed"
)

// ConfinementLevel describes how strongly the active backend enforces policy.
type ConfinementLevel string

const (
	ConfinementOSEnforced     ConfinementLevel = "os-enforced"
	ConfinementPolicyFallback ConfinementLevel = "policy-fallback"
)

// Status reports the active sandbox strength.
type Status struct {
	Level  ConfinementLevel
	Reason string
}

// Grants are additive policy extensions from configuration or permission
// decisions.
type Grants struct {
	ExtraReadRoots  []string
	ExtraWriteRoots []string
	Network         bool
}

// PolicyInputs are the deterministic inputs to policy derivation.
type PolicyInputs struct {
	WorkingDirectory string
	ScratchRoot      string
	Settings         Grants
	Permission       Grants
}

// Policy is the backend-neutral sandbox contract.
type Policy struct {
	ReadRoots   []string
	WriteRoots  []string
	ScratchRoot string
	Network     NetworkMode
}

// DerivePolicy builds a stable sandbox policy from the safe baseline plus
// additive grants.
func DerivePolicy(in PolicyInputs) (Policy, error) {
	wd, err := canonicalPath(in.WorkingDirectory)
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: working directory: %w", err)
	}
	scratch := in.ScratchRoot
	if scratch == "" {
		scratch = os.TempDir()
	}
	scratch, err = canonicalPath(scratch)
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: scratch root: %w", err)
	}

	var readRoots []string
	readRoots = append(readRoots, wd)
	readRoots, err = appendCanonical(readRoots, defaultReadRoots()...)
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: default read root: %w", err)
	}
	readRoots, err = appendCanonical(readRoots, in.Settings.ExtraReadRoots...)
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: extra read root: %w", err)
	}
	readRoots, err = appendCanonical(readRoots, in.Permission.ExtraReadRoots...)
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: permission read root: %w", err)
	}

	var writeRoots []string
	writeRoots = append(writeRoots, wd, scratch)
	writeRoots, err = appendCanonical(writeRoots, in.Settings.ExtraWriteRoots...)
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: extra write root: %w", err)
	}
	writeRoots, err = appendCanonical(writeRoots, in.Permission.ExtraWriteRoots...)
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: permission write root: %w", err)
	}

	network := NetworkDenied
	if in.Settings.Network || in.Permission.Network {
		network = NetworkAllowed
	}

	return Policy{
		ReadRoots:   stableRoots(readRoots),
		WriteRoots:  stableRoots(writeRoots),
		ScratchRoot: scratch,
		Network:     network,
	}, nil
}

// CanWrite reports whether path is inside one of the policy's write roots.
func (p Policy) CanWrite(path string) bool {
	canon, err := canonicalPath(path)
	if err != nil {
		return false
	}
	return underAnyRoot(canon, p.WriteRoots)
}

// CanRead reports whether path is inside one of the policy's read or write roots.
func (p Policy) CanRead(path string) bool {
	canon, err := canonicalPath(path)
	if err != nil {
		return false
	}
	return underAnyRoot(canon, append(append([]string(nil), p.ReadRoots...), p.WriteRoots...))
}

func appendCanonical(dst []string, paths ...string) ([]string, error) {
	for _, p := range paths {
		canon, err := canonicalPath(p)
		if err != nil {
			return nil, err
		}
		dst = append(dst, canon)
	}
	return dst, nil
}

func canonicalPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, nil
	}
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	if realParent, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(realParent, base), nil
	}
	return path, nil
}

func stableRoots(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func defaultReadRoots() []string {
	roots := []string{"/bin", "/usr/bin", "/usr/lib", "/usr/local/bin"}
	if runtime.GOOS == "darwin" {
		roots = append(roots,
			"/System",
			"/Library",
			"/usr/libexec",
			"/private/var/select",
			"/private/etc",
			"/opt/homebrew/bin",
			"/opt/homebrew/opt",
			"/opt/homebrew/Cellar",
		)
		// macOS developer tools (git, etc.) need read access to Xcode or
		// Command Line Tools. Git stat()s Xcode's Info.plist as part of
		// its startup path, so the entire .app bundle must be readable,
		// not just Contents/Developer.
		for _, candidate := range []string{
			"/Applications/Xcode.app",
			"/Library/Developer/CommandLineTools",
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				roots = append(roots, candidate)
			}
		}
	}
	if goroot := discoverGoRoot(); filepath.IsAbs(goroot) {
		roots = append(roots, goroot)
	}
	for _, pathRoot := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(pathRoot) {
			roots = append(roots, pathRoot)
		}
	}
	if moduleRoot := os.Getenv("GOMODCACHE"); filepath.IsAbs(moduleRoot) {
		roots = append(roots, moduleRoot)
	} else if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, root := range filepath.SplitList(gopath) {
			if filepath.IsAbs(root) {
				roots = append(roots, filepath.Join(root, "pkg", "mod"))
			}
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "go", "pkg", "mod"))
	}
	return roots
}

func discoverGoRoot() string {
	if configured := os.Getenv("GOROOT"); filepath.IsAbs(configured) {
		return filepath.Clean(configured)
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(goPath)
	if err != nil {
		resolved = goPath
	}
	candidate := filepath.Dir(filepath.Dir(resolved))
	if info, err := os.Stat(filepath.Join(candidate, "src")); err == nil && info.IsDir() {
		return candidate
	}
	return ""
}

func underAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if path == root {
			return true
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != "." && rel != ".." && !startsWithParent(rel) {
			return true
		}
	}
	return false
}

func startsWithParent(path string) bool {
	return path == ".." || len(path) > 3 && path[:3] == "../"
}
