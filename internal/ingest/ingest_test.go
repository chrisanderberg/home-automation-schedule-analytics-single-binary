package ingest

import (
	"context"
	"database/sql"
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
	cfg := Config{TimeZone: "UTC", Latitude: 0, Longitude: 0}

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

	utcIdx, _ := domain.HoldIndex(1, domain.ClockUTC, 0, 3)
	v, _ := b.GetU64(utcIdx)
	if v != uint64(expectedMs) {
		t.Fatalf("UTC holding: got %d want %d", v, expectedMs)
	}

	localIdx, _ := domain.HoldIndex(1, domain.ClockLocal, 0, 3)
	v, _ = b.GetU64(localIdx)
	if v != uint64(expectedMs) {
		t.Fatalf("Local holding: got %d want %d", v, expectedMs)
	}

	otherIdx, _ := domain.HoldIndex(0, domain.ClockUTC, 0, 3)
	v, _ = b.GetU64(otherIdx)
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

	utcIdx, _ := domain.TransIndex(0, 2, domain.ClockUTC, 0, 3)
	v, _ := b.GetU64(utcIdx)
	if v != 1 {
		t.Fatalf("UTC transition count: got %d want 1", v)
	}

	localIdx, _ := domain.TransIndex(0, 2, domain.ClockLocal, 0, 3)
	v, _ = b.GetU64(localIdx)
	if v != 1 {
		t.Fatalf("Local transition count: got %d want 1", v)
	}

	otherIdx, _ := domain.TransIndex(0, 1, domain.ClockUTC, 0, 3)
	v, _ = b.GetU64(otherIdx)
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
