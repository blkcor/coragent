package executor

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/blkcor/coragent/internal/core"
)

// DefaultOutputBudget is the byte budget applied to every tool's output when no
// explicit budget is configured. One giant file or a chatty command cannot blow
// up the conversation past this.
const DefaultOutputBudget = 30_000

// truncate clips s to at most budget bytes on a clean UTF-8 rune boundary so the
// result is always valid text, appending a machine-legible marker stating how
// many bytes were elided. A non-positive budget disables truncation.
func truncate(s string, budget int) string {
	result, _ := truncateDetailed(s, budget)
	return result
}

func truncateDetailed(s string, budget int) (string, *core.Omission) {
	originalBytes := len(s)
	s = strings.ToValidUTF8(s, "�")
	if budget <= 0 || len(s) <= budget {
		return s, nil
	}
	cut := budget
	// Back off to the start of a rune so we never split a multi-byte character.
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	elided := len(s) - cut
	retained := s[:cut]
	omission := &core.Omission{
		Kind: core.OmissionOutputBudget, Scope: core.OmissionScopeToolOutput,
		Recoverability: core.RecoverabilityUnrecoverable, Continuation: core.ContinuationUnavailable,
		OriginalBytes: core.OptionalUint64{Known: true, Value: uint64(originalBytes)},
		RetainedBytes: core.OptionalUint64{Known: true, Value: uint64(len(retained))},
		OriginalLines: core.OptionalUint64{Known: true, Value: uint64(logicalLineCount(s))},
		RetainedLines: core.OptionalUint64{Known: true, Value: uint64(logicalLineCount(retained))},
	}
	return retained + fmt.Sprintf("\n[output truncated: %d bytes elided]", elided), omission
}

func logicalLineCount(value string) int {
	if value == "" {
		return 0
	}
	count := strings.Count(value, "\n") + 1
	if strings.HasSuffix(value, "\n") {
		count--
	}
	return count
}
