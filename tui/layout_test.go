package tui

import "testing"

func TestClassifyLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		width  int
		height int
		want   LayoutClass
	}{
		{width: 160, height: 48, want: LayoutWide},
		{width: 159, height: 48, want: LayoutStandard},
		{width: 160, height: 47, want: LayoutStandard},
		{width: 120, height: 36, want: LayoutStandard},
		{width: 119, height: 36, want: LayoutCompact},
		{width: 120, height: 35, want: LayoutCompact},
		{width: 80, height: 24, want: LayoutCompact},
		{width: 79, height: 24, want: LayoutMinimal},
		{width: 80, height: 23, want: LayoutMinimal},
		{width: 60, height: 20, want: LayoutMinimal},
		{width: 59, height: 20, want: LayoutTooSmall},
		{width: 60, height: 19, want: LayoutTooSmall},
	}

	for _, test := range tests {
		if got := ClassifyLayout(test.width, test.height); got != test.want {
			t.Errorf("ClassifyLayout(%d, %d) = %v, want %v", test.width, test.height, got, test.want)
		}
	}
}

func TestLayoutForSizeAppliesPriorityGeometry(t *testing.T) {
	t.Parallel()

	wide := LayoutForSize(160, 48)
	if wide.HorizontalPadding != 2 || !wide.TwoColumnDetail || !wide.ShowFullMetadata {
		t.Fatalf("wide layout = %#v", wide)
	}
	if wide.ProseWidth != MaximumProseWidth {
		t.Fatalf("wide prose width = %d, want %d", wide.ProseWidth, MaximumProseWidth)
	}

	compact := LayoutForSize(80, 24)
	if compact.HorizontalPadding != 2 || !compact.ShowModel || compact.ShowFullPath {
		t.Fatalf("compact layout = %#v", compact)
	}
	if compact.ComposerMaxRows != 4 {
		t.Fatalf("compact composer max rows = %d, want 4", compact.ComposerMaxRows)
	}

	minimal := LayoutForSize(60, 20)
	if minimal.HorizontalPadding != 1 || minimal.ShowModel || !minimal.MinimalBorders {
		t.Fatalf("minimal layout = %#v", minimal)
	}
	if minimal.ComposerMinRows != 3 || minimal.ComposerMaxRows != 3 {
		t.Fatalf("minimal composer rows = %d..%d, want 3..3", minimal.ComposerMinRows, minimal.ComposerMaxRows)
	}

	tiny := LayoutForSize(59, 19)
	if tiny.Class != LayoutTooSmall || tiny.ContentWidth != 59 || tiny.TranscriptRows != 19 {
		t.Fatalf("too-small layout = %#v", tiny)
	}
}

func TestRunLedgerGeometryDoesNotReserveHintRow(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 36}, {160, 48}} {
		layout := LayoutForSize(size[0], size[1])
		fixed := 2 + layout.ComposerMinRows
		if got, want := layout.TranscriptRows, layout.Height-fixed; got != want {
			t.Fatalf("%dx%d transcript rows = %d, want %d after removing hint row", size[0], size[1], got, want)
		}
	}
}

func TestCellWidthUsesUnicodeTerminalCells(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "ascii", value: "agent", want: 5},
		{name: "cjk", value: "中文", want: 4},
		{name: "emoji grapheme", value: "👩‍💻", want: 2},
		{name: "combining mark", value: "e\u0301", want: 1},
		{name: "untrusted ansi removed", value: "a\x1b[31mb", want: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := CellWidth(test.value); got != test.want {
				t.Fatalf("CellWidth(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestCompressPathUsesWholeTrailingSegments(t *testing.T) {
	t.Parallel()

	path := "/workspace/internal/executor/chain.go"
	want := "…/executor/chain.go"
	got := CompressPath(path, CellWidth(want))
	if got != want {
		t.Fatalf("CompressPath() = %q, want %q", got, want)
	}
	if CellWidth(got) > CellWidth(want) {
		t.Fatalf("compressed path width = %d, max %d", CellWidth(got), CellWidth(want))
	}

	asciiWant := ".../executor/chain.go"
	asciiGot := CompressPathForMode(path, CellWidth(asciiWant), ASCIIMode())
	if asciiGot != asciiWant {
		t.Fatalf("ASCII compressed path = %q, want %q", asciiGot, asciiWant)
	}

	filenameWidth := CellWidth("chain.go")
	if filename := CompressPath(path, filenameWidth); filename != "chain.go" {
		t.Fatalf("filename-only compression = %q, want chain.go", filename)
	}
}

func TestCompressPathHandlesWideSegmentsByCellWidth(t *testing.T) {
	t.Parallel()

	path := "/工作区/内部/执行器/文件.go"
	want := "…/执行器/文件.go"
	if got := CompressPath(path, CellWidth(want)); got != want {
		t.Fatalf("CompressPath(%q) = %q, want %q", path, got, want)
	}
}

func TestMetricLabelsAreNeverTailEllipsized(t *testing.T) {
	t.Parallel()

	metric := "ctx 38.4k / 128k 30% est"
	if got := FitMetricLabel(metric, CellWidth(metric)); got != metric {
		t.Fatalf("fitting metric = %q, want %q", got, metric)
	}
	if got := FitMetricLabel(metric, CellWidth(metric)-1); got != "" {
		t.Fatalf("non-fitting metric = %q, want whole-label omission", got)
	}
	if got := TruncateMetric(metric, 8); got != "" {
		t.Fatalf("TruncateMetric() = %q, want no ambiguous fragment", got)
	}
}
