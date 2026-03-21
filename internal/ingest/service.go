package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
	"home-automation-schedule-analytics-single-bin/internal/domain/bucketing"
	"home-automation-schedule-analytics-single-bin/internal/domain/control"
	"home-automation-schedule-analytics-single-bin/internal/domain/quarter"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

var ErrValidation = errors.New("validation error")

type HoldingInterval struct {
	ControlID   string
	State       int
	StartTimeMs int64
	EndTimeMs   int64
}

type Transition struct {
	ControlID    string
	FromState    int
	ToState      int
	TimestampMs  int64
}

type Service struct {
	store   *storage.Store
	buckets *bucketing.Engine
	now     func() time.Time
}

func NewService(store *storage.Store, buckets *bucketing.Engine) *Service {
	return &Service{
		store:   store,
		buckets: buckets,
		now:     time.Now,
	}
}

func (s *Service) RegisterControl(ctx context.Context, c control.Control) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return s.store.UpsertControl(ctx, c, s.now())
}

func (s *Service) IngestHolding(ctx context.Context, item HoldingInterval) error {
	if item.ControlID == "" {
		return fmt.Errorf("%w: controlID is required", ErrValidation)
	}
	start := time.UnixMilli(item.StartTimeMs).UTC()
	end := time.UnixMilli(item.EndTimeMs).UTC()
	if !start.Before(end) {
		return fmt.Errorf("%w: holding interval must satisfy start < end", ErrValidation)
	}

	c, err := s.store.GetControl(ctx, item.ControlID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("%w: unknown control", ErrValidation)
		}
		return err
	}
	if item.State < 0 || item.State >= c.NumStates {
		return fmt.Errorf("%w: state out of range", ErrValidation)
	}

	for _, segment := range quarter.Split(start, end) {
		acc, err := blob.NewAccumulator(c.NumStates)
		if err != nil {
			return err
		}
		for _, clock := range allClocks() {
			pieces, err := s.buckets.HoldingSegments(clock, segment.Start, segment.End)
			if err != nil {
				return err
			}
			for _, piece := range pieces {
				if err := acc.AddHolding(item.State, int(clock), piece.Bucket, uint64(piece.Span/time.Millisecond)); err != nil {
					return err
				}
			}
		}
		if err := s.store.ApplyAggregateDelta(ctx, c.ID, segment.QuarterIndex, c.NumStates, acc.Bytes(), s.now()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) IngestTransition(ctx context.Context, item Transition) error {
	if item.ControlID == "" {
		return fmt.Errorf("%w: controlID is required", ErrValidation)
	}
	if item.FromState == item.ToState {
		return fmt.Errorf("%w: self-transition is invalid", ErrValidation)
	}
	c, err := s.store.GetControl(ctx, item.ControlID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("%w: unknown control", ErrValidation)
		}
		return err
	}
	if item.FromState < 0 || item.FromState >= c.NumStates || item.ToState < 0 || item.ToState >= c.NumStates {
		return fmt.Errorf("%w: transition states out of range", ErrValidation)
	}

	ts := time.UnixMilli(item.TimestampMs).UTC()
	acc, err := blob.NewAccumulator(c.NumStates)
	if err != nil {
		return err
	}
	for _, clock := range allClocks() {
		bucket, err := s.buckets.TransitionBucket(clock, ts)
		if err != nil {
			return err
		}
		if err := acc.AddTransition(item.FromState, item.ToState, int(clock), bucket, 1); err != nil {
			return err
		}
	}
	return s.store.ApplyAggregateDelta(ctx, c.ID, quarter.Index(ts), c.NumStates, acc.Bytes(), s.now())
}

func allClocks() []bucketing.Clock {
	return []bucketing.Clock{
		bucketing.ClockUTC,
		bucketing.ClockLocal,
		bucketing.ClockMeanSolar,
		bucketing.ClockApparentSolar,
		bucketing.ClockUnequalHours,
	}
}
