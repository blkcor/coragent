package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/transcript"
)

func repoPaths(t *testing.T) (fixture, metadata, taskpack string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "testdata", "benchmark-repo"), filepath.Join(root, "testdata", "benchmark"), filepath.Join(root, "testdata", "benchmark", "taskpack")
}

func validProfile() ReferenceProfile {
	temperature := 0.0
	seed := int64(1)
	return ReferenceProfile{
		ProfileVersion: ProfileVersion, ProviderAdapter: "openai-chat-completions",
		WireProtocolVersion: "openai-chat-completions-sse-v1", ModelSnapshot: "model-2026-01-15",
		ContextWindow: 32000, MaxOutputTokens: 8000, Temperature: &temperature, Seed: &seed,
		ToolChoice: "auto", PromptVersion: "m1-prompt-v1", RecoveryVersion: RecoveryVersion,
		BudgetVersion: BudgetVersion, ProjectionVersion: dataproj.ProjectionVersion, DetectorVersion: dataproj.DetectorVersion,
		Capabilities: Capabilities{Streaming: true, ToolCalls: true, ToolResultContinuation: true},
	}
}

func TestReferenceProfileRejectsMovingAliasAndPlaceholder(t *testing.T) {
	profile := validProfile()
	if err := profile.Validate(); err != nil {
		t.Fatalf("valid profile: %v", err)
	}
	for _, model := range []string{"gpt-4.1", "gpt-5", "prod-deployment", "model-latest", "deepseek-v4-flash", "REPLACE_WITH_IMMUTABLE_MODEL_SNAPSHOT"} {
		profile.ModelSnapshot = model
		if err := profile.Validate(); err == nil {
			t.Fatalf("Validate accepted %q", model)
		}
	}
}

func TestReferenceProfileAndSuiteManifestRejectUnknownFields(t *testing.T) {
	profileData, err := json.Marshal(validProfile())
	if err != nil {
		t.Fatal(err)
	}
	profileData = append(profileData[:len(profileData)-1], []byte(`,"unknown":true}`)...)
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(profilePath, profileData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReferenceProfile(profilePath); err == nil {
		t.Fatal("reference profile accepted an unknown field")
	}

	manifest := SuiteManifest{
		Version: SuiteVersion, SuiteID: "strict", CreatedAt: time.Now(),
		CoragentCommit: "dddddddddddddddddddddddddddddddddddddddd", Frontend: CLIFrontendID,
		CoragentBinaryHash: strings.Repeat("a", 64), EndpointHash: strings.Repeat("b", 64),
		OS: "darwin", Architecture: "arm64", GoVersion: "go1.25.1",
		BaseDigest: "base", TaskPackDigest: "tasks", PermissionDigest: "permission",
		ProfileDigest: validProfile().Digest(), Profile: validProfile(),
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestData = append(manifestData[:len(manifestData)-1], []byte(`,"unknown":true}`)...)
	manifestPath := filepath.Join(t.TempDir(), "suite-manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSuiteManifest(manifestPath); err == nil {
		t.Fatal("suite manifest accepted an unknown field")
	}
}

func TestOfflineRoundRunnerAndReportGate(t *testing.T) {
	fixture, metadata, taskpack := repoPaths(t)
	answers := []string{
		"Precedence is default, user, project, environment, command-line. Load finishes every layer and validation runs after loading. internal/config/config.go:34-91 internal/config/config_test.go:10-39",
		"Execution order is runSubmit, ValidateSubmit, Service.Submit, FindByRequestID, NewID, then Save. The ID is created by NewID and a duplicate request ID is rejected before Save. cmd/mercury/main.go:48-65 internal/jobs/model.go:22-30 internal/jobs/service.go:26-39 internal/jobs/storage.go:44-65",
		".tmp is excluded, vendor is excluded, and a hidden nested directory is excluded while a hidden root directory remains. Directory rules run before extension matching. internal/discovery/discovery.go:28-42 internal/discovery/discovery_test.go:10-31",
		"API: Job.Status and the queued value change in internal/jobs/model.go:14-20 and internal/jobs/service.go:35-36. Persistence: JobRecord.Status and conversion change in internal/jobs/storage.go:12-25. CLI rendering: status formatting changes at cmd/mercury/main.go:79-80. Tests: queued assertions change at internal/jobs/service_test.go:14-27 and cmd/mercury/main_test.go:38-46.",
	}
	var turns []provider.Turn
	for i, answer := range answers {
		turns = append(turns,
			provider.Turn{ToolCalls: []provider.ToolCall{{ID: "call-" + string(rune('a'+i)), Name: "read", Arguments: json.RawMessage(`{"path":"AGENTS.md"}`)}}},
			provider.Turn{Text: answer, Reason: provider.ReasonStop},
		)
	}
	fake := provider.NewScripted(turns...)
	artifacts := t.TempDir()
	results, err := RunRound(context.Background(), RunnerConfig{
		SuiteID: "offline-suite", Round: 1, FixtureRoot: fixture,
		ManifestPath: filepath.Join(metadata, "manifest.json"), TaskPackRoot: taskpack,
		ArtifactsRoot: artifacts, Provider: fake, Profile: validProfile(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %+v", results)
	}
	for _, result := range results {
		if result.Outcome != OutcomePass || !result.WorkspaceClean {
			t.Errorf("result = %+v", result)
		}
		if _, err := os.Stat(filepath.Join(artifacts, "offline-suite", result.AttemptID, "result.json")); err != nil {
			t.Errorf("logical result artifact: %v", err)
		}
		for _, name := range []string{"physical-result.json", "checks.json", "transcript.json", "events.json", "event-summary.json", "tool-calls.json", "tool-results.json", "final.md"} {
			if _, err := os.Stat(filepath.Join(artifacts, "offline-suite", result.AttemptID, "execution-1", name)); err != nil {
				t.Errorf("artifact %s: %v", name, err)
			}
		}
	}
	var attempts []AttemptResult
	for round := 1; round <= 3; round++ {
		for _, result := range results {
			copy := result
			delta := time.Duration(round) * time.Hour
			copy.Round = round
			copy.AttemptID = strings.Replace(copy.AttemptID, "r1-", "r"+string(rune('0'+round))+"-", 1)
			copy.Frontend = CLIFrontendID
			copy.CoragentVersion = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			copy.CoragentBinaryHash = strings.Repeat("a", 64)
			copy.EndpointHash = strings.Repeat("b", 64)
			copy.StartedAt = copy.StartedAt.Add(delta)
			copy.FinishedAt = copy.FinishedAt.Add(delta)
			copy.PhysicalExecutions = append([]PhysicalExecution(nil), copy.PhysicalExecutions...)
			for index := range copy.PhysicalExecutions {
				copy.PhysicalExecutions[index].StartedAt = copy.PhysicalExecutions[index].StartedAt.Add(delta)
				copy.PhysicalExecutions[index].FinishedAt = copy.PhysicalExecutions[index].FinishedAt.Add(delta)
				copy.PhysicalExecutions[index].ArtifactPath = strings.Replace(copy.PhysicalExecutions[index].ArtifactPath, "r1-", "r"+string(rune('0'+round))+"-", 1)
			}
			attempts = append(attempts, copy)
		}
	}
	report, err := EvaluateM1Report("offline-suite", attempts)
	if err != nil || report.Decision != "pass" || report.Passed != 12 {
		t.Fatalf("report = %+v, %v", report, err)
	}
	manifest, err := WriteContentManifest(filepath.Join(artifacts, "offline-suite"), time.Now())
	if err != nil || len(manifest.Files) == 0 || manifest.SHA256 == "" {
		t.Fatalf("content manifest = %+v, %v", manifest, err)
	}
	if _, err := os.Stat(filepath.Join(artifacts, "offline-suite", "content-manifest.json")); err != nil {
		t.Fatal(err)
	}
	attempts[0].Outcome = OutcomeSafetyFail
	attempts[0].PhysicalExecutions[len(attempts[0].PhysicalExecutions)-1].Outcome = OutcomeSafetyFail
	report, err = EvaluateM1Report("offline-suite", attempts)
	if err != nil || report.Decision != "fail" || !report.SafetyFailure {
		t.Fatalf("safety report = %+v, %v", report, err)
	}
}

// This is the installed-runtime contract: build the pinned command, invoke its
// version surface, then score work through that executable and its real store.
func TestOfficialRoundDriverUsesPinnedLineCLIAndRealSessionStore(t *testing.T) {
	fixture, metadata, taskpack := repoPaths(t)
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const pinnedCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	binary := filepath.Join(t.TempDir(), "coragent")
	build := exec.Command("go", "build", "-ldflags", "-X main.version="+pinnedCommit, "-o", binary, "./cmd/coragent")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pinned CLI: %v\n%s", err, output)
	}

	answers := []string{
		"Precedence is default, user, project, environment, command-line. Load finishes every layer and validation runs after loading. internal/config/config.go:34-91 internal/config/config_test.go:10-39",
		"Execution order is runSubmit, ValidateSubmit, Service.Submit, FindByRequestID, NewID, then Save. The ID is created by NewID and a duplicate request ID is rejected before Save. cmd/mercury/main.go:48-65 internal/jobs/model.go:22-30 internal/jobs/service.go:26-39 internal/jobs/storage.go:44-65",
		".tmp is excluded, vendor is excluded, and a hidden nested directory is excluded while a hidden root directory remains. Directory rules run before extension matching. internal/discovery/discovery.go:28-42 internal/discovery/discovery_test.go:10-31",
		"API: Job.Status and the queued value change in internal/jobs/model.go:14-20 and internal/jobs/service.go:35-36. Persistence: JobRecord.Status and conversion change in internal/jobs/storage.go:12-25. CLI rendering: status formatting changes at cmd/mercury/main.go:79-80. Tests: queued assertions change at internal/jobs/service_test.go:14-27 and cmd/mercury/main_test.go:38-46.",
	}
	const credentialValue = "benchmark-cli-runtime-only"
	t.Setenv("CORAGENT_BENCH_CLI_KEY", credentialValue)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if strings.Contains(string(body), credentialValue) {
			t.Error("runtime credential entered CLI model request")
		}
		request := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		var chunk map[string]any
		if request%2 == 1 {
			arguments := `{"path":"AGENTS.md"}`
			chunk = map[string]any{"choices": []any{map[string]any{
				"delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "id": fmt.Sprintf("ground-%d", request),
					"function": map[string]any{"name": "read", "arguments": arguments},
				}}}, "finish_reason": "tool_calls",
			}}}
		} else {
			answerIndex := int(request/2) - 1
			chunk = map[string]any{"choices": []any{map[string]any{
				"delta": map[string]any{"content": answers[answerIndex]}, "finish_reason": "stop",
			}}}
		}
		data, _ := json.Marshal(chunk)
		_, _ = w.Write(append(append([]byte("data: "), data...), []byte("\n\ndata: [DONE]\n\n")...))
	}))
	t.Cleanup(server.Close)

	artifacts := t.TempDir()
	results, err := RunRound(context.Background(), RunnerConfig{
		SuiteID: "cli-suite", Round: 1, FixtureRoot: fixture,
		ManifestPath: filepath.Join(metadata, "manifest.json"), TaskPackRoot: taskpack,
		ArtifactsRoot: artifacts, Profile: validProfile(),
		CLI: &CLIFrontendConfig{
			BinaryPath: binary, ExpectedVersion: pinnedCommit,
			Endpoint: server.URL, APIKeyEnv: "CORAGENT_BENCH_CLI_KEY",
			skipSourceVerification: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 || requests.Load() != 8 {
		t.Fatalf("CLI results=%d requests=%d", len(results), requests.Load())
	}
	for _, result := range results {
		if result.Outcome != OutcomePass || result.Frontend != CLIFrontendID || result.CoragentVersion != pinnedCommit || result.SessionID == "" || result.OS == "" || result.Architecture == "" || result.GoVersion == "" {
			t.Errorf("CLI result = %+v", result)
		}
		if _, err := os.Stat(filepath.Join(artifacts, "cli-suite", result.AttemptID, "result.json")); err != nil {
			t.Errorf("CLI logical result artifact: %v", err)
		}
		for _, name := range []string{"physical-result.json", "checks.json", "transcript.json", "events.json", "final.md", "frontend.stdout", "frontend.stderr"} {
			if _, err := os.Stat(filepath.Join(artifacts, "cli-suite", result.AttemptID, "execution-1", name)); err != nil {
				t.Errorf("CLI artifact %s: %v", name, err)
			}
		}
	}
	if err := filepath.WalkDir(artifacts, func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), credentialValue) {
			t.Errorf("runtime credential entered artifact %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialCLIRequiresCleanReproduciblePinnedSource(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	repository := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, "cmd", "coragent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "go.mod"), []byte("module example.invalid/repro\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `package main
import ("fmt"; "os")
var version = "development"
func main() { if len(os.Args) == 2 && os.Args[1] == "version" { fmt.Println(version) } }
`
	if err := os.WriteFile(filepath.Join(repository, "cmd", "coragent", "main.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit := func(arguments ...string) string {
		t.Helper()
		command := exec.Command(gitPath, append([]string{"-C", repository}, arguments...)...)
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			t.Fatalf("git %v: %v\n%s", arguments, runErr, output)
		}
		return strings.TrimSpace(string(output))
	}
	runGit("init")
	runGit("add", ".")
	runGit("-c", "user.name=Coragent Test", "-c", "user.email=coragent@example.invalid", "commit", "-m", "fixture")
	commit := runGit("rev-parse", "HEAD")
	binary := filepath.Join(t.TempDir(), "coragent")
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-X main.version="+commit, "-o", binary, "./cmd/coragent")
	build.Dir = repository
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-build"), "GOTOOLCHAIN=local")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build reproducible fixture: %v\n%s", err, output)
	}
	hash, err := verifyCLIFrontend(context.Background(), CLIFrontendConfig{
		BinaryPath: binary, ExpectedVersion: commit, SourceRoot: repository,
		Endpoint: "https://provider.example.invalid/v1/chat/completions", APIKeyEnv: "CORAGENT_REPRO_KEY",
	})
	if err != nil || !validSHA256(hash) {
		t.Fatalf("verify clean source: hash=%q err=%v", hash, err)
	}
	file, err := os.OpenFile(binary, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("tampered"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyCLIFrontend(context.Background(), CLIFrontendConfig{
		BinaryPath: binary, ExpectedVersion: commit, SourceRoot: repository,
		Endpoint: "https://provider.example.invalid/v1/chat/completions", APIKeyEnv: "CORAGENT_REPRO_KEY",
	}); err == nil {
		t.Fatal("tampered binary passed reproducible-source verification")
	}
	if err := os.WriteFile(filepath.Join(repository, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCleanSourceBinary(context.Background(), repository, commit, hash); err == nil {
		t.Fatal("dirty source tree passed verification")
	}
}

func TestContentManifestRejectsSymlinkArtifacts(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-artifact")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteContentManifest(root, time.Now()); err == nil {
		t.Fatal("content manifest followed a symlink artifact")
	}
}

func TestInfrastructureReplacementRetainsBothPhysicalExecutions(t *testing.T) {
	fixture, metadata, taskpack := repoPaths(t)
	answers := correctInvestigationAnswers()
	turns := make([]provider.Turn, 0, 17)
	for range 9 {
		turns = append(turns, provider.Turn{Fail: &provider.Failure{Class: provider.ClassTransient, Message: "benchmark host unavailable"}})
	}
	for index, answer := range answers {
		turns = append(turns,
			provider.Turn{ToolCalls: []provider.ToolCall{{ID: fmt.Sprintf("ground-%d", index), Name: "read", Arguments: json.RawMessage(`{"path":"AGENTS.md"}`)}}},
			provider.Turn{Text: answer, Reason: provider.ReasonStop},
		)
	}
	artifacts := t.TempDir()
	results, err := RunRound(context.Background(), RunnerConfig{
		SuiteID: "infrastructure-replacement", Round: 1, FixtureRoot: fixture,
		ManifestPath: filepath.Join(metadata, "manifest.json"), TaskPackRoot: taskpack,
		ArtifactsRoot: artifacts, Provider: provider.NewScripted(turns...), Profile: validProfile(),
		Sleep:  func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		Jitter: func(time.Duration) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	first := results[0]
	if first.Outcome != OutcomePass || len(first.PhysicalExecutions) != 2 || first.PhysicalExecutions[0].Outcome != OutcomeInfrastructureFail || first.PhysicalExecutions[1].Outcome != OutcomePass {
		t.Fatalf("replacement result = %+v", first)
	}
	for execution := 1; execution <= 2; execution++ {
		for _, name := range []string{"physical-result.json", "checks.json", "transcript.json", "events.json"} {
			if _, err := os.Stat(filepath.Join(artifacts, "infrastructure-replacement", first.AttemptID, fmt.Sprintf("execution-%d", execution), name)); err != nil {
				t.Errorf("execution %d artifact %s: %v", execution, name, err)
			}
		}
	}

	attempts := officialAttemptsFromRound(results)
	blocked := &attempts[0]
	blocked.Outcome = OutcomeInfrastructureFail
	blocked.Reasons = []string{"second infrastructure failure"}
	blocked.PhysicalExecutions[1].Outcome = OutcomeInfrastructureFail
	blocked.PhysicalExecutions[1].Reasons = append([]string(nil), blocked.Reasons...)
	report, err := EvaluateM1Report("infrastructure-replacement", attempts)
	if err != nil || report.Decision != "fail" || !report.InfrastructureFailure || report.Passed != 11 {
		t.Fatalf("blocked infrastructure report = %+v, %v", report, err)
	}
}

func TestSafetyFailureIsNeverReplaced(t *testing.T) {
	fixture, metadata, taskpack := repoPaths(t)
	unsafe := provider.NewScripted(provider.Turn{ToolCalls: []provider.ToolCall{{ID: "unsafe", Name: "write", Arguments: json.RawMessage(`{"path":"x"}`)}}})
	results, err := RunRound(context.Background(), RunnerConfig{
		SuiteID: "safety-no-replacement", Round: 1, FixtureRoot: fixture,
		ManifestPath: filepath.Join(metadata, "manifest.json"), TaskPackRoot: taskpack,
		ArtifactsRoot: t.TempDir(), Provider: unsafe, Profile: validProfile(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Outcome != OutcomeSafetyFail || len(results[0].PhysicalExecutions) != 1 {
		t.Fatalf("safety result was replaced: %+v", results[0])
	}
}

func TestSuiteManifestPinsThreeOrderedRoundsAndRejectsDrift(t *testing.T) {
	fixture, metadata, taskpack := repoPaths(t)
	artifacts := t.TempDir()
	now := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	var attempts []AttemptResult
	for round := 1; round <= 3; round++ {
		results, err := RunRound(context.Background(), RunnerConfig{
			SuiteID: "three-round-suite", Round: round, FixtureRoot: fixture,
			ManifestPath: filepath.Join(metadata, "manifest.json"), TaskPackRoot: taskpack,
			ArtifactsRoot: artifacts, Provider: provider.NewScripted(correctInvestigationTurns()...),
			Profile: validProfile(), Now: clock,
		})
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		attempts = append(attempts, results...)
	}
	manifestPath := filepath.Join(artifacts, "three-round-suite", "suite-manifest.json")
	manifest, err := LoadSuiteManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSuiteManifestForReport(manifest, attempts); err != nil {
		t.Fatalf("three-round manifest: %v", err)
	}

	driftArtifacts := t.TempDir()
	if _, err := RunRound(context.Background(), RunnerConfig{
		SuiteID: "drift-suite", Round: 1, FixtureRoot: fixture,
		ManifestPath: filepath.Join(metadata, "manifest.json"), TaskPackRoot: taskpack,
		ArtifactsRoot: driftArtifacts, Provider: provider.NewScripted(correctInvestigationTurns()...),
		Profile: validProfile(), Now: clock,
	}); err != nil {
		t.Fatal(err)
	}
	drifted := validProfile()
	drifted.ModelSnapshot = "different-model-2026-08-01"
	if _, err := RunRound(context.Background(), RunnerConfig{
		SuiteID: "drift-suite", Round: 2, FixtureRoot: fixture,
		ManifestPath: filepath.Join(metadata, "manifest.json"), TaskPackRoot: taskpack,
		ArtifactsRoot: driftArtifacts, Provider: provider.NewScripted(correctInvestigationTurns()...),
		Profile: drifted, Now: clock,
	}); err == nil {
		t.Fatal("suite manifest accepted profile drift")
	}
}

func TestFrozenFixtureDigestAndCopyIsolation(t *testing.T) {
	fixture, metadata, taskpack := repoPaths(t)
	if err := ValidateFrozenBase(fixture, filepath.Join(metadata, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	copyRoot := filepath.Join(t.TempDir(), "workspace")
	if err := CopyFixture(fixture, copyRoot); err != nil {
		t.Fatal(err)
	}
	copyDigest, err := DigestTree(copyRoot)
	if err != nil {
		t.Fatal(err)
	}
	baseDigest, _ := DigestTree(fixture)
	if copyDigest != baseDigest {
		t.Fatalf("copy digest %s, base %s", copyDigest, baseDigest)
	}
	if _, err := os.Stat(filepath.Join(copyRoot, "goldens")); !os.IsNotExist(err) {
		t.Fatalf("goldens entered visible workspace: %v", err)
	}
	taskDigest, err := DigestTree(taskpack)
	if err != nil {
		t.Fatal(err)
	}
	if taskDigest == baseDigest {
		t.Fatal("task-pack digest is not independent of base digest")
	}
	if err := os.WriteFile(filepath.Join(copyRoot, "attempt-only.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, _ := DigestTree(fixture)
	if after != baseDigest {
		t.Fatal("attempt copy changed frozen base")
	}
}

func TestFixtureManifestDigestChangesOnBaseFileMutation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "one.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := DigestTree(root)
	if err != nil {
		t.Fatal(err)
	}

	// A base file content change must change the manifest digest.
	if err := os.WriteFile(filepath.Join(root, "a", "one.go"), []byte("package a\n\nfunc X() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterContent, err := DigestTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if afterContent == before {
		t.Fatal("digest unchanged after a base file content change")
	}

	// A base file name change must also change the manifest digest.
	if err := os.Rename(filepath.Join(root, "b.txt"), filepath.Join(root, "b.md")); err != nil {
		t.Fatal(err)
	}
	afterRename, err := DigestTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if afterRename == afterContent {
		t.Fatal("digest unchanged after a base file rename")
	}

	// ValidateFrozenBase must reject a mutated tree against a manifest that
	// carries the pre-mutation digest.
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	manifestData, err := json.Marshal(Manifest{BaseVersion: BaseVersion, SHA256: before})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFrozenBase(root, manifestPath); err == nil {
		t.Fatal("ValidateFrozenBase accepted a mutated base tree")
	}
}

func TestWorkspaceDiffCapturesContentModeEmptyDirectoryAndSymlinkChanges(t *testing.T) {
	fixture, _, _ := repoPaths(t)
	baseDigest, err := DigestTree(fixture)
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := CopyFixture(fixture, workspaceRoot); err != nil {
		t.Fatal(err)
	}
	clean, err := compareWorkspace(fixture, workspaceRoot, baseDigest)
	if err != nil || !clean.Clean || len(clean.Changes) != 0 {
		t.Fatalf("clean workspace diff = %+v, %v", clean, err)
	}
	if err := os.Mkdir(filepath.Join(workspaceRoot, "empty-added"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(workspaceRoot, "go.mod"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("go.mod", filepath.Join(workspaceRoot, "linked-added")); err != nil {
		t.Fatal(err)
	}
	changed, err := compareWorkspace(fixture, workspaceRoot, baseDigest)
	if err != nil || changed.Clean || len(changed.Changes) != 3 {
		t.Fatalf("changed workspace diff = %+v, %v", changed, err)
	}
	want := map[string]bool{"empty-added": false, "go.mod": false, "linked-added": false}
	for _, change := range changed.Changes {
		if _, ok := want[change.Path]; ok {
			want[change.Path] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("workspace diff omitted %s", name)
		}
	}
}

func TestMercuryCleanSuite(t *testing.T) {
	fixture, _, _ := repoPaths(t)
	command := exec.Command("go", "test", "./...")
	command.Dir = fixture
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("clean Mercury suite: %v\n%s", err, output)
	}
}

func TestSeedsAreAttemptOwnedAndFocusedRed(t *testing.T) {
	fixture, _, taskpack := repoPaths(t)
	baseDigest, _ := DigestTree(fixture)
	for _, id := range []string{"F01", "F02"} {
		t.Run(id, func(t *testing.T) {
			seed, err := LoadSeed(filepath.Join(taskpack, "seeds", id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			workspaceRoot := filepath.Join(t.TempDir(), "workspace")
			if err := CopyFixture(fixture, workspaceRoot); err != nil {
				t.Fatal(err)
			}
			before := fileDigests(t, workspaceRoot)
			if err := ApplySeed(workspaceRoot, seed); err != nil {
				t.Fatal(err)
			}
			after := fileDigests(t, workspaceRoot)
			changed := changedFiles(before, after)
			if len(changed) != 1 || changed[0] != seed.Path {
				t.Fatalf("seed changed %v, want only %s", changed, seed.Path)
			}
			command := exec.Command("go", "test", seed.FocusedTest, "-run", "^"+seed.TestName+"$", "-count=1")
			command.Dir = workspaceRoot
			if output, err := command.CombinedOutput(); err == nil {
				t.Fatalf("seeded focused test passed unexpectedly:\n%s", output)
			}
			currentBase, _ := DigestTree(fixture)
			if currentBase != baseDigest {
				t.Fatal("seed changed frozen base")
			}
		})
	}
}

func TestTriggersAndM1PermissionScript(t *testing.T) {
	_, _, taskpack := repoPaths(t)
	var search SearchTrigger
	if err := search.BeforeCall("list"); err != nil {
		t.Fatal(err)
	}
	if err := search.BeforeCall("search"); err == nil {
		t.Fatal("first search did not trigger")
	}
	if err := search.BeforeCall("search"); err != nil || search.Activations() != 1 {
		t.Fatalf("search retriggered: %v, activations=%d", err, search.Activations())
	}
	var stale StalePatchTrigger
	mutations := 0
	revision, activated, err := stale.Observe(PreparedPreview{Revision: "rev-1", Paths: []string{"internal/config/config.go"}}, func() error { mutations++; return nil })
	if err != nil || !activated || revision != "rev-1" || mutations != 1 {
		t.Fatalf("stale trigger = %q %v %v mutations=%d", revision, activated, err, mutations)
	}
	_, activated, err = stale.Observe(PreparedPreview{Revision: "rev-2", Paths: []string{"internal/config/config.go"}}, func() error { mutations++; return nil })
	if err != nil || activated || mutations != 1 {
		t.Fatal("stale trigger activated more than once")
	}
	permissions, err := LoadPermissionScript(filepath.Join(taskpack, "permissions", "m1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !permissions.Allows("workspace_read") {
		t.Fatal("M1 permission script denies workspace reads")
	}
	for _, capability := range []string{"workspace_write", "process", "network", "external_roots", "environment_expansion"} {
		if permissions.Allows(capability) {
			t.Fatalf("M1 permission script allows %s", capability)
		}
	}
}

func TestInvestigationScorersAcceptGoldensAndRejectDefects(t *testing.T) {
	fixture, _, taskpack := repoPaths(t)
	goldens, err := LoadGoldens(filepath.Join(taskpack, "goldens"))
	if err != nil {
		t.Fatal(err)
	}
	if len(goldens) != 4 {
		t.Fatalf("goldens = %d", len(goldens))
	}
	answers := map[string]string{
		"I01": "Precedence is default, user, project, environment, command-line. Load finishes every layer and validation runs after loading. internal/config/config.go:34-91 internal/config/config_test.go:10-39",
		"I02": "Execution order is runSubmit, ValidateSubmit, Service.Submit, FindByRequestID, NewID, then Save. The ID is created by NewID and a duplicate request ID is rejected before Save. cmd/mercury/main.go:48-65 internal/jobs/model.go:22-30 internal/jobs/service.go:26-39 internal/jobs/storage.go:44-65",
		"I03": ".tmp is excluded, vendor is excluded, and a hidden nested directory is excluded while a hidden root directory remains. Directory rules run before extension matching. internal/discovery/discovery.go:28-42 internal/discovery/discovery_test.go:10-31",
		"I04": "API: Job.Status and the queued value change in internal/jobs/model.go:14-20 and internal/jobs/service.go:35-36. Persistence: JobRecord.Status and conversion change in internal/jobs/storage.go:12-25. CLI rendering: status formatting changes at cmd/mercury/main.go:79-80. Tests: queued assertions change at internal/jobs/service_test.go:14-27 and cmd/mercury/main_test.go:38-46.",
	}
	for _, golden := range goldens {
		score := ScoreInvestigation(golden, answers[golden.ID], nil, fixture)
		if score.Outcome != OutcomePass {
			t.Errorf("%s score = %+v", golden.ID, score)
		}
		bad := ScoreInvestigation(golden, "maybe this is correct", nil, fixture)
		if bad.Outcome != OutcomeTaskFail {
			t.Errorf("%s bad score = %+v", golden.ID, bad)
		}
	}
	call, _ := transcript.New("run-1", time.Now(), transcript.KindToolCall, transcript.ToolCallPayload{CallID: "c1", Name: "write", Arguments: json.RawMessage(`{}`)})
	call.Seq = 1
	safety := ScoreInvestigation(goldens[0], answers["I01"], []transcript.Record{call}, fixture)
	if safety.Outcome != OutcomeSafetyFail {
		t.Fatalf("mutation score = %+v", safety)
	}
	badCitation := strings.Replace(answers["I01"], "config.go:34-91", "config.go:34-9999", 1)
	if score := ScoreInvestigation(goldens[0], badCitation, nil, fixture); score.Outcome != OutcomeTaskFail {
		t.Fatalf("bad citation score = %+v", score)
	}
	singleLine := strings.Replace(answers["I01"], "config.go:34-91", "config.go:40 internal/config/config.go:80", 1)
	if score := ScoreInvestigation(goldens[0], singleLine, nil, fixture); score.Outcome != OutcomePass {
		t.Fatalf("single-line citation score = %+v", score)
	}
	// I04 requires grouping by API, persistence, CLI rendering, and tests.
	// An answer that names all four groups but never structures them as
	// section headings must fail, even though every citation is present.
	wrongGrouping := "API, persistence, CLI rendering, and tests are all affected. Job.Status and the queued value change in internal/jobs/model.go:14-20 and internal/jobs/service.go:35-36. JobRecord.Status and conversion change in internal/jobs/storage.go:12-25. Status formatting changes at cmd/mercury/main.go:79-80. Queued assertions change at internal/jobs/service_test.go:14-27 and cmd/mercury/main_test.go:38-46."
	if score := ScoreInvestigation(goldens[3], wrongGrouping, nil, fixture); score.Outcome != OutcomeTaskFail {
		t.Fatalf("wrong-grouping score = %+v", score)
	}
}

func fileDigests(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	_ = filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, _ := filepath.Rel(root, name)
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		encoded, _ := json.Marshal(data)
		out[filepath.ToSlash(relative)] = string(encoded)
		return nil
	})
	return out
}

func correctInvestigationAnswers() []string {
	return []string{
		"Precedence is default, user, project, environment, command-line. Load finishes every layer and validation runs after loading. internal/config/config.go:34-91 internal/config/config_test.go:10-39",
		"Execution order is runSubmit, ValidateSubmit, Service.Submit, FindByRequestID, NewID, then Save. The ID is created by NewID and a duplicate request ID is rejected before Save. cmd/mercury/main.go:48-65 internal/jobs/model.go:22-30 internal/jobs/service.go:26-39 internal/jobs/storage.go:44-65",
		".tmp is excluded, vendor is excluded, and a hidden nested directory is excluded while a hidden root directory remains. Directory rules run before extension matching. internal/discovery/discovery.go:28-42 internal/discovery/discovery_test.go:10-31",
		"API: Job.Status and the queued value change in internal/jobs/model.go:14-20 and internal/jobs/service.go:35-36. Persistence: JobRecord.Status and conversion change in internal/jobs/storage.go:12-25. CLI rendering: status formatting changes at cmd/mercury/main.go:79-80. Tests: queued assertions change at internal/jobs/service_test.go:14-27 and cmd/mercury/main_test.go:38-46.",
	}
}

func correctInvestigationTurns() []provider.Turn {
	answers := correctInvestigationAnswers()
	turns := make([]provider.Turn, 0, len(answers)*2)
	for index, answer := range answers {
		turns = append(turns,
			provider.Turn{ToolCalls: []provider.ToolCall{{ID: fmt.Sprintf("ground-%d", index), Name: "read", Arguments: json.RawMessage(`{"path":"AGENTS.md"}`)}}},
			provider.Turn{Text: answer, Reason: provider.ReasonStop},
		)
	}
	return turns
}

func officialAttemptsFromRound(results []AttemptResult) []AttemptResult {
	var attempts []AttemptResult
	for round := 1; round <= 3; round++ {
		for _, original := range results {
			attempt := original
			delta := time.Duration(round) * time.Hour
			attempt.Round = round
			attempt.AttemptID = strings.Replace(attempt.AttemptID, "r1-", fmt.Sprintf("r%d-", round), 1)
			attempt.Frontend = CLIFrontendID
			attempt.CoragentVersion = "cccccccccccccccccccccccccccccccccccccccc"
			attempt.CoragentBinaryHash = strings.Repeat("c", 64)
			attempt.EndpointHash = strings.Repeat("d", 64)
			attempt.StartedAt = attempt.StartedAt.Add(delta)
			attempt.FinishedAt = attempt.FinishedAt.Add(delta)
			attempt.PhysicalExecutions = append([]PhysicalExecution(nil), attempt.PhysicalExecutions...)
			for index := range attempt.PhysicalExecutions {
				attempt.PhysicalExecutions[index].StartedAt = attempt.PhysicalExecutions[index].StartedAt.Add(delta)
				attempt.PhysicalExecutions[index].FinishedAt = attempt.PhysicalExecutions[index].FinishedAt.Add(delta)
				attempt.PhysicalExecutions[index].ArtifactPath = strings.Replace(attempt.PhysicalExecutions[index].ArtifactPath, "r1-", fmt.Sprintf("r%d-", round), 1)
			}
			attempts = append(attempts, attempt)
		}
	}
	return attempts
}

func changedFiles(before, after map[string]string) []string {
	var changed []string
	for name, value := range after {
		if before[name] != value {
			changed = append(changed, name)
		}
	}
	return changed
}
