package domain

import (
	"testing"
	"time"
)

// TestSplitQuarterIntervalUTCQ1ToQ2 verifies quarter splitting inserts a boundary at the UTC quarter transition.
func TestSplitQuarterIntervalUTCQ1ToQ2(t *testing.T) {
	start := time.Date(2020, 3, 31, 23, 0, 0, 0, time.UTC)
	end := time.Date(2020, 4, 1, 1, 0, 0, 0, time.UTC)

	spans, err := SplitQuarterIntervalUTC(start.UnixMilli(), end.UnixMilli())
	if err != nil {
		t.Fatalf("split interval: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	q1 := spans[0]
	q2 := spans[1]

	if q1.QuarterIndex >= q2.QuarterIndex {
		t.Fatalf("quarter ordering invalid: %d >= %d", q1.QuarterIndex, q2.QuarterIndex)
	}

	if q1.StartMs != start.UnixMilli() {
		t.Fatalf("q1 start mismatch: got %d want %d", q1.StartMs, start.UnixMilli())
	}
	if q1.EndMs != time.Date(2020, 4, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("q1 end mismatch: got %d", q1.EndMs)
	}
	if q2.StartMs != q1.EndMs {
		t.Fatalf("q2 start mismatch: got %d want %d", q2.StartMs, q1.EndMs)
	}
	if q2.EndMs != end.UnixMilli() {
		t.Fatalf("q2 end mismatch: got %d want %d", q2.EndMs, end.UnixMilli())
	}
}
