package bucketing_test

import (
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
	"home-automation-schedule-analytics-single-bin/internal/domain/bucketing"
)

func newEngine(t *testing.T) *bucketing.Engine {
	t.Helper()
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	engine, err := bucketing.New(bucketing.Config{
		Location:  loc,
		Latitude:  37.7749,
		Longitude: -122.4194,
	})
	if err != nil {
		t.Fatalf("bucketing.New() error = %v", err)
	}
	return engine
}

func TestHoldingSegmentsSplitAcrossFiveMinuteBuckets(t *testing.T) {
	t.Parallel()

	engine := newEngine(t)
	start := time.Date(2026, time.March, 16, 10, 4, 30, 0, time.UTC)
	end := start.Add(90 * time.Second)

	segments, err := engine.HoldingSegments(bucketing.ClockUTC, start, end)
	if err != nil {
		t.Fatalf("HoldingSegments() error = %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("len(HoldingSegments()) = %d, want 2", len(segments))
	}
	if got, want := segments[0].Span, 30*time.Second; got != want {
		t.Fatalf("first segment span = %v, want %v", got, want)
	}
	if got, want := segments[1].Span, 60*time.Second; got != want {
		t.Fatalf("second segment span = %v, want %v", got, want)
	}
}

func TestTransitionBucketUsesMondayStart(t *testing.T) {
	t.Parallel()

	engine := newEngine(t)
	ts := time.Date(2026, time.March, 16, 0, 5, 0, 0, time.UTC) // Monday
	bucket, err := engine.TransitionBucket(bucketing.ClockUTC, ts)
	if err != nil {
		t.Fatalf("TransitionBucket() error = %v", err)
	}
	if got, want := bucket, 1; got != want {
		t.Fatalf("bucket = %d, want %d", got, want)
	}
}

func TestLocalTimeDSTFallbackCanMoveBackward(t *testing.T) {
	t.Parallel()

	engine := newEngine(t)
	first := time.Date(2026, time.November, 1, 8, 30, 0, 0, time.UTC)
	second := first.Add(45 * time.Minute)

	firstBucket, err := engine.TransitionBucket(bucketing.ClockLocal, first)
	if err != nil {
		t.Fatalf("TransitionBucket(first) error = %v", err)
	}
	secondBucket, err := engine.TransitionBucket(bucketing.ClockLocal, second)
	if err != nil {
		t.Fatalf("TransitionBucket(second) error = %v", err)
	}
	if secondBucket >= firstBucket {
		t.Fatalf("expected fallback bucket ordering to move backward, got first=%d second=%d", firstBucket, secondBucket)
	}
}

func TestAllClockBucketsAreInRange(t *testing.T) {
	t.Parallel()

	engine := newEngine(t)
	ts := time.Date(2026, time.June, 15, 12, 34, 0, 0, time.UTC)

	for _, clock := range []bucketing.Clock{
		bucketing.ClockUTC,
		bucketing.ClockLocal,
		bucketing.ClockMeanSolar,
		bucketing.ClockApparentSolar,
		bucketing.ClockUnequalHours,
	} {
		bucket, err := engine.TransitionBucket(clock, ts)
		if err != nil {
			t.Fatalf("TransitionBucket(%d) error = %v", clock, err)
		}
		if bucket < 0 || bucket >= blob.BucketsPerWeek {
			t.Fatalf("TransitionBucket(%d) = %d, out of range", clock, bucket)
		}
	}
}
