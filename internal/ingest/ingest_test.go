package ingest

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := storage.InitSchema(context.Background(), db); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestHoldingIngestSingleBucketUTC(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cfg := Config{TimeZone: "UTC", Latitude: 37.7749, Longitude: -122.4194}

	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "light", ControlType: storage.ControlTypeDiscrete, NumStates: 3,
	}); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	start := time.Date(2020, 1, 6, 0, 1, 0, 0, time.UTC)
	end := time.Date(2020, 1, 6, 0, 3, 0, 0, time.UTC)
	input := HoldingInput{
		ControlID: "light", ModelID: "default", State: 1,
		StartTimeMs: start.UnixMilli(), EndTimeMs: end.UnixMilli(),
	}

	if err := IngestHolding(ctx, db, cfg, input); err != nil {
		t.Fatalf("ingest holding: %v", err)
	}

	key := storage.AggregateKey{ControlID: "light", ModelID: "default", QuarterIndex: domain.QuarterIndexUTC(start.UnixMilli())}
	data, err := storage.GetOrCreateAggregate(ctx, db, key, 3)
	if err != nil {
		t.Fatalf("get aggregate: %v", err)
	}

	b, _ := domain.NewBlob(3)
	copy(b.Data(), data)

	expectedMs := end.Sub(start).Milliseconds()

	checkClockSpans := func(clock int, spans []domain.BucketSpan, name string) {
		var total uint64
		for _, span := range spans {
			idx, err := domain.HoldIndex(1, clock, span.Bucket, 3)
			if err != nil {
				t.Fatalf("%s hold index: %v", name, err)
			}
			v, err := b.GetU64(idx)
			if err != nil {
				t.Fatalf("%s get value: %v", name, err)
			}
			if v != uint64(span.Millis) {
				t.Fatalf("%s holding bucket %d: got %d want %d", name, span.Bucket, v, span.Millis)
			}
			total += v
		}
		if total != uint64(expectedMs) {
			t.Fatalf("%s total holding: got %d want %d", name, total, expectedMs)
		}
	}

	utcSpans, err := domain.SplitIntervalUTC(input.StartTimeMs, input.EndTimeMs)
	if err != nil {
		t.Fatalf("split interval UTC: %v", err)
	}
	checkClockSpans(domain.ClockUTC, utcSpans, "UTC")

	localSpans, err := domain.SplitIntervalLocal(input.StartTimeMs, input.EndTimeMs, time.UTC)
	if err != nil {
		t.Fatalf("split interval local: %v", err)
	}
	checkClockSpans(domain.ClockLocal, localSpans, "Local")

	meanSolarSpans, err := domain.SplitIntervalMeanSolar(input.StartTimeMs, input.EndTimeMs, cfg.Latitude, cfg.Longitude)
	if err != nil {
		t.Fatalf("split interval mean solar: %v", err)
	}
	checkClockSpans(domain.ClockMeanSolar, meanSolarSpans, "MeanSolar")

	apparentSolarSpans, err := domain.SplitIntervalApparentSolar(input.StartTimeMs, input.EndTimeMs, cfg.Latitude, cfg.Longitude)
	if err != nil {
		t.Fatalf("split interval apparent solar: %v", err)
	}
	checkClockSpans(domain.ClockApparentSolar, apparentSolarSpans, "ApparentSolar")

	unequalHoursSpans, err := domain.SplitIntervalUnequalHours(input.StartTimeMs, input.EndTimeMs, cfg.Latitude, cfg.Longitude)
	if err != nil {
		t.Fatalf("split interval unequal hours: %v", err)
	}
	checkClockSpans(domain.ClockUnequalHours, unequalHoursSpans, "UnequalHours")

	otherIdx, _ := domain.HoldIndex(0, domain.ClockUTC, 0, 3)
	v, _ := b.GetU64(otherIdx)
	if v != 0 {
		t.Fatalf("other state should be 0, got %d", v)
	}
}

func TestTransitionIngestSingleBucketUTC(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cfg := Config{TimeZone: "UTC", Latitude: 0, Longitude: 0}

	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "mode", ControlType: storage.ControlTypeDiscrete, NumStates: 3,
	}); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	ts := time.Date(2020, 1, 6, 0, 1, 0, 0, time.UTC)
	input := TransitionInput{
		ControlID: "mode", ModelID: "default",
		FromState: 0, ToState: 2,
		TimestampMs: ts.UnixMilli(),
	}

	if err := IngestTransition(ctx, db, cfg, input); err != nil {
		t.Fatalf("ingest transition: %v", err)
	}

	key := storage.AggregateKey{ControlID: "mode", ModelID: "default", QuarterIndex: domain.QuarterIndexUTC(ts.UnixMilli())}
	data, err := storage.GetOrCreateAggregate(ctx, db, key, 3)
	if err != nil {
		t.Fatalf("get aggregate: %v", err)
	}

	b, _ := domain.NewBlob(3)
	copy(b.Data(), data)

	checkTransitionClock := func(clock int, bucket int, name string) {
		idx, err := domain.TransIndex(0, 2, clock, bucket, 3)
		if err != nil {
			t.Fatalf("%s transition index: %v", name, err)
		}
		v, err := b.GetU64(idx)
		if err != nil {
			t.Fatalf("%s get value: %v", name, err)
		}
		if v != 1 {
			t.Fatalf("%s transition count: got %d want 1", name, v)
		}
	}

	utcBucket, err := domain.BucketAtUTC(input.TimestampMs)
	if err != nil {
		t.Fatalf("bucket at UTC: %v", err)
	}
	checkTransitionClock(domain.ClockUTC, utcBucket, "UTC")

	localBucket, err := domain.BucketAtLocal(input.TimestampMs, time.UTC)
	if err != nil {
		t.Fatalf("bucket at local: %v", err)
	}
	checkTransitionClock(domain.ClockLocal, localBucket, "Local")

	meanSolarBucket, err := domain.BucketAtMeanSolar(input.TimestampMs, cfg.Latitude, cfg.Longitude)
	if err != nil {
		t.Fatalf("bucket at mean solar: %v", err)
	}
	checkTransitionClock(domain.ClockMeanSolar, meanSolarBucket, "MeanSolar")

	apparentSolarBucket, err := domain.BucketAtApparentSolar(input.TimestampMs, cfg.Latitude, cfg.Longitude)
	if err != nil {
		t.Fatalf("bucket at apparent solar: %v", err)
	}
	checkTransitionClock(domain.ClockApparentSolar, apparentSolarBucket, "ApparentSolar")

	unequalHoursBucket, err := domain.BucketAtUnequalHours(input.TimestampMs, cfg.Latitude, cfg.Longitude)
	if err != nil {
		t.Fatalf("bucket at unequal hours: %v", err)
	}
	checkTransitionClock(domain.ClockUnequalHours, unequalHoursBucket, "UnequalHours")

	otherIdx, _ := domain.TransIndex(0, 1, domain.ClockUTC, 0, 3)
	v, _ := b.GetU64(otherIdx)
	if v != 0 {
		t.Fatalf("other transition should be 0, got %d", v)
	}
}

func TestIngestValidationErrors(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cfg := Config{TimeZone: "UTC", Latitude: 0, Longitude: 0}

	err := IngestHolding(ctx, db, cfg, HoldingInput{ControlID: "missing", ModelID: "m", State: 0, StartTimeMs: 1000, EndTimeMs: 2000})
	if !IsValidationError(err) {
		t.Fatalf("expected validation error for unknown control, got %v", err)
	}

	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "c", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	err = IngestHolding(ctx, db, cfg, HoldingInput{ControlID: "c", ModelID: "m", State: 5, StartTimeMs: 1000, EndTimeMs: 2000})
	if !IsValidationError(err) {
		t.Fatalf("expected validation error for out-of-range state, got %v", err)
	}

	err = IngestTransition(ctx, db, cfg, TransitionInput{ControlID: "c", ModelID: "m", FromState: 0, ToState: 0, TimestampMs: 1000})
	if !IsValidationError(err) {
		t.Fatalf("expected validation error for self-transition, got %v", err)
	}
}

func TestApplyHoldingClockSpansSaturatesAtMaxUint64(t *testing.T) {
	b, err := domain.NewBlob(2)
	if err != nil {
		t.Fatalf("new blob: %v", err)
	}

	idx, err := domain.HoldIndex(1, domain.ClockUTC, 0, 2)
	if err != nil {
		t.Fatalf("hold index: %v", err)
	}
	if err := b.SetU64(idx, math.MaxUint64-10); err != nil {
		t.Fatalf("seed value: %v", err)
	}

	err = applyHoldingClockSpans(b, 2, 1, domain.ClockUTC, []domain.BucketSpan{{Bucket: 0, Millis: 25}})
	if err != nil {
		t.Fatalf("apply holding spans: %v", err)
	}

	got, err := b.GetU64(idx)
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if got != math.MaxUint64 {
		t.Fatalf("expected saturation at MaxUint64, got %d", got)
	}
}

func TestIncrementTransitionCountSaturatesAtMaxUint64(t *testing.T) {
	b, err := domain.NewBlob(2)
	if err != nil {
		t.Fatalf("new blob: %v", err)
	}

	idx, err := domain.TransIndex(0, 1, domain.ClockUTC, 0, 2)
	if err != nil {
		t.Fatalf("trans index: %v", err)
	}
	if err := b.SetU64(idx, math.MaxUint64); err != nil {
		t.Fatalf("seed value: %v", err)
	}

	if err := incrementTransitionCount(b, 0, 1, 2, domain.ClockUTC, 0); err != nil {
		t.Fatalf("increment transition count: %v", err)
	}

	got, err := b.GetU64(idx)
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if got != math.MaxUint64 {
		t.Fatalf("expected saturation at MaxUint64, got %d", got)
	}
}
