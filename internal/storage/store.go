package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	_ "modernc.org/sqlite"

	"home-automation-schedule-analytics-single-bin/internal/domain"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var ErrValidation = errors.New("validation error")
var ErrStructureLocked = errors.New("structure locked")
var ErrAggregateBlobSizeMismatch = errors.New("aggregate blob size mismatch")

// Open creates a SQLite handle configured for this repository's schema expectations.
func Open(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, err
	}
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	return db, nil
}

// InitSchema applies the full schema to an opened database.
func InitSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, Schema)
	return err
}

// UpsertControl creates or replaces the metadata row for one control definition.
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

// insertControl inserts a new control row and reports conflicts without updating existing rows.
func insertControl(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, control Control) error {
	labels, err := encodeLabels(control.StateLabels)
	if err != nil {
		return err
	}
	result, err := exec.ExecContext(
		ctx,
		`INSERT INTO controls (control_id, control_type, num_states, state_labels)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(control_id) DO NOTHING`,
		control.ControlID,
		string(control.ControlType),
		control.NumStates,
		labels,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

// SaveControl creates a new control or updates an existing control, including control-id renames.
func SaveControl(ctx context.Context, db *sql.DB, previousControlID string, control Control) error {
	if previousControlID == "" {
		return insertControl(ctx, db, control)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existing Control
	var existingType string
	if err := tx.QueryRowContext(ctx,
		`SELECT control_id, control_type, num_states, state_labels FROM controls WHERE control_id = ?`,
		previousControlID,
	).Scan(&existing.ControlID, &existingType, &existing.NumStates, new(sql.NullString)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	existing.ControlType = normalizeControlType(ControlType(existingType))

	var aggregateExists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM aggregates WHERE control_id = ? LIMIT 1`, previousControlID).Scan(&aggregateExists)
	switch {
	case err == nil:
		// Once aggregates exist, only label-like metadata can change without
		// invalidating the packed blob layout already stored for this control.
		if existing.NumStates != control.NumStates || existing.ControlType != control.ControlType {
			return ErrStructureLocked
		}
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	if previousControlID == control.ControlID {
		labels, err := encodeLabels(control.StateLabels)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE controls
			 SET control_type = ?, num_states = ?, state_labels = ?
			 WHERE control_id = ?`,
			string(control.ControlType),
			control.NumStates,
			labels,
			control.ControlID,
		); err != nil {
			return err
		}
	} else {
		// Control renames are implemented by creating the destination row first and
		// then moving aggregates and models so the change remains atomic.
		row := tx.QueryRowContext(ctx, `SELECT 1 FROM controls WHERE control_id = ?`, control.ControlID)
		var exists int
		switch err := row.Scan(&exists); {
		case err == nil:
			return ErrConflict
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}

		if err := insertControl(ctx, tx, control); err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE aggregates SET control_id = ? WHERE control_id = ?`,
			control.ControlID,
			previousControlID,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE models SET control_id = ? WHERE control_id = ?`,
			control.ControlID,
			previousControlID,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM controls WHERE control_id = ?`, previousControlID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// GetControl loads one control definition by identifier.
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
	control.ControlType = normalizeControlType(ControlType(controlType))
	if labels.Valid && labels.String != "" {
		decoded, err := decodeLabels(labels.String)
		if err != nil {
			return Control{}, err
		}
		control.StateLabels = decoded
	}
	return control, nil
}

// ListControls returns all control definitions sorted by control id.
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
		c.ControlType = normalizeControlType(ControlType(controlType))
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

// normalizeControlType maps legacy stored values onto the canonical control-type set.
func normalizeControlType(controlType ControlType) ControlType {
	normalized := strings.TrimSpace(string(controlType))
	switch normalized {
	case "discrete", string(ControlTypeRadioButtons):
		return ControlTypeRadioButtons
	case "slider", "continuous", string(ControlTypeSliders):
		return ControlTypeSliders
	default:
		log.Printf("unknown control type: %q", normalized)
		return controlType
	}
}

// ListModels returns registered and inferred models for one control in stable order.
func ListModels(ctx context.Context, db *sql.DB, controlID string) ([]Model, error) {
	keys, err := ListAggregateKeys(ctx, db, controlID)
	if err != nil {
		return nil, err
	}
	return ListModelsWithKeys(ctx, db, controlID, keys)
}

// ListModelsWithKeys returns registered and inferred models using preloaded aggregate keys.
func ListModelsWithKeys(ctx context.Context, db *sql.DB, controlID string, keys []AggregateKey) ([]Model, error) {
	modelMap := make(map[string]Model)

	rows, err := db.QueryContext(ctx, `SELECT control_id, model_id FROM models WHERE control_id = ? ORDER BY model_id`, controlID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var model Model
		if err := rows.Scan(&model.ControlID, &model.ModelID); err != nil {
			return nil, err
		}
		modelMap[model.ModelID] = model
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Aggregate-derived model IDs remain visible even before explicit metadata is created.
	for _, key := range keys {
		if _, ok := modelMap[key.ModelID]; !ok {
			modelMap[key.ModelID] = Model{ControlID: controlID, ModelID: key.ModelID}
		}
	}

	modelIDs := make([]string, 0, len(modelMap))
	for modelID := range modelMap {
		modelIDs = append(modelIDs, modelID)
	}
	slices.Sort(modelIDs)

	models := make([]Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, modelMap[modelID])
	}
	return models, nil
}

// insertModel inserts a new model row and reports conflicts without updating existing rows.
func insertModel(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, controlID, modelID string) error {
	result, err := exec.ExecContext(
		ctx,
		`INSERT INTO models (control_id, model_id) VALUES (?, ?)
		 ON CONFLICT(control_id, model_id) DO NOTHING`,
		controlID,
		modelID,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

// SaveModel creates or updates one control model, including model-id renames.
func SaveModel(ctx context.Context, db *sql.DB, controlID, previousModelID string, model Model) error {
	model.ControlID = controlID
	model.ModelID = strings.TrimSpace(model.ModelID)
	if model.ModelID == "" {
		return ErrValidation
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if previousModelID != "" {
		// Renames are allowed for models inferred only from aggregates, so the
		// source lookup checks both explicit model rows and aggregate-backed presence.
		var sourceExists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM models WHERE control_id = ? AND model_id = ?`, controlID, previousModelID).Scan(&sourceExists)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			err = tx.QueryRowContext(ctx, `SELECT 1 FROM aggregates WHERE control_id = ? AND model_id = ?`, controlID, previousModelID).Scan(&sourceExists)
			switch {
			case err == nil:
			case errors.Is(err, sql.ErrNoRows):
				return ErrNotFound
			default:
				return err
			}
		default:
			return err
		}
	}

	if previousModelID == "" {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM models WHERE control_id = ? AND model_id = ?`, controlID, model.ModelID).Scan(&exists)
		switch {
		case err == nil:
			return ErrConflict
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if err := insertModel(ctx, tx, controlID, model.ModelID); err != nil {
			return err
		}
	} else if previousModelID == model.ModelID {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO models (control_id, model_id) VALUES (?, ?)
			 ON CONFLICT(control_id, model_id) DO NOTHING`,
			controlID, model.ModelID,
		); err != nil {
			return err
		}
	} else {
		// Model-id renames must be conflict-free in both metadata and aggregate rows
		// before the aggregate ownership can move to the new identifier.
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM models WHERE control_id = ? AND model_id = ?`, controlID, model.ModelID).Scan(&exists)
		switch {
		case err == nil:
			return ErrConflict
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM aggregates WHERE control_id = ? AND model_id = ?`, controlID, model.ModelID).Scan(&exists)
		switch {
		case err == nil:
			return ErrConflict
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}

		if err := insertModel(ctx, tx, controlID, model.ModelID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE aggregates SET model_id = ? WHERE control_id = ? AND model_id = ?`, model.ModelID, controlID, previousModelID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM models WHERE control_id = ? AND model_id = ?`, controlID, previousModelID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// ListAggregateKeys returns the aggregate keys recorded for a control in stable display order.
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

// validateAggregateBlobSize checks that a stored blob matches the expected packed layout.
func validateAggregateBlobSize(key AggregateKey, numStates int, blobBytes []byte) error {
	expectedLen := numStates * numStates * domain.GroupSize * 8
	if len(blobBytes) != expectedLen {
		return fmt.Errorf(
			"%w for control_id=%q model_id=%q quarter_index=%d: got %d bytes, expected %d",
			ErrAggregateBlobSizeMismatch,
			key.ControlID, key.ModelID, key.QuarterIndex, len(blobBytes), expectedLen,
		)
	}
	return nil
}

// GetOrCreateAggregate returns an existing aggregate blob or inserts a zeroed one.
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
		// This insert path is race-safe: concurrent creators can collide on the
		// unique key and then fall through to the shared reread below.
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

// GetAggregate returns an existing aggregate blob without creating missing rows.
func GetAggregate(ctx context.Context, db *sql.DB, key AggregateKey, numStates int) ([]byte, error) {
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
	if err := validateAggregateBlobSize(key, numStates, blobBytes); err != nil {
		return nil, err
	}
	return blobBytes, nil
}

// sqliteDSN appends the required SQLite pragmas to a database path or DSN.
func sqliteDSN(dbPath string) string {
	const pragma = "_pragma=foreign_keys(1)"
	if strings.Contains(dbPath, "?") {
		return dbPath + "&" + pragma
	}
	return dbPath + "?" + pragma
}

// UpdateAggregate applies a serialized read-modify-write update to one aggregate blob.
func UpdateAggregate(ctx context.Context, db *sql.DB, key AggregateKey, numStates int, update func([]byte) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Aggregate updates are read-modify-write on a single blob, so we take an
	// IMMEDIATE transaction to serialize writers before reading current bytes.
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

// queryRower captures the query methods shared by database and transaction handles.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// execContexter captures the exec methods shared by database and transaction handles.
type execContexter interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// updateAggregateWithQueryExec runs aggregate mutation logic against abstract query and exec dependencies.
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
		// Callers treat aggregate existence as an implementation detail, so the
		// update path materializes the zeroed blob on first write.
		b, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		blobBytes = b.Data()
	default:
		return err
	}

	working := make([]byte, len(blobBytes))
	// The callback always mutates a detached copy so partially applied writes are
	// never observed if the update function returns an error.
	copy(working, blobBytes)
	if err := update(working); err != nil {
		return err
	}

	// The final write is a single upsert, so callers see either the old blob or
	// the fully updated blob, never an intermediate mutation.
	_, err := execDB.ExecContext(
		ctx,
		`INSERT INTO aggregates (control_id, model_id, quarter_index, blob)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(control_id, model_id, quarter_index) DO UPDATE SET blob=excluded.blob`,
		key.ControlID, key.ModelID, key.QuarterIndex, working,
	)
	return err
}

// encodeLabels serializes optional control labels for storage.
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

// decodeLabels deserializes optional control labels from storage.
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
