package storage

import (
	"context"
	"errors"
	"sync"
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/testutil"
)

// TestControlCRUD verifies controls round-trip through storage and missing lookups return ErrNotFound.
func TestControlCRUD(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	c := Control{
		ControlID:   "light",
		ControlType: ControlTypeDiscrete,
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
	if got.ControlID != "light" || got.NumStates != 3 || got.ControlType != ControlTypeDiscrete {
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

// TestAggregateCreateUpdate verifies aggregates can be created, mutated, and read back.
func TestAggregateCreateUpdate(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "light", ControlType: ControlTypeDiscrete, NumStates: 3}); err != nil {
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

	if err := UpsertControl(ctx, db, Control{ControlID: "c", ControlType: ControlTypeDiscrete, NumStates: 2}); err != nil {
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

	if err := UpsertControl(ctx, db, Control{ControlID: "bad", ControlType: ControlTypeDiscrete, NumStates: 2}); err != nil {
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

	if err := UpsertControl(ctx, db, Control{ControlID: "bad", ControlType: ControlTypeDiscrete, NumStates: 2}); err != nil {
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
		"bad", string(ControlTypeDiscrete), 2, "",
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

	if err := UpsertControl(ctx, db, Control{ControlID: "b", ControlType: ControlTypeDiscrete, NumStates: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := UpsertControl(ctx, db, Control{ControlID: "a", ControlType: ControlTypeSlider, NumStates: 6}); err != nil {
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

// TestListAggregateKeys verifies aggregate keys are returned in stable model ordering.
func TestListAggregateKeys(t *testing.T) {
	db := testutil.OpenTestDB(t, Open, InitSchema)
	ctx := context.Background()

	if err := UpsertControl(ctx, db, Control{ControlID: "c", ControlType: ControlTypeDiscrete, NumStates: 2}); err != nil {
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
