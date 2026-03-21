package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
	"home-automation-schedule-analytics-single-bin/internal/domain/control"
)

var ErrNotFound = errors.New("not found")

type AggregateRecord struct {
	ControlID    string
	QuarterIndex int
	NumStates    int
	Data         []byte
	UpdatedAtMs  int64
}

type SnapshotRecord struct {
	ID          int64
	Name        string
	Path        string
	CreatedAtMs int64
}

type ControlSummary struct {
	Control       control.Control
	QuarterCount  int
	LastUpdatedMs int64
}

type Store struct {
	db *sql.DB
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &Store{db: db}
	if err := store.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func NewFromDB(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Init(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS controls (
			control_id TEXT PRIMARY KEY,
			control_type TEXT NOT NULL,
			num_states INTEGER NOT NULL,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS aggregates (
			control_id TEXT NOT NULL,
			quarter_index INTEGER NOT NULL,
			num_states INTEGER NOT NULL,
			data BLOB NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			PRIMARY KEY (control_id, quarter_index),
			FOREIGN KEY (control_id) REFERENCES controls(control_id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertControl(ctx context.Context, c control.Control, now time.Time) error {
	if err := c.Validate(); err != nil {
		return err
	}
	nowMs := now.UTC().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO controls(control_id, control_type, num_states, created_at_ms, updated_at_ms)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(control_id) DO UPDATE SET
			control_type = excluded.control_type,
			num_states = excluded.num_states,
			updated_at_ms = excluded.updated_at_ms
	`, c.ID, string(c.Type), c.NumStates, nowMs, nowMs)
	if err != nil {
		return fmt.Errorf("upsert control: %w", err)
	}
	return nil
}

func (s *Store) GetControl(ctx context.Context, controlID string) (control.Control, error) {
	var c control.Control
	var kind string
	err := s.db.QueryRowContext(ctx, `
		SELECT control_id, control_type, num_states
		FROM controls
		WHERE control_id = ?
	`, controlID).Scan(&c.ID, &kind, &c.NumStates)
	if errors.Is(err, sql.ErrNoRows) {
		return control.Control{}, ErrNotFound
	}
	if err != nil {
		return control.Control{}, fmt.Errorf("get control: %w", err)
	}
	c.Type = control.Type(kind)
	return c, nil
}

func (s *Store) ListControls(ctx context.Context) ([]ControlSummary, error) {
	return listControls(ctx, s.db)
}

func listControls(ctx context.Context, q queryer) ([]ControlSummary, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT
			c.control_id,
			c.control_type,
			c.num_states,
			COUNT(a.quarter_index) AS quarter_count,
			COALESCE(MAX(a.updated_at_ms), c.updated_at_ms) AS last_updated_ms
		FROM controls c
		LEFT JOIN aggregates a ON a.control_id = c.control_id
		GROUP BY c.control_id, c.control_type, c.num_states, c.updated_at_ms
		ORDER BY c.control_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list controls: %w", err)
	}
	defer rows.Close()

	var items []ControlSummary
	for rows.Next() {
		var item ControlSummary
		var kind string
		if err := rows.Scan(&item.Control.ID, &kind, &item.Control.NumStates, &item.QuarterCount, &item.LastUpdatedMs); err != nil {
			return nil, fmt.Errorf("scan control: %w", err)
		}
		item.Control.Type = control.Type(kind)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate controls: %w", err)
	}
	return items, nil
}

func (s *Store) UpsertAggregate(ctx context.Context, record AggregateRecord) error {
	layout, err := blob.NewLayout(record.NumStates)
	if err != nil {
		return err
	}
	if len(record.Data) != layout.ByteSize() {
		return fmt.Errorf("aggregate blob size mismatch")
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO aggregates(control_id, quarter_index, num_states, data, updated_at_ms)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(control_id, quarter_index) DO UPDATE SET
			num_states = excluded.num_states,
			data = excluded.data,
			updated_at_ms = excluded.updated_at_ms
	`, record.ControlID, record.QuarterIndex, record.NumStates, record.Data, record.UpdatedAtMs)
	if err != nil {
		return fmt.Errorf("upsert aggregate: %w", err)
	}
	return nil
}

func (s *Store) ApplyAggregateDelta(ctx context.Context, controlID string, quarterIndex, numStates int, delta []byte, now time.Time) error {
	layout, err := blob.NewLayout(numStates)
	if err != nil {
		return err
	}
	if len(delta) != layout.ByteSize() {
		return fmt.Errorf("aggregate delta size mismatch")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var existingData []byte
	var existingStates int
	row := tx.QueryRowContext(ctx, `
		SELECT num_states, data
		FROM aggregates
		WHERE control_id = ? AND quarter_index = ?
	`, controlID, quarterIndex)
	switch scanErr := row.Scan(&existingStates, &existingData); {
	case errors.Is(scanErr, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `
			INSERT INTO aggregates(control_id, quarter_index, num_states, data, updated_at_ms)
			VALUES(?, ?, ?, ?, ?)
		`, controlID, quarterIndex, numStates, delta, now.UTC().UnixMilli())
		if err != nil {
			return fmt.Errorf("insert aggregate: %w", err)
		}
	case scanErr != nil:
		err = fmt.Errorf("load aggregate: %w", scanErr)
		return err
	default:
		if existingStates != numStates {
			err = fmt.Errorf("aggregate state count mismatch")
			return err
		}
		acc, mergeErr := blob.FromBytes(numStates, existingData)
		if mergeErr != nil {
			err = mergeErr
			return err
		}
		deltaAcc, mergeErr := blob.FromBytes(numStates, delta)
		if mergeErr != nil {
			err = mergeErr
			return err
		}
		if mergeErr := acc.Merge(deltaAcc); mergeErr != nil {
			err = mergeErr
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE aggregates
			SET data = ?, updated_at_ms = ?
			WHERE control_id = ? AND quarter_index = ?
		`, acc.Bytes(), now.UTC().UnixMilli(), controlID, quarterIndex)
		if err != nil {
			return fmt.Errorf("update aggregate: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit aggregate tx: %w", err)
	}
	return nil
}

func (s *Store) GetAggregate(ctx context.Context, controlID string, quarterIndex int) (AggregateRecord, error) {
	var record AggregateRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT control_id, quarter_index, num_states, data, updated_at_ms
		FROM aggregates
		WHERE control_id = ? AND quarter_index = ?
	`, controlID, quarterIndex).Scan(&record.ControlID, &record.QuarterIndex, &record.NumStates, &record.Data, &record.UpdatedAtMs)
	if errors.Is(err, sql.ErrNoRows) {
		return AggregateRecord{}, ErrNotFound
	}
	if err != nil {
		return AggregateRecord{}, fmt.Errorf("get aggregate: %w", err)
	}
	return record, nil
}

func (s *Store) ListAggregatesForControl(ctx context.Context, controlID string) ([]AggregateRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT control_id, quarter_index, num_states, data, updated_at_ms
		FROM aggregates
		WHERE control_id = ?
		ORDER BY quarter_index
	`, controlID)
	if err != nil {
		return nil, fmt.Errorf("list aggregates: %w", err)
	}
	defer rows.Close()

	var items []AggregateRecord
	for rows.Next() {
		var item AggregateRecord
		if err := rows.Scan(&item.ControlID, &item.QuarterIndex, &item.NumStates, &item.Data, &item.UpdatedAtMs); err != nil {
			return nil, fmt.Errorf("scan aggregate: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregates: %w", err)
	}
	return items, nil
}

func (s *Store) ListQuarterIndices(ctx context.Context, controlID string) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT quarter_index
		FROM aggregates
		WHERE control_id = ?
		ORDER BY quarter_index
	`, controlID)
	if err != nil {
		return nil, fmt.Errorf("list quarter indices: %w", err)
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var idx int
		if err := rows.Scan(&idx); err != nil {
			return nil, fmt.Errorf("scan quarter index: %w", err)
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

func (s *Store) ListAllAggregates(ctx context.Context) ([]AggregateRecord, error) {
	return listAllAggregates(ctx, s.db)
}

func listAllAggregates(ctx context.Context, q queryer) ([]AggregateRecord, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT control_id, quarter_index, num_states, data, updated_at_ms
		FROM aggregates
		ORDER BY control_id, quarter_index
	`)
	if err != nil {
		return nil, fmt.Errorf("list all aggregates: %w", err)
	}
	defer rows.Close()

	var items []AggregateRecord
	for rows.Next() {
		var item AggregateRecord
		if err := rows.Scan(&item.ControlID, &item.QuarterIndex, &item.NumStates, &item.Data, &item.UpdatedAtMs); err != nil {
			return nil, fmt.Errorf("scan aggregate: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ExportSnapshotData loads controls and aggregates from a single read transaction
// so callers can copy a point-in-time snapshot without mixing concurrent commits.
func (s *Store) ExportSnapshotData(ctx context.Context) ([]ControlSummary, []AggregateRecord, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin read tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	controls, err := listControls(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	aggregates, err := listAllAggregates(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit read tx: %w", err)
	}
	return controls, aggregates, nil
}

func (s *Store) CreateSnapshot(ctx context.Context, name, path string, now time.Time) (SnapshotRecord, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO snapshots(name, path, created_at_ms)
		VALUES(?, ?, ?)
	`, name, path, now.UTC().UnixMilli())
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("create snapshot: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("snapshot id: %w", err)
	}
	return SnapshotRecord{
		ID:          id,
		Name:        name,
		Path:        path,
		CreatedAtMs: now.UTC().UnixMilli(),
	}, nil
}

func (s *Store) ListSnapshots(ctx context.Context) ([]SnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, path, created_at_ms
		FROM snapshots
		ORDER BY created_at_ms DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()

	var items []SnapshotRecord
	for rows.Next() {
		var item SnapshotRecord
		if err := rows.Scan(&item.ID, &item.Name, &item.Path, &item.CreatedAtMs); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
