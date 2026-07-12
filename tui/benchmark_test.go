package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func BenchmarkBurstStreaming(b *testing.B) {
	theme := ThemeForMode(NoColorMode())
	for iteration := 0; iteration < b.N; iteration++ {
		store := NewTranscriptStore()
		_ = store.StartRun("run-burst")
		_ = store.StartAssistant("assistant-burst", time.Time{})
		for index := 0; index < 200; index++ {
			_ = store.AppendAssistant("assistant-burst", "stream **chunk** ", time.Time{})
			store.RenderRows(theme, 100, index)
		}
	}
}

func BenchmarkLongHistoryCached(b *testing.B) {
	store := cachedHistoryFixture(300)
	theme := ThemeForMode(NoColorMode())
	store.RenderRows(theme, 100, 0)
	b.ReportMetric(float64(len(store.renderCache)), "cached-blocks")
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		store.RenderRows(theme, 100, 0)
	}
}

func TestStreamingTailReusesCompletedBlockRenders(t *testing.T) {
	store := cachedHistoryFixture(120)
	theme := ThemeForMode(NoColorMode())
	store.RenderRows(theme, 100, 0)
	firstMisses := store.renderCacheMisses
	firstHits := store.renderCacheHits
	if err := store.AppendAssistant("active-tail", "next delta", time.Time{}); err != nil {
		t.Fatalf("AppendAssistant: %v", err)
	}
	store.RenderRows(theme, 100, 1)
	if misses := store.renderCacheMisses - firstMisses; misses != 1 {
		t.Fatalf("second render recomputed %d blocks, want only the active tail", misses)
	}
	if hits := store.renderCacheHits - firstHits; hits < 120 {
		t.Fatalf("second render reused %d completed blocks, want at least 120", hits)
	}

	allocations := testing.AllocsPerRun(10, func() {
		_ = store.RenderRows(theme, 100, 1)
	})
	if allocations > 2500 {
		t.Fatalf("cached 120-block render allocations = %.0f, want <= 2500", allocations)
	}
}

func cachedHistoryFixture(count int) TranscriptStore {
	store := NewTranscriptStore()
	_ = store.StartRun("run-history")
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("assistant-%d", index)
		_ = store.StartAssistant(id, time.Time{})
		_ = store.AppendAssistant(id, strings.Repeat("bounded history ", 8), time.Time{})
		_ = store.FinishAssistant(id)
	}
	_ = store.StartAssistant("active-tail", time.Time{})
	_ = store.AppendAssistant("active-tail", "active", time.Time{})
	return store
}
