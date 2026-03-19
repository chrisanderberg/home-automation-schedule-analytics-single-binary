package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"home-automation-schedule-analytics-single-bin/internal/domain"
)

var ErrNotFound = errors.New("not found")

func Open(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	return db, nil
}

func InitSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, Schema)
	return err
}

func UpsertControl(ctx context.Context, db *sql.DB, control Control) error {
	labels, err := encodeLabels(control.StateLabels)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO controls (control_id, control_type, num_states, state_labels)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(control_id) DO UPDATE SET
		   control_type=excluded.control_type,
		   num_states=excluded.num_states,
		   state_labels=excluded.state_labels`,
		control.ControlID,
		string(control.ControlType),
		control.NumStates,
		labels,
	)
	return err
}

func GetControl(ctx context.Context, db *sql.DB, controlID string) (Control, error) {
	row := db.QueryRowContext(ctx, `SELECT control_id, control_type, num_states, state_labels FROM controls WHERE control_id = ?`, controlID)
	var control Control
	var controlType string
	var labels sql.NullString
	if err := row.Scan(&control.ControlID, &controlType, &control.NumStates, &labels); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Control{}, ErrNotFound
		}
		return Control{}, err
	}
	control.ControlType = ControlType(controlType)
	if labels.Valid && labels.String != "" {
		decoded, err := decodeLabels(labels.String)
		if err != nil {
			return Control{}, err
		}
		control.StateLabels = decoded
	}
	return control, nil
}

func ListControls(ctx context.Context, db *sql.DB) ([]Control, error) {
	rows, err := db.QueryContext(ctx, `SELECT control_id, control_type, num_states, state_labels FROM controls ORDER BY control_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var controls []Control
	for rows.Next() {
		var c Control
		var controlType string
		var labels sql.NullString
		if err := rows.Scan(&c.ControlID, &controlType, &c.NumStates, &labels); err != nil {
			return nil, err
		}
		c.ControlType = ControlType(controlType)
		if labels.Valid && labels.String != "" {
			decoded, err := decodeLabels(labels.String)
			if err != nil {
				return nil, err
			}
			c.StateLabels = decoded
		}
		controls = append(controls, c)
	}
	return controls, rows.Err()
}

func ListAggregateKeys(ctx context.Context, db *sql.DB, controlID string) ([]AggregateKey, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT control_id, model_id, quarter_index FROM aggregates WHERE control_id = ? ORDER BY model_id, quarter_index`,
		controlID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []AggregateKey
	for rows.Next() {
		var k AggregateKey
		if err := rows.Scan(&k.ControlID, &k.ModelID, &k.QuarterIndex); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func validateAggregateBlobSize(key AggregateKey, numStates int, blobBytes []byte) error {
	expectedLen := numStates * numStates * domain.GroupSize * 8
	if len(blobBytes) != expectedLen {
		return fmt.Errorf(
			"aggregate blob size mismatch for control_id=%q model_id=%q quarter_index=%d: got %d bytes, expected %d",
			key.ControlID, key.ModelID, key.QuarterIndex, len(blobBytes), expectedLen,
		)
	}
	return nil
}

func GetOrCreateAggregate(ctx context.Context, db *sql.DB, key AggregateKey, numStates int) ([]byte, error) {
	row := db.QueryRowContext(
		ctx,
		`SELECT blob FROM aggregates WHERE control_id = ? AND model_id = ? AND quarter_index = ?`,
		key.ControlID, key.ModelID, key.QuarterIndex,
	)
	var blobBytes []byte
	switch err := row.Scan(&blobBytes); err {
	case nil:
		if err := validateAggregateBlobSize(key, numStates, blobBytes); err != nil {
			return nil, err
		}
		return blobBytes, nil
	case sql.ErrNoRows:
		b, err := domain.NewBlob(numStates)
		if err != nil {
			return nil, err
		}
		blobBytes = b.Data()
		_, err = db.ExecContext(
			ctx,
			`INSERT INTO aggregates (control_id, model_id, quarter_index, blob)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(control_id, model_id, quarter_index) DO NOTHING`,
			key.ControlID, key.ModelID, key.QuarterIndex, blobBytes,
		)
		if err != nil {
			return nil, err
		}
		row = db.QueryRowContext(
			ctx,
			`SELECT blob FROM aggregates WHERE control_id = ? AND model_id = ? AND quarter_index = ?`,
			key.ControlID, key.ModelID, key.QuarterIndex,
		)
		if err := row.Scan(&blobBytes); err != nil {
			return nil, err
		}
		if err := validateAggregateBlobSize(key, numStates, blobBytes); err != nil {
			return nil, err
		}
		return blobBytes, nil
	default:
		return nil, err
	}
}

func GetAggregate(ctx context.Context, db *sql.DB, key AggregateKey) ([]byte, error) {
	row := db.QueryRowContext(
		ctx,
		`SELECT blob FROM aggregates WHERE control_id = ? AND model_id = ? AND quarter_index = ?`,
		key.ControlID, key.ModelID, key.QuarterIndex,
	)
	var blobBytes []byte
	if err := row.Scan(&blobBytes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return blobBytes, nil
}

func UpdateAggregate(ctx context.Context, db *sql.DB, key AggregateKey, numStates int, update func([]byte) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	if err := updateAggregateWithQueryExec(ctx, conn, conn, key, numStates, update); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type execContexter interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func updateAggregateWithQueryExec(
	ctx context.Context,
	queryDB queryRower,
	execDB execContexter,
	key AggregateKey,
	numStates int,
	update func([]byte) error,
) error {
	row := queryDB.QueryRowContext(
		ctx,
		`SELECT blob FROM aggregates WHERE control_id = ? AND model_id = ? AND quarter_index = ?`,
		key.ControlID, key.ModelID, key.QuarterIndex,
	)
	var blobBytes []byte
	switch err := row.Scan(&blobBytes); err {
	case nil:
		if err := validateAggregateBlobSize(key, numStates, blobBytes); err != nil {
			return err
		}
	case sql.ErrNoRows:
		b, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		blobBytes = b.Data()
	default:
		return err
	}

	working := make([]byte, len(blobBytes))
	copy(working, blobBytes)
	if err := update(working); err != nil {
		return err
	}

	_, err := execDB.ExecContext(
		ctx,
		`INSERT INTO aggregates (control_id, model_id, quarter_index, blob)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(control_id, model_id, quarter_index) DO UPDATE SET blob=excluded.blob`,
		key.ControlID, key.ModelID, key.QuarterIndex, working,
	)
	return err
}

func encodeLabels(labels []string) (string, error) {
	if len(labels) == 0 {
		return "", nil
	}
	data, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeLabels(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var labels []string
	if err := json.Unmarshal([]byte(raw), &labels); err != nil {
		return nil, err
	}
	return labels, nil
}
