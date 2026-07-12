package tui

import (
	"strings"
	"testing"
)

func TestSanitizeStringNeutralizesTerminalControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "CSI style", input: "before\x1b[31mafter", want: "beforeafter"},
		{name: "CSI cursor", input: "left\x1b[999Cright", want: "leftright"},
		{name: "raw C1 CSI", input: "a" + string([]byte{0x9B}) + "31mb", want: "ab"},
		{name: "UTF-8 C1 CSI", input: "a\u009b31mb", want: "ab"},
		{name: "OSC title BEL", input: "a\x1b]0;owned\ab", want: "ab"},
		{name: "OSC title ST", input: "a\x1b]0;owned\x1b\\b", want: "ab"},
		{name: "OSC clipboard", input: "a\x1b]52;c;c2VjcmV0\ab", want: "ab"},
		{name: "OSC hyperlink", input: "\x1b]8;;https://example.test\x1b\\label\x1b]8;;\x1b\\", want: "label"},
		{name: "DCS", input: "a\x1bPpayload\x1b\\b", want: "ab"},
		{name: "APC", input: "a\x1b_payload\x1b\\b", want: "ab"},
		{name: "PM", input: "a\x1b^payload\x1b\\b", want: "ab"},
		{name: "SOS", input: "a\x1bXpayload\x1b\\b", want: "ab"},
		{name: "two-byte escape", input: "a\x1b7b", want: "ab"},
		{name: "C0 and DEL", input: "a\x00\x01\x07\x08\x0b\x0c\x7fb\tc\nd", want: "ab\tc\nd"},
		{name: "carriage return overwrite", input: "one\rOVER\r\ntwo", want: "one\nOVER\ntwo"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := SanitizeString(test.input); got != test.want {
				t.Fatalf("SanitizeString() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSanitizerNeutralizesSequencesAcrossEveryByteBoundary(t *testing.T) {
	t.Parallel()

	inputs := []struct {
		input string
		want  string
	}{
		{input: "before\x1b[38;2;255;0;0mafter", want: "beforeafter"},
		{input: "before\x1b]52;c;c2VjcmV0\x1b\\after", want: "beforeafter"},
		{input: "before\x1bPignored\x1b\\after", want: "beforeafter"},
		{input: "before\u009dignored\u009cafter", want: "beforeafter"},
	}

	for _, test := range inputs {
		var sanitizer Sanitizer
		var output strings.Builder
		for _, current := range []byte(test.input) {
			output.WriteString(sanitizer.Write(string([]byte{current})))
		}
		output.WriteString(sanitizer.Flush())
		if got := output.String(); got != test.want {
			t.Fatalf("one-byte chunks for %q = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestSanitizerPreservesSplitUnicodeGraphemes(t *testing.T) {
	t.Parallel()

	input := "中文 👩‍💻 e\u0301"
	var sanitizer Sanitizer
	var output strings.Builder
	for _, current := range []byte(input) {
		output.WriteString(sanitizer.Write(string([]byte{current})))
	}
	output.WriteString(sanitizer.Flush())
	if got := output.String(); got != input {
		t.Fatalf("split Unicode output = %q, want %q", got, input)
	}
}

func TestSanitizerFlushFailsClosedAndResets(t *testing.T) {
	t.Parallel()

	var sanitizer Sanitizer
	if got := sanitizer.Write("safe\x1b]52;c;unfinished"); got != "safe" {
		t.Fatalf("Write() = %q, want safe", got)
	}
	if got := sanitizer.Flush(); got != "" {
		t.Fatalf("Flush() exposed unfinished control content: %q", got)
	}
	if got := sanitizer.Write("next"); got != "next" {
		t.Fatalf("Write() after Flush = %q, want next", got)
	}
	if got := sanitizer.Flush(); got != "" {
		t.Fatalf("plain Flush() = %q, want empty", got)
	}
}

func TestSanitizerFlushReplacesIncompletePlainUTF8(t *testing.T) {
	t.Parallel()

	var sanitizer Sanitizer
	if got := sanitizer.Write(string([]byte{0xF0, 0x9F})); got != "" {
		t.Fatalf("incomplete UTF-8 Write() = %q, want buffered", got)
	}
	if got := sanitizer.Flush(); got != "�" {
		t.Fatalf("incomplete UTF-8 Flush() = %q, want replacement", got)
	}
}

func TestSanitizerDoesNotSwallowTextAfterCompleteSequence(t *testing.T) {
	t.Parallel()

	var sanitizer Sanitizer
	chunks := []string{"plain", "\x1b", "[31", "m", " tail", "\x1b]0;title", "\a", " end"}
	var output strings.Builder
	for _, chunk := range chunks {
		output.WriteString(sanitizer.Write(chunk))
	}
	output.WriteString(sanitizer.Flush())
	if got, want := output.String(), "plain tail end"; got != want {
		t.Fatalf("stream output = %q, want %q", got, want)
	}
}
