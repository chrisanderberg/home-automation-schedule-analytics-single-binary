package blob

import (
	"encoding/binary"
	"fmt"
)

const (
	BucketsPerDay  = 288
	DaysPerWeek    = 7
	BucketsPerWeek = BucketsPerDay * DaysPerWeek
	ClocksPerEvent = 5
	GroupsPerState = ClocksPerEvent * BucketsPerWeek
	MaxNumStates   = 64
)

type Layout struct {
	NumStates int
}

func NewLayout(numStates int) (Layout, error) {
	if numStates < 2 || numStates > MaxNumStates {
		return Layout{}, fmt.Errorf("numStates must be in range [2, %d]", MaxNumStates)
	}
	return Layout{NumStates: numStates}, nil
}

func (l Layout) holdWordCount() int {
	return l.NumStates * GroupsPerState
}

func (l Layout) transitionWordCount() int {
	return l.NumStates * (l.NumStates - 1) * GroupsPerState
}

func (l Layout) WordCount() int {
	return l.holdWordCount() + l.transitionWordCount()
}

func (l Layout) ByteSize() int {
	return l.WordCount() * 8
}

func (l Layout) HoldIndex(state, clock, bucket int) int {
	return (state * GroupsPerState) + (clock * BucketsPerWeek) + bucket
}

// TransitionGroupIndex flattens transitions by from-state while skipping the
// self-transition slot in each group so the compacted list stays dense.
func (l Layout) TransitionGroupIndex(from, to int) int {
	offset := to
	if to > from {
		offset--
	}
	return from*(l.NumStates-1) + offset
}

func (l Layout) TransitionIndex(from, to, clock, bucket int) int {
	return l.holdWordCount() + (l.TransitionGroupIndex(from, to) * GroupsPerState) + (clock * BucketsPerWeek) + bucket
}

type Accumulator struct {
	layout Layout
	words  []uint64
}

func NewAccumulator(numStates int) (*Accumulator, error) {
	layout, err := NewLayout(numStates)
	if err != nil {
		return nil, err
	}
	return &Accumulator{
		layout: layout,
		words:  make([]uint64, layout.WordCount()),
	}, nil
}

func FromBytes(numStates int, data []byte) (*Accumulator, error) {
	layout, err := NewLayout(numStates)
	if err != nil {
		return nil, err
	}
	if len(data) != layout.ByteSize() {
		return nil, fmt.Errorf("blob size mismatch: got %d want %d", len(data), layout.ByteSize())
	}
	acc := &Accumulator{
		layout: layout,
		words:  make([]uint64, layout.WordCount()),
	}
	for i := range acc.words {
		acc.words[i] = binary.LittleEndian.Uint64(data[i*8:])
	}
	return acc, nil
}

func (a *Accumulator) Layout() Layout {
	return a.layout
}

func (a *Accumulator) AddHolding(state, clock, bucket int, value uint64) error {
	if err := a.validateState(state); err != nil {
		return err
	}
	if err := validateClockBucket(clock, bucket); err != nil {
		return err
	}
	a.words[a.layout.HoldIndex(state, clock, bucket)] += value
	return nil
}

func (a *Accumulator) AddTransition(from, to, clock, bucket int, value uint64) error {
	if err := a.validateState(from); err != nil {
		return err
	}
	if err := a.validateState(to); err != nil {
		return err
	}
	if from == to {
		return fmt.Errorf("self-transition is invalid")
	}
	if err := validateClockBucket(clock, bucket); err != nil {
		return err
	}
	a.words[a.layout.TransitionIndex(from, to, clock, bucket)] += value
	return nil
}

func (a *Accumulator) Holding(state, clock, bucket int) (uint64, error) {
	if err := a.validateState(state); err != nil {
		return 0, err
	}
	if err := validateClockBucket(clock, bucket); err != nil {
		return 0, err
	}
	return a.words[a.layout.HoldIndex(state, clock, bucket)], nil
}

func (a *Accumulator) Transition(from, to, clock, bucket int) (uint64, error) {
	if err := a.validateState(from); err != nil {
		return 0, err
	}
	if err := a.validateState(to); err != nil {
		return 0, err
	}
	if from == to {
		return 0, fmt.Errorf("self-transition is invalid")
	}
	if err := validateClockBucket(clock, bucket); err != nil {
		return 0, err
	}
	return a.words[a.layout.TransitionIndex(from, to, clock, bucket)], nil
}

func (a *Accumulator) Merge(other *Accumulator) error {
	if other == nil {
		return fmt.Errorf("other accumulator is required")
	}
	if a.layout != other.layout {
		return fmt.Errorf("layout mismatch")
	}
	for i := range a.words {
		a.words[i] += other.words[i]
	}
	return nil
}

func (a *Accumulator) Bytes() []byte {
	data := make([]byte, a.layout.ByteSize())
	for i, v := range a.words {
		binary.LittleEndian.PutUint64(data[i*8:], v)
	}
	return data
}

func (a *Accumulator) validateState(state int) error {
	if state < 0 || state >= a.layout.NumStates {
		return fmt.Errorf("state out of range")
	}
	return nil
}

func validateClockBucket(clock, bucket int) error {
	if clock < 0 || clock >= ClocksPerEvent {
		return fmt.Errorf("clock out of range")
	}
	if bucket < 0 || bucket >= BucketsPerWeek {
		return fmt.Errorf("bucket out of range")
	}
	return nil
}
