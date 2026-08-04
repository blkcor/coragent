// Package prompt discovers scoped project instructions and assembles Provider
// requests from current runtime facts instead of a growing global string.
package prompt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

// Instruction is one deduplicated, projected instruction document.
type Instruction struct {
	Sources    []string
	Scope      string
	SHA256     string
	Precedence int
	Content    string
}

// DiscoverInstructions loads optional CLAUDE.md before AGENTS.md from the
// repository root through activeDir. Results are ordered low to high priority.
func DiscoverInstructions(ctx context.Context, w *workspace.FS, activeDir string, projector *dataproj.Projector) ([]Instruction, error) {
	if projector == nil {
		projector = dataproj.New()
	}
	clean, err := w.Clean(activeDir)
	if err != nil {
		return nil, err
	}
	info, _, err := w.Stat(clean)
	if err != nil {
		return nil, fmt.Errorf("prompt: stat active path: %w", err)
	}
	if !info.IsDir() {
		clean = path.Dir(clean)
	}
	dirs := scopeDirs(clean)
	byHash := make(map[string]int)
	var docs []Instruction
	for depth, dir := range dirs {
		for order, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			rel := name
			if dir != "." {
				rel = path.Join(dir, name)
			}
			f, _, err := w.OpenFile(rel)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("prompt: read instruction %s: %w", rel, err)
			}
			raw, readErr := io.ReadAll(io.LimitReader(f, 256*1024+1))
			closeErr := f.Close()
			if readErr != nil {
				return nil, fmt.Errorf("prompt: read instruction %s: %w", rel, readErr)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("prompt: close instruction %s: %w", rel, closeErr)
			}
			if len(raw) > 256*1024 {
				return nil, fmt.Errorf("prompt: instruction %s exceeds 256 KiB", rel)
			}
			digest := sha256.Sum256(raw)
			hash := hex.EncodeToString(digest[:])
			precedence := 10 + depth*2 + order
			if index, ok := byHash[hash]; ok {
				docs[index].Sources = append(docs[index].Sources, rel)
				if precedence > docs[index].Precedence {
					docs[index].Precedence = precedence
					docs[index].Scope = dir
				}
				continue
			}
			projected := projector.ProjectText(string(raw))
			byHash[hash] = len(docs)
			docs = append(docs, Instruction{
				Sources: []string{rel}, Scope: dir, SHA256: hash,
				Precedence: precedence, Content: projected.Content,
			})
		}
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Precedence < docs[j].Precedence })
	return docs, nil
}

func scopeDirs(active string) []string {
	dirs := []string{"."}
	if active == "." || active == "" {
		return dirs
	}
	current := ""
	for _, part := range strings.Split(active, "/") {
		if part == "" || part == "." {
			continue
		}
		current = path.Join(current, part)
		dirs = append(dirs, current)
	}
	return dirs
}

// TranscriptPayload returns safe instruction provenance for durable history.
func TranscriptPayload(docs []Instruction) transcript.InstructionsLoadedPayload {
	out := transcript.InstructionsLoadedPayload{Sources: make([]transcript.InstructionSource, 0, len(docs))}
	for _, doc := range docs {
		out.Sources = append(out.Sources, transcript.InstructionSource{
			Sources: append([]string(nil), doc.Sources...), Scope: doc.Scope,
			SHA256: doc.SHA256, Precedence: doc.Precedence,
		})
	}
	return out
}
