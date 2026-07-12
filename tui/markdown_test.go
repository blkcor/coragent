package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const markdownRegressionFixture = `# 机器学习路线

先看 **核心概念** 和 *实践顺序*。

- Python
- 线性代数

> 先理解假设，再记公式。

| 阶段 | 算法 |
| --- | --- |
| 入门 | 线性回归 |

` + "```go\nfmt.Println(\"verified\")\n```"

func TestCompletedAssistantMarkdownRendersSemanticStructure(t *testing.T) {
	store := completedAssistantStore(t, markdownRegressionFixture)
	lines := store.RenderLines(ThemeForMode(NoColorMode()), 64, 0)
	rendered := strings.Join(lines, "\n")

	for _, raw := range []string{"# 机器学习路线", "**核心概念**", "*实践顺序*", "| --- | --- |", "```go", "> 先理解"} {
		if strings.Contains(rendered, raw) {
			t.Fatalf("raw Markdown marker %q leaked into completed assistant output:\n%s", raw, rendered)
		}
	}
	for _, content := range []string{"机器学习路线", "核心概念", "实践顺序", "Python", "线性代数", "先理解假设", "阶段", "算法", "线性回归", "verified"} {
		if !strings.Contains(rendered, content) {
			t.Fatalf("rendered Markdown lost %q:\n%s", content, rendered)
		}
	}
	if strings.Contains(rendered, "\x1b") {
		t.Fatalf("no-color Markdown emitted terminal control sequences: %q", rendered)
	}
	for index, line := range lines {
		if width := ansi.StringWidth(line); width > 64 {
			t.Fatalf("Markdown line %d is %d cells wide, want <= 64: %q", index, width, line)
		}
	}
}

func TestStreamingAssistantMarkdownStylesRecognizableActiveTail(t *testing.T) {
	store := NewTranscriptStore()
	if err := store.StartAssistant("assistant-streaming-markdown", time.Time{}); err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}

	appendAndRender := func(chunk string) string {
		t.Helper()
		if err := store.AppendAssistant("assistant-streaming-markdown", chunk, time.Time{}); err != nil {
			t.Fatalf("AppendAssistant(%q): %v", chunk, err)
		}
		return strings.Join(store.RenderLines(ThemeForMode(NoColorMode()), 64, 0), "\n")
	}

	heading := appendAndRender("# 学习路线")
	if strings.Contains(heading, "# 学习路线") || !strings.Contains(heading, "学习路线") {
		t.Fatalf("first recognizable heading stayed raw:\n%s", heading)
	}

	partialEmphasis := appendAndRender("\n\n**重")
	if !strings.Contains(partialEmphasis, "重") {
		t.Fatalf("partial emphasis dropped visible content:\n%s", partialEmphasis)
	}
	emphasis := appendAndRender("点**")
	if strings.Contains(emphasis, "**重点**") || !strings.Contains(emphasis, "重点") {
		t.Fatalf("completed streaming emphasis stayed raw:\n%s", emphasis)
	}

	list := appendAndRender("\n\n- Python")
	if strings.Contains(list, "- Python") || !strings.Contains(list, "• Python") {
		t.Fatalf("first recognizable list item stayed raw:\n%s", list)
	}

	tableHeader := appendAndRender("\n\n| phase | tool |")
	if !strings.Contains(tableHeader, "phase") || !strings.Contains(tableHeader, "tool") {
		t.Fatalf("ambiguous streaming table header lost visible content:\n%s", tableHeader)
	}
	table := appendAndRender("\n| --- | --- |\n| start | go |")
	if strings.Contains(table, "| --- | --- |") || !strings.Contains(table, "start") || !strings.Contains(table, "go") {
		t.Fatalf("streaming table was not promoted after its delimiter arrived:\n%s", table)
	}

	openFence := appendAndRender("\n\n```go\nfmt.Println(\"streaming\")")
	if strings.Contains(openFence, "```go") || !strings.Contains(openFence, "streaming") {
		t.Fatalf("open fence was not rendered as safe code:\n%s", openFence)
	}
}

func TestCompletedMarkdownIsDeterministicAndHasNoDocumentMargin(t *testing.T) {
	theme := ThemeForMode(NoColorMode())
	first := renderMarkdownLines(theme, markdownRegressionFixture, 61)
	second := renderMarkdownLines(theme, markdownRegressionFixture, 61)
	if !slices.Equal(first, second) {
		t.Fatalf("whole-source Markdown rendering changed between passes:\nfirst: %#v\nsecond: %#v", first, second)
	}

	plain := renderMarkdownLines(theme, "plain", 20)
	if len(plain) == 0 || strings.HasPrefix(plain[0], " ") {
		t.Fatalf("custom Markdown document retained an outer margin: %#v", plain)
	}
}

func TestMarkdownOutputAllowsOnlyRendererSGR(t *testing.T) {
	source := "# Safe\n\n[link](https://example.test) [unsafe](javascript:alert(1)) ![alt](https://example.test/image.png)" +
		"\n\nraw\x1b]52;c;c2VjcmV0\a control" +
		"\n\nentity &#x1b;[31mowned&#x1b;[0m" +
		"\n\n<b>html</b><script>ignored()</script>"
	rendered := strings.Join(renderMarkdownLines(DefaultTheme(), source, 64), "\n")

	for _, forbidden := range []string{"\x1b]", "\x1bP", "\x1b_", "\x9d", "\a"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("Markdown output retained forbidden terminal sequence %q: %q", forbidden, rendered)
		}
	}
	if plain := ansi.Strip(rendered); !strings.Contains(plain, "link") || !strings.Contains(plain, "unsafe") || !strings.Contains(plain, "alt") || !strings.Contains(plain, "raw control") || !strings.Contains(plain, "html") {
		t.Fatalf("terminal filtering dropped printable content: %q", plain)
	}
	if plain := ansi.Strip(rendered); !strings.Contains(plain, "&#x1b;[31mowned&#x1b;[0m") {
		t.Fatalf("numeric control entities were decoded into terminal styling: %q", plain)
	}
	if plain := ansi.Strip(rendered); strings.Contains(plain, "<b>") || strings.Contains(plain, "<script>") {
		t.Fatalf("raw HTML reached the rendered transcript: %q", plain)
	}
	assertOnlySGRControls(t, rendered)

	noColor := strings.Join(renderMarkdownLines(ThemeForMode(NoColorMode()), source, 64), "\n")
	if strings.Contains(noColor, "\x1b") {
		t.Fatalf("no-color Markdown emitted ANSI: %q", noColor)
	}
}

func TestASCIIMarkdownUsesOnlyASCIIStructure(t *testing.T) {
	source := `# Route

- item

> quote

| phase | tool |
| --- | --- |
| start | go |

---

` + "```go\nfmt.Println(\"ok\")\n```"
	store := completedAssistantStore(t, source)
	rendered := ansi.Strip(strings.Join(store.RenderLines(ThemeForMode(ASCIIMode()), 64, 0), "\n"))
	for _, character := range rendered {
		if character > 127 {
			t.Fatalf("ASCII Markdown emitted Unicode structure %q:\n%s", character, rendered)
		}
	}
	for _, content := range []string{"Route", "- item", ">quote", "phase", "tool", "fmt.Println"} {
		if !strings.Contains(rendered, content) {
			t.Fatalf("ASCII Markdown lost %q:\n%s", content, rendered)
		}
	}
}

func TestOnlyAssistantBlocksInterpretMarkdown(t *testing.T) {
	store := NewTranscriptStore()
	store.AddUser("# user **literal**", time.Time{})
	if err := store.StartTool("literal-tool", "run", "**arguments**", time.Time{}); err != nil {
		t.Fatalf("StartTool: %v", err)
	}
	if err := store.FinishTool("literal-tool", "# result **literal**", ToolSucceeded, time.Time{}); err != nil {
		t.Fatalf("FinishTool: %v", err)
	}
	store.AddNotice("# notice **literal**", time.Time{})

	rendered := strings.Join(store.RenderLines(ThemeForMode(NoColorMode()), 80, 0), "\n")
	for _, literal := range []string{"# user **literal**", "**arguments**", "# result **literal**", "# notice **literal**"} {
		if !strings.Contains(rendered, literal) {
			t.Fatalf("non-assistant Markdown punctuation was interpreted or lost, want %q:\n%s", literal, rendered)
		}
	}
}

func TestMarkdownRenderCacheIsModeAwareAndBounded(t *testing.T) {
	store := completedAssistantStore(t, markdownRegressionFixture)
	theme := ThemeForMode(NoColorMode())
	store.RenderLines(theme, 64, 0)
	store.RenderLines(theme, 64, 7)
	if got := len(store.markdownCache.entries); got != 1 {
		t.Fatalf("same width/mode/source created %d cache entries, want 1", got)
	}

	store.RenderLines(ThemeForMode(VisualMode{Color: ColorANSI256}), 64, 0)
	if got := len(store.markdownCache.entries); got != 2 {
		t.Fatalf("visual mode did not create an independent cache entry: %d", got)
	}

	modes := []VisualMode{TrueColorMode(), {Color: ColorANSI256}, NoColorMode(), ASCIIMode()}
	for _, mode := range modes {
		for width := 8; width < 120; width++ {
			store.RenderLines(ThemeForMode(mode), width, 0)
		}
	}
	if got := len(store.markdownCache.entries); got != markdownRenderCacheCapacity {
		t.Fatalf("bounded cache retained %d entries, want capacity %d", got, markdownRenderCacheCapacity)
	}
	if got := len(store.markdownCache.order); got != markdownRenderCacheCapacity {
		t.Fatalf("bounded cache LRU retained %d keys, want %d", got, markdownRenderCacheCapacity)
	}
}

func TestStreamingMarkdownReplacesItsCachedVariant(t *testing.T) {
	store := NewTranscriptStore()
	if err := store.StartAssistant("stream-cache", time.Time{}); err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	if err := store.AppendAssistant("stream-cache", "# First", time.Time{}); err != nil {
		t.Fatalf("AppendAssistant: %v", err)
	}
	store.RenderLines(ThemeForMode(NoColorMode()), 64, 0)
	if err := store.AppendAssistant("stream-cache", " update", time.Time{}); err != nil {
		t.Fatalf("AppendAssistant update: %v", err)
	}
	rendered := strings.Join(store.RenderLines(ThemeForMode(NoColorMode()), 64, 1), "\n")
	if got := len(store.markdownCache.entries); got != 1 {
		t.Fatalf("streaming update accumulated %d cache entries, want replacement", got)
	}
	if !strings.Contains(rendered, "First update") {
		t.Fatalf("streaming cache returned stale content:\n%s", rendered)
	}
}

func TestStreamingPlanUsesStableBlankBoundariesAndIgnoresBlankLinesInsideFence(t *testing.T) {
	open := "intro\n\n```go\n\nfmt.Println(\"active\")\n"
	plan := planStreamingMarkdown(open)
	if len(plan.Stable) != 1 || plan.TailStart != len("intro\n\n") {
		t.Fatalf("open fence produced the wrong stable prefix: %#v", plan)
	}
	if tail := open[plan.TailStart:]; !strings.HasPrefix(tail, "```go") {
		t.Fatalf("blank line inside an open fence split the active construct: %q", tail)
	}

	closed := open + "```\nnext"
	plan = planStreamingMarkdown(closed)
	if len(plan.Stable) != 2 {
		t.Fatalf("closed fence was not promoted as one stable segment: %#v", plan.Stable)
	}
	if tail := closed[plan.TailStart:]; tail != "next" {
		t.Fatalf("closed fence tail = %q, want next", tail)
	}
}

func TestStreamingPlanBoundsOpenFenceTailWithoutDroppingSource(t *testing.T) {
	source := "```go\n" + strings.Repeat("fmt.Println(\"bounded\")\n", markdownStreamingTailLimit/8)
	plan := planStreamingMarkdown(source)
	if tailBytes := len(source) - plan.TailStart; tailBytes > markdownStreamingTailLimit {
		t.Fatalf("active Markdown tail retained %d bytes, limit %d", tailBytes, markdownStreamingTailLimit)
	}
	if plan.TailStart == 0 || plan.TailPrefix != "```go\n" {
		t.Fatalf("long open fence did not retain a synthetic rendering prefix: start=%d prefix=%q", plan.TailStart, plan.TailPrefix)
	}

	var reconstructed strings.Builder
	for _, segment := range plan.Stable {
		if segment.Start != reconstructed.Len() {
			t.Fatalf("stream segments are discontinuous at %d, reconstructed %d", segment.Start, reconstructed.Len())
		}
		reconstructed.WriteString(source[segment.Start:segment.End])
	}
	reconstructed.WriteString(source[plan.TailStart:])
	if reconstructed.String() != source {
		t.Fatal("bounded stream plan dropped or duplicated source bytes")
	}
}

func TestStreamingCacheReusesStablePrefixAndCompletionUsesWholeDocument(t *testing.T) {
	store := NewTranscriptStore()
	const id = "stable-prefix"
	if err := store.StartAssistant(id, time.Time{}); err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	if err := store.AppendAssistant(id, "first paragraph\n\n# active", time.Time{}); err != nil {
		t.Fatalf("AppendAssistant: %v", err)
	}
	theme := ThemeForMode(NoColorMode())
	store.RenderLines(theme, 64, 0)
	streamKey := markdownRenderCacheKey{BlockID: id, Width: 61, Mode: theme.Mode, Streaming: true}
	before, exists := store.markdownCache.peek(streamKey)
	if !exists || before.StableBytes != len("first paragraph\n\n") {
		t.Fatalf("stream cache did not retain the stable paragraph: %#v", before)
	}

	if err := store.AppendAssistant(id, " heading", time.Time{}); err != nil {
		t.Fatalf("AppendAssistant tail: %v", err)
	}
	store.RenderLines(theme, 64, 1)
	after, exists := store.markdownCache.peek(streamKey)
	if !exists || after.StableBytes != before.StableBytes || !slices.Equal(after.StableLines, before.StableLines) {
		t.Fatalf("active-tail update rerendered or changed the stable prefix:\nbefore: %#v\nafter: %#v", before, after)
	}

	if err := store.FinishAssistant(id); err != nil {
		t.Fatalf("FinishAssistant: %v", err)
	}
	store.RenderLines(theme, 64, 2)
	completeKey := markdownRenderCacheKey{BlockID: id, Width: 61, Mode: theme.Mode}
	completed, exists := store.markdownCache.peek(completeKey)
	if !exists {
		t.Fatal("completion did not create a distinct whole-document cache entry")
	}
	want := renderMarkdownLines(theme, store.Blocks[0].Text, 61)
	if !slices.Equal(completed.Lines, want) {
		t.Fatalf("completion reused progressive output instead of whole-source render:\nwant: %#v\ngot: %#v", want, completed.Lines)
	}
}

func TestMarkdownFailureFallsBackWithoutDroppingSource(t *testing.T) {
	source := "# keep **every** token\nsecond line"
	lines := finalizeMarkdownRender(ThemeForMode(NoColorMode()), source, 24, "", errors.New("renderer failed"))
	rendered := strings.Join(lines, "\n")
	for _, content := range []string{"# keep **every** token", "second line"} {
		if !strings.Contains(rendered, content) {
			t.Fatalf("Markdown fallback dropped %q: %q", content, rendered)
		}
	}
	if strings.Contains(rendered, "\x1b") {
		t.Fatalf("no-color Markdown fallback emitted ANSI: %q", rendered)
	}
}

func TestCompletedMarkdownIsIndependentOfProviderChunkBoundaries(t *testing.T) {
	oneChunk := completedAssistantStore(t, markdownRegressionFixture)
	byteChunks := NewTranscriptStore()
	if err := byteChunks.StartAssistant("assistant-markdown", time.Time{}); err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	for _, current := range []byte(markdownRegressionFixture) {
		if err := byteChunks.AppendAssistant("assistant-markdown", string([]byte{current}), time.Time{}); err != nil {
			t.Fatalf("AppendAssistant byte: %v", err)
		}
	}
	if err := byteChunks.FinishAssistant("assistant-markdown"); err != nil {
		t.Fatalf("FinishAssistant: %v", err)
	}

	theme := ThemeForMode(NoColorMode())
	want := oneChunk.RenderLines(theme, 64, 0)
	got := byteChunks.RenderLines(theme, 64, 0)
	if !slices.Equal(got, want) {
		t.Fatalf("provider chunk boundaries changed completed Markdown:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func assertOnlySGRControls(t *testing.T, value string) {
	t.Helper()
	parser := ansi.NewParser()
	state := byte(ansi.NormalState)
	for len(value) > 0 {
		sequence, _, consumed, nextState := ansi.DecodeSequence(value, state, parser)
		if consumed <= 0 {
			t.Fatalf("ANSI decoder did not advance for %q", value)
		}
		state = nextState
		if !markdownPrintableSequence(sequence) && !markdownSGRSequence(sequence, parser) {
			t.Fatalf("rendered control is not SGR: %q in %q", sequence, value)
		}
		value = value[consumed:]
	}
}

func completedAssistantStore(t *testing.T, source string) TranscriptStore {
	t.Helper()
	store := NewTranscriptStore()
	if err := store.StartAssistant("assistant-markdown", time.Time{}); err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	if err := store.AppendAssistant("assistant-markdown", source, time.Time{}); err != nil {
		t.Fatalf("AppendAssistant: %v", err)
	}
	if err := store.FinishAssistant("assistant-markdown"); err != nil {
		t.Fatalf("FinishAssistant: %v", err)
	}
	return store
}
