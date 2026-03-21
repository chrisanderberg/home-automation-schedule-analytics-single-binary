package snapshot

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
	"home-automation-schedule-analytics-single-bin/internal/domain/control"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func TestExporterCreatesStandaloneSQLiteSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	store := storage.NewFromDB(db)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, time.March, 20, 15, 30, 0, 0, time.UTC)
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
	if err := acc.AddHolding(1, 0, 10, 5000); err != nil {
		t.Fatalf("AddHolding() error = %v", err)
	}
	if err := store.UpsertAggregate(ctx, storage.AggregateRecord{
		ControlID:    "lamp",
		QuarterIndex: 224,
		NumStates:    2,
		Data:         acc.Bytes(),
		UpdatedAtMs:  now.UnixMilli(),
	}); err != nil {
		t.Fatalf("UpsertAggregate() error = %v", err)
	}

	exporter := NewExporterWithDir(store, t.TempDir())
	record, err := exporter.Export(ctx, "Q1 Report")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if filepath.Ext(record.Path) != ".sqlite" {
		t.Fatalf("snapshot path = %q, want .sqlite file", record.Path)
	}

	snapshotStore, err := storage.Open(record.Path)
	if err != nil {
		t.Fatalf("storage.Open(snapshot) error = %v", err)
	}
	t.Cleanup(func() { _ = snapshotStore.Close() })

	controlRecord, err := snapshotStore.GetControl(ctx, "lamp")
	if err != nil {
		t.Fatalf("GetControl(snapshot) error = %v", err)
	}
	if controlRecord.NumStates != 2 {
		t.Fatalf("snapshot control = %+v", controlRecord)
	}
	aggregateRecord, err := snapshotStore.GetAggregate(ctx, "lamp", 224)
	if err != nil {
		t.Fatalf("GetAggregate(snapshot) error = %v", err)
	}
	copyAcc, err := blob.FromBytes(aggregateRecord.NumStates, aggregateRecord.Data)
	if err != nil {
		t.Fatalf("FromBytes(snapshot) error = %v", err)
	}
	value, err := copyAcc.Holding(1, 0, 10)
	if err != nil {
		t.Fatalf("Holding(snapshot) error = %v", err)
	}
	if value != 5000 {
		t.Fatalf("Holding(snapshot) = %d, want 5000", value)
	}

	snapshots, err := store.ListSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Name != "Q1 Report" {
		t.Fatalf("ListSnapshots() = %+v", snapshots)
	}
}

func TestExporterRejectsBlankName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	store := storage.NewFromDB(db)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	exporter := NewExporterWithDir(store, t.TempDir())
	_, err = exporter.Export(ctx, "   ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Export() error = %v, want validation error", err)
	}
}

func TestExporterAvoidsFilenameCollisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	store := storage.NewFromDB(db)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	dir := t.TempDir()
	firstPath := filepath.Join(dir, "20260320T153000.000Z-report.sqlite")
	if err := os.WriteFile(firstPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, reserved, err := reserveNextSnapshotPath(dir, "report", time.Date(2026, time.March, 20, 15, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("reserveNextSnapshotPath() error = %v", err)
	}
	if err := reserved.Close(); err != nil {
		t.Fatalf("reserved.Close() error = %v", err)
	}
	if got == firstPath {
		t.Fatalf("collision path reused: %s", got)
	}
}
