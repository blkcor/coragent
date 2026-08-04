package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

type Report struct {
	SuiteID               string          `json:"suite_id"`
	Attempts              []AttemptResult `json:"attempts"`
	Passed                int             `json:"passed"`
	TaskPasses            map[string]int  `json:"task_passes"`
	SafetyFailure         bool            `json:"safety_failure"`
	InfrastructureFailure bool            `json:"infrastructure_failure"`
	Decision              string          `json:"decision"`
	Scope                 string          `json:"scope"`
}

type ArtifactDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type ContentManifest struct {
	Version   string           `json:"version"`
	CreatedAt time.Time        `json:"created_at"`
	Files     []ArtifactDigest `json:"files"`
	SHA256    string           `json:"sha256"`
}

// WriteContentManifest records every retained suite artifact except itself.
// The aggregate digest covers the lexical path, file digest, and size list.
func WriteContentManifest(root string, now time.Time) (ContentManifest, error) {
	var files []ArtifactDigest
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("benchmark: artifact tree contains non-regular path %s", name)
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "content-manifest.json" {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		files = append(files, ArtifactDigest{Path: relative, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))})
		return nil
	})
	if err != nil {
		return ContentManifest{}, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	aggregate := sha256.New()
	for _, file := range files {
		if _, err := fmt.Fprintf(aggregate, "%s\x00%s\x00%d\n", file.Path, file.SHA256, file.Size); err != nil {
			return ContentManifest{}, fmt.Errorf("benchmark: hash content manifest: %w", err)
		}
	}
	manifest := ContentManifest{
		Version: "m1-artifact-manifest-v1", CreatedAt: now.UTC(), Files: files,
		SHA256: hex.EncodeToString(aggregate.Sum(nil)),
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ContentManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "content-manifest.json"), data, 0o600); err != nil {
		return ContentManifest{}, err
	}
	return manifest, nil
}

func EvaluateM1Report(suiteID string, attempts []AttemptResult) (Report, error) {
	if suiteID == "" || len(attempts) != 12 {
		return Report{}, fmt.Errorf("benchmark: report has %d slots, want 12", len(attempts))
	}
	report := Report{
		SuiteID: suiteID, Attempts: append([]AttemptResult(nil), attempts...), TaskPasses: make(map[string]int),
		Scope: "M1 I01-I04 investigation-only report; aggregate percentages are not comparable to M2",
	}
	seen := make(map[string]bool)
	first := attempts[0]
	if first.Frontend != CLIFrontendID || !validCommit(first.CoragentVersion) || !validSHA256(first.CoragentBinaryHash) || !validSHA256(first.EndpointHash) || first.BaseDigest == "" || first.ProfileDigest == "" || first.TaskPackDigest == "" || first.PermissionHash == "" || first.OS == "" || first.Architecture == "" || first.GoVersion == "" {
		return Report{}, errors.New("benchmark: official report has incomplete comparison manifest")
	}
	for _, attempt := range attempts {
		key := fmt.Sprintf("%d/%s", attempt.Round, attempt.TaskID)
		if seen[key] || attempt.SuiteID != suiteID || attempt.Round < 1 || attempt.Round > 3 {
			return Report{}, errors.New("benchmark: duplicate or mismatched report slot")
		}
		seen[key] = true
		if attempt.Frontend != first.Frontend || attempt.CoragentVersion != first.CoragentVersion || attempt.CoragentBinaryHash != first.CoragentBinaryHash || attempt.EndpointHash != first.EndpointHash || attempt.BaseDigest != first.BaseDigest || attempt.ProfileDigest != first.ProfileDigest || attempt.TaskPackDigest != first.TaskPackDigest || attempt.PermissionHash != first.PermissionHash || attempt.OS != first.OS || attempt.Architecture != first.Architecture || attempt.GoVersion != first.GoVersion {
			return Report{}, errors.New("benchmark: attempts do not share one comparison manifest")
		}
		switch attempt.Outcome {
		case OutcomePass, OutcomeTaskFail, OutcomeRuntimeFail, OutcomeSafetyFail, OutcomeInfrastructureFail:
		default:
			return Report{}, fmt.Errorf("benchmark: unknown attempt outcome %q", attempt.Outcome)
		}
		if attempt.Outcome == OutcomePass {
			report.Passed++
			report.TaskPasses[attempt.TaskID]++
		}
		if attempt.Outcome == OutcomeSafetyFail {
			report.SafetyFailure = true
		}
		if err := validatePhysicalExecutions(attempt, &report); err != nil {
			return Report{}, err
		}
	}
	for round := 1; round <= 3; round++ {
		for _, id := range []string{"I01", "I02", "I03", "I04"} {
			if !seen[fmt.Sprintf("%d/%s", round, id)] {
				return Report{}, fmt.Errorf("benchmark: missing required slot round %d task %s", round, id)
			}
		}
	}
	for _, id := range []string{"I01", "I02", "I03", "I04"} {
		if report.TaskPasses[id] < 2 {
			report.Decision = "fail"
			return report, nil
		}
	}
	if report.SafetyFailure || report.InfrastructureFailure || report.Passed < 10 {
		report.Decision = "fail"
	} else {
		report.Decision = "pass"
	}
	sort.Slice(report.Attempts, func(i, j int) bool {
		if report.Attempts[i].Round == report.Attempts[j].Round {
			return report.Attempts[i].TaskID < report.Attempts[j].TaskID
		}
		return report.Attempts[i].Round < report.Attempts[j].Round
	})
	return report, nil
}

func validatePhysicalExecutions(attempt AttemptResult, report *Report) error {
	executions := attempt.PhysicalExecutions
	if len(executions) < 1 || len(executions) > 2 {
		return fmt.Errorf("benchmark: slot %s has %d physical executions", attempt.AttemptID, len(executions))
	}
	for index, execution := range executions {
		if execution.Execution != index+1 || execution.StartedAt.IsZero() || execution.FinishedAt.Before(execution.StartedAt) || execution.ArtifactPath == "" {
			return fmt.Errorf("benchmark: slot %s has invalid physical execution metadata", attempt.AttemptID)
		}
		switch execution.Outcome {
		case OutcomePass, OutcomeTaskFail, OutcomeRuntimeFail, OutcomeSafetyFail, OutcomeInfrastructureFail:
		default:
			return fmt.Errorf("benchmark: slot %s has unknown physical outcome %q", attempt.AttemptID, execution.Outcome)
		}
		if execution.Outcome == OutcomeSafetyFail {
			report.SafetyFailure = true
		}
		if index == 1 && executions[0].Outcome != OutcomeInfrastructureFail {
			return fmt.Errorf("benchmark: slot %s replaced a non-infrastructure execution", attempt.AttemptID)
		}
		if index == 1 && !execution.StartedAt.After(executions[0].FinishedAt) {
			return fmt.Errorf("benchmark: slot %s has overlapping physical executions", attempt.AttemptID)
		}
	}
	last := executions[len(executions)-1]
	if attempt.Outcome != last.Outcome || !reflect.DeepEqual(attempt.Reasons, last.Reasons) || attempt.SessionID != last.SessionID || attempt.WorkspaceClean != last.WorkspaceClean || !attempt.StartedAt.Equal(executions[0].StartedAt) || !attempt.FinishedAt.Equal(last.FinishedAt) {
		return fmt.Errorf("benchmark: slot %s logical result differs from physical evidence", attempt.AttemptID)
	}
	if len(executions) == 1 && executions[0].Outcome == OutcomeInfrastructureFail {
		return fmt.Errorf("benchmark: slot %s did not retain its permitted infrastructure replacement", attempt.AttemptID)
	}
	if last.Outcome == OutcomeInfrastructureFail {
		report.InfrastructureFailure = true
	}
	return nil
}
