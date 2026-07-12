package skill

import (
	"regexp"
	"strings"
)

// slashPattern matches /skill-name tokens in user input.
var slashPattern = regexp.MustCompile(`/\S+`)

// ParseInvocations extracts skill invocations from user input.
// Returns the cleaned input (with /name tokens stripped) and the list of
// skill names referenced. Skills not found in the registry are silently
// left in the input — they may be emphasis or unrelated slash text.
func ParseInvocations(input string, reg *Registry) (cleaned string, skillNames []string) {
	if reg == nil || reg.Len() == 0 {
		return input, nil
	}

	matches := slashPattern.FindAllString(input, -1)
	if len(matches) == 0 {
		return input, nil
	}

	cleaned = input
	for _, match := range matches {
		name := strings.TrimPrefix(match, "/")
		// Strip trailing punctuation that might be part of the match
		name = strings.TrimRight(name, ".,;:!?")
		if name == "" {
			continue
		}
		if reg.Lookup(name) != nil {
			skillNames = append(skillNames, name)
			cleaned = strings.Replace(cleaned, match, "", 1)
		}
	}

	// Collapse whitespace from removed tokens.
	cleaned = strings.TrimSpace(cleaned)
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")

	if cleaned == "" {
		cleaned = input // fallback: don't send empty string
	}

	return cleaned, skillNames
}

// SkillBodies returns the bodies for the given skill names from the registry.
func SkillBodies(names []string, reg *Registry) []string {
	if reg == nil || len(names) == 0 {
		return nil
	}
	bodies := make([]string, 0, len(names))
	for _, name := range names {
		if s := reg.Lookup(name); s != nil {
			bodies = append(bodies, s.Body)
		}
	}
	return bodies
}
