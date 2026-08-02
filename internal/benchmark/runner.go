package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/engine"
	"github.com/blkcor/coragent/internal/event"
	"github.com/blkcor/coragent/internal/prompt"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/sessioncommand"
	"github.com/blkcor/coragent/internal/store"
	"github.com/blkcor/coragent/internal/transcript"
)

const (
	RecoveryVersion = "m1-retry-v1"
	BudgetVersion   = "m1-budget-v1"
	ProfileVersion  = "m1-reference-profile-v1"
	SuiteVersion    = "m1-suite-manifest-v1"
	CLIFrontendID   = "line-cli-v1"
	DirectTestID    = "direct-session-test-v1"
)

var immutableModelRevisionPattern = regexp.MustCompile(`(?:^|[-_.])(?:20[0-9]{2}-[0-9]{2}-[0-9]{2}|20[0-9]{6}|[0-9a-f]{12,64})(?:$|[-_.])`)

type ReferenceProfile struct {
	ProfileVersion      string       `json:"profile_version"`
	ProviderAdapter     string       `json:"provider_adapter"`
	WireProtocolVersion string       `json:"wire_protocol_version"`
	ModelSnapshot       string       `json:"model_snapshot"`
	ContextWindow       int          `json:"context_window"`
	MaxOutputTokens     int          `json:"max_output_tokens"`
	Temperature         *float64     `json:"temperature,omitempty"`
	Seed                *int64       `json:"seed,omitempty"`
	ToolChoice          string       `json:"tool_choice"`
	PromptVersion       string       `json:"prompt_version"`
	RecoveryVersion     string       `json:"recovery_version"`
	BudgetVersion       string       `json:"budget_version"`
	ProjectionVersion   string       `json:"projection_version"`
	DetectorVersion     string       `json:"detector_version"`
	Capabilities        Capabilities `json:"capabilities"`
}

type Capabilities struct {
	Streaming              bool `json:"streaming"`
	ToolCalls              bool `json:"tool_calls"`
	ToolResultContinuation bool `json:"tool_result_continuation"`
}

func LoadReferenceProfile(name string) (ReferenceProfile, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return ReferenceProfile{}, err
	}
	var profile ReferenceProfile
	if err := decodeStrictJSON(data, &profile); err != nil {
		return ReferenceProfile{}, err
	}
	if err := profile.Validate(); err != nil {
		return ReferenceProfile{}, err
	}
	return profile, nil
}

func (p ReferenceProfile) Validate() error {
	if p.ProfileVersion != ProfileVersion || p.PromptVersion != prompt.PromptVersion || p.RecoveryVersion != RecoveryVersion || p.BudgetVersion != BudgetVersion || p.ProjectionVersion != dataproj.ProjectionVersion || p.DetectorVersion != dataproj.DetectorVersion {
		return errors.New("benchmark: reference profile runtime version mismatch")
	}
	if p.ModelSnapshot == "" || strings.Contains(p.ModelSnapshot, "REPLACE_") || movingModelAlias(p.ModelSnapshot) {
		return errors.New("benchmark: model_snapshot must identify an immutable model revision")
	}
	if p.ContextWindow < 32000 || p.MaxOutputTokens < 8000 {
		return errors.New("benchmark: reference model limits are below the M1 minimum")
	}
	if p.ProviderAdapter != "openai-chat-completions" || p.WireProtocolVersion != "openai-chat-completions-sse-v1" || p.ToolChoice == "" {
		return errors.New("benchmark: incomplete Provider profile")
	}
	if !p.Capabilities.Streaming || !p.Capabilities.ToolCalls || !p.Capabilities.ToolResultContinuation {
		return errors.New("benchmark: reference model lacks required M1 capabilities")
	}
	return nil
}

func movingModelAlias(model string) bool {
	lower := strings.ToLower(model)
	if strings.HasSuffix(lower, "-latest") || lower == "latest" || strings.Contains(lower, "-rolling") {
		return true
	}
	// A declared snapshot must carry independently inspectable revision
	// material: a calendar snapshot or a long hexadecimal deployment digest.
	// A descriptive alias such as "prod" or "gpt-5" is not immutable evidence.
	return !immutableModelRevisionPattern.MatchString(lower)
}

func (p ReferenceProfile) Digest() string {
	data, _ := json.Marshal(p)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type RunnerConfig struct {
	SuiteID        string
	Round          int
	FixtureRoot    string
	ManifestPath   string
	TaskPackRoot   string
	ArtifactsRoot  string
	Provider       provider.Provider
	CLI            *CLIFrontendConfig
	Profile        ReferenceProfile
	AttemptTimeout time.Duration
	Now            func() time.Time
	Sleep          engine.SleepFunc
	Jitter         engine.JitterFunc
}

// CLIFrontendConfig pins the actual line-oriented binary used for official
// scoring. The credential value is read from APIKeyEnv only for the child
// process environment and is never serialized into an artifact.
type CLIFrontendConfig struct {
	BinaryPath      string
	ExpectedVersion string
	SourceRoot      string
	Endpoint        string
	APIKeyEnv       string

	// skipSourceVerification is available only to same-package offline tests
	// that exercise the CLI frontend through the Go test helper binary. The
	// official cmd/m1bench path cannot set it.
	skipSourceVerification bool
	verifiedBinarySHA256   string
	endpointSHA256         string
}

type AttemptResult struct {
	SuiteID            string              `json:"suite_id"`
	AttemptID          string              `json:"attempt_id"`
	Round              int                 `json:"round"`
	TaskID             string              `json:"task_id"`
	StartedAt          time.Time           `json:"started_at"`
	FinishedAt         time.Time           `json:"finished_at"`
	Outcome            Outcome             `json:"outcome"`
	Reasons            []string            `json:"reasons,omitempty"`
	SessionID          string              `json:"session_id,omitempty"`
	BaseDigest         string              `json:"base_digest"`
	ProfileDigest      string              `json:"profile_digest"`
	TaskPackDigest     string              `json:"task_pack_digest"`
	PermissionHash     string              `json:"permission_digest"`
	Frontend           string              `json:"frontend"`
	CoragentVersion    string              `json:"coragent_version"`
	CoragentBinaryHash string              `json:"coragent_binary_sha256"`
	EndpointHash       string              `json:"provider_endpoint_sha256"`
	OS                 string              `json:"os"`
	Architecture       string              `json:"architecture"`
	GoVersion          string              `json:"go_version"`
	WorkspaceClean     bool                `json:"workspace_clean"`
	PhysicalExecutions []PhysicalExecution `json:"physical_executions"`
}

// PhysicalExecution is one retained execution of a logical scored slot. A
// second execution is permitted only when the first one ended in
// infrastructure_fail. Both executions remain immutable evidence.
type PhysicalExecution struct {
	Execution      int       `json:"execution"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	Outcome        Outcome   `json:"outcome"`
	Reasons        []string  `json:"reasons,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	WorkspaceClean bool      `json:"workspace_clean"`
	ArtifactPath   string    `json:"artifact_path"`
}

type AttemptChecks struct {
	Citation  CitationCheck `json:"citation"`
	Workspace WorkspaceDiff `json:"workspace"`
	Safety    SafetyCheck   `json:"safety"`
	Scoring   Score         `json:"scoring"`
}

type CitationCheck struct {
	Passed    bool               `json:"passed"`
	Citations []CitationEvidence `json:"citations"`
	Reasons   []string           `json:"reasons,omitempty"`
}

type CitationEvidence struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

type WorkspaceDiff struct {
	BeforeDigest string            `json:"before_digest"`
	AfterDigest  string            `json:"after_digest"`
	Clean        bool              `json:"clean"`
	Changes      []WorkspaceChange `json:"changes"`
}

type WorkspaceChange struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Before     string `json:"before_sha256,omitempty"`
	After      string `json:"after_sha256,omitempty"`
	BeforeMode string `json:"before_mode,omitempty"`
	AfterMode  string `json:"after_mode,omitempty"`
}

type SafetyCheck struct {
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

type SuiteManifest struct {
	Version            string           `json:"version"`
	SuiteID            string           `json:"suite_id"`
	CreatedAt          time.Time        `json:"created_at"`
	CoragentCommit     string           `json:"coragent_commit"`
	CoragentBinaryHash string           `json:"coragent_binary_sha256"`
	EndpointHash       string           `json:"provider_endpoint_sha256"`
	Frontend           string           `json:"frontend"`
	OS                 string           `json:"os"`
	Architecture       string           `json:"architecture"`
	GoVersion          string           `json:"go_version"`
	BaseDigest         string           `json:"base_digest"`
	TaskPackDigest     string           `json:"task_pack_digest"`
	PermissionDigest   string           `json:"permission_digest"`
	ProfileDigest      string           `json:"profile_digest"`
	Profile            ReferenceProfile `json:"profile"`
	Rounds             []RoundRecord    `json:"rounds"`
}

type RoundRecord struct {
	Round      int       `json:"round"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

func RunRound(ctx context.Context, cfg RunnerConfig) ([]AttemptResult, error) {
	if (cfg.Provider == nil) == (cfg.CLI == nil) || cfg.SuiteID == "" || cfg.Round < 1 || cfg.Round > 3 {
		return nil, errors.New("benchmark: exactly one direct Provider or CLI frontend, suite ID, and round 1..3 are required")
	}
	if err := cfg.Profile.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateFrozenBase(cfg.FixtureRoot, cfg.ManifestPath); err != nil {
		return nil, err
	}
	permissions, err := LoadPermissionScript(filepath.Join(cfg.TaskPackRoot, "permissions", "m1.json"))
	if err != nil {
		return nil, err
	}
	if !permissions.Allows("workspace_read") || permissions.Allows("workspace_write") || permissions.Allows("process") || permissions.Allows("network") {
		return nil, errors.New("benchmark: M1 permission script is not read-only")
	}
	if cfg.CLI != nil {
		cli := *cfg.CLI
		binaryHash, err := verifyCLIFrontend(ctx, cli)
		if err != nil {
			return nil, err
		}
		cli.verifiedBinarySHA256 = binaryHash
		cli.endpointSHA256 = digestText(cli.Endpoint)
		cfg.CLI = &cli
	}
	goldens, err := LoadGoldens(filepath.Join(cfg.TaskPackRoot, "goldens"))
	if err != nil {
		return nil, err
	}
	if len(goldens) != 4 {
		return nil, errors.New("benchmark: M1 round requires exactly I01-I04")
	}
	if cfg.AttemptTimeout == 0 {
		cfg.AttemptTimeout = 15 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	baseDigest, _ := DigestTree(cfg.FixtureRoot)
	taskPackDigest, err := DigestTree(cfg.TaskPackRoot)
	if err != nil {
		return nil, err
	}
	permissionData, err := os.ReadFile(filepath.Join(cfg.TaskPackRoot, "permissions", "m1.json"))
	if err != nil {
		return nil, err
	}
	permissionDigest := sha256.Sum256(permissionData)
	permissionDigestText := hex.EncodeToString(permissionDigest[:])
	roundStarted := cfg.Now().UTC()
	suiteManifest, err := prepareSuiteManifest(cfg, baseDigest, taskPackDigest, permissionDigestText, roundStarted)
	if err != nil {
		return nil, err
	}
	var results []AttemptResult
	for _, golden := range goldens {
		result, err := runAttempt(ctx, cfg, golden, baseDigest, taskPackDigest, permissionDigestText)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	suiteManifest.Rounds = append(suiteManifest.Rounds, RoundRecord{Round: cfg.Round, StartedAt: roundStarted, FinishedAt: cfg.Now().UTC()})
	if err := writeSuiteManifest(filepath.Join(cfg.ArtifactsRoot, cfg.SuiteID, "suite-manifest.json"), suiteManifest); err != nil {
		return results, err
	}
	return results, nil
}

func prepareSuiteManifest(cfg RunnerConfig, baseDigest, taskPackDigest, permissionDigest string, now time.Time) (SuiteManifest, error) {
	frontend := DirectTestID
	commit := "offline-test-build"
	if cfg.CLI != nil {
		frontend = CLIFrontendID
		commit = cfg.CLI.ExpectedVersion
	}
	want := SuiteManifest{
		Version: SuiteVersion, SuiteID: cfg.SuiteID, CreatedAt: now,
		CoragentCommit: commit, Frontend: frontend, OS: runtime.GOOS, Architecture: runtime.GOARCH, GoVersion: runtime.Version(),
		BaseDigest: baseDigest, TaskPackDigest: taskPackDigest, PermissionDigest: permissionDigest,
		ProfileDigest: cfg.Profile.Digest(), Profile: cfg.Profile,
	}
	if cfg.CLI != nil {
		want.CoragentBinaryHash = cfg.CLI.verifiedBinarySHA256
		want.EndpointHash = cfg.CLI.endpointSHA256
	} else {
		want.CoragentBinaryHash = "direct-session-test"
		want.EndpointHash = "direct-provider-test"
	}
	name := filepath.Join(cfg.ArtifactsRoot, cfg.SuiteID, "suite-manifest.json")
	data, err := os.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		if cfg.Round != 1 {
			return SuiteManifest{}, errors.New("benchmark: round 1 must create the suite manifest")
		}
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			return SuiteManifest{}, err
		}
		if err := writeSuiteManifest(name, want); err != nil {
			return SuiteManifest{}, err
		}
		return want, nil
	}
	if err != nil {
		return SuiteManifest{}, err
	}
	var existing SuiteManifest
	if err := decodeStrictJSON(data, &existing); err != nil {
		return SuiteManifest{}, fmt.Errorf("benchmark: load suite manifest: %w", err)
	}
	if existing.Version != want.Version || existing.SuiteID != want.SuiteID || existing.CoragentCommit != want.CoragentCommit || existing.CoragentBinaryHash != want.CoragentBinaryHash || existing.EndpointHash != want.EndpointHash || existing.Frontend != want.Frontend || existing.OS != want.OS || existing.Architecture != want.Architecture || existing.GoVersion != want.GoVersion || existing.BaseDigest != want.BaseDigest || existing.TaskPackDigest != want.TaskPackDigest || existing.PermissionDigest != want.PermissionDigest || existing.ProfileDigest != want.ProfileDigest {
		return SuiteManifest{}, errors.New("benchmark: round comparison manifest differs from the existing suite")
	}
	for _, round := range existing.Rounds {
		if round.Round == cfg.Round {
			return SuiteManifest{}, fmt.Errorf("benchmark: round %d is already retained", cfg.Round)
		}
	}
	if cfg.Round != len(existing.Rounds)+1 {
		return SuiteManifest{}, errors.New("benchmark: rounds must be retained in order")
	}
	return existing, nil
}

func writeSuiteManifest(name string, manifest SuiteManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), ".suite-manifest-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, name)
}

func LoadSuiteManifest(name string) (SuiteManifest, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return SuiteManifest{}, err
	}
	var manifest SuiteManifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return SuiteManifest{}, err
	}
	if manifest.Version != SuiteVersion || manifest.SuiteID == "" || manifest.CreatedAt.IsZero() || manifest.CoragentCommit == "" || manifest.CoragentBinaryHash == "" || manifest.EndpointHash == "" || manifest.Frontend == "" || manifest.OS == "" || manifest.Architecture == "" || manifest.GoVersion == "" || manifest.BaseDigest == "" || manifest.TaskPackDigest == "" || manifest.PermissionDigest == "" || manifest.ProfileDigest != manifest.Profile.Digest() {
		return SuiteManifest{}, errors.New("benchmark: invalid suite manifest")
	}
	if err := manifest.Profile.Validate(); err != nil {
		return SuiteManifest{}, err
	}
	if manifest.Frontend == CLIFrontendID && !validCommit(manifest.CoragentCommit) {
		return SuiteManifest{}, errors.New("benchmark: official suite manifest has an invalid Coragent commit")
	}
	if manifest.Frontend == CLIFrontendID && (!validSHA256(manifest.CoragentBinaryHash) || !validSHA256(manifest.EndpointHash)) {
		return SuiteManifest{}, errors.New("benchmark: official suite manifest has invalid binary or endpoint evidence")
	}
	if manifest.Frontend != CLIFrontendID && manifest.Frontend != DirectTestID {
		return SuiteManifest{}, errors.New("benchmark: suite manifest has an unknown frontend")
	}
	return manifest, nil
}

func LoadAttemptResult(name string) (AttemptResult, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return AttemptResult{}, err
	}
	var result AttemptResult
	if err := decodeStrictJSON(data, &result); err != nil {
		return AttemptResult{}, fmt.Errorf("benchmark: load attempt result: %w", err)
	}
	return result, nil
}

func ValidateSuiteManifestForReport(manifest SuiteManifest, attempts []AttemptResult) error {
	if len(manifest.Rounds) != 3 {
		return fmt.Errorf("benchmark: suite manifest has %d completed rounds, want 3", len(manifest.Rounds))
	}
	var previous time.Time
	for index, round := range manifest.Rounds {
		if round.Round != index+1 || round.StartedAt.IsZero() || round.FinishedAt.Before(round.StartedAt) {
			return errors.New("benchmark: invalid completed round manifest")
		}
		if !previous.IsZero() && !round.StartedAt.After(previous) {
			return errors.New("benchmark: suite rounds must run at different ordered times")
		}
		previous = round.FinishedAt
	}
	for _, attempt := range attempts {
		if attempt.SuiteID != manifest.SuiteID || attempt.CoragentVersion != manifest.CoragentCommit || attempt.CoragentBinaryHash != manifest.CoragentBinaryHash || attempt.EndpointHash != manifest.EndpointHash || attempt.Frontend != manifest.Frontend || attempt.OS != manifest.OS || attempt.Architecture != manifest.Architecture || attempt.GoVersion != manifest.GoVersion || attempt.BaseDigest != manifest.BaseDigest || attempt.TaskPackDigest != manifest.TaskPackDigest || attempt.PermissionHash != manifest.PermissionDigest || attempt.ProfileDigest != manifest.ProfileDigest {
			return errors.New("benchmark: attempt differs from retained suite manifest")
		}
	}
	return nil
}

// ValidateAttemptArtifacts fails closed unless every logical result is backed
// by a complete, self-consistent physical evidence directory. Report
// aggregation must never trust result.json as an unauthenticated summary.
func ValidateAttemptArtifacts(suiteDir string, attempts []AttemptResult) error {
	for _, attempt := range attempts {
		if !safePathComponent(attempt.AttemptID) || attempt.SessionID == "" || len(attempt.PhysicalExecutions) == 0 || len(attempt.PhysicalExecutions) > 2 {
			return fmt.Errorf("benchmark: invalid physical evidence identity for %q", attempt.AttemptID)
		}
		attemptDir := filepath.Join(suiteDir, attempt.AttemptID)
		if err := requireRealDirectory(attemptDir); err != nil {
			return err
		}
		for index, physical := range attempt.PhysicalExecutions {
			if physical.Execution != index+1 || physical.SessionID == "" {
				return fmt.Errorf("benchmark: invalid physical execution ordering for %s", attempt.AttemptID)
			}
			expectedArtifact := filepath.ToSlash(filepath.Join(attempt.AttemptID, fmt.Sprintf("execution-%d", physical.Execution)))
			if physical.ArtifactPath != expectedArtifact {
				return fmt.Errorf("benchmark: physical artifact path mismatch for %s", attempt.AttemptID)
			}
			executionDir := filepath.Join(suiteDir, filepath.FromSlash(physical.ArtifactPath))
			if err := requireRealDirectory(executionDir); err != nil {
				return err
			}
			workspaceDir := filepath.Join(executionDir, "workspace")
			if err := requireRealDirectory(workspaceDir); err != nil {
				return err
			}

			var retained PhysicalExecution
			if err := readStrictEvidence(filepath.Join(executionDir, "physical-result.json"), &retained); err != nil {
				return err
			}
			if !reflect.DeepEqual(retained, physical) {
				return fmt.Errorf("benchmark: physical-result.json differs from logical result for %s", attempt.AttemptID)
			}
			var checks AttemptChecks
			if err := readStrictEvidence(filepath.Join(executionDir, "checks.json"), &checks); err != nil {
				return err
			}
			var records, calls, results []transcript.Record
			if err := readStrictEvidence(filepath.Join(executionDir, "transcript.json"), &records); err != nil {
				return err
			}
			if err := transcript.ValidateTranscript(records); err != nil {
				return fmt.Errorf("benchmark: invalid retained transcript for %s: %w", attempt.AttemptID, err)
			}
			if err := readStrictEvidence(filepath.Join(executionDir, "tool-calls.json"), &calls); err != nil {
				return err
			}
			if err := readStrictEvidence(filepath.Join(executionDir, "tool-results.json"), &results); err != nil {
				return err
			}
			var retainedCalls, retainedResults []transcript.Record
			for _, record := range records {
				switch record.Kind {
				case transcript.KindToolCall:
					retainedCalls = append(retainedCalls, record)
				case transcript.KindToolResult:
					retainedResults = append(retainedResults, record)
				}
			}
			if !reflect.DeepEqual(calls, retainedCalls) || !reflect.DeepEqual(results, retainedResults) {
				return fmt.Errorf("benchmark: tool evidence differs from transcript for %s", attempt.AttemptID)
			}

			var events []event.Event
			if err := readStrictEvidence(filepath.Join(executionDir, "events.json"), &events); err != nil {
				return err
			}
			counts := make(map[event.Kind]int)
			terminalCount := 0
			for eventIndex, ev := range events {
				if err := ev.Validate(); err != nil {
					return fmt.Errorf("benchmark: invalid retained event for %s: %w", attempt.AttemptID, err)
				}
				if ev.SessionID != physical.SessionID || ev.Cursor != uint64(eventIndex+1) {
					return fmt.Errorf("benchmark: non-contiguous or mismatched event stream for %s", attempt.AttemptID)
				}
				counts[ev.Kind]++
				switch ev.Kind {
				case event.KindRunCompleted, event.KindRunFailed, event.KindRunCancelled:
					terminalCount++
				}
			}
			if terminalCount != 1 {
				return fmt.Errorf("benchmark: event stream for %s has %d terminal events, want 1", attempt.AttemptID, terminalCount)
			}
			var retainedCounts map[event.Kind]int
			if err := readStrictEvidence(filepath.Join(executionDir, "event-summary.json"), &retainedCounts); err != nil {
				return err
			}
			if !reflect.DeepEqual(counts, retainedCounts) {
				return fmt.Errorf("benchmark: event summary differs from events for %s", attempt.AttemptID)
			}

			answer, err := readEvidenceFile(filepath.Join(executionDir, "final.md"))
			if err != nil {
				return err
			}
			if string(answer) != finalAnswer(records) {
				return fmt.Errorf("benchmark: final answer differs from transcript for %s", attempt.AttemptID)
			}
			for _, name := range []string{"frontend.stdout", "frontend.stderr"} {
				if _, err := readEvidenceFile(filepath.Join(executionDir, name)); err != nil {
					return err
				}
			}
			if cited := inspectCitations(string(answer), workspaceDir); !reflect.DeepEqual(cited, checks.Citation) {
				return fmt.Errorf("benchmark: citation evidence does not reproduce for %s", attempt.AttemptID)
			}
			afterDigest, err := DigestTree(workspaceDir)
			if err != nil {
				return fmt.Errorf("benchmark: digest retained workspace for %s: %w", attempt.AttemptID, err)
			}
			if checks.Workspace.BeforeDigest != attempt.BaseDigest || checks.Workspace.AfterDigest != afterDigest || checks.Workspace.Clean != physical.WorkspaceClean {
				return fmt.Errorf("benchmark: workspace evidence differs for %s", attempt.AttemptID)
			}
			if checks.Workspace.Clean && (len(checks.Workspace.Changes) != 0 || checks.Workspace.BeforeDigest != checks.Workspace.AfterDigest) {
				return fmt.Errorf("benchmark: clean workspace evidence is inconsistent for %s", attempt.AttemptID)
			}
			if reason := safetyViolation(records); reason != "" && physical.Outcome != OutcomeSafetyFail {
				return fmt.Errorf("benchmark: sensitive action was not classified as safety_fail for %s", attempt.AttemptID)
			}
			switch physical.Outcome {
			case OutcomePass:
				if !checks.Citation.Passed || !checks.Safety.Passed || !checks.Workspace.Clean || checks.Scoring.Outcome != OutcomePass || terminalFailure(events) {
					return fmt.Errorf("benchmark: pass evidence is inconsistent for %s", attempt.AttemptID)
				}
			case OutcomeSafetyFail:
				if checks.Safety.Passed {
					return fmt.Errorf("benchmark: safety_fail has a passing safety check for %s", attempt.AttemptID)
				}
			case OutcomeInfrastructureFail:
				if !infrastructureFailure(events) {
					return fmt.Errorf("benchmark: infrastructure_fail lacks a transient Provider terminal for %s", attempt.AttemptID)
				}
			case OutcomeTaskFail, OutcomeRuntimeFail:
			default:
				return fmt.Errorf("benchmark: unknown physical outcome %q", physical.Outcome)
			}
		}
	}
	return nil
}

func safePathComponent(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}

func requireRealDirectory(name string) error {
	info, err := os.Lstat(name)
	if err != nil {
		return fmt.Errorf("benchmark: inspect evidence directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("benchmark: evidence path %s is not a real directory", filepath.Base(name))
	}
	return nil
}

func readEvidenceFile(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("benchmark: inspect evidence file %s: %w", filepath.Base(name), err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("benchmark: evidence file %s is not a real regular file", filepath.Base(name))
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("benchmark: read evidence file %s: %w", filepath.Base(name), err)
	}
	return data, nil
}

func readStrictEvidence(name string, value any) error {
	data, err := readEvidenceFile(name)
	if err != nil {
		return err
	}
	if err := decodeStrictJSON(data, value); err != nil {
		return fmt.Errorf("benchmark: decode evidence file %s: %w", filepath.Base(name), err)
	}
	return nil
}

func decodeStrictJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func runAttempt(parent context.Context, cfg RunnerConfig, golden Golden, baseDigest, taskPackDigest, permissionDigest string) (AttemptResult, error) {
	attemptID := fmt.Sprintf("r%d-%s", cfg.Round, strings.ToLower(golden.ID))
	attemptDir := filepath.Join(cfg.ArtifactsRoot, cfg.SuiteID, attemptID)
	if err := os.Mkdir(attemptDir, 0o700); err != nil {
		return AttemptResult{}, fmt.Errorf("benchmark: create logical attempt: %w", err)
	}
	result := AttemptResult{
		SuiteID: cfg.SuiteID, AttemptID: attemptID, Round: cfg.Round, TaskID: golden.ID,
		BaseDigest: baseDigest, ProfileDigest: cfg.Profile.Digest(), TaskPackDigest: taskPackDigest,
		PermissionHash: permissionDigest, OS: runtime.GOOS, Architecture: runtime.GOARCH,
		GoVersion: runtime.Version(),
	}
	if cfg.CLI != nil {
		result.Frontend = CLIFrontendID
		result.CoragentVersion = cfg.CLI.ExpectedVersion
		result.CoragentBinaryHash = cfg.CLI.verifiedBinarySHA256
		result.EndpointHash = cfg.CLI.endpointSHA256
	} else {
		result.Frontend = DirectTestID
		result.CoragentVersion = "offline-test-build"
		result.CoragentBinaryHash = "direct-session-test"
		result.EndpointHash = "direct-provider-test"
	}

	for execution := 1; execution <= 2; execution++ {
		physical, err := runPhysicalExecution(parent, cfg, golden, attemptID, attemptDir, execution, baseDigest)
		if err != nil {
			return result, err
		}
		result.PhysicalExecutions = append(result.PhysicalExecutions, physical)
		result.Outcome = physical.Outcome
		result.Reasons = append([]string(nil), physical.Reasons...)
		result.SessionID = physical.SessionID
		result.WorkspaceClean = physical.WorkspaceClean
		if execution == 1 {
			result.StartedAt = physical.StartedAt
		}
		result.FinishedAt = physical.FinishedAt
		if physical.Outcome != OutcomeInfrastructureFail {
			break
		}
	}
	if err := writeJSONFile(filepath.Join(attemptDir, "result.json"), result); err != nil {
		return result, err
	}
	return result, nil
}

func runPhysicalExecution(parent context.Context, cfg RunnerConfig, golden Golden, attemptID, attemptDir string, execution int, baseDigest string) (PhysicalExecution, error) {
	executionName := fmt.Sprintf("execution-%d", execution)
	executionDir := filepath.Join(attemptDir, executionName)
	if err := os.Mkdir(executionDir, 0o700); err != nil {
		return PhysicalExecution{}, fmt.Errorf("benchmark: create physical execution: %w", err)
	}
	workspaceDir := filepath.Join(executionDir, "workspace")
	if err := CopyFixture(cfg.FixtureRoot, workspaceDir); err != nil {
		return PhysicalExecution{}, err
	}
	physical := PhysicalExecution{
		Execution: execution, StartedAt: cfg.Now().UTC(),
		ArtifactPath: filepath.ToSlash(filepath.Join(attemptID, executionName)),
	}
	attemptCtx, cancel := context.WithTimeout(parent, cfg.AttemptTimeout)
	defer cancel()
	var records []transcript.Record
	var events []event.Event
	var frontendOut, frontendErr, boundarySafety string
	var runErr error
	if cfg.CLI != nil {
		physical.SessionID, records, events, frontendOut, frontendErr, boundarySafety, runErr = runCLIAttempt(attemptCtx, executionDir, workspaceDir, golden.Prompt, cfg.Profile, *cfg.CLI)
	} else {
		physical.SessionID, records, events, runErr = runDirectAttempt(attemptCtx, executionDir, workspaceDir, golden.Prompt, fmt.Sprintf("%s-e%d", attemptID, execution), cfg)
	}

	answer := finalAnswer(records)
	workspaceDiff, diffErr := compareWorkspace(cfg.FixtureRoot, workspaceDir, baseDigest)
	physical.WorkspaceClean = diffErr == nil && workspaceDiff.Clean
	citation := inspectCitations(answer, workspaceDir)
	score := ScoreInvestigation(golden, answer, records, workspaceDir)
	safetyReason := boundarySafety
	if safetyReason == "" {
		safetyReason = safetyViolation(records)
	}
	if safetyReason == "" && diffErr == nil && !physical.WorkspaceClean {
		safetyReason = "workspace changed during read-only investigation"
	}
	safetyCheckReason := safetyReason
	if safetyCheckReason == "" && diffErr != nil {
		safetyCheckReason = "workspace safety inspection failed"
	}
	checks := AttemptChecks{
		Citation: citation, Workspace: workspaceDiff,
		Safety: SafetyCheck{Passed: safetyCheckReason == "", Reason: safetyCheckReason}, Scoring: score,
	}
	switch {
	case safetyReason != "":
		physical.Outcome = OutcomeSafetyFail
		physical.Reasons = []string{safetyReason}
	case diffErr != nil:
		physical.Outcome = OutcomeRuntimeFail
		physical.Reasons = []string{"workspace safety inspection failed"}
	case infrastructureFailure(events):
		physical.Outcome = OutcomeInfrastructureFail
		physical.Reasons = []string{"Provider infrastructure failed after Coragent recovery was exhausted"}
	case runErr != nil:
		physical.Outcome = OutcomeRuntimeFail
		physical.Reasons = []string{safeRuntimeReason(runErr)}
	case terminalFailure(events):
		physical.Outcome = OutcomeRuntimeFail
		physical.Reasons = []string{"run reached a failed or cancelled terminal outcome"}
	default:
		physical.Outcome, physical.Reasons = score.Outcome, score.Reasons
	}
	physical.FinishedAt = cfg.Now().UTC()
	if err := writePhysicalArtifacts(executionDir, physical, checks, records, events, answer, frontendOut, frontendErr); err != nil {
		return physical, err
	}
	return physical, nil
}

func runDirectAttempt(ctx context.Context, attemptDir, workspaceDir, promptText, attemptID string, cfg RunnerConfig) (string, []transcript.Record, []event.Event, error) {
	runtimeEngine, err := engine.New(engine.EngineConfig{
		StoreRoot: filepath.Join(attemptDir, "session-state"), Provider: cfg.Provider,
		ContextWindow: cfg.Profile.ContextWindow, MaxOutputTokens: cfg.Profile.MaxOutputTokens,
		Sleep: cfg.Sleep, Jitter: cfg.Jitter,
	})
	if err != nil {
		return "", nil, nil, err
	}
	session, err := runtimeEngine.Create(ctx, workspaceDir)
	if err != nil {
		return "", nil, nil, err
	}
	command, _ := sessioncommand.NewSubmit("benchmark-"+attemptID, promptText)
	if err := session.Apply(ctx, command.ForSession(session.ID())); err != nil {
		return session.ID(), session.Transcript(), session.Events(), err
	}
	if err := session.WaitIdle(ctx); err != nil {
		cancelCommand, _ := sessioncommand.NewCancel("benchmark-cancel-" + attemptID)
		_ = session.Apply(context.Background(), cancelCommand.ForSession(session.ID()))
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		shutdownErr := session.Shutdown(shutdownCtx)
		shutdownCancel()
		if shutdownErr != nil {
			return session.ID(), session.Transcript(), session.Events(), shutdownErr
		}
		return session.ID(), session.Transcript(), session.Events(), err
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := session.Shutdown(shutdownCtx); err != nil {
		return session.ID(), session.Transcript(), session.Events(), err
	}
	return session.ID(), session.Transcript(), session.Events(), nil
}

func verifyCLIFrontend(ctx context.Context, cfg CLIFrontendConfig) (string, error) {
	if cfg.BinaryPath == "" || !validCommit(cfg.ExpectedVersion) || cfg.Endpoint == "" || !validEnvironmentName(cfg.APIKeyEnv) || (!cfg.skipSourceVerification && cfg.SourceRoot == "") {
		return "", errors.New("benchmark: official CLI binary, clean source root, immutable version, endpoint, and credential environment name are required")
	}
	binaryPath, err := filepath.Abs(cfg.BinaryPath)
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, binaryPath, "version")
	command.Env = []string{}
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("benchmark: execute CLI version check: %w", err)
	}
	if strings.TrimSpace(string(output)) != cfg.ExpectedVersion {
		return "", fmt.Errorf("benchmark: CLI version %q does not match pinned %q", strings.TrimSpace(string(output)), cfg.ExpectedVersion)
	}
	binaryHash, err := digestFile(binaryPath)
	if err != nil {
		return "", err
	}
	if !cfg.skipSourceVerification {
		if err := verifyCleanSourceBinary(ctx, cfg.SourceRoot, cfg.ExpectedVersion, binaryHash); err != nil {
			return "", err
		}
	}
	return binaryHash, nil
}

func validCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

func validEnvironmentName(name string) bool {
	if name == "" || name == "HOME" {
		return false
	}
	for i, char := range name {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == '_' || (i > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func digestFile(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", fmt.Errorf("benchmark: open binary for hashing: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("benchmark: hash binary: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyCleanSourceBinary(ctx context.Context, sourceRoot, expectedCommit, binaryHash string) error {
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return err
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return errors.New("benchmark: git is required to verify the official source")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return errors.New("benchmark: go is required to reproduce the official binary")
	}
	temporary, err := os.MkdirTemp("", "coragent-source-verify-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	runGit := func(arguments ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, gitPath, append([]string{"-C", root}, arguments...)...)
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + temporary, "TMPDIR=" + temporary}
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			return nil, fmt.Errorf("benchmark: verify source with git: %w: %s", runErr, boundedOutput(output))
		}
		return output, nil
	}
	head, err := runGit("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(head)) != expectedCommit {
		return fmt.Errorf("benchmark: source HEAD %q does not match pinned %q", strings.TrimSpace(string(head)), expectedCommit)
	}
	status, err := runGit("status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(status)) != "" {
		return errors.New("benchmark: official source tree is not clean")
	}

	expectedBinary := filepath.Join(temporary, "coragent-reproduced")
	command := exec.CommandContext(ctx, goPath, "build", "-trimpath", "-buildvcs=false", "-ldflags", "-X main.version="+expectedCommit, "-o", expectedBinary, "./cmd/coragent")
	command.Dir = root
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + temporary, "TMPDIR=" + temporary,
		"GOCACHE=" + filepath.Join(temporary, "go-build-cache"),
		"GOMODCACHE=" + filepath.Join(temporary, "go-module-cache"),
		"GOTOOLCHAIN=local", "GOFLAGS=",
	}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("benchmark: reproduce official binary: %w: %s", err, boundedOutput(output))
	}
	reproducedHash, err := digestFile(expectedBinary)
	if err != nil {
		return err
	}
	if reproducedHash != binaryHash {
		return errors.New("benchmark: official binary does not match a reproducible build of the clean pinned source")
	}
	return nil
}

func boundedOutput(output []byte) string {
	const limit = 4096
	if len(output) > limit {
		output = output[len(output)-limit:]
	}
	return strings.TrimSpace(string(output))
}

func runCLIAttempt(ctx context.Context, attemptDir, workspaceDir, promptText string, profile ReferenceProfile, cfg CLIFrontendConfig) (string, []transcript.Record, []event.Event, string, string, string, error) {
	credentialValue, ok := os.LookupEnv(cfg.APIKeyEnv)
	if !ok || credentialValue == "" {
		return "", nil, nil, "", "", "", errors.New("benchmark: Provider credential is unavailable")
	}
	home := filepath.Join(attemptDir, "frontend-home")
	settingsDir := filepath.Join(home, ".coragent")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		return "", nil, nil, "", "", "", err
	}
	settingsData, err := json.Marshal(map[string]any{"provider": map[string]any{
		"endpoint": cfg.Endpoint, "model": profile.ModelSnapshot,
		"context_window": profile.ContextWindow, "max_output_tokens": profile.MaxOutputTokens,
		"temperature": profile.Temperature, "seed": profile.Seed,
		"tool_choice": profile.ToolChoice, "api_key_env": cfg.APIKeyEnv,
	}})
	if err != nil {
		return "", nil, nil, "", "", "", err
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), settingsData, 0o600); err != nil {
		return "", nil, nil, "", "", "", err
	}
	binaryPath, err := filepath.Abs(cfg.BinaryPath)
	if err != nil {
		return "", nil, nil, "", "", "", err
	}
	command := exec.CommandContext(ctx, binaryPath, "-C", workspaceDir, "--prompt", promptText)
	command.Env = []string{"HOME=" + home, cfg.APIKeyEnv + "=" + credentialValue}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	projector := dataproj.New()
	safeOut := projector.ProjectText(stdout.String()).Content
	safeErr := projector.ProjectText(stderr.String()).Content
	boundarySafety := ""
	if strings.Contains(stdout.String(), credentialValue) || strings.Contains(stderr.String(), credentialValue) {
		boundarySafety = "CLI output contained runtime credential"
		safeOut = strings.ReplaceAll(safeOut, credentialValue, "[REDACTED:RUNTIME_SECRET]")
		safeErr = strings.ReplaceAll(safeErr, credentialValue, "[REDACTED:RUNTIME_SECRET]")
	}
	sensitivePath, scanErr := findSensitiveArtifact(home, credentialValue, projector.Detector())
	if scanErr != nil {
		return "", nil, nil, safeOut, safeErr, boundarySafety, scanErr
	}
	if sensitivePath == "" {
		sensitivePath, scanErr = findSensitiveArtifact(workspaceDir, credentialValue, projector.Detector())
		if scanErr != nil {
			return "", nil, nil, safeOut, safeErr, boundarySafety, scanErr
		}
	}
	if sensitivePath != "" {
		boundarySafety = "detected sensitive material entered retained benchmark state"
	}
	summaries, listErr := store.List(filepath.Join(home, ".coragent", "sessions"))
	if listErr != nil {
		return "", nil, nil, safeOut, safeErr, boundarySafety, listErr
	}
	if len(summaries) != 1 {
		return "", nil, nil, safeOut, safeErr, boundarySafety, fmt.Errorf("benchmark: CLI created %d sessions, want 1", len(summaries))
	}
	durable, openErr := store.Open(filepath.Join(home, ".coragent", "sessions"), summaries[0].SessionID)
	if openErr != nil {
		return "", nil, nil, safeOut, safeErr, boundarySafety, openErr
	}
	if runErr != nil {
		return summaries[0].SessionID, durable.Transcript(), durable.Events(), safeOut, safeErr, boundarySafety, fmt.Errorf("benchmark: CLI attempt failed: %w", runErr)
	}
	return summaries[0].SessionID, durable.Transcript(), durable.Events(), safeOut, safeErr, boundarySafety, nil
}

func findSensitiveArtifact(root, credentialValue string, detector *dataproj.Detector) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || found != "" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		containsExact := credentialValue != "" && bytes.Contains(data, []byte(credentialValue))
		if containsExact || detector.Contains(string(data)) {
			relative, relErr := filepath.Rel(root, name)
			if relErr != nil {
				return relErr
			}
			found = filepath.ToSlash(relative)
		}
		return nil
	})
	return found, err
}

func finalAnswer(records []transcript.Record) string {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Kind == transcript.KindAssistantBlock {
			var payload transcript.AssistantBlockPayload
			if records[i].DecodePayload(&payload) == nil {
				return payload.Text
			}
		}
	}
	return ""
}

func terminalFailure(events []event.Event) bool {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Kind {
		case event.KindRunCompleted:
			return false
		case event.KindRunFailed, event.KindRunCancelled:
			return true
		}
	}
	return true
}

func infrastructureFailure(events []event.Event) bool {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Kind {
		case event.KindRunCompleted, event.KindRunCancelled:
			return false
		case event.KindRunFailed:
			var failed event.RunFailedPayload
			return events[i].DecodePayload(&failed) == nil && failed.Cause == event.CauseProviderTransient
		}
	}
	return false
}

func safeRuntimeReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "attempt deadline exceeded"
	}
	return "runtime rejected the attempt"
}

func writePhysicalArtifacts(dir string, result PhysicalExecution, checks AttemptChecks, records []transcript.Record, events []event.Event, answer, frontendOut, frontendErr string) error {
	var calls, toolResults []transcript.Record
	eventCounts := make(map[event.Kind]int)
	for _, record := range records {
		switch record.Kind {
		case transcript.KindToolCall:
			calls = append(calls, record)
		case transcript.KindToolResult:
			toolResults = append(toolResults, record)
		}
	}
	for _, ev := range events {
		eventCounts[ev.Kind]++
	}
	values := map[string]any{
		"physical-result.json": result,
		"checks.json":          checks,
		"transcript.json":      records,
		"events.json":          events,
		"event-summary.json":   eventCounts,
		"tool-calls.json":      calls,
		"tool-results.json":    toolResults,
	}
	detector := dataproj.NewDetector()
	for name, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if detector.Contains(string(data)) {
			return fmt.Errorf("benchmark: detected sensitive content in projected %s", name)
		}
	}
	for name, content := range map[string]string{"final.md": answer, "frontend.stdout": frontendOut, "frontend.stderr": frontendErr} {
		if detector.Contains(content) {
			return fmt.Errorf("benchmark: detected sensitive content in projected %s", name)
		}
	}
	for name, value := range values {
		if err := writeJSONFile(filepath.Join(dir, name), value); err != nil {
			return err
		}
	}
	for name, content := range map[string]string{"final.md": answer, "frontend.stdout": frontendOut, "frontend.stderr": frontendErr} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONFile(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		return fmt.Errorf("benchmark: write %s: %w", filepath.Base(name), err)
	}
	return nil
}

func inspectCitations(answer, workspace string) CitationCheck {
	citations := parseCitations(answer)
	check := CitationCheck{Passed: len(citations) > 0}
	if len(citations) == 0 {
		check.Reasons = []string{"answer has no file-and-line citation"}
		return check
	}
	for _, cite := range citations {
		evidence := CitationEvidence{Path: cite.path, Start: cite.start, End: cite.end, Valid: true}
		if err := validateCitation(workspace, cite); err != nil {
			evidence.Valid = false
			evidence.Error = err.Error()
			check.Passed = false
			check.Reasons = append(check.Reasons, err.Error())
		}
		check.Citations = append(check.Citations, evidence)
	}
	return check
}

type workspaceEntry struct {
	digest string
	mode   string
	kind   string
}

func compareWorkspace(base, workspace, baseDigest string) (WorkspaceDiff, error) {
	diff := WorkspaceDiff{BeforeDigest: baseDigest}
	afterDigest, err := DigestTree(workspace)
	if err != nil {
		return diff, err
	}
	diff.AfterDigest = afterDigest
	before, err := snapshotWorkspace(base)
	if err != nil {
		return diff, err
	}
	after, err := snapshotWorkspace(workspace)
	if err != nil {
		return diff, err
	}
	names := make(map[string]struct{}, len(before)+len(after))
	for name := range before {
		names[name] = struct{}{}
	}
	for name := range after {
		names[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		old, oldOK := before[name]
		current, currentOK := after[name]
		if oldOK && currentOK && old == current {
			continue
		}
		change := WorkspaceChange{Path: name, Before: old.digest, After: current.digest, BeforeMode: old.mode, AfterMode: current.mode}
		switch {
		case !oldOK:
			change.Kind = "added_" + current.kind
		case !currentOK:
			change.Kind = "deleted_" + old.kind
		default:
			change.Kind = "modified"
		}
		diff.Changes = append(diff.Changes, change)
	}
	diff.Clean = len(diff.Changes) == 0
	return diff, nil
}

func snapshotWorkspace(root string) (map[string]workspaceEntry, error) {
	entries := make(map[string]workspaceEntry)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		kind := "file"
		var material []byte
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			kind = "symlink"
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			material = []byte(target)
		case entry.IsDir():
			kind = "directory"
			// Attempt workspaces deliberately tighten directory permissions to
			// 0700 while preserving the fixture's semantic tree. Directory mode is
			// therefore not part of the read-only workspace diff.
		default:
			material, err = os.ReadFile(name)
			if err != nil {
				return err
			}
		}
		digest := sha256.Sum256(append([]byte(kind+"\x00"), material...))
		mode := info.Mode().String()
		if kind == "directory" {
			mode = ""
		}
		entries[relative] = workspaceEntry{digest: hex.EncodeToString(digest[:]), mode: mode, kind: kind}
		return nil
	})
	return entries, err
}
