package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/benchmark"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/prompt"
	"github.com/blkcor/coragent/internal/transcript"
)

func TestRequiresCommand(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("run(nil) = %d", code)
	}
	if code := run([]string{"help"}); code != 0 {
		t.Fatalf("run(help) = %d", code)
	}
}

func TestReportCommandPassesCompleteEvidenceAndBlocksSecondInfrastructureFailure(t *testing.T) {
	root := t.TempDir()
	suiteID := "report-command"
	suiteDir := filepath.Join(root, suiteID)
	if err := os.MkdirAll(suiteDir, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := commandTestProfile()
	baseTime := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "internal", "file.go"), []byte("package internal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	baseDigest, err := benchmark.DigestTree(fixture)
	if err != nil {
		t.Fatal(err)
	}
	manifest := benchmark.SuiteManifest{
		Version: benchmark.SuiteVersion, SuiteID: suiteID, CreatedAt: baseTime,
		CoragentCommit: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Frontend: benchmark.CLIFrontendID,
		CoragentBinaryHash: strings.Repeat("e", 64), EndpointHash: strings.Repeat("f", 64),
		OS: "darwin", Architecture: "arm64", GoVersion: "go1.25.1",
		BaseDigest: baseDigest, TaskPackDigest: "tasks", PermissionDigest: "permission",
		ProfileDigest: profile.Digest(), Profile: profile,
	}
	for round := 1; round <= 3; round++ {
		started := baseTime.Add(time.Duration(round) * time.Hour)
		manifest.Rounds = append(manifest.Rounds, benchmark.RoundRecord{Round: round, StartedAt: started, FinishedAt: started.Add(time.Minute)})
		for _, taskID := range []string{"I01", "I02", "I03", "I04"} {
			attemptID := "r" + string(rune('0'+round)) + "-" + taskID
			execution := benchmark.PhysicalExecution{
				Execution: 1, StartedAt: started, FinishedAt: started.Add(time.Minute), Outcome: benchmark.OutcomePass,
				SessionID: attemptID, WorkspaceClean: true, ArtifactPath: attemptID + "/execution-1",
			}
			attempt := benchmark.AttemptResult{
				SuiteID: suiteID, AttemptID: attemptID, Round: round, TaskID: taskID,
				StartedAt: execution.StartedAt, FinishedAt: execution.FinishedAt, Outcome: benchmark.OutcomePass,
				SessionID: execution.SessionID, BaseDigest: manifest.BaseDigest, ProfileDigest: manifest.ProfileDigest,
				TaskPackDigest: manifest.TaskPackDigest, PermissionHash: manifest.PermissionDigest,
				Frontend: manifest.Frontend, CoragentVersion: manifest.CoragentCommit,
				CoragentBinaryHash: manifest.CoragentBinaryHash, EndpointHash: manifest.EndpointHash,
				OS: manifest.OS, Architecture: manifest.Architecture, GoVersion: manifest.GoVersion,
				WorkspaceClean: true, PhysicalExecutions: []benchmark.PhysicalExecution{execution},
			}
			writeTestJSON(t, filepath.Join(suiteDir, attemptID, "result.json"), attempt)
			writePassingPhysicalEvidence(t, suiteDir, fixture, attempt, execution)
		}
	}
	writeTestJSON(t, filepath.Join(suiteDir, "suite-manifest.json"), manifest)
	if code := run([]string{"report", "--suite", suiteID, "--artifacts", root}); code != 0 {
		t.Fatalf("passing report exit = %d", code)
	}

	tamperedFinal := filepath.Join(suiteDir, "r1-I01", "execution-1", "final.md")
	originalFinal, err := os.ReadFile(tamperedFinal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tamperedFinal, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"report", "--suite", suiteID, "--artifacts", root}); code != 1 {
		t.Fatalf("tampered evidence report exit = %d", code)
	}
	if err := os.WriteFile(tamperedFinal, originalFinal, 0o600); err != nil {
		t.Fatal(err)
	}

	blockedPath := filepath.Join(suiteDir, "r1-I01", "result.json")
	blocked, err := benchmark.LoadAttemptResult(blockedPath)
	if err != nil {
		t.Fatal(err)
	}
	first := blocked.PhysicalExecutions[0]
	first.Outcome = benchmark.OutcomeInfrastructureFail
	first.Reasons = []string{"Provider infrastructure failed after Coragent recovery was exhausted"}
	second := first
	second.Execution = 2
	second.StartedAt = first.FinishedAt.Add(time.Second)
	second.FinishedAt = second.StartedAt.Add(time.Minute)
	second.ArtifactPath = blocked.AttemptID + "/execution-2"
	second.SessionID += "-replacement"
	blocked.PhysicalExecutions = []benchmark.PhysicalExecution{first, second}
	blocked.Outcome = benchmark.OutcomeInfrastructureFail
	blocked.Reasons = append([]string(nil), second.Reasons...)
	blocked.StartedAt = first.StartedAt
	blocked.FinishedAt = second.FinishedAt
	blocked.SessionID = second.SessionID
	writeTestJSON(t, blockedPath, blocked)
	writeInfrastructurePhysicalEvidence(t, suiteDir, fixture, blocked, first)
	writeInfrastructurePhysicalEvidence(t, suiteDir, fixture, blocked, second)
	if code := run([]string{"report", "--suite", suiteID, "--artifacts", root}); code != 1 {
		t.Fatalf("blocked infrastructure report exit = %d", code)
	}
}

func writeInfrastructurePhysicalEvidence(t *testing.T, suiteDir, fixture string, attempt benchmark.AttemptResult, physical benchmark.PhysicalExecution) {
	t.Helper()
	dir := filepath.Join(suiteDir, filepath.FromSlash(physical.ArtifactPath))
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "internal", "file.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "file.go"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	runID := "run-" + attempt.AttemptID + "-" + string(rune('0'+physical.Execution))
	user, err := transcript.New(runID, physical.StartedAt, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := transcript.New(runID, physical.FinishedAt, transcript.KindRunOutcome, transcript.RunOutcomePayload{Outcome: transcript.RunOutcomeFailed, Cause: transcript.CauseProviderTransient})
	if err != nil {
		t.Fatal(err)
	}
	user.Seq = 1
	outcome.Seq = 2
	records := []transcript.Record{user, outcome}
	started, err := event.New(physical.SessionID, runID, 1, physical.StartedAt, event.KindRunStarted, event.RunStartedPayload{Prompt: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := event.New(physical.SessionID, runID, 2, physical.FinishedAt, event.KindRunFailed, event.RunFailedPayload{Cause: event.CauseProviderTransient})
	if err != nil {
		t.Fatal(err)
	}
	events := []event.Event{started, failed}
	checks := benchmark.AttemptChecks{
		Citation:  benchmark.CitationCheck{Passed: false, Reasons: []string{"answer has no file-and-line citation"}},
		Workspace: benchmark.WorkspaceDiff{BeforeDigest: attempt.BaseDigest, AfterDigest: attempt.BaseDigest, Clean: true},
		Safety:    benchmark.SafetyCheck{Passed: true},
		Scoring:   benchmark.Score{Outcome: benchmark.OutcomeTaskFail, Reasons: []string{"Provider did not return an answer"}},
	}
	writeTestJSON(t, filepath.Join(dir, "physical-result.json"), physical)
	writeTestJSON(t, filepath.Join(dir, "checks.json"), checks)
	writeTestJSON(t, filepath.Join(dir, "transcript.json"), records)
	writeTestJSON(t, filepath.Join(dir, "events.json"), events)
	writeTestJSON(t, filepath.Join(dir, "event-summary.json"), map[event.Kind]int{event.KindRunStarted: 1, event.KindRunFailed: 1})
	writeTestJSON(t, filepath.Join(dir, "tool-calls.json"), []transcript.Record(nil))
	writeTestJSON(t, filepath.Join(dir, "tool-results.json"), []transcript.Record(nil))
	for _, name := range []string{"final.md", "frontend.stdout", "frontend.stderr"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writePassingPhysicalEvidence(t *testing.T, suiteDir, fixture string, attempt benchmark.AttemptResult, physical benchmark.PhysicalExecution) {
	t.Helper()
	dir := filepath.Join(suiteDir, filepath.FromSlash(physical.ArtifactPath))
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "internal", "file.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "file.go"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	runID := "run-" + attempt.AttemptID
	answer := "Evidence is retained. internal/file.go:1-1"
	user, err := transcript.New(runID, physical.StartedAt, transcript.KindUserMessage, transcript.UserMessagePayload{Text: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := transcript.New(runID, physical.StartedAt.Add(time.Second), transcript.KindAssistantBlock, transcript.AssistantBlockPayload{Text: answer})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := transcript.New(runID, physical.FinishedAt, transcript.KindRunOutcome, transcript.RunOutcomePayload{Outcome: transcript.RunOutcomeCompleted})
	if err != nil {
		t.Fatal(err)
	}
	records := []transcript.Record{user, assistant, outcome}
	for index := range records {
		records[index].Seq = uint64(index + 1)
	}
	started, err := event.New(physical.SessionID, runID, 1, physical.StartedAt, event.KindRunStarted, event.RunStartedPayload{Prompt: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	textEvent, err := event.New(physical.SessionID, runID, 2, physical.StartedAt.Add(time.Second), event.KindAssistantText, event.AssistantTextPayload{Text: answer})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := event.New(physical.SessionID, runID, 3, physical.FinishedAt, event.KindRunCompleted, event.RunCompletedPayload{})
	if err != nil {
		t.Fatal(err)
	}
	events := []event.Event{started, textEvent, completed}
	checks := benchmark.AttemptChecks{
		Citation:  benchmark.CitationCheck{Passed: true, Citations: []benchmark.CitationEvidence{{Path: "internal/file.go", Start: 1, End: 1, Valid: true}}},
		Workspace: benchmark.WorkspaceDiff{BeforeDigest: attempt.BaseDigest, AfterDigest: attempt.BaseDigest, Clean: true},
		Safety:    benchmark.SafetyCheck{Passed: true},
		Scoring:   benchmark.Score{Outcome: benchmark.OutcomePass},
	}
	writeTestJSON(t, filepath.Join(dir, "physical-result.json"), physical)
	writeTestJSON(t, filepath.Join(dir, "checks.json"), checks)
	writeTestJSON(t, filepath.Join(dir, "transcript.json"), records)
	writeTestJSON(t, filepath.Join(dir, "events.json"), events)
	writeTestJSON(t, filepath.Join(dir, "event-summary.json"), map[event.Kind]int{event.KindRunStarted: 1, event.KindAssistantText: 1, event.KindRunCompleted: 1})
	writeTestJSON(t, filepath.Join(dir, "tool-calls.json"), []transcript.Record(nil))
	writeTestJSON(t, filepath.Join(dir, "tool-results.json"), []transcript.Record(nil))
	for name, content := range map[string]string{"final.md": answer, "frontend.stdout": "", "frontend.stderr": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func commandTestProfile() benchmark.ReferenceProfile {
	temperature := 0.0
	seed := int64(1)
	return benchmark.ReferenceProfile{
		ProfileVersion: benchmark.ProfileVersion, ProviderAdapter: "openai-chat-completions",
		WireProtocolVersion: "openai-chat-completions-sse-v1", ModelSnapshot: "model-2026-08-01",
		ContextWindow: 32000, MaxOutputTokens: 8000, Temperature: &temperature, Seed: &seed,
		ToolChoice: "auto", PromptVersion: prompt.PromptVersion, RecoveryVersion: benchmark.RecoveryVersion,
		BudgetVersion: benchmark.BudgetVersion, ProjectionVersion: dataproj.ProjectionVersion,
		DetectorVersion: dataproj.DetectorVersion,
		Capabilities:    benchmark.Capabilities{Streaming: true, ToolCalls: true, ToolResultContinuation: true},
	}
}

func writeTestJSON(t *testing.T, name string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
