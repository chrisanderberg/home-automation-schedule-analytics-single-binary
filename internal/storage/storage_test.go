package storage

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/testutil"
)

// setAggregateValue writes one counter into a stored aggregate for storage tests.
func setAggregateValue(t *testing.T, ctx context.Context, db *sql.DB, key AggregateKey, numStates, idx int, value uint64) {
	t.Helper()
	if err := UpdateAggregate(ctx, db, key, numStates, func(blob []byte) error {
		b, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		copy(b.Data(), blob)
		if err := b.SetU64(idx, value); err != nil {
			return err
		}
		copy(blob, b.Data())
		return nil
	}); err != nil {
		t.Fatalf("update aggregate: %v", err)
	}
}

// requireAggregateValue asserts one counter value inside a stored aggregate.
func requireAggregateValue(t *testing.T, ctx context.Context, db *sql.DB, key AggregateKey, numStates, idx int, want uint64) {
	t.Helper()
	data, err := GetAggregate(ctx, db, key, numStates)
	if err != nil {
		t.Fatalf("get aggregate: %v", err)
	}
	b, err := domain.NewBlob(numStates)
	if err != nil {
		t.Fatalf("new blob: %v", err)
	}
	copy(b.Data(), data)
	got, err := b.GetU64(idx)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if got != want {
		t.Fatalf("expected aggregate value %d, got %d", want, got)
	}
}

// TestControlCRUD verifies controls round-trip through storage and missing lookups return ErrNotFound.
func TestControlCRUD(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	c := Control{
		ControlID:   "light",
		ControlType: ControlTypeRadioButtons,
		NumStates:   3,
		StateLabels: []string{"off", "dim", "bright"},
	}
	if err := UpsertControl(ctx, db, c); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := GetControl(ctx, db, "light")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ControlID != "light" || got.NumStates != 3 || got.ControlType != ControlTypeRadioButtons {
		t.Fatalf("control mismatch: %+v", got)
	}
	if len(got.StateLabels) != 3 || got.StateLabels[0] != "off" {
		t.Fatalf("labels mismatch: %+v", got.StateLabels)
	}

	_, err = GetControl(ctx, db, "missing")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestSaveModelListsRegisteredModels verifies saved models are returned for the control.
func TestSaveModelListsRegisteredModels(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()
	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := SaveModel(ctx, db, "light", "", Model{ModelID: "weekday"}); err != nil {
		t.Fatalf("save first model: %v", err)
	}
	if err := SaveModel(ctx, db, "light", "", Model{ModelID: "weekend"}); err != nil {
		t.Fatalf("save second model: %v", err)
	}

	models, err := ListModels(ctx, db, "light")
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 2 || models[0].ModelID != "weekday" || models[1].ModelID != "weekend" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

// TestSaveModelRenameMovesAggregates verifies model-id edits preserve aggregate rows.
func TestSaveModelRenameMovesAggregates(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()
	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := SaveModel(ctx, db, "light", "", Model{ModelID: "old"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := GetOrCreateAggregate(ctx, db, AggregateKey{ControlID: "light", ModelID: "old", QuarterIndex: 1}, 2); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}
	setAggregateValue(t, ctx, db, AggregateKey{ControlID: "light", ModelID: "old", QuarterIndex: 1}, 2, 0, 42)

	if err := SaveModel(ctx, db, "light", "old", Model{ModelID: "new"}); err != nil {
		t.Fatalf("rename model: %v", err)
	}
	keys, err := ListAggregateKeys(ctx, db, "light")
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 || keys[0].ModelID != "new" {
		t.Fatalf("expected aggregate to move with model rename, got %+v", keys)
	}
	requireAggregateValue(t, ctx, db, AggregateKey{ControlID: "light", ModelID: "new", QuarterIndex: 1}, 2, 0, 42)
}

// TestSaveModelRejectsEmptyModelID verifies blank model IDs are treated as validation failures.
func TestSaveModelRejectsEmptyModelID(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()
	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	err := SaveModel(ctx, db, "light", "", Model{ModelID: "   "})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// TestSaveModelRejectsMissingSource verifies updates do not silently create a destination when the source is absent.
func TestSaveModelRejectsMissingSource(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()
	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	err := SaveModel(ctx, db, "light", "missing", Model{ModelID: "new"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestAggregateCreateUpdate verifies aggregates can be created, mutated, and read back.
func TestAggregateCreateUpdate(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 3}); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	key := AggregateKey{ControlID: "light", ModelID: "default", QuarterIndex: 200}
	numStates := 3

	data, err := GetOrCreateAggregate(ctx, db, key, numStates)
	if err != nil {
		t.Fatalf("get or create: %v", err)
	}
	expectedLen := numStates * numStates * domain.GroupSize * 8
	if len(data) != expectedLen {
		t.Fatalf("blob size: got %d want %d", len(data), expectedLen)
	}

	err = UpdateAggregate(ctx, db, key, numStates, func(blob []byte) error {
		b, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		copy(b.Data(), blob)
		if err := b.SetU64(0, 42); err != nil {
			return err
		}
		copy(blob, b.Data())
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	data, err = GetOrCreateAggregate(ctx, db, key, numStates)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	b, _ := domain.NewBlob(numStates)
	copy(b.Data(), data)
	v, _ := b.GetU64(0)
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

// TestGetAggregateReturnsNotFoundWithoutCreatingRow verifies reads do not materialize missing aggregate rows.
func TestGetAggregateReturnsNotFoundWithoutCreatingRow(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	_, err := GetAggregate(ctx, db, AggregateKey{ControlID: "missing", ModelID: "m", QuarterIndex: 1}, 2)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	keys, err := ListAggregateKeys(ctx, db, "missing")
	if err != nil {
		t.Fatalf("list aggregate keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no aggregates, got %+v", keys)
	}
}

// TestAggregateConcurrentUpdates verifies serialized aggregate updates preserve all increments.
func TestAggregateConcurrentUpdates(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "c", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	key := AggregateKey{ControlID: "c", ModelID: "m", QuarterIndex: 1}
	numStates := 2

	var wg sync.WaitGroup
	addN := func(n uint64) {
		defer wg.Done()
		for i := uint64(0); i < n; i++ {
			if err := UpdateAggregate(ctx, db, key, numStates, func(blob []byte) error {
				b, _ := domain.NewBlob(numStates)
				copy(b.Data(), blob)
				v, _ := b.GetU64(0)
				if err := b.SetU64(0, v+1); err != nil {
					return err
				}
				copy(blob, b.Data())
				return nil
			}); err != nil {
				t.Errorf("update: %v", err)
			}
		}
	}

	wg.Add(2)
	go addN(3)
	go addN(4)
	wg.Wait()

	data, err := GetOrCreateAggregate(ctx, db, key, numStates)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	b, _ := domain.NewBlob(numStates)
	copy(b.Data(), data)
	v, _ := b.GetU64(0)
	if v != 7 {
		t.Fatalf("expected 7, got %d", v)
	}
}

// TestUpdateAggregateRejectsMismatchedBlobSize verifies update paths reject corrupt aggregate blobs.
func TestUpdateAggregateRejectsMismatchedBlobSize(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "bad", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO aggregates (control_id, model_id, quarter_index, blob) VALUES (?, ?, ?, ?)`,
		"bad", "m", 0, []byte{0x01, 0x02},
	)
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	err = UpdateAggregate(ctx, db, AggregateKey{ControlID: "bad", ModelID: "m", QuarterIndex: 0}, 2, func(blob []byte) error {
		return nil
	})
	if !errors.Is(err, ErrAggregateBlobSizeMismatch) {
		t.Fatalf("expected blob size mismatch error, got %v", err)
	}
}

// TestGetOrCreateAggregateRejectsMismatchedBlobSize verifies get-or-create also rejects corrupt aggregate blobs.
func TestGetOrCreateAggregateRejectsMismatchedBlobSize(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "bad", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO aggregates (control_id, model_id, quarter_index, blob) VALUES (?, ?, ?, ?)`,
		"bad", "m", 0, []byte{0x01, 0x02},
	)
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	_, err = GetOrCreateAggregate(ctx, db, AggregateKey{ControlID: "bad", ModelID: "m", QuarterIndex: 0}, 2)
	if !errors.Is(err, ErrAggregateBlobSizeMismatch) {
		t.Fatalf("expected blob size mismatch error, got %v", err)
	}
}

// TestGetAggregateRejectsMismatchedBlobSize verifies direct aggregate reads reject corrupt blob sizes.
func TestGetAggregateRejectsMismatchedBlobSize(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO controls (control_id, control_type, num_states, state_labels) VALUES (?, ?, ?, ?)`,
		"bad", string(ControlTypeRadioButtons), 2, "",
	)
	if err != nil {
		t.Fatalf("seed control: %v", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO aggregates (control_id, model_id, quarter_index, blob) VALUES (?, ?, ?, ?)`,
		"bad", "m", 0, []byte{0x01, 0x02},
	)
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	_, err = GetAggregate(ctx, db, AggregateKey{ControlID: "bad", ModelID: "m", QuarterIndex: 0}, 2)
	if !errors.Is(err, ErrAggregateBlobSizeMismatch) {
		t.Fatalf("expected blob size mismatch error, got %v", err)
	}
}

// TestListControls verifies controls are returned in control-id order.
func TestListControls(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "b", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := UpsertControl(ctx, db, Control{ControlID: "a", ControlType: ControlTypeSliders, NumStates: 6}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	controls, err := ListControls(ctx, db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(controls) != 2 {
		t.Fatalf("expected 2, got %d", len(controls))
	}
	if controls[0].ControlID != "a" || controls[1].ControlID != "b" {
		t.Fatalf("expected sorted by id: %+v", controls)
	}
}

// TestSaveControlRenameMovesAggregates verifies control-id edits preserve aggregate rows.
func TestSaveControlRenameMovesAggregates(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "old", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := SaveModel(ctx, db, "old", "", Model{ModelID: "m"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := GetOrCreateAggregate(ctx, db, AggregateKey{ControlID: "old", ModelID: "m", QuarterIndex: 1}, 2); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}
	setAggregateValue(t, ctx, db, AggregateKey{ControlID: "old", ModelID: "m", QuarterIndex: 1}, 2, 0, 99)

	if err := SaveControl(ctx, db, "old", Control{
		ControlID:   "new",
		ControlType: ControlTypeRadioButtons,
		NumStates:   2,
		StateLabels: []string{"off", "on"},
	}); err != nil {
		t.Fatalf("save control: %v", err)
	}

	if _, err := GetControl(ctx, db, "old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected old control to be gone, got %v", err)
	}
	got, err := GetControl(ctx, db, "new")
	if err != nil {
		t.Fatalf("get new control: %v", err)
	}
	if got.ControlID != "new" || len(got.StateLabels) != 2 {
		t.Fatalf("unexpected control: %+v", got)
	}

	keys, err := ListAggregateKeys(ctx, db, "new")
	if err != nil {
		t.Fatalf("list aggregate keys: %v", err)
	}
	if len(keys) != 1 || keys[0].ControlID != "new" {
		t.Fatalf("expected aggregate to move to new control id, got %+v", keys)
	}
	requireAggregateValue(t, ctx, db, AggregateKey{ControlID: "new", ModelID: "m", QuarterIndex: 1}, 2, 0, 99)
	models, err := ListModels(ctx, db, "new")
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "m" || models[0].ControlID != "new" {
		t.Fatalf("expected model metadata to move with renamed control, got %+v", models)
	}
}

// TestSaveControlRejectsConflictingCreate verifies the UI save path does not overwrite existing controls.
func TestSaveControlRejectsConflictingCreate(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	err := SaveControl(ctx, db, "", Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 3})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

// TestSaveModelRenameRejectsAggregateOnlyConflict verifies renames cannot target model IDs that only exist in aggregates.
func TestSaveModelRenameRejectsAggregateOnlyConflict(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := SaveModel(ctx, db, "light", "", Model{ModelID: "weekday"}); err != nil {
		t.Fatalf("save model: %v", err)
	}
	if _, err := GetOrCreateAggregate(ctx, db, AggregateKey{ControlID: "light", ModelID: "vacation", QuarterIndex: 1}, 2); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}

	err := SaveModel(ctx, db, "light", "weekday", Model{ModelID: "vacation"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

// TestListAggregateKeys verifies aggregate keys are returned in stable model ordering.
func TestListAggregateKeys(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "c", ControlType: ControlTypeRadioButtons, NumStates: 2}); err != nil {
		t.Fatalf("upsert control: %v", err)
	}

	key1 := AggregateKey{ControlID: "c", ModelID: "m1", QuarterIndex: 200}
	key2 := AggregateKey{ControlID: "c", ModelID: "m2", QuarterIndex: 201}
	if _, err := GetOrCreateAggregate(ctx, db, key1, 2); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := GetOrCreateAggregate(ctx, db, key2, 2); err != nil {
		t.Fatalf("create: %v", err)
	}

	keys, err := ListAggregateKeys(ctx, db, "c")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2, got %d", len(keys))
	}
	if keys[0].ModelID != "m1" || keys[1].ModelID != "m2" {
		t.Fatalf("expected sorted by model_id: %+v", keys)
	}
}
