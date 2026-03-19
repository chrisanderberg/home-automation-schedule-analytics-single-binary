package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/storage"
)

var locationCache sync.Map

func resolveControlAndLocation(ctx context.Context, db *sql.DB, cfg Config, controlID string) (storage.Control, *time.Location, error) {
	control, err := storage.GetControl(ctx, db, controlID)
	if err != nil {
		return storage.Control{}, nil, fmt.Errorf("get control %q: %w", controlID, err)
	}

	loc, err := loadLocation(cfg.TimeZone)
	if err != nil {
		return storage.Control{}, nil, fmt.Errorf("load location %q: %w", cfg.TimeZone, err)
	}

	return control, loc, nil
}

func loadLocation(name string) (*time.Location, error) {
	if cached, ok := locationCache.Load(name); ok {
		return cached.(*time.Location), nil
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	actual, _ := locationCache.LoadOrStore(name, loc)
	return actual.(*time.Location), nil
}
