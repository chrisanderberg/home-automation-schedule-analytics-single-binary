package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func TestExportCreatesConsistentCopy(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := storage.InitSchema(ctx, db); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	if err := storage.UpsertControl(ctx, db, storage.Control{
		ControlID: "c1", ControlType: storage.ControlTypeDiscrete, NumStates: 2,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	dir := t.TempDir()
	snapDir := filepath.Join(dir, "snapshots")

	path, err := Export(ctx, db, snapDir)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
	if name := filepath.Base(path); !strings.HasPrefix(name, "snapshot-") || !strings.HasSuffix(name, ".sqlite") {
		t.Fatalf("unexpected snapshot filename %q", name)
	}

	snapDB, err := storage.Open(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer snapDB.Close()

	control, err := storage.GetControl(ctx, snapDB, "c1")
	if err != nil {
		t.Fatalf("get control from snapshot: %v", err)
	}
	if control.NumStates != 2 {
		t.Fatalf("expected 2 states, got %d", control.NumStates)
	}
}

func TestExportGeneratesUniquePaths(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := storage.InitSchema(ctx, db); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	snapDir := t.TempDir()
	path1, err := Export(ctx, db, snapDir)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	path2, err := Export(ctx, db, snapDir)
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if path1 == path2 {
		t.Fatalf("expected unique snapshot paths, got %q", path1)
	}
}

func TestListSnapshotsOrder(t *testing.T) {
	dir := t.TempDir()

	oldPath := filepath.Join(dir, "snapshot-20260101-000000.sqlite")
	newPath := filepath.Join(dir, "snapshot-20260102-000000.sqlite")
	for _, path := range []string{oldPath, newPath} {
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		f.Close()
	}

	base := time.Now().UTC().Add(-time.Hour)
	if err := os.Chtimes(oldPath, base, base); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	infos, err := ListSnapshots(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2, got %d", len(infos))
	}
	if infos[0].Name != "snapshot-20260102-000000.sqlite" {
		t.Fatalf("expected newest first, got %s", infos[0].Name)
	}
}

func TestListSnapshotsMissingDir(t *testing.T) {
	infos, err := ListSnapshots("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("expected empty list, got %d", len(infos))
	}
}
