package tui

import (
	"strings"
	"unicode/utf8"
)

type sanitizeState uint8

const (
	sanitizeText sanitizeState = iota
	sanitizeEscape
	sanitizeEscapeIntermediate
	sanitizeCSI
	sanitizeOSC
	sanitizeOSCEscape
	sanitizeControlString
	sanitizeControlStringEscape
)

const (
	c1DCS = rune(0x90)
	c1SOS = rune(0x98)
	c1CSI = rune(0x9B)
	c1ST  = rune(0x9C)
	c1OSC = rune(0x9D)
	c1PM  = rune(0x9E)
	c1APC = rune(0x9F)
)

// Sanitizer strips terminal control sequences while preserving state across
// streamed chunks. A Sanitizer is intentionally not safe for concurrent use;
// one instance belongs to one ordered text stream.
type Sanitizer struct {
	state       sanitizeState
	pendingUTF8 []byte
	skipLF      bool
}

// Write consumes one streamed text chunk and returns only content safe to pass
// to Markdown, width measurement, wrapping, or rendering.
func (sanitizer *Sanitizer) Write(chunk string) string {
	data := make([]byte, 0, len(sanitizer.pendingUTF8)+len(chunk))
	data = append(data, sanitizer.pendingUTF8...)
	data = append(data, chunk...)
	sanitizer.pendingUTF8 = sanitizer.pendingUTF8[:0]

	var output strings.Builder
	output.Grow(len(data))

	for index := 0; index < len(data); {
		current := data[index]

		// Raw single-byte C1 controls are common in adversarial byte streams
		// even though they are not valid UTF-8. Treat them as controls rather
		// than exposing replacement glyphs that can obscure their intent.
		if current >= 0x80 && current <= 0x9F {
			sanitizer.consumeRune(rune(current), &output)
			index++
			continue
		}

		if current < utf8.RuneSelf {
			sanitizer.consumeRune(rune(current), &output)
			index++
			continue
		}

		if !utf8.FullRune(data[index:]) {
			sanitizer.pendingUTF8 = append(sanitizer.pendingUTF8, data[index:]...)
			break
		}

		decoded, size := utf8.DecodeRune(data[index:])
		if decoded == utf8.RuneError && size == 1 {
			if sanitizer.state == sanitizeText {
				sanitizer.writePrintable(utf8.RuneError, &output)
			}
			index++
			continue
		}

		sanitizer.consumeRune(decoded, &output)
		index += size
	}

	return output.String()
}

// WriteBytes is a convenience for byte-oriented provider and tool streams.
func (sanitizer *Sanitizer) WriteBytes(chunk []byte) []byte {
	return []byte(sanitizer.Write(string(chunk)))
}

// Flush ends the current stream. Incomplete UTF-8 in ordinary text becomes one
// replacement glyph; every unfinished terminal control sequence is discarded
// fail-closed. The Sanitizer is reset and can then be reused for another stream.
func (sanitizer *Sanitizer) Flush() string {
	var output string
	if sanitizer.state == sanitizeText && len(sanitizer.pendingUTF8) > 0 {
		output = string(utf8.RuneError)
	}
	sanitizer.Reset()
	return output
}

// Reset discards pending control and UTF-8 state.
func (sanitizer *Sanitizer) Reset() {
	sanitizer.state = sanitizeText
	sanitizer.pendingUTF8 = sanitizer.pendingUTF8[:0]
	sanitizer.skipLF = false
}

// SanitizeString sanitizes a complete untrusted string in one pass.
func SanitizeString(value string) string {
	var sanitizer Sanitizer
	return sanitizer.Write(value) + sanitizer.Flush()
}

func (sanitizer *Sanitizer) consumeRune(current rune, output *strings.Builder) {
	if sanitizer.skipLF {
		sanitizer.skipLF = false
		if sanitizer.state == sanitizeText && current == '\n' {
			return
		}
	}

	switch sanitizer.state {
	case sanitizeText:
		sanitizer.consumeText(current, output)
	case sanitizeEscape:
		sanitizer.consumeEscape(current)
	case sanitizeEscapeIntermediate:
		if current >= 0x30 && current <= 0x7E {
			sanitizer.state = sanitizeText
		} else if current == 0x1B {
			sanitizer.state = sanitizeEscape
		}
	case sanitizeCSI:
		if current >= 0x40 && current <= 0x7E {
			sanitizer.state = sanitizeText
		} else if current == 0x1B {
			sanitizer.state = sanitizeEscape
		} else if current == c1ST {
			sanitizer.state = sanitizeText
		}
	case sanitizeOSC:
		switch current {
		case '\a', c1ST:
			sanitizer.state = sanitizeText
		case 0x1B:
			sanitizer.state = sanitizeOSCEscape
		}
	case sanitizeOSCEscape:
		switch current {
		case '\\', c1ST:
			sanitizer.state = sanitizeText
		case 0x1B:
			// Stay poised for a following ST terminator.
		default:
			sanitizer.state = sanitizeOSC
		}
	case sanitizeControlString:
		switch current {
		case c1ST:
			sanitizer.state = sanitizeText
		case 0x1B:
			sanitizer.state = sanitizeControlStringEscape
		}
	case sanitizeControlStringEscape:
		switch current {
		case '\\', c1ST:
			sanitizer.state = sanitizeText
		case 0x1B:
			// Stay poised for a following ST terminator.
		default:
			sanitizer.state = sanitizeControlString
		}
	}
}

func (sanitizer *Sanitizer) consumeText(current rune, output *strings.Builder) {
	switch current {
	case 0x1B:
		sanitizer.state = sanitizeEscape
	case c1CSI:
		sanitizer.state = sanitizeCSI
	case c1OSC:
		sanitizer.state = sanitizeOSC
	case c1DCS, c1SOS, c1PM, c1APC:
		sanitizer.state = sanitizeControlString
	case '\r':
		// A carriage return is converted to a line break so progress output
		// cannot overwrite previously rendered cells. CRLF remains one break.
		output.WriteByte('\n')
		sanitizer.skipLF = true
	case '\n', '\t':
		output.WriteRune(current)
	default:
		if isDisallowedControl(current) {
			return
		}
		sanitizer.writePrintable(current, output)
	}
}

func (sanitizer *Sanitizer) consumeEscape(current rune) {
	switch current {
	case '[':
		sanitizer.state = sanitizeCSI
	case ']':
		sanitizer.state = sanitizeOSC
	case 'P', 'X', '^', '_':
		sanitizer.state = sanitizeControlString
	case 0x1B:
		// A repeated ESC starts a fresh escape sequence.
	case c1CSI:
		sanitizer.state = sanitizeCSI
	case c1OSC:
		sanitizer.state = sanitizeOSC
	case c1DCS, c1SOS, c1PM, c1APC:
		sanitizer.state = sanitizeControlString
	default:
		if current >= 0x20 && current <= 0x2F {
			sanitizer.state = sanitizeEscapeIntermediate
			return
		}
		// ESC plus a final byte is a complete two-byte escape. Unknown or
		// malformed input is also dropped and the next byte starts as text.
		sanitizer.state = sanitizeText
	}
}

func (sanitizer *Sanitizer) writePrintable(current rune, output *strings.Builder) {
	if current == utf8.RuneError {
		output.WriteRune(utf8.RuneError)
		return
	}
	output.WriteRune(current)
}

func isDisallowedControl(current rune) bool {
	return current < 0x20 || (current >= 0x7F && current <= 0x9F)
}
