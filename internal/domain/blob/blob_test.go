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
	hold, _ := copyAcc.Holding(1, 0, 42)
	if hold != 1500 {
		t.Fatalf("Holding() = %d, want 1500", hold)
	}
	trans, _ := copyAcc.Transition(2, 1, 4, 100)
	if trans != 3 {
		t.Fatalf("Transition() = %d, want 3", trans)
	}

	other, _ := blob.NewAccumulator(3)
	_ = other.AddHolding(1, 0, 42, 500)
	if err := copyAcc.Merge(other); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	hold, _ = copyAcc.Holding(1, 0, 42)
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
