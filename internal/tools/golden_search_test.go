package tools_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/dataproj"
	"github.com/blkcor/coragent/internal/provider"
	"github.com/blkcor/coragent/internal/tools"
	"github.com/blkcor/coragent/internal/transcript"
	"github.com/blkcor/coragent/internal/workspace"
)

// goldenSearch runs the real search tool through the Action Broker against the
// frozen Mercury fixture. The base is immutable, so the expected symbols,
// paths, and line numbers in TestSearchFindsExpectedMercurySymbols... are
// stable golden fixtures.
func goldenSearch(t *testing.T, pattern string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "benchmark-repo"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	broker, err := action.NewBroker(tools.NewCatalog(workspace.NewFileService(w), dataproj.New())...)
	if err != nil {
		t.Fatal(err)
	}
	arguments, _ := json.Marshal(map[string]string{"pattern": pattern})
	result := broker.Execute(context.Background(), provider.ToolCall{ID: "golden", Name: "search", Arguments: arguments})
	if result.Outcome != transcript.ToolResultSuccess {
		t.Fatalf("search %q = %+v", pattern, result)
	}
	return result.Content
}

func TestSearchFindsExpectedMercurySymbolsDeterministically(t *testing.T) {
	cases := []struct {
		pattern string
		want    []string // workspace-relative path:line prefixes that must appear
	}{
		{pattern: `FindByRequestID`, want: []string{
			"internal/jobs/service.go:30",
			"internal/jobs/storage.go:29",
			"internal/jobs/storage.go:44",
		}},
		{pattern: `func \(s \*Service\) Submit`, want: []string{
			"internal/jobs/service.go:26",
		}},
		{pattern: `NewID`, want: []string{
			"cmd/mercury/main.go:19",
			"internal/jobs/service.go:13",
			"internal/jobs/service.go:35",
		}},
		{pattern: `type Config struct`, want: []string{
			"internal/config/config.go:16",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			first := goldenSearch(t, tc.pattern)
			second := goldenSearch(t, tc.pattern)
			if first != second {
				t.Fatalf("search %q is not deterministic", tc.pattern)
			}
			for _, want := range tc.want {
				if !strings.Contains(first, want+":") {
					t.Errorf("search %q missing %q in:\n%s", tc.pattern, want, first)
				}
			}
		})
	}
}
