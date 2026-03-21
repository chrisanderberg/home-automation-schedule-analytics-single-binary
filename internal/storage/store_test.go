package storage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
	"home-automation-schedule-analytics-single-bin/internal/domain/control"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func newStore(t *testing.T) *storage.Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := storage.NewFromDB(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestStoreControlLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newStore(t)
	now := time.Date(2026, time.March, 20, 10, 0, 0, 0, time.UTC)

	err := store.UpsertControl(ctx, control.Control{
		ID:        "lamp",
		Type:      control.TypeDiscrete,
		NumStates: 3,
	}, now)
	if err != nil {
		t.Fatalf("UpsertControl() error = %v", err)
	}

	got, err := store.GetControl(ctx, "lamp")
	if err != nil {
		t.Fatalf("GetControl() error = %v", err)
	}
	if got.NumStates != 3 || got.Type != control.TypeDiscrete {
		t.Fatalf("GetControl() = %+v", got)
	}

	controls, err := store.ListControls(ctx)
	if err != nil {
		t.Fatalf("ListControls() error = %v", err)
	}
	if len(controls) != 1 {
		t.Fatalf("len(ListControls()) = %d, want 1", len(controls))
	}
}

func TestStoreAggregateRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newStore(t)
	now := time.Date(2026, time.March, 20, 11, 0, 0, 0, time.UTC)
	if err := store.UpsertControl(ctx, control.Control{
		ID:        "lamp",
		Type:      control.TypeDiscrete,
		NumStates: 2,
	}, now); err != nil {
		t.Fatalf("UpsertControl() error = %v", err)
	}

	acc, err := blob.NewAccumulator(2)
	if err != nil {
		t.Fatalf("NewAccumulator() error = %v", err)
	}
	if err := acc.AddHolding(1, 0, 10, 2500); err != nil {
		t.Fatalf("AddHolding() error = %v", err)
	}

	record := storage.AggregateRecord{
		ControlID:    "lamp",
		QuarterIndex: 220,
		NumStates:    2,
		Data:         acc.Bytes(),
		UpdatedAtMs:  now.UnixMilli(),
	}
	if err := store.UpsertAggregate(ctx, record); err != nil {
		t.Fatalf("UpsertAggregate() error = %v", err)
	}

	got, err := store.GetAggregate(ctx, "lamp", 220)
	if err != nil {
		t.Fatalf("GetAggregate() error = %v", err)
	}
	copyAcc, err := blob.FromBytes(got.NumStates, got.Data)
	if err != nil {
		t.Fatalf("FromBytes() error = %v", err)
	}
	value, err := copyAcc.Holding(1, 0, 10)
	if err != nil {
		t.Fatalf("Holding() error = %v", err)
	}
	if value != 2500 {
		t.Fatalf("Holding() = %d, want 2500", value)
	}
}

func TestStoreSnapshots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newStore(t)
	now := time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)
	snapshotPath := filepath.Join(t.TempDir(), "q1.sqlite")

	record, err := store.CreateSnapshot(ctx, "q1", snapshotPath, now)
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if record.ID == 0 {
		t.Fatal("CreateSnapshot() returned empty id")
	}

	items, err := store.ListSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "q1" {
		t.Fatalf("ListSnapshots() = %+v", items)
	}
}
