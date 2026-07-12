package tui

import (
	"crypto/sha256"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	glamour "charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	xansi "github.com/charmbracelet/x/ansi"
)

// markdownRenderCacheCapacity bounds the number of completed render variants
// retained by one transcript. A variant is one assistant block at one terminal
// width and visual mode; streaming updates replace that variant in place.
const markdownRenderCacheCapacity = 96

// markdownStreamingTailLimit keeps per-delta parsing independent of the full
// reply length. Older content is promoted at deterministic line or UTF-8-safe
// boundaries when a model streams one very long paragraph or code fence.
const markdownStreamingTailLimit = 8 * 1024

var (
	markdownNumericEntity = regexp.MustCompile(`(?i)&#(?:x[0-9a-f]+|[0-9]+);?`)
	markdownImage         = regexp.MustCompile(`!\[([^\]\n]{0,512})\]\((?:\\.|[^)\n])*\)`)
	markdownLink          = regexp.MustCompile(`\[([^\]\n]{0,512})\]\((?:\\.|[^)\n])*\)`)
	markdownRawHTML       = regexp.MustCompile(`<[A-Za-z/!][^>\n]{0,1000}>`)
)

type markdownRenderCacheKey struct {
	BlockID string
	Width   int
	Mode    VisualMode
	// Streaming keeps the progressive approximation separate from the final
	// whole-document render, even when their source bytes are identical.
	Streaming bool
}

type markdownRenderCacheEntry struct {
	Digest       [sha256.Size]byte
	Lines        []string
	StableBytes  int
	StableDigest [sha256.Size]byte
	StableLines  []string
}

// markdownRenderCache is a small LRU. It intentionally caches rendered body
// lines rather than complete transcript rows so animation frames never enter
// the key and the assistant rail can keep updating independently.
type markdownRenderCache struct {
	entries map[markdownRenderCacheKey]markdownRenderCacheEntry
	order   []markdownRenderCacheKey
}

func (cache *markdownRenderCache) get(key markdownRenderCacheKey, source string) ([]string, bool) {
	if cache == nil || cache.entries == nil {
		return nil, false
	}
	entry, exists := cache.entries[key]
	if !exists || entry.Digest != sha256.Sum256([]byte(source)) {
		return nil, false
	}
	cache.touch(key)
	return cloneMarkdownLines(entry.Lines), true
}

func (cache *markdownRenderCache) put(key markdownRenderCacheKey, source string, lines []string) {
	cache.store(key, markdownRenderCacheEntry{
		Digest: sha256.Sum256([]byte(source)),
		Lines:  cloneMarkdownLines(lines),
	})
}

func (cache *markdownRenderCache) putStreaming(
	key markdownRenderCacheKey,
	source string,
	lines []string,
	stableBytes int,
	stableLines []string,
) {
	stableBytes = min(max(0, stableBytes), len(source))
	cache.store(key, markdownRenderCacheEntry{
		Digest:       sha256.Sum256([]byte(source)),
		Lines:        cloneMarkdownLines(lines),
		StableBytes:  stableBytes,
		StableDigest: sha256.Sum256([]byte(source[:stableBytes])),
		StableLines:  cloneMarkdownLines(stableLines),
	})
}

func (cache *markdownRenderCache) peek(key markdownRenderCacheKey) (markdownRenderCacheEntry, bool) {
	if cache == nil || cache.entries == nil {
		return markdownRenderCacheEntry{}, false
	}
	entry, exists := cache.entries[key]
	return entry, exists
}

func (cache *markdownRenderCache) store(key markdownRenderCacheKey, entry markdownRenderCacheEntry) {
	if cache == nil {
		return
	}
	if cache.entries == nil {
		cache.entries = make(map[markdownRenderCacheKey]markdownRenderCacheEntry)
	}
	if _, exists := cache.entries[key]; exists {
		cache.entries[key] = entry
		cache.touch(key)
		return
	}
	if len(cache.entries) >= markdownRenderCacheCapacity && len(cache.order) > 0 {
		delete(cache.entries, cache.order[0])
		cache.order = cache.order[1:]
	}
	cache.entries[key] = entry
	cache.order = append(cache.order, key)
}

func (cache *markdownRenderCache) touch(key markdownRenderCacheKey) {
	for index, current := range cache.order {
		if current != key {
			continue
		}
		copy(cache.order[index:], cache.order[index+1:])
		cache.order[len(cache.order)-1] = key
		return
	}
	cache.order = append(cache.order, key)
}

func cloneMarkdownLines(lines []string) []string {
	if lines == nil {
		return nil
	}
	return append([]string(nil), lines...)
}

type markdownStreamSegment struct {
	Start          int
	End            int
	RenderPrefix   string
	SeparatorAfter bool
}

type markdownStreamPlan struct {
	Stable     []markdownStreamSegment
	TailStart  int
	TailPrefix string
}

type markdownFenceRange struct {
	Start  int
	End    int
	Prefix string
}

type markdownOpenFence struct {
	Character byte
	Length    int
	ContentAt int
	Prefix    string
}

// planStreamingMarkdown promotes complete blank-line-delimited blocks and
// closed fences into a stable prefix. A pathological active construct is cut at
// deterministic, UTF-8-safe boundaries so only a bounded tail is reparsed.
func planStreamingMarkdown(source string) markdownStreamPlan {
	boundaries, fences := scanMarkdownBoundaries(source)
	plan := markdownStreamPlan{}
	start := 0

	appendSegment := func(end int, separator bool) {
		for end-start > markdownStreamingTailLimit {
			cut := markdownBoundedCut(source, start, end)
			plan.Stable = append(plan.Stable, markdownStreamSegment{
				Start:        start,
				End:          cut,
				RenderPrefix: markdownFencePrefixAt(fences, start),
			})
			start = cut
		}
		if end > start {
			plan.Stable = append(plan.Stable, markdownStreamSegment{
				Start:          start,
				End:            end,
				RenderPrefix:   markdownFencePrefixAt(fences, start),
				SeparatorAfter: separator,
			})
			start = end
		} else if separator && len(plan.Stable) > 0 {
			plan.Stable[len(plan.Stable)-1].SeparatorAfter = true
		}
	}

	for _, boundary := range boundaries {
		if boundary > start {
			appendSegment(boundary, true)
		}
	}
	for len(source)-start > markdownStreamingTailLimit {
		cut := markdownBoundedCut(source, start, len(source))
		plan.Stable = append(plan.Stable, markdownStreamSegment{
			Start:        start,
			End:          cut,
			RenderPrefix: markdownFencePrefixAt(fences, start),
		})
		start = cut
	}
	plan.TailStart = start
	plan.TailPrefix = markdownFencePrefixAt(fences, start)
	return plan
}

func scanMarkdownBoundaries(source string) ([]int, []markdownFenceRange) {
	boundaries := make([]int, 0, strings.Count(source, "\n")/2)
	fences := make([]markdownFenceRange, 0, 2)
	var open *markdownOpenFence

	for position := 0; position < len(source); {
		lineEnd := len(source)
		if relative := strings.IndexByte(source[position:], '\n'); relative >= 0 {
			lineEnd = position + relative + 1
		}
		line := source[position:lineEnd]
		lineBody := strings.TrimSuffix(line, "\n")

		if open == nil {
			character, length, ok := markdownFenceOpening(lineBody)
			if ok {
				prefix := line
				if !strings.HasSuffix(prefix, "\n") {
					prefix += "\n"
				}
				open = &markdownOpenFence{
					Character: character,
					Length:    length,
					ContentAt: lineEnd,
					Prefix:    prefix,
				}
			} else if strings.TrimSpace(lineBody) == "" {
				boundaries = append(boundaries, lineEnd)
			}
		} else if markdownFenceClosing(lineBody, *open) {
			fences = append(fences, markdownFenceRange{Start: open.ContentAt, End: lineEnd, Prefix: open.Prefix})
			boundaries = append(boundaries, lineEnd)
			open = nil
		}
		position = lineEnd
	}
	if open != nil {
		fences = append(fences, markdownFenceRange{Start: open.ContentAt, End: len(source), Prefix: open.Prefix})
	}
	return boundaries, fences
}

func markdownFenceOpening(line string) (byte, int, bool) {
	rest, ok := markdownFenceIndent(line)
	if !ok || rest == "" || (rest[0] != '`' && rest[0] != '~') {
		return 0, 0, false
	}
	character := rest[0]
	length := 0
	for length < len(rest) && rest[length] == character {
		length++
	}
	return character, length, length >= 3
}

func markdownFenceClosing(line string, open markdownOpenFence) bool {
	rest, ok := markdownFenceIndent(line)
	if !ok {
		return false
	}
	length := 0
	for length < len(rest) && rest[length] == open.Character {
		length++
	}
	return length >= open.Length && strings.TrimSpace(rest[length:]) == ""
}

func markdownFenceIndent(line string) (string, bool) {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces > 3 {
		return "", false
	}
	return line[spaces:], true
}

func markdownFencePrefixAt(fences []markdownFenceRange, position int) string {
	for _, fence := range fences {
		if position >= fence.Start && position < fence.End {
			return fence.Prefix
		}
	}
	return ""
}

func markdownBoundedCut(source string, start, end int) int {
	target := min(start+markdownStreamingTailLimit, end)
	if target >= end {
		return end
	}
	window := source[start:target]
	if newline := strings.LastIndexByte(window, '\n'); newline >= markdownStreamingTailLimit/2 {
		return start + newline + 1
	}
	cut := target
	for cut > start && !utf8.RuneStart(source[cut]) {
		cut--
	}
	if cut > start {
		return cut
	}
	return target
}

func renderStreamingMarkdownLines(
	theme Theme,
	source string,
	width int,
	cache *markdownRenderCache,
	key markdownRenderCacheKey,
) []string {
	source = SanitizeString(source)
	source = neutralizeMarkdownUnsafeConstructs(source)
	plan := planStreamingMarkdown(source)
	stableBytes := 0
	var stableLines []string

	if entry, exists := cache.peek(key); exists && entry.StableBytes <= plan.TailStart &&
		entry.StableBytes <= len(source) &&
		entry.StableDigest == sha256.Sum256([]byte(source[:entry.StableBytes])) {
		stableBytes = entry.StableBytes
		stableLines = cloneMarkdownLines(entry.StableLines)
	}

	for _, segment := range plan.Stable {
		if segment.End <= stableBytes {
			continue
		}
		if segment.Start != stableBytes {
			stableLines = nil
		}
		fragment := source[segment.Start:segment.End]
		if strings.TrimSpace(fragment) != "" {
			stableLines = append(stableLines, renderMarkdownLines(theme, segment.RenderPrefix+fragment, width)...)
		}
		if segment.SeparatorAfter && (len(stableLines) == 0 || !markdownLineBlank(stableLines[len(stableLines)-1])) {
			stableLines = append(stableLines, "")
		}
		stableBytes = segment.End
	}

	lines := cloneMarkdownLines(stableLines)
	tail := source[plan.TailStart:]
	if tail != "" {
		lines = append(lines, renderMarkdownLines(theme, plan.TailPrefix+tail, width)...)
	}
	for len(lines) > 1 && markdownLineBlank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	cache.putStreaming(key, source, lines, stableBytes, stableLines)
	return lines
}

// neutralizeMarkdownUnsafeConstructs prevents assistant-controlled HTML and
// image destinations from becoming renderer features. Link destinations are
// separately made inert by filterMarkdownTerminalOutput, which rejects every
// renderer-produced OSC hyperlink while retaining its printable label.
func neutralizeMarkdownUnsafeConstructs(source string) string {
	source = markdownImage.ReplaceAllStringFunc(source, func(value string) string {
		matches := markdownImage.FindStringSubmatch(value)
		label := "image omitted"
		if len(matches) > 1 && strings.TrimSpace(matches[1]) != "" {
			label += ": " + matches[1]
		}
		return "[" + label + "]"
	})
	source = markdownLink.ReplaceAllStringFunc(source, func(value string) string {
		matches := markdownLink.FindStringSubmatch(value)
		if len(matches) > 1 {
			return matches[1]
		}
		return "link"
	})
	return markdownRawHTML.ReplaceAllStringFunc(source, func(value string) string {
		return strings.ReplaceAll(strings.ReplaceAll(value, "<", "‹"), ">", "›")
	})
}

// renderMarkdownLines renders one sanitized assistant source as a complete
// document. Goldmark, used by Glamour, also recognizes active headings,
// emphasis, lists, and open fenced code blocks, so the same deterministic path
// progressively styles a streaming tail without inventing a second parser.
func renderMarkdownLines(theme Theme, source string, width int) (lines []string) {
	width = max(1, width)
	source = SanitizeString(source)
	source = neutralizeMarkdownUnsafeConstructs(source)
	if source == "" {
		return []string{""}
	}

	// A renderer bug must not take down the terminal or discard the model's
	// visible response. The fallback remains sanitized literal prose.
	defer func() {
		if recover() != nil {
			lines = markdownFallbackLines(theme, source, width)
		}
	}()

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle(theme)),
		glamour.WithWordWrap(width),
		glamour.WithTableWrap(true),
	)
	if err != nil {
		return markdownFallbackLines(theme, source, width)
	}
	rendered, err := renderer.Render(neutralizeMarkdownControlEntities(source))
	return finalizeMarkdownRender(theme, source, width, rendered, err)
}

// Goldmark decodes HTML character references in ordinary text. Neutralizing
// numeric references to control characters before parsing prevents a source
// such as "&#x1b;[31m" from manufacturing an SGR sequence after the input
// sanitizer has already run. The visible entity text is retained verbatim.
func neutralizeMarkdownControlEntities(source string) string {
	return markdownNumericEntity.ReplaceAllStringFunc(source, func(entity string) string {
		decoded := html.UnescapeString(entity)
		for _, value := range decoded {
			if unicode.IsControl(value) {
				return "&amp;" + entity[1:]
			}
		}
		return entity
	})
}

// finalizeMarkdownRender is split from renderer construction so the error
// fallback and terminal-safety boundary remain directly testable offline.
func finalizeMarkdownRender(theme Theme, source string, width int, rendered string, renderErr error) []string {
	if renderErr != nil {
		return markdownFallbackLines(theme, source, width)
	}

	filtered := filterMarkdownTerminalOutput(rendered, theme.Mode.Color != ColorNoColor)
	return splitMarkdownOutput(filtered, max(1, width))
}

func markdownFallbackLines(theme Theme, source string, width int) []string {
	lines := wrapProse(SanitizeString(source), max(1, width))
	for index := range lines {
		lines[index] = theme.TextStyle.Render(lines[index])
	}
	return lines
}

func splitMarkdownOutput(rendered string, width int) []string {
	rendered = strings.Trim(rendered, "\n")
	if rendered == "" {
		return []string{""}
	}

	lines := strings.Split(rendered, "\n")
	for len(lines) > 1 && markdownLineBlank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 1 && markdownLineBlank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}

	bounded := make([]string, 0, len(lines))
	for _, line := range lines {
		if xansi.StringWidth(line) <= width {
			bounded = append(bounded, line)
			continue
		}
		bounded = append(bounded, strings.Split(xansi.Hardwrap(line, width, false), "\n")...)
	}
	if len(bounded) == 0 {
		return []string{""}
	}
	return bounded
}

func markdownLineBlank(line string) bool {
	return strings.TrimSpace(xansi.Strip(line)) == ""
}

// filterMarkdownTerminalOutput treats Glamour as an untrusted sequence
// producer. Assistant input is sanitized before parsing, but Glamour creates
// OSC 8 hyperlinks for Markdown links and images. Only ordinary printable text,
// newline/tab, and renderer-generated SGR styling are allowed to reach the TUI.
func filterMarkdownTerminalOutput(value string, allowSGR bool) string {
	var output strings.Builder
	parser := xansi.NewParser()
	state := byte(xansi.NormalState)

	for len(value) > 0 {
		sequence, _, consumed, nextState := xansi.DecodeSequence(value, state, parser)
		if consumed <= 0 {
			// Fail safe without dropping printable content if the decoder cannot
			// advance for an unexpected byte sequence.
			output.WriteString(SanitizeString(xansi.Strip(value)))
			break
		}
		state = nextState
		if markdownPrintableSequence(sequence) || (allowSGR && markdownSGRSequence(sequence, parser)) {
			output.WriteString(sequence)
		}
		value = value[consumed:]
	}
	return output.String()
}

func markdownPrintableSequence(sequence string) bool {
	if sequence == "\n" || sequence == "\t" {
		return true
	}
	if sequence == "" {
		return false
	}
	value, _ := utf8.DecodeRuneInString(sequence)
	return !unicode.IsControl(value)
}

func markdownSGRSequence(sequence string, parser *xansi.Parser) bool {
	if !strings.HasPrefix(sequence, "\x1b[") {
		return false
	}
	command := xansi.Cmd(parser.Command())
	return command.Final() == 'm' && command.Prefix() == 0 && command.Intermediate() == 0
}

func markdownStyle(theme Theme) glamouransi.StyleConfig {
	quoteToken := "│"
	itemPrefix := "• "
	horizontalRule := "\n──────\n"
	centerSeparator := "┼"
	columnSeparator := "│"
	rowSeparator := "─"
	definitionPrefix := "\n• "
	if theme.Mode.ASCII {
		quoteToken = ">"
		itemPrefix = "- "
		horizontalRule = "\n------\n"
		centerSeparator = "+"
		columnSeparator = "|"
		rowSeparator = "-"
		definitionPrefix = "\n- "
	}

	zero := uint(0)
	one := uint(1)
	two := uint(2)
	textColor := markdownColor(theme, theme.Palette.Text, 254)
	mutedColor := markdownColor(theme, theme.Palette.Muted, 102)
	accentColor := markdownColor(theme, theme.Palette.Accent, 173)
	infoColor := markdownColor(theme, theme.Palette.Info, 109)
	borderColor := markdownColor(theme, theme.Palette.Border, 237)

	var strong, emphasis, underline, crossedOut *bool
	if theme.Mode.Color != ColorNoColor {
		strong = markdownBool(true)
		emphasis = markdownBool(true)
		underline = markdownBool(true)
		crossedOut = markdownBool(true)
	}

	return glamouransi.StyleConfig{
		Document: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: textColor},
			Margin:         &zero,
		},
		BlockQuote: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: mutedColor},
			Indent:         &one,
			IndentToken:    &quoteToken,
		},
		Paragraph: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: textColor},
		},
		List: glamouransi.StyleList{
			StyleBlock: glamouransi.StyleBlock{
				StylePrimitive: glamouransi.StylePrimitive{Color: textColor},
			},
			LevelIndent: 2,
		},
		Heading: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       accentColor,
				Bold:        strong,
			},
		},
		Text:          glamouransi.StylePrimitive{Color: textColor},
		Strikethrough: glamouransi.StylePrimitive{Color: mutedColor, CrossedOut: crossedOut},
		Emph:          glamouransi.StylePrimitive{Color: textColor, Italic: emphasis},
		Strong:        glamouransi.StylePrimitive{Color: textColor, Bold: strong},
		HorizontalRule: glamouransi.StylePrimitive{
			Color:  borderColor,
			Format: horizontalRule,
		},
		Item:        glamouransi.StylePrimitive{BlockPrefix: itemPrefix},
		Enumeration: glamouransi.StylePrimitive{BlockPrefix: ". "},
		Task: glamouransi.StyleTask{
			Ticked:   "[x] ",
			Unticked: "[ ] ",
		},
		Link:     glamouransi.StylePrimitive{Color: accentColor, Underline: underline},
		LinkText: glamouransi.StylePrimitive{Color: accentColor, Underline: underline},
		Image:    glamouransi.StylePrimitive{Color: mutedColor},
		ImageText: glamouransi.StylePrimitive{
			Color:  mutedColor,
			Format: "Image: {{.text}}",
		},
		Code: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: infoColor},
		},
		CodeBlock: glamouransi.StyleCodeBlock{
			StyleBlock: glamouransi.StyleBlock{
				StylePrimitive: glamouransi.StylePrimitive{Color: infoColor},
				Indent:         &two,
				Margin:         &zero,
			},
		},
		Table: glamouransi.StyleTable{
			StyleBlock: glamouransi.StyleBlock{
				StylePrimitive: glamouransi.StylePrimitive{Color: textColor},
				Margin:         &zero,
			},
			CenterSeparator: &centerSeparator,
			ColumnSeparator: &columnSeparator,
			RowSeparator:    &rowSeparator,
		},
		DefinitionList: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: textColor},
		},
		DefinitionTerm: glamouransi.StylePrimitive{Color: accentColor, Bold: strong},
		DefinitionDescription: glamouransi.StylePrimitive{
			Color:       textColor,
			BlockPrefix: definitionPrefix,
		},
		HTMLBlock: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: textColor},
		},
		HTMLSpan: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: textColor},
		},
	}
}

func markdownColor(theme Theme, trueColor string, ansi256 int) *string {
	if theme.Mode.Color == ColorNoColor {
		return nil
	}
	value := trueColor
	if theme.Mode.Color == ColorANSI256 {
		value = strconv.Itoa(ansi256)
	}
	return &value
}

func markdownBool(value bool) *bool {
	return &value
}
