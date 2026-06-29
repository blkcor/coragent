package executor

import (
	"fmt"
	"unicode/utf8"
)

// DefaultOutputBudget is the byte budget applied to every tool's output when no
// explicit budget is configured. One giant file or a chatty command cannot blow
// up the conversation past this.
const DefaultOutputBudget = 30_000

// truncate clips s to at most budget bytes on a clean UTF-8 rune boundary so the
// result is always valid text, appending a machine-legible marker stating how
// many bytes were elided. A non-positive budget disables truncation.
func truncate(s string, budget int) string {
	if budget <= 0 || len(s) <= budget {
		return s
	}
	cut := budget
	// Back off to the start of a rune so we never split a multi-byte character.
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	elided := len(s) - cut
	return s[:cut] + fmt.Sprintf("\n[output truncated: %d bytes elided]", elided)
}
