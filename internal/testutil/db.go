package testutil

import (
	"context"
	"database/sql"
	"testing"
)

// OpenTestDB provisions an in-memory database with schema initialized for tests.
func OpenTestDB(
	t *testing.T,
	open func(string) (*sql.DB, error),
	initSchema func(context.Context, *sql.DB) error,
) *sql.DB {
	t.Helper()

	db, err := open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := initSchema(context.Background(), db); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
