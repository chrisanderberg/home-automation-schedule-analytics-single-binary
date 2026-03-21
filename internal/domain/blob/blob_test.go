package blob_test

import (
	"math"
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
)

func TestLayoutMatchesSpec(t *testing.T) {
	t.Parallel()

	layout, err := blob.NewLayout(4)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}

	// These assertions mirror the layout formulas so future changes can be
	// checked against the index contract. layout.HoldIndex is computed as
	// state*blob.GroupsPerState + clock*blob.BucketsPerWeek + bucket.
	// layout.TransitionGroupIndex packs the off-diagonal transition matrix as
	// from*(numStates-1)+adjustedTo, where adjustedTo skips the diagonal entry.
	// layout.TransitionIndex then adds the transition section offset
	// (numStates*blob.GroupsPerState), plus
	// layout.TransitionGroupIndex(from, to)*blob.GroupsPerState, plus the bucket
	// offset and position within the group.
	if got, want := layout.HoldIndex(2, 3, 17), (2*blob.GroupsPerState)+(3*blob.BucketsPerWeek)+17; got != want {
		t.Fatalf("HoldIndex() = %d, want %d", got, want)
	}
	if got, want := layout.TransitionGroupIndex(1, 3), 1*3+2; got != want {
		t.Fatalf("TransitionGroupIndex() = %d, want %d", got, want)
	}
	if got, want := layout.TransitionIndex(1, 3, 2, 99), (4*blob.GroupsPerState)+((1*3+2)*blob.GroupsPerState)+(2*blob.BucketsPerWeek)+99; got != want {
		t.Fatalf("TransitionIndex() = %d, want %d", got, want)
	}
}

func TestLayoutExposesValidatedStateCount(t *testing.T) {
	t.Parallel()

	layout, err := blob.NewLayout(4)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	if got := layout.NumStates(); got != 4 {
		t.Fatalf("NumStates() = %d, want 4", got)
	}
}

func TestLayoutNumStatesPanicsForInvalidZeroValue(t *testing.T) {
	t.Parallel()

	var layout blob.Layout

	defer func() {
		if recover() == nil {
			t.Fatal("NumStates() expected panic for invalid zero-value layout")
		}
	}()

	_ = layout.NumStates()
}

func TestAccumulatorRoundTripAndMerge(t *testing.T) {
	t.Parallel()

	acc, err := blob.NewAccumulator(3)
	if err != nil {
		t.Fatalf("NewAccumulator() error = %v", err)
	}
	if err := acc.AddHolding(1, 0, 42, 1500); err != nil {
		t.Fatalf("AddHolding() error = %v", err)
	}
	if err := acc.AddTransition(2, 1, 4, 100, 3); err != nil {
		t.Fatalf("AddTransition() error = %v", err)
	}

	copyAcc, err := blob.FromBytes(3, acc.Bytes())
	if err != nil {
		t.Fatalf("FromBytes() error = %v", err)
	}
	hold, err := copyAcc.Holding(1, 0, 42)
	if err != nil {
		t.Fatalf("Holding() error = %v", err)
	}
	if hold != 1500 {
		t.Fatalf("Holding() = %d, want 1500", hold)
	}
	trans, err := copyAcc.Transition(2, 1, 4, 100)
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if trans != 3 {
		t.Fatalf("Transition() = %d, want 3", trans)
	}

	other, err := blob.NewAccumulator(3)
	if err != nil {
		t.Fatalf("NewAccumulator(other) error = %v", err)
	}
	if err := other.AddHolding(1, 0, 42, 500); err != nil {
		t.Fatalf("other.AddHolding() error = %v", err)
	}
	if err := copyAcc.Merge(other); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	hold, err = copyAcc.Holding(1, 0, 42)
	if err != nil {
		t.Fatalf("merged Holding() error = %v", err)
	}
	if hold != 2000 {
		t.Fatalf("merged Holding() = %d, want 2000", hold)
	}
}

func TestAccumulatorRejectsSelfTransition(t *testing.T) {
	t.Parallel()

	acc, err := blob.NewAccumulator(2)
	if err != nil {
		t.Fatalf("NewAccumulator() error = %v", err)
	}
	if err := acc.AddTransition(1, 1, 0, 0, 1); err == nil {
		t.Fatal("AddTransition() expected error for self-transition")
	}
}

func TestAccumulatorRejectsNegativeInputs(t *testing.T) {
	t.Parallel()

	acc, err := blob.NewAccumulator(2)
	if err != nil {
		t.Fatalf("NewAccumulator() error = %v", err)
	}

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "holding negative state",
			fn: func() error {
				return acc.AddHolding(-1, 0, 0, 1)
			},
		},
		{
			name: "holding negative clock",
			fn: func() error {
				return acc.AddHolding(0, -1, 0, 1)
			},
		},
		{
			name: "holding negative bucket",
			fn: func() error {
				return acc.AddHolding(0, 0, -1, 1)
			},
		},
		{
			name: "transition negative from",
			fn: func() error {
				return acc.AddTransition(-1, 1, 0, 0, 1)
			},
		},
		{
			name: "transition negative to",
			fn: func() error {
				return acc.AddTransition(0, -1, 0, 0, 1)
			},
		},
		{
			name: "transition negative clock",
			fn: func() error {
				return acc.AddTransition(0, 1, -1, 0, 1)
			},
		},
		{
			name: "transition negative bucket",
			fn: func() error {
				return acc.AddTransition(0, 1, 0, -1, 1)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.fn(); err == nil {
				t.Fatalf("%s: expected error", tc.name)
			}
		})
	}
}

func TestAccumulatorMergeSaturatesAtMaxUint64(t *testing.T) {
	t.Parallel()

	left, err := blob.NewAccumulator(2)
	if err != nil {
		t.Fatalf("NewAccumulator(left) error = %v", err)
	}
	right, err := blob.NewAccumulator(2)
	if err != nil {
		t.Fatalf("NewAccumulator(right) error = %v", err)
	}
	if err := left.AddHolding(0, 0, 0, math.MaxUint64); err != nil {
		t.Fatalf("left.AddHolding() error = %v", err)
	}
	if err := right.AddHolding(0, 0, 0, 1); err != nil {
		t.Fatalf("right.AddHolding() error = %v", err)
	}

	if err := left.Merge(right); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	got, err := left.Holding(0, 0, 0)
	if err != nil {
		t.Fatalf("Holding() error = %v", err)
	}
	if got != math.MaxUint64 {
		t.Fatalf("Holding() = %d, want %d", got, uint64(math.MaxUint64))
	}
}

func TestAccumulatorAddHoldingSaturatesAtMaxUint64(t *testing.T) {
	t.Parallel()

	acc, err := blob.NewAccumulator(2)
	if err != nil {
		t.Fatalf("NewAccumulator() error = %v", err)
	}
	if err := acc.AddHolding(0, 0, 0, math.MaxUint64); err != nil {
		t.Fatalf("AddHolding() error = %v", err)
	}
	if err := acc.AddHolding(0, 0, 0, 1); err != nil {
		t.Fatalf("AddHolding() overflow error = %v", err)
	}

	got, err := acc.Holding(0, 0, 0)
	if err != nil {
		t.Fatalf("Holding() error = %v", err)
	}
	if got != math.MaxUint64 {
		t.Fatalf("Holding() = %d, want %d", got, uint64(math.MaxUint64))
	}
}

func TestAccumulatorAddTransitionSaturatesAtMaxUint64(t *testing.T) {
	t.Parallel()

	acc, err := blob.NewAccumulator(2)
	if err != nil {
		t.Fatalf("NewAccumulator() error = %v", err)
	}
	if err := acc.AddTransition(0, 1, 0, 0, math.MaxUint64); err != nil {
		t.Fatalf("AddTransition() error = %v", err)
	}
	if err := acc.AddTransition(0, 1, 0, 0, 1); err != nil {
		t.Fatalf("AddTransition() overflow error = %v", err)
	}

	got, err := acc.Transition(0, 1, 0, 0)
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if got != math.MaxUint64 {
		t.Fatalf("Transition() = %d, want %d", got, uint64(math.MaxUint64))
	}
}

func TestFromBytesRejectsWrongSize(t *testing.T) {
	t.Parallel()

	if _, err := blob.FromBytes(2, []byte{0, 1, 2, 3}); err == nil {
		t.Fatal("FromBytes() expected error for wrong size")
	}
}

func TestNewLayoutBoundaryValues(t *testing.T) {
	t.Parallel()

	if _, err := blob.NewLayout(1); err == nil {
		t.Fatal("NewLayout() expected error for too few states")
	}
	if _, err := blob.NewLayout(2); err != nil {
		t.Fatalf("NewLayout(2) error = %v", err)
	}
	if _, err := blob.NewLayout(blob.MaxNumStates); err != nil {
		t.Fatalf("NewLayout(MaxNumStates) error = %v", err)
	}
}

func TestNewLayoutRejectsTooManyStates(t *testing.T) {
	t.Parallel()

	if _, err := blob.NewLayout(blob.MaxNumStates + 1); err == nil {
		t.Fatal("NewLayout() expected error for too many states")
	}
}

func TestLayoutSizeHelpersPanicOnInvalidLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func(blob.Layout)
	}{
		{
			name: "WordCount",
			fn: func(layout blob.Layout) {
				_ = layout.WordCount()
			},
		},
		{
			name: "ByteSize",
			fn: func(layout blob.Layout) {
				_ = layout.ByteSize()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatalf("%s expected panic for invalid layout", tc.name)
				}
			}()
			tc.fn(blob.Layout{})
		})
	}
}

func TestTransitionGroupIndexPanicsOnSelfTransition(t *testing.T) {
	t.Parallel()

	layout, err := blob.NewLayout(2)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("TransitionGroupIndex() expected panic for self-transition")
		}
	}()
	_ = layout.TransitionGroupIndex(1, 1)
}

func TestHoldIndexPanicsOnInvalidInput(t *testing.T) {
	t.Parallel()

	layout, err := blob.NewLayout(2)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}

	tests := []struct {
		name string
		fn   func()
	}{
		{
			name: "state",
			fn: func() {
				_ = layout.HoldIndex(2, 0, 0)
			},
		},
		{
			name: "clock",
			fn: func() {
				_ = layout.HoldIndex(0, blob.ClocksPerEvent, 0)
			},
		},
		{
			name: "bucket",
			fn: func() {
				_ = layout.HoldIndex(0, 0, blob.BucketsPerWeek)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatalf("HoldIndex() expected panic for invalid %s", tc.name)
				}
			}()
			tc.fn()
		})
	}
}

func TestTransitionIndexPanicsOnInvalidInput(t *testing.T) {
	t.Parallel()

	layout, err := blob.NewLayout(2)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}

	tests := []struct {
		name string
		fn   func()
	}{
		{
			name: "from",
			fn: func() {
				_ = layout.TransitionIndex(2, 1, 0, 0)
			},
		},
		{
			name: "to",
			fn: func() {
				_ = layout.TransitionIndex(0, 2, 0, 0)
			},
		},
		{
			name: "clock",
			fn: func() {
				_ = layout.TransitionIndex(0, 1, blob.ClocksPerEvent, 0)
			},
		},
		{
			name: "bucket",
			fn: func() {
				_ = layout.TransitionIndex(0, 1, 0, blob.BucketsPerWeek)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatalf("TransitionIndex() expected panic for invalid %s", tc.name)
				}
			}()
			tc.fn()
		})
	}
}
