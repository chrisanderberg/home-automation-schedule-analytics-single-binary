package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"home-automation-schedule-analytics-single-bin/internal/config"
	"home-automation-schedule-analytics-single-bin/internal/demodata"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

// main seeds a demo database from the command line.
func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

// run opens the database, verifies it is empty, and writes the demo dataset.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := storage.InitSchema(ctx, db); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	if err := ensureEmptyDatabase(ctx, db); err != nil {
		return err
	}

	seedCfg := ingest.Config{
		TimeZone:  cfg.TimeZone,
		Latitude:  cfg.Latitude,
		Longitude: cfg.Longitude,
	}
	if err := demodata.SeedDemoData(ctx, db, seedCfg); err != nil {
		return err
	}

	log.Printf("seeded demo data into %s", cfg.DBPath)
	return nil
}

// ensureEmptyDatabase rejects seeding into a database that already has controls.
func ensureEmptyDatabase(ctx context.Context, db *sql.DB) error {
	tables := []string{"controls", "models", "aggregates"}
	counts := make(map[string]int, len(tables))
	for _, table := range tables {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("count %s: %w", table, err)
		}
		counts[table] = count
	}
	if counts["controls"] == 0 && counts["models"] == 0 && counts["aggregates"] == 0 {
		return nil
	}
	return fmt.Errorf(
		"refusing to seed non-empty database %q (controls=%d models=%d aggregates=%d); use a fresh HAA_DB_PATH",
		dbPath(db),
		counts["controls"],
		counts["models"],
		counts["aggregates"],
	)
}

// dbPath returns the SQLite filename backing the opened database handle.
func dbPath(db *sql.DB) string {
	var path string
	err := db.QueryRow("PRAGMA database_list").Scan(new(int), new(string), &path)
	if err != nil {
		return "<unknown>"
	}
	return path
}
