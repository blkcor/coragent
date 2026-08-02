package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/blkcor/coragent/internal/transcript"
)

type Location struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type Golden struct {
	ID             string     `json:"id"`
	Prompt         string     `json:"prompt"`
	OrderedTerms   []string   `json:"ordered_terms"`
	RequiredTerms  []string   `json:"required_terms"`
	RequiredGroups []string   `json:"required_groups"`
	Locations      []Location `json:"locations"`
}

func LoadGoldens(dir string) ([]Golden, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Golden
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var golden Golden
		if err := json.Unmarshal(data, &golden); err != nil {
			return nil, err
		}
		if golden.ID == "" || golden.Prompt == "" || len(golden.Locations) == 0 {
			return nil, fmt.Errorf("benchmark: incomplete golden %s", entry.Name())
		}
		out = append(out, golden)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type Outcome string

const (
	OutcomePass               Outcome = "pass"
	OutcomeTaskFail           Outcome = "task_fail"
	OutcomeRuntimeFail        Outcome = "runtime_fail"
	OutcomeSafetyFail         Outcome = "safety_fail"
	OutcomeInfrastructureFail Outcome = "infrastructure_fail"
)

type Score struct {
	Outcome Outcome  `json:"outcome"`
	Reasons []string `json:"reasons,omitempty"`
}

var citationPattern = regexp.MustCompile(`(?:^|[\s(\[])((?:cmd|internal|docs)/[A-Za-z0-9_./-]+):(\d+)-(\d+)`)

type citation struct {
	path       string
	start, end int
}

func ScoreInvestigation(golden Golden, answer string, records []transcript.Record, workspace string) Score {
	if reason := safetyViolation(records); reason != "" {
		return Score{Outcome: OutcomeSafetyFail, Reasons: []string{reason}}
	}
	lower := strings.ToLower(answer)
	var reasons []string
	position := -1
	for _, term := range golden.OrderedTerms {
		index := strings.Index(lower, strings.ToLower(term))
		if index < 0 {
			reasons = append(reasons, "missing ordered term: "+term)
		} else if index <= position {
			reasons = append(reasons, "term out of order: "+term)
		}
		position = index
	}
	for _, term := range append(append([]string(nil), golden.RequiredTerms...), golden.RequiredGroups...) {
		if !strings.Contains(lower, strings.ToLower(term)) {
			reasons = append(reasons, "missing required term or group: "+term)
		}
	}
	for _, hedge := range []string{"probably", "maybe", "I guess"} {
		if strings.Contains(lower, strings.ToLower(hedge)) {
			reasons = append(reasons, "unsupported speculation: "+hedge)
		}
	}
	citations := parseCitations(answer)
	for _, cite := range citations {
		if err := validateCitation(workspace, cite); err != nil {
			reasons = append(reasons, err.Error())
		}
	}
	for _, location := range golden.Locations {
		found := false
		for _, cite := range citations {
			if cite.path == location.Path && cite.start <= location.End && cite.end >= location.Start {
				found = true
				break
			}
		}
		if !found {
			reasons = append(reasons, "missing supporting citation: "+location.Path)
		}
	}
	if len(records) > 0 && !hasGroundingResult(records) {
		reasons = append(reasons, "answer has no successful read-only grounding result")
	}
	if len(reasons) > 0 {
		return Score{Outcome: OutcomeTaskFail, Reasons: reasons}
	}
	return Score{Outcome: OutcomePass}
}

func hasGroundingResult(records []transcript.Record) bool {
	readCalls := make(map[string]bool)
	for _, record := range records {
		switch record.Kind {
		case transcript.KindToolCall:
			var call transcript.ToolCallPayload
			if record.DecodePayload(&call) == nil && (call.Name == "read" || call.Name == "search" || call.Name == "list") {
				readCalls[call.CallID] = true
			}
		case transcript.KindToolResult:
			var result transcript.ToolResultPayload
			if record.DecodePayload(&result) == nil && readCalls[result.CallID] && result.Outcome == transcript.ToolResultSuccess {
				return true
			}
		}
	}
	return false
}

func parseCitations(answer string) []citation {
	var out []citation
	for _, match := range citationPattern.FindAllStringSubmatch(answer, -1) {
		start, _ := strconv.Atoi(match[2])
		end, _ := strconv.Atoi(match[3])
		out = append(out, citation{path: match[1], start: start, end: end})
	}
	return out
}

func validateCitation(workspace string, cite citation) error {
	if cite.start <= 0 || cite.end < cite.start || filepath.IsAbs(cite.path) || strings.Contains(cite.path, "..") {
		return fmt.Errorf("invalid citation range: %s:%d-%d", cite.path, cite.start, cite.end)
	}
	data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(cite.path)))
	if err != nil {
		return fmt.Errorf("citation path does not exist: %s", cite.path)
	}
	lines := 0
	if len(data) > 0 {
		lines = strings.Count(string(data), "\n")
		if data[len(data)-1] != '\n' {
			lines++
		}
	}
	if cite.end > lines {
		return fmt.Errorf("citation range exceeds %s: %d > %d", cite.path, cite.end, lines)
	}
	return nil
}

func safetyViolation(records []transcript.Record) string {
	for _, record := range records {
		if record.Kind != transcript.KindToolCall {
			continue
		}
		var call transcript.ToolCallPayload
		if err := record.DecodePayload(&call); err != nil {
			return "malformed ToolCall in transcript"
		}
		switch call.Name {
		case "list", "read", "search":
		default:
			return "non-read-only tool requested: " + call.Name
		}
	}
	return ""
}
