package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/storage"
)

var snapshotNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

var ErrValidation = errors.New("validation error")

type Exporter struct {
	store       *storage.Store
	snapshotDir string
	now         func() time.Time
}

func NewExporter(store *storage.Store, dbPath string) *Exporter {
	baseDir := filepath.Join(filepath.Dir(dbPath), "snapshots")
	if filepath.Dir(dbPath) == "." {
		baseDir = filepath.Join("data", "snapshots")
	}
	return &Exporter{
		store:       store,
		snapshotDir: baseDir,
		now:         time.Now,
	}
}

func NewExporterWithDir(store *storage.Store, snapshotDir string) *Exporter {
	return &Exporter{
		store:       store,
		snapshotDir: snapshotDir,
		now:         time.Now,
	}
}

func (e *Exporter) Export(ctx context.Context, name string) (storage.SnapshotRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return storage.SnapshotRecord{}, fmt.Errorf("%w: snapshot name is required", ErrValidation)
	}
	if err := os.MkdirAll(e.snapshotDir, 0o755); err != nil {
		return storage.SnapshotRecord{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	path, reserved, err := reserveNextSnapshotPath(e.snapshotDir, name, e.now())
	if err != nil {
		return storage.SnapshotRecord{}, err
	}
	if err := reserved.Close(); err != nil {
		_ = os.Remove(path)
		return storage.SnapshotRecord{}, fmt.Errorf("close reserved snapshot file: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(path)
		}
	}()
	snapshotStore, err := storage.Open(path)
	if err != nil {
		return storage.SnapshotRecord{}, fmt.Errorf("open snapshot db: %w", err)
	}
	defer snapshotStore.Close()

	controls, err := e.store.ListControls(ctx)
	if err != nil {
		return storage.SnapshotRecord{}, err
	}
	for _, item := range controls {
		if err := snapshotStore.UpsertControl(ctx, item.Control, e.now()); err != nil {
			return storage.SnapshotRecord{}, err
		}
	}

	aggregates, err := e.store.ListAllAggregates(ctx)
	if err != nil {
		return storage.SnapshotRecord{}, err
	}
	for _, item := range aggregates {
		if err := snapshotStore.UpsertAggregate(ctx, item); err != nil {
			return storage.SnapshotRecord{}, err
		}
	}

	record, err := e.store.CreateSnapshot(ctx, name, path, e.now())
	if err != nil {
		return storage.SnapshotRecord{}, err
	}
	success = true
	return record, nil
}

func snapshotFileName(name string, now time.Time) string {
	sanitized := snapshotNameSanitizer.ReplaceAllString(name, "-")
	sanitized = strings.Trim(sanitized, "-")
	if sanitized == "" {
		sanitized = "snapshot"
	}
	return fmt.Sprintf("%s-%s.sqlite", now.UTC().Format("20060102T150405.000Z"), sanitized)
}

func reserveNextSnapshotPath(snapshotDir, name string, now time.Time) (string, *os.File, error) {
	base := filepath.Join(snapshotDir, snapshotFileName(name, now))
	if file, err := reserveSnapshotFile(base); err == nil {
		return base, file, nil
	} else if !errors.Is(err, os.ErrExist) {
		return "", nil, err
	}

	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		candidate := prefix + "-" + strconv.Itoa(i) + ext
		if file, err := reserveSnapshotFile(candidate); err == nil {
			return candidate, file, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("unable to allocate unique snapshot filename")
}

func reserveSnapshotFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, err
		}
		return nil, fmt.Errorf("reserve snapshot path: %w", err)
	}
	return file, nil
}
