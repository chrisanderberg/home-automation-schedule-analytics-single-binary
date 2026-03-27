package domain

import (
	"encoding/binary"
	"errors"
)

const (
	ClockUTC = iota
	ClockLocal
	ClockMeanSolar
	ClockApparentSolar
	ClockUnequalHours
	clockCount
)

const (
	BucketsPerDay  = 288
	BucketsPerWeek = 7 * BucketsPerDay
	Clocks         = clockCount
	GroupSize      = BucketsPerWeek * Clocks
)

const (
	MinStates = 2
	MaxStates = 10
)

var (
	ErrInvalidNumStates = errors.New("invalid number of states")
	ErrIndexOutOfRange  = errors.New("index out of range")
	ErrSelfTransition   = errors.New("self transition is not allowed")
)

// Blob stores packed holding and transition counters for one control state layout.
type Blob struct {
	numStates int
	data      []byte
}

// NewBlob allocates a zeroed aggregate blob for the requested state cardinality.
func NewBlob(numStates int) (*Blob, error) {
	if numStates < MinStates || numStates > MaxStates {
		return nil, ErrInvalidNumStates
	}
	// A blob stores one contiguous holding section for each state, followed by
	// one contiguous transition section for each valid state-to-state pair.
	valueCount := numStates * numStates * GroupSize
	data := make([]byte, valueCount*8)
	return &Blob{numStates: numStates, data: data}, nil
}

// NumStates returns the state cardinality encoded by the blob layout.
func (b *Blob) NumStates() int { return b.numStates }

// Data exposes the raw little-endian byte backing used for persistence.
func (b *Blob) Data() []byte { return b.data }

// ValueCount returns the number of uint64 counters stored in the blob.
func (b *Blob) ValueCount() int { return b.numStates * b.numStates * GroupSize }

// GetU64 reads one counter value from the packed blob.
func (b *Blob) GetU64(index int) (uint64, error) {
	if index < 0 || index >= b.ValueCount() {
		return 0, ErrIndexOutOfRange
	}
	offset := index * 8
	return binary.LittleEndian.Uint64(b.data[offset : offset+8]), nil
}

// SetU64 writes one counter value into the packed blob.
func (b *Blob) SetU64(index int, value uint64) error {
	if index < 0 || index >= b.ValueCount() {
		return ErrIndexOutOfRange
	}
	offset := index * 8
	binary.LittleEndian.PutUint64(b.data[offset:offset+8], value)
	return nil
}

// HoldIndex returns the counter slot for one state, clock, and bucket combination.
func HoldIndex(state, clock, bucket, numStates int) (int, error) {
	if numStates < MinStates || numStates > MaxStates {
		return 0, ErrInvalidNumStates
	}
	if state < 0 || state >= numStates {
		return 0, ErrIndexOutOfRange
	}
	if clock < 0 || clock >= Clocks {
		return 0, ErrIndexOutOfRange
	}
	if bucket < 0 || bucket >= BucketsPerWeek {
		return 0, ErrIndexOutOfRange
	}
	// Holdings occupy the first numStates groups so each state's time series is
	// addressable without needing any transition metadata.
	return (state * GroupSize) + (clock * BucketsPerWeek) + bucket, nil
}

// TransGroupIndex returns the transition-group slot for a non-self state pair.
func TransGroupIndex(fromState, toState, numStates int) (int, error) {
	if numStates < MinStates || numStates > MaxStates {
		return 0, ErrInvalidNumStates
	}
	if fromState < 0 || fromState >= numStates || toState < 0 || toState >= numStates {
		return 0, ErrIndexOutOfRange
	}
	if fromState == toState {
		return 0, ErrSelfTransition
	}
	// Transition groups are packed as "all destinations except self" for each
	// source state, so later destinations shift left by one slot.
	offset := toState
	if toState > fromState {
		offset = toState - 1
	}
	return fromState*(numStates-1) + offset, nil
}

// TransIndex returns the counter slot for one transition, clock, and bucket combination.
func TransIndex(fromState, toState, clock, bucket, numStates int) (int, error) {
	if clock < 0 || clock >= Clocks {
		return 0, ErrIndexOutOfRange
	}
	if bucket < 0 || bucket >= BucketsPerWeek {
		return 0, ErrIndexOutOfRange
	}
	groupIndex, err := TransGroupIndex(fromState, toState, numStates)
	if err != nil {
		return 0, err
	}
	// Transition groups begin immediately after the holding groups and reuse the
	// same per-clock, per-bucket packing as holdings.
	return (numStates * GroupSize) + (groupIndex * GroupSize) + (clock * BucketsPerWeek) + bucket, nil
}
