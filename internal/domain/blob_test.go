package domain

import "testing"

func TestBlobValueCountFormula(t *testing.T) {
	for n := MinStates; n <= MaxStates; n++ {
		b, err := NewBlob(n)
		if err != nil {
			t.Fatalf("unexpected error for n=%d: %v", n, err)
		}
		expected := n * n * GroupSize
		if got := b.ValueCount(); got != expected {
			t.Fatalf("value count mismatch for n=%d: got %d want %d", n, got, expected)
		}
		if gotBytes := len(b.Data()); gotBytes != expected*8 {
			t.Fatalf("byte length mismatch for n=%d: got %d want %d", n, gotBytes, expected*8)
		}
	}
}

func TestHoldingRegionBounds(t *testing.T) {
	n := 4
	for s := 0; s < n; s++ {
		for c := 0; c < Clocks; c++ {
			for bkt := 0; bkt < BucketsPerWeek; bkt++ {
				idx, err := HoldIndex(s, c, bkt, n)
				if err != nil {
					t.Fatalf("hold index error: %v", err)
				}
				if idx < 0 || idx >= n*GroupSize {
					t.Fatalf("hold index out of bounds: %d", idx)
				}
			}
		}
	}
}

func TestTransitionRegionBounds(t *testing.T) {
	n := 4
	for from := 0; from < n; from++ {
		for to := 0; to < n; to++ {
			if from == to {
				continue
			}
			for c := 0; c < Clocks; c++ {
				for bkt := 0; bkt < BucketsPerWeek; bkt++ {
					idx, err := TransIndex(from, to, c, bkt, n)
					if err != nil {
						t.Fatalf("trans index error: %v", err)
					}
					if idx < n*GroupSize || idx >= n*n*GroupSize {
						t.Fatalf("trans index out of bounds: %d", idx)
					}
				}
			}
		}
	}
}

func TestTransitionGroupsCoverAllPairs(t *testing.T) {
	n := 5
	seen := make(map[int]struct{})
	for from := 0; from < n; from++ {
		for to := 0; to < n; to++ {
			if from == to {
				continue
			}
			g, err := TransGroupIndex(from, to, n)
			if err != nil {
				t.Fatalf("group index error: %v", err)
			}
			if g < 0 || g >= n*(n-1) {
				t.Fatalf("group index out of bounds: %d", g)
			}
			if _, exists := seen[g]; exists {
				t.Fatalf("duplicate group index %d for pair (%d,%d)", g, from, to)
			}
			seen[g] = struct{}{}
		}
	}

	expected := n * (n - 1)
	if len(seen) != expected {
		t.Fatalf("group coverage mismatch: got %d want %d", len(seen), expected)
	}
}
