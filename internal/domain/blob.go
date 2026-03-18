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

type Blob struct {
	numStates int
	data      []byte
}

func NewBlob(numStates int) (*Blob, error) {
	if numStates < MinStates || numStates > MaxStates {
		return nil, ErrInvalidNumStates
	}
	valueCount := numStates * numStates * GroupSize
	data := make([]byte, valueCount*8)
	return &Blob{numStates: numStates, data: data}, nil
}

func (b *Blob) NumStates() int  { return b.numStates }
func (b *Blob) Data() []byte    { return b.data }
func (b *Blob) ValueCount() int { return b.numStates * b.numStates * GroupSize }

func (b *Blob) GetU64(index int) (uint64, error) {
	if index < 0 || index >= b.ValueCount() {
		return 0, ErrIndexOutOfRange
	}
	offset := index * 8
	return binary.LittleEndian.Uint64(b.data[offset : offset+8]), nil
}

func (b *Blob) SetU64(index int, value uint64) error {
	if index < 0 || index >= b.ValueCount() {
		return ErrIndexOutOfRange
	}
	offset := index * 8
	binary.LittleEndian.PutUint64(b.data[offset:offset+8], value)
	return nil
}

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
	return (state * GroupSize) + (clock * BucketsPerWeek) + bucket, nil
}

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
	offset := toState
	if toState > fromState {
		offset = toState - 1
	}
	return fromState*(numStates-1) + offset, nil
}

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
	return (numStates * GroupSize) + (groupIndex * GroupSize) + (clock * BucketsPerWeek) + bucket, nil
}
