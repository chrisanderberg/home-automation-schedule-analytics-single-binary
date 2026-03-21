package quarter_test

import (
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain/quarter"
)

func TestIndexUsesUTCCalendarQuarter(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.FixedZone("west", -7*3600))
	if got, want := quarter.Index(ts), (2026-1970)*4+1; got != want {
		t.Fatalf("Index() = %d, want %d", got, want)
	}
}

func TestSplitAcrossQuarterBoundary(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.March, 31, 23, 50, 0, 0, time.UTC)
	end := time.Date(2026, time.April, 1, 0, 10, 0, 0, time.UTC)

	segments := quarter.Split(start, end)
	if len(segments) != 2 {
		t.Fatalf("len(Split()) = %d, want 2", len(segments))
	}
	if !segments[0].End.Equal(time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first segment end = %v", segments[0].End)
	}
	if !segments[1].Start.Equal(segments[0].End) {
		t.Fatalf("second segment start = %v, want %v", segments[1].Start, segments[0].End)
	}
}
