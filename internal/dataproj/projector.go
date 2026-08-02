// Package dataproj classifies content and builds safe Transcript, model, and
// display projections before content crosses those boundaries.
package dataproj

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const DetectorVersion = "credential-detector-v2"
const ProjectionVersion = "data-projection-v2"

var ErrSensitivePrompt = errors.New("data projection: prompt contains detected credential material")

// Class is the data classification applied before projection.
type Class string

const (
	ClassNormal        Class = "normal"
	ClassSensitive     Class = "sensitive"
	ClassRuntimeSecret Class = "runtime_secret"
)

type detectorRule struct {
	name string
	re   *regexp.Regexp
}

// Detector contains only high-confidence patterns. It intentionally does not
// claim to identify arbitrary unlabeled confidential values.
type Detector struct {
	rules []detectorRule
}

func NewDetector() *Detector {
	return &Detector{rules: []detectorRule{
		{name: "openai_key", re: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
		{name: "github_token", re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`)},
		{name: "github_fine_grained_token", re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`)},
		{name: "npm_token", re: regexp.MustCompile(`\bnpm_[A-Za-z0-9]{30,}\b`)},
		{name: "gitlab_token", re: regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)},
		{name: "pypi_token", re: regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]{30,}\b`)},
		{name: "aws_access_key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
		{name: "google_api_key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
		{name: "private_key", re: regexp.MustCompile(`(?s)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----.*?(?:-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----|\z)`)},
	}}
}

// Match reports safe metadata about one match, never the matched value.
type Match struct {
	Rule  string
	Start int
	End   int
}

func (d *Detector) Matches(text string) []Match {
	var matches []Match
	for _, rule := range d.rules {
		for _, loc := range rule.re.FindAllStringIndex(text, -1) {
			matches = append(matches, Match{Rule: rule.name, Start: loc[0], End: loc[1]})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].End > matches[j].End
		}
		return matches[i].Start < matches[j].Start
	})
	// Merge overlaps so replacement never reveals a suffix from a second rule.
	merged := matches[:0]
	for _, match := range matches {
		if len(merged) > 0 && match.Start <= merged[len(merged)-1].End {
			if match.End > merged[len(merged)-1].End {
				merged[len(merged)-1].End = match.End
			}
			continue
		}
		merged = append(merged, match)
	}
	return merged
}

func (d *Detector) Contains(text string) bool { return len(d.Matches(text)) != 0 }

// Redact replaces every detected value with a stable non-sensitive marker.
func (d *Detector) Redact(text string) (string, int) {
	matches := d.Matches(text)
	if len(matches) == 0 {
		return text, 0
	}
	var out strings.Builder
	last := 0
	for _, match := range matches {
		out.WriteString(text[last:match.Start])
		out.WriteString("[REDACTED:CREDENTIAL]")
		last = match.End
	}
	out.WriteString(text[last:])
	return out.String(), len(matches)
}

// Projector owns the versioned detector and protected-path policy.
type Projector struct {
	detector *Detector
}

func New() *Projector { return &Projector{detector: NewDetector()} }

func (p *Projector) Detector() *Detector { return p.detector }

// ValidatePrompt rejects a detected credential without returning or logging
// the matched value.
func (p *Projector) ValidatePrompt(text string) error {
	if p.detector.Contains(text) {
		return ErrSensitivePrompt
	}
	return nil
}

// Text is a projected content value safe for transcript, model, and display.
type Text struct {
	Content       string
	Class         Class
	RedactedCount int
}

func (p *Projector) ProjectText(raw string) Text {
	content, count := p.detector.Redact(raw)
	class := ClassNormal
	if count > 0 {
		class = ClassSensitive
	}
	return Text{Content: content, Class: class, RedactedCount: count}
}

// ProtectedPath reports paths whose raw contents may not cross any product
// boundary. Names can still appear in list results.
func ProtectedPath(name string) bool {
	clean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./")
	if strings.EqualFold(clean, ".config/gh/hosts.yml") || strings.HasSuffix(strings.ToLower(clean), "/.config/gh/hosts.yml") {
		return true
	}
	base := path.Base(clean)
	lower := strings.ToLower(base)
	parts := strings.Split(strings.ToLower(clean), "/")
	for _, part := range parts {
		switch part {
		case ".ssh", ".aws", ".gnupg", ".kube", ".docker":
			return true
		}
	}
	if lower == ".env" || strings.HasPrefix(lower, ".env.") {
		return !strings.HasSuffix(lower, ".example") && !strings.HasSuffix(lower, ".sample")
	}
	switch lower {
	case ".netrc", ".npmrc", ".pypirc", ".git-credentials", "credentials", "auth.json", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	switch strings.ToLower(path.Ext(lower)) {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	}
	return false
}

// ProtectedProjection returns structural metadata without raw content.
func ProtectedProjection(name string, size int64, mode string, raw []byte) string {
	digest := sha256.Sum256(raw)
	return ProtectedProjectionDigest(name, size, mode, hex.EncodeToString(digest[:]))
}

// ProtectedProjectionDigest builds the safe structural view when the caller
// hashed a large protected file incrementally without retaining raw content.
func ProtectedProjectionDigest(name string, size int64, mode, digest string) string {
	return fmt.Sprintf("path: %s\nclassification: sensitive\nsize: %d\nmode: %s\nsha256: %s\ncontent: [REDACTED:PROTECTED_PATH]",
		name, size, mode, digest)
}

// IsText rejects invalid UTF-8 and likely binary buffers.
func IsText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

var (
	privateKeyBegin = regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)
	privateKeyEnd   = regexp.MustCompile(`-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)
)

// LineRedactor retains private-key block state across lines. It is used by
// file tools and stream projection so a PEM body can never be emitted merely
// because its BEGIN and END markers arrived in different chunks.
type LineRedactor struct {
	detector     *Detector
	inPrivateKey bool
}

func NewLineRedactor(d *Detector) *LineRedactor {
	if d == nil {
		d = NewDetector()
	}
	return &LineRedactor{detector: d}
}

func (r *LineRedactor) Redact(line string) (string, int) {
	const marker = "[REDACTED:CREDENTIAL]"
	if r.inPrivateKey {
		end := privateKeyEnd.FindStringIndex(line)
		if end == nil {
			return marker, 1
		}
		r.inPrivateKey = false
		suffix, count := r.detector.Redact(line[end[1]:])
		return marker + suffix, count + 1
	}
	begin := privateKeyBegin.FindStringIndex(line)
	if begin == nil {
		return r.detector.Redact(line)
	}
	prefix, prefixCount := r.detector.Redact(line[:begin[0]])
	remainder := line[begin[1]:]
	end := privateKeyEnd.FindStringIndex(remainder)
	if end == nil {
		r.inPrivateKey = true
		return prefix + marker, prefixCount + 1
	}
	suffix, suffixCount := r.detector.Redact(remainder[end[1]:])
	return prefix + marker + suffix, prefixCount + suffixCount + 1
}

// StreamRedactor holds an unfinished line until a newline or stream end and
// projects complete lines through one stateful LineRedactor.
type StreamRedactor struct {
	lines   *LineRedactor
	pending string
}

func NewStreamRedactor(d *Detector) *StreamRedactor {
	if d == nil {
		d = NewDetector()
	}
	return &StreamRedactor{lines: NewLineRedactor(d)}
}

// Write returns complete projected lines. The unfinished final line remains
// buffered because a later chunk could turn it into a credential match.
func (r *StreamRedactor) Write(chunk string) string {
	r.pending += chunk
	cut := strings.LastIndexByte(r.pending, '\n')
	if cut < 0 {
		return ""
	}
	cut++
	prefix := r.pending[:cut]
	r.pending = r.pending[cut:]
	var projected strings.Builder
	for _, line := range strings.SplitAfter(prefix, "\n") {
		if line == "" {
			continue
		}
		content := strings.TrimSuffix(line, "\n")
		safe, _ := r.lines.Redact(content)
		projected.WriteString(safe)
		projected.WriteByte('\n')
	}
	return projected.String()
}

func (r *StreamRedactor) Close() string {
	projected, _ := r.lines.Redact(r.pending)
	r.pending = ""
	return projected
}
