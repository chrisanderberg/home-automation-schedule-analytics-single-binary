package domain

import (
	"testing"
	"time"
)

// TestBucketAtUTCGoldens verifies representative UTC times map to the expected weekly buckets.
func TestBucketAtUTCGoldens(t *testing.T) {
	cases := []struct {
		name string
		time time.Time
		want int
	}{
		{
			name: "monday-midnight",
			time: time.Date(2020, 1, 6, 0, 0, 0, 0, time.UTC),
			want: 0,
		},
		{
			name: "monday-00-05",
			time: time.Date(2020, 1, 6, 0, 5, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "monday-12-34",
			time: time.Date(2020, 1, 6, 12, 34, 0, 0, time.UTC),
			want: 150,
		},
		{
			name: "sunday-23-55",
			time: time.Date(2020, 1, 12, 23, 55, 0, 0, time.UTC),
			want: 6*288 + 287,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BucketAtUTC(tc.time.UnixMilli())
			if err != nil {
				t.Fatalf("bucketAtUTC: %v", err)
			}
			if got != tc.want {
				t.Fatalf("bucket mismatch: got %d want %d", got, tc.want)
			}
		})
	}
}

// TestSplitIntervalUTCBoundary verifies UTC intervals split cleanly across five-minute boundaries.
func TestSplitIntervalUTCBoundary(t *testing.T) {
	start := time.Date(2020, 1, 6, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 1, 6, 0, 10, 0, 0, time.UTC)

	spans, err := SplitIntervalUTC(start.UnixMilli(), end.UnixMilli())
	if err != nil {
		t.Fatalf("split interval: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	if spans[0].Bucket != 0 || spans[0].Millis != 5*60*1000 {
		t.Fatalf("span0 mismatch: %+v", spans[0])
	}
	if spans[1].Bucket != 1 || spans[1].Millis != 5*60*1000 {
		t.Fatalf("span1 mismatch: %+v", spans[1])
	}
}

// TestSplitIntervalLocalDSTInvariants verifies local interval splitting preserves duration across a DST jump.
func TestSplitIntervalLocalDSTInvariants(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	start := time.Date(2020, 3, 8, 1, 30, 0, 0, loc)
	end := time.Date(2020, 3, 8, 3, 30, 0, 0, loc)

	spans, err := SplitIntervalLocal(start.UnixMilli(), end.UnixMilli(), loc)
	if err != nil {
		t.Fatalf("split interval local: %v", err)
	}
	if len(spans) == 0 {
		t.Fatalf("expected non-empty spans")
	}

	var sum int64
	for _, span := range spans {
		if span.Bucket < 0 || span.Bucket >= 7*288 {
			t.Fatalf("bucket out of range: %d", span.Bucket)
		}
		if span.Millis <= 0 {
			t.Fatalf("non-positive span: %d", span.Millis)
		}
		sum += span.Millis
	}

	expected := end.Sub(start).Milliseconds()
	if sum != expected {
		t.Fatalf("duration mismatch: got %d want %d", sum, expected)
	}
}

// TestSplitIntervalLocalDSTFallBack revisits the repeated local hour instead of forcing monotonic bucket indices.
func TestSplitIntervalLocalDSTFallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	start := time.Date(2020, 11, 1, 1, 55, 0, 0, loc)
	end := start.Add(15 * time.Minute)

	spans, err := SplitIntervalLocal(start.UnixMilli(), end.UnixMilli(), loc)
	if err != nil {
		t.Fatalf("split interval local: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}

	wantBuckets := []int{
		6*288 + 23,
		6*288 + 12,
		6*288 + 13,
	}
	for i, want := range wantBuckets {
		if spans[i].Bucket != want {
			t.Fatalf("span %d bucket mismatch: got %d want %d", i, spans[i].Bucket, want)
		}
	}

	for i, span := range spans {
		if span.Millis != 5*60*1000 && i != len(spans)-1 {
			t.Fatalf("span %d duration mismatch: got %d want %d", i, span.Millis, 5*60*1000)
		}
	}

	var sum int64
	for _, span := range spans {
		sum += span.Millis
	}
	if sum != end.Sub(start).Milliseconds() {
		t.Fatalf("duration mismatch: got %d want %d", sum, end.Sub(start).Milliseconds())
	}
}
