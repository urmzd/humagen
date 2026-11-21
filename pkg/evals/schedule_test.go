package evals

import (
	"testing"
	"time"
)

func TestNextRunCountsFromTheLatestStart(t *testing.T) {
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	suite := Suite{EverySeconds: 6 * 3600}
	if _, ok := NextRun(Suite{}, []Run{{StartedAt: at}}); ok {
		t.Fatal("a suite without a schedule has a next run")
	}
	if next, ok := NextRun(suite, nil); !ok || !next.IsZero() {
		t.Fatalf("a scheduled suite that never ran: %v %v", next, ok)
	}
	runs := []Run{{StartedAt: at.Add(-3 * time.Hour)}, {StartedAt: at}, {StartedAt: at.Add(-9 * time.Hour)}}
	if next, ok := NextRun(suite, runs); !ok || !next.Equal(at.Add(6*time.Hour)) {
		t.Fatalf("next run: %v %v", next, ok)
	}
}
