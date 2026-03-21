package ingest_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
	"home-automation-schedule-analytics-single-bin/internal/domain/bucketing"
	"home-automation-schedule-analytics-single-bin/internal/domain/control"
	"home-automation-schedule-analytics-single-bin/internal/domain/quarter"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func newService(t *testing.T) (*ingest.Service, *storage.Store) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	store := storage.NewFromDB(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	engine, err := bucketing.New(bucketing.Config{
		Location:  loc,
		Latitude:  59.33,
		Longitude: 18.07,
	})
	if err != nil {
		t.Fatalf("bucketing.New() error = %v", err)
	}
	svc := ingest.NewService(store, engine)
	t.Cleanup(func() { _ = store.Close() })
	return svc, store
}

func TestIngestHoldingSplitsAcrossQuarterBoundary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, store := newService(t)
	if err := svc.RegisterControl(ctx, control.Control{
		ID:        "lamp",
		Type:      control.TypeDiscrete,
		NumStates: 3,
	}); err != nil {
		t.Fatalf("RegisterControl() error = %v", err)
	}

	start := time.Date(2026, time.March, 31, 23, 55, 0, 0, time.UTC)
	end := time.Date(2026, time.April, 1, 0, 5, 0, 0, time.UTC)
	if err := svc.IngestHolding(ctx, ingest.HoldingInterval{
		ControlID:   "lamp",
		State:       2,
		StartTimeMs: start.UnixMilli(),
		EndTimeMs:   end.UnixMilli(),
	}); err != nil {
		t.Fatalf("IngestHolding() error = %v", err)
	}

	for _, ts := range []time.Time{start, end.Add(-time.Millisecond)} {
		record, err := store.GetAggregate(ctx, "lamp", quarter.Index(ts))
		if err != nil {
			t.Fatalf("GetAggregate() error = %v", err)
		}
		acc, err := blob.FromBytes(record.NumStates, record.Data)
		if err != nil {
			t.Fatalf("FromBytes() error = %v", err)
		}
		bucket, err := svcTransitionBucket(bucketing.ClockUTC, ts)
		if err != nil {
			t.Fatalf("svcTransitionBucket() error = %v", err)
		}
		total, err := acc.Holding(2, int(bucketing.ClockUTC), bucket)
		if err != nil {
			t.Fatalf("Holding() error = %v", err)
		}
		if total != 300000 {
			t.Fatalf("quarter %d Holding() = %d, want 300000", quarter.Index(ts), total)
		}
	}
}

func TestIngestTransitionCountsAllClocks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, store := newService(t)
	if err := svc.RegisterControl(ctx, control.Control{
		ID:        "lamp",
		Type:      control.TypeDiscrete,
		NumStates: 2,
	}); err != nil {
		t.Fatalf("RegisterControl() error = %v", err)
	}

	ts := time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)
	if err := svc.IngestTransition(ctx, ingest.Transition{
		ControlID:   "lamp",
		FromState:   0,
		ToState:     1,
		TimestampMs: ts.UnixMilli(),
	}); err != nil {
		t.Fatalf("IngestTransition() error = %v", err)
	}

	record, err := store.GetAggregate(ctx, "lamp", quarter.Index(ts))
	if err != nil {
		t.Fatalf("GetAggregate() error = %v", err)
	}
	acc, err := blob.FromBytes(record.NumStates, record.Data)
	if err != nil {
		t.Fatalf("FromBytes() error = %v", err)
	}
	for _, clock := range []bucketing.Clock{
		bucketing.ClockUTC,
		bucketing.ClockLocal,
		bucketing.ClockMeanSolar,
		bucketing.ClockApparentSolar,
		bucketing.ClockUnequalHours,
	} {
		bucket, err := svcTransitionBucket(clock, ts)
		if err != nil {
			t.Fatalf("svcTransitionBucket() error = %v", err)
		}
		count, err := acc.Transition(0, 1, int(clock), bucket)
		if err != nil {
			t.Fatalf("Transition() error = %v", err)
		}
		if count != 1 {
			t.Fatalf("clock %d Transition() = %d, want 1", clock, count)
		}
	}
}

func TestIngestRejectsSelfTransition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _ := newService(t)
	if err := svc.RegisterControl(ctx, control.Control{
		ID:        "lamp",
		Type:      control.TypeDiscrete,
		NumStates: 2,
	}); err != nil {
		t.Fatalf("RegisterControl() error = %v", err)
	}
	err := svc.IngestTransition(ctx, ingest.Transition{
		ControlID:   "lamp",
		FromState:   1,
		ToState:     1,
		TimestampMs: time.Now().UnixMilli(),
	})
	if !errors.Is(err, ingest.ErrValidation) {
		t.Fatalf("IngestTransition() error = %v, want validation error", err)
	}
}

func svcTransitionBucket(clock bucketing.Clock, ts time.Time) (int, error) {
	loc, _ := time.LoadLocation("UTC")
	engine, err := bucketing.New(bucketing.Config{
		Location:  loc,
		Latitude:  59.33,
		Longitude: 18.07,
	})
	if err != nil {
		return 0, err
	}
	return engine.TransitionBucket(clock, ts)
}
