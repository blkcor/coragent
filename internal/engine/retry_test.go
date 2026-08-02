package engine

import (
	"testing"
	"time"
)

func TestRetryDelaySequenceAndCap(t *testing.T) {
	want := []time.Duration{
		500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second,
	}
	for retry, expected := range want {
		if got := retryDelay(retry); got != expected {
			t.Errorf("retryDelay(%d) = %v, want %v", retry, got, expected)
		}
	}
}

func TestDefaultJitterStaysWithinTwentyPercent(t *testing.T) {
	delay := 10 * time.Second
	for range 1000 {
		got := defaultJitter(delay)
		if got < 8*time.Second || got > 12*time.Second {
			t.Fatalf("jitter = %v, outside [8s, 12s]", got)
		}
	}
}
