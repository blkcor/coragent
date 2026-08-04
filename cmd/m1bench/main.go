// Command m1bench runs one official M1 I01-I04 round or aggregates three
// retained rounds. It never stores the Provider credential value.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/blkcor/coragent/internal/benchmark"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "round":
		return runRound(args[1:])
	case "report":
		return runReport(args[1:])
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		writeFormatted(os.Stderr, "m1bench: unknown command %q\n", args[0])
		return 2
	}
}

func runRound(args []string) int {
	flags := flag.NewFlagSet("m1bench round", flag.ContinueOnError)
	suite := flags.String("suite", "", "suite ID shared by three rounds")
	round := flags.Int("round", 0, "round number (1, 2, or 3)")
	profilePath := flags.String("profile", "benchmarks/reference-profile.json", "immutable reference profile")
	endpoint := flags.String("endpoint", "", "fixed OpenAI-compatible chat-completions endpoint")
	apiKeyEnv := flags.String("api-key-env", "", "environment variable containing the runtime credential")
	coragentBin := flags.String("coragent-bin", "./coragent", "pinned line-oriented Coragent binary")
	coragentCommit := flags.String("coragent-commit", "", "full Coragent commit embedded in the binary")
	sourceRoot := flags.String("source-root", ".", "clean source tree used to reproduce the pinned binary")
	fixture := flags.String("fixture", "testdata/benchmark-repo", "frozen Mercury fixture")
	metadata := flags.String("metadata", "testdata/benchmark", "fixture metadata directory")
	taskpack := flags.String("taskpack", "testdata/benchmark/taskpack", "task-pack directory")
	artifacts := flags.String("artifacts", "artifacts/benchmarks", "artifact root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *suite == "" || *round < 1 || *round > 3 || *endpoint == "" || *apiKeyEnv == "" || *coragentCommit == "" {
		writeLine(os.Stderr, "m1bench round: --suite, --round 1..3, --endpoint, --api-key-env, and --coragent-commit are required")
		return 2
	}
	profile, err := benchmark.LoadReferenceProfile(*profilePath)
	if err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	results, err := benchmark.RunRound(context.Background(), benchmark.RunnerConfig{
		SuiteID: *suite, Round: *round, FixtureRoot: *fixture,
		ManifestPath: filepath.Join(*metadata, "manifest.json"), TaskPackRoot: *taskpack,
		ArtifactsRoot: *artifacts, Profile: profile,
		CLI: &benchmark.CLIFrontendConfig{
			BinaryPath: *coragentBin, ExpectedVersion: *coragentCommit,
			SourceRoot: *sourceRoot, Endpoint: *endpoint, APIKeyEnv: *apiKeyEnv,
		},
	})
	if err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	writeLine(os.Stdout, string(data))
	return 0
}

func runReport(args []string) int {
	flags := flag.NewFlagSet("m1bench report", flag.ContinueOnError)
	suite := flags.String("suite", "", "suite ID")
	artifacts := flags.String("artifacts", "artifacts/benchmarks", "artifact root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *suite == "" {
		writeLine(os.Stderr, "m1bench report: --suite is required")
		return 2
	}
	dir := filepath.Join(*artifacts, *suite)
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	var attempts []benchmark.AttemptResult
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		attempt, err := benchmark.LoadAttemptResult(filepath.Join(dir, entry.Name(), "result.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			writeLine(os.Stderr, err)
			return 1
		}
		attempts = append(attempts, attempt)
	}
	sort.Slice(attempts, func(i, j int) bool {
		if attempts[i].Round == attempts[j].Round {
			return attempts[i].TaskID < attempts[j].TaskID
		}
		return attempts[i].Round < attempts[j].Round
	})
	manifest, err := benchmark.LoadSuiteManifest(filepath.Join(dir, "suite-manifest.json"))
	if err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	if err := benchmark.ValidateSuiteManifestForReport(manifest, attempts); err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	if err := benchmark.ValidateAttemptArtifacts(dir, attempts); err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	report, err := benchmark.EvaluateM1Report(*suite, attempts)
	if err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), data, 0o600); err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	if _, err := benchmark.WriteContentManifest(dir, time.Now()); err != nil {
		writeLine(os.Stderr, err)
		return 1
	}
	writeLine(os.Stdout, string(data))
	if report.Decision != "pass" {
		return 1
	}
	return 0
}

func usage() {
	writeLine(os.Stderr, "usage:")
	writeLine(os.Stderr, "  m1bench round --suite ID --round N --endpoint URL --api-key-env NAME --coragent-bin PATH --coragent-commit FULL_SHA --source-root PATH")
	writeLine(os.Stderr, "  m1bench report --suite ID")
}

func writeLine(w io.Writer, values ...any) {
	_, _ = fmt.Fprintln(w, values...)
}

func writeFormatted(w io.Writer, format string, values ...any) {
	_, _ = fmt.Fprintf(w, format, values...)
}
