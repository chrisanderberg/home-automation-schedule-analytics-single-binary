package domain

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestSolarBucketsDefined(t *testing.T) {
	lat := 37.7749
	lon := -122.4194
	timestamp := time.Date(2020, 6, 1, 12, 0, 0, 0, time.UTC).UnixMilli()

	checks := []struct {
		name string
		fn   func(int64, float64, float64) (int, error)
	}{
		{"mean", BucketAtMeanSolar},
		{"apparent", BucketAtApparentSolar},
		{"unequal", BucketAtUnequalHours},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			bucket, err := tc.fn(timestamp, lat, lon)
			if err != nil {
				t.Fatalf("bucket error: %v", err)
			}
			if bucket < 0 || bucket >= 7*288 {
				t.Fatalf("bucket out of range: %d", bucket)
			}
		})
	}
}

func TestUnequalHoursUndefinedOnly(t *testing.T) {
	lat := 78.2232
	lon := 15.6469
	timestamp := time.Date(2020, 12, 21, 12, 0, 0, 0, time.UTC).UnixMilli()

	bucket, err := BucketAtMeanSolar(timestamp, lat, lon)
	if err != nil {
		t.Fatalf("mean solar error: %v", err)
	}
	if bucket < 0 || bucket >= 7*288 {
		t.Fatalf("mean solar bucket out of range: %d", bucket)
	}

	bucket, err = BucketAtApparentSolar(timestamp, lat, lon)
	if err != nil {
		t.Fatalf("apparent solar error: %v", err)
	}
	if bucket < 0 || bucket >= 7*288 {
		t.Fatalf("apparent solar bucket out of range: %d", bucket)
	}

	_, err = BucketAtUnequalHours(timestamp, lat, lon)
	if err == nil {
		t.Fatalf("expected unequal hours to be undefined")
	}
	if !errors.Is(err, ErrUndefinedClock) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSolarCoordinateValidation(t *testing.T) {
	timestamp := time.Date(2020, 6, 1, 12, 0, 0, 0, time.UTC).UnixMilli()

	bucketChecks := []struct {
		name string
		fn   func(int64, float64, float64) (int, error)
	}{
		{"mean", BucketAtMeanSolar},
		{"apparent", BucketAtApparentSolar},
		{"unequal", BucketAtUnequalHours},
	}
	for _, tc := range bucketChecks {
		t.Run("bucket_"+tc.name, func(t *testing.T) {
			_, err := tc.fn(timestamp, 95, 0)
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates, got %v", err)
			}
		})
	}
	for _, tc := range bucketChecks {
		t.Run("bucket_"+tc.name+"_negative_oob", func(t *testing.T) {
			_, err := tc.fn(timestamp, -91, 0)
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates, got %v", err)
			}
		})
	}
	for _, tc := range bucketChecks {
		t.Run("bucket_"+tc.name+"_boundaries_allowed", func(t *testing.T) {
			_, err := tc.fn(timestamp, -90, 180)
			if errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected boundary coordinates to be allowed, got %v", err)
			}
		})
	}
	for _, tc := range bucketChecks {
		t.Run("bucket_"+tc.name+"_nan_lat", func(t *testing.T) {
			_, err := tc.fn(timestamp, math.NaN(), 0)
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates for NaN lat, got %v", err)
			}
		})
	}
	for _, tc := range bucketChecks {
		t.Run("bucket_"+tc.name+"_inf_lon", func(t *testing.T) {
			_, err := tc.fn(timestamp, 0, math.Inf(1))
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates for +Inf lon, got %v", err)
			}
		})
	}

	splitChecks := []struct {
		name string
		fn   func(int64, int64, float64, float64) ([]BucketSpan, error)
	}{
		{"mean", SplitIntervalMeanSolar},
		{"apparent", SplitIntervalApparentSolar},
		{"unequal", SplitIntervalUnequalHours},
	}
	for _, tc := range splitChecks {
		t.Run("split_"+tc.name, func(t *testing.T) {
			_, err := tc.fn(timestamp, timestamp+60_000, 0, 200)
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates, got %v", err)
			}
		})
	}
	for _, tc := range splitChecks {
		t.Run("split_"+tc.name+"_negative_oob", func(t *testing.T) {
			_, err := tc.fn(timestamp, timestamp+60_000, -91, 0)
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates, got %v", err)
			}
		})
	}
	for _, tc := range splitChecks {
		t.Run("split_"+tc.name+"_boundaries_allowed", func(t *testing.T) {
			_, err := tc.fn(timestamp, timestamp+60_000, 90, -180)
			if errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected boundary coordinates to be allowed, got %v", err)
			}
		})
	}
	for _, tc := range splitChecks {
		t.Run("split_"+tc.name+"_nan_lat", func(t *testing.T) {
			_, err := tc.fn(timestamp, timestamp+60_000, math.NaN(), 0)
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates for NaN lat, got %v", err)
			}
		})
	}
	for _, tc := range splitChecks {
		t.Run("split_"+tc.name+"_inf_lon", func(t *testing.T) {
			_, err := tc.fn(timestamp, timestamp+60_000, 0, math.Inf(-1))
			if !errors.Is(err, ErrInvalidCoordinates) {
				t.Fatalf("expected ErrInvalidCoordinates for -Inf lon, got %v", err)
			}
		})
	}
}
