package sandbox

import (
	"path/filepath"
	"strconv"
	"strings"
)

// DarwinProfile generates a Seatbelt profile from a derived policy.
func DarwinProfile(p Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	writeReadDeny(&b, p)
	b.WriteString("(deny file-write*)\n")
	writeLiteral(&b, "file-write*", "/dev/null")
	for _, root := range seatbeltRoots(p.WriteRoots) {
		writeSubpath(&b, "file-read*", root)
		writeSubpath(&b, "file-write*", root)
	}
	if p.Network == NetworkAllowed {
		b.WriteString("(allow network*)\n")
	} else {
		b.WriteString("(deny network*)\n")
	}
	return b.String()
}

func writeReadDeny(b *strings.Builder, p Policy) {
	roots := seatbeltRoots(append(append([]string(nil), p.ReadRoots...), p.WriteRoots...))
	b.WriteString("(deny file-read* (require-all")
	writeRequireNotLiteral(b, "/")
	writeRequireNotLiteral(b, "/dev/null")
	for _, root := range roots {
		for _, parent := range parentLiterals(root) {
			writeRequireNotLiteral(b, parent)
		}
		writeRequireNotSubpath(b, root)
	}
	b.WriteString("))\n")
}

func seatbeltRoots(roots []string) []string {
	var out []string
	for _, root := range roots {
		out = append(out, root)
		if alias, ok := publicSymlinkAlias(root); ok {
			out = append(out, alias)
		}
	}
	return stableRoots(out)
}

func publicSymlinkAlias(path string) (string, bool) {
	for _, pair := range []struct {
		private string
		public  string
	}{
		{private: "/private/var", public: "/var"},
		{private: "/private/tmp", public: "/tmp"},
		{private: "/private/etc", public: "/etc"},
	} {
		if path == pair.private {
			return pair.public, true
		}
		prefix := pair.private + string(filepath.Separator)
		if strings.HasPrefix(path, prefix) {
			return pair.public + path[len(pair.private):], true
		}
	}
	return "", false
}

func writeSubpath(b *strings.Builder, op, path string) {
	b.WriteString("(allow ")
	b.WriteString(op)
	b.WriteString(" (subpath ")
	b.WriteString(strconv.Quote(path))
	b.WriteString("))\n")
}

func writeLiteral(b *strings.Builder, op, path string) {
	b.WriteString("(allow ")
	b.WriteString(op)
	b.WriteString(" (literal ")
	b.WriteString(strconv.Quote(path))
	b.WriteString("))\n")
}

func writeRequireNotSubpath(b *strings.Builder, path string) {
	b.WriteString(" (require-not (subpath ")
	b.WriteString(strconv.Quote(path))
	b.WriteString("))")
}

func writeRequireNotLiteral(b *strings.Builder, path string) {
	b.WriteString(" (require-not (literal ")
	b.WriteString(strconv.Quote(path))
	b.WriteString("))")
}

func parentLiterals(path string) []string {
	path = filepath.Clean(path)
	if path == "." || path == "/" {
		return nil
	}
	var parents []string
	for parent := filepath.Dir(path); parent != "." && parent != "/"; parent = filepath.Dir(parent) {
		parents = append(parents, parent)
	}
	return parents
}
