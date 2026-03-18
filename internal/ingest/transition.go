package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func ValidateTransition(input TransitionInput) error {
	if input.ControlID == "" || input.ModelID == "" {
		return ErrInvalidInput
	}
	if input.FromState < 0 || input.ToState < 0 {
		return ErrInvalidInput
	}
	if input.FromState == input.ToState {
		return ErrInvalidInput
	}
	return nil
}

func IngestTransition(ctx context.Context, db *sql.DB, cfg Config, input TransitionInput) error {
	if err := ValidateTransition(input); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}

	control, loc, err := resolveControlAndLocation(ctx, db, cfg, input.ControlID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
		return err
	}
	if input.FromState >= control.NumStates || input.ToState >= control.NumStates {
		return fmt.Errorf("%w: %w", ErrValidation, ErrInvalidInput)
	}

	quarterIndex := domain.QuarterIndexUTC(input.TimestampMs)
	key := storage.AggregateKey{ControlID: input.ControlID, ModelID: input.ModelID, QuarterIndex: quarterIndex}

	return storage.UpdateAggregate(ctx, db, key, control.NumStates, func(data []byte) error {
		b, err := domain.NewBlob(control.NumStates)
		if err != nil {
			return err
		}
		copy(b.Data(), data)

		bucketUTC, err := domain.BucketAtUTC(input.TimestampMs)
		if err != nil {
			return err
		}
		if err := incrementTransitionCount(b, input.FromState, input.ToState, control.NumStates, domain.ClockUTC, bucketUTC); err != nil {
			return err
		}

		bucketLocal, err := domain.BucketAtLocal(input.TimestampMs, loc)
		if err != nil {
			return err
		}
		if err := incrementTransitionCount(b, input.FromState, input.ToState, control.NumStates, domain.ClockLocal, bucketLocal); err != nil {
			return err
		}

		copy(data, b.Data())
		return nil
	})
}

func incrementTransitionCount(b *domain.Blob, fromState int, toState int, numStates int, clock int, bucket int) error {
	idx, err := domain.TransIndex(fromState, toState, clock, bucket, numStates)
	if err != nil {
		return err
	}
	v, err := b.GetU64(idx)
	if err != nil {
		return err
	}
	return b.SetU64(idx, v+1)
}
