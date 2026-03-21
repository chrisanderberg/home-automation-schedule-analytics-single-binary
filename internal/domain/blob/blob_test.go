package blob_test

import (
	"testing"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
)

func TestLayoutMatchesSpec(t *testing.T) {
	t.Parallel()

	layout, err := blob.NewLayout(4)
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}

	if got, want := layout.HoldIndex(2, 3, 17), (2*10080)+(3*2016)+17; got != want {
		t.Fatalf("HoldIndex() = %d, want %d", got, want)
	}
	if got, want := layout.TransitionGroupIndex(1, 3), 1*3+2; got != want {
		t.Fatalf("TransitionGroupIndex() = %d, want %d", got, want)
	}
	if got, want := layout.TransitionIndex(1, 3, 2, 99), (4*10080)+((1*3+2)*10080)+(2*2016)+99; got != want {
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

func TestNewLayoutRejectsTooManyStates(t *testing.T) {
	t.Parallel()

	if _, err := blob.NewLayout(blob.MaxNumStates + 1); err == nil {
		t.Fatal("NewLayout() expected error for too many states")
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
