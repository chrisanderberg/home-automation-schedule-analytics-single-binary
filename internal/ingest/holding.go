package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func ValidateHolding(input HoldingInput) error {
	if input.ControlID == "" || input.ModelID == "" {
		return ErrInvalidInput
	}
	if input.State < 0 {
		return ErrInvalidInput
	}
	if input.EndTimeMs <= input.StartTimeMs {
		return ErrInvalidInput
	}
	return nil
}

func IngestHolding(ctx context.Context, db *sql.DB, cfg Config, input HoldingInput) error {
	if err := ValidateHolding(input); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}

	control, loc, err := resolveControlAndLocation(ctx, db, cfg, input.ControlID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
		return err
	}
	if input.State >= control.NumStates {
		return fmt.Errorf("%w: %w", ErrValidation, ErrInvalidInput)
	}

	quarterSpans, err := domain.SplitQuarterIntervalUTC(input.StartTimeMs, input.EndTimeMs)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidInterval) {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
		return err
	}

	for _, span := range quarterSpans {
		key := storage.AggregateKey{ControlID: input.ControlID, ModelID: input.ModelID, QuarterIndex: span.QuarterIndex}
		if err := applyHoldingQuarter(ctx, db, key, control.NumStates, input.State, span.StartMs, span.EndMs, loc); err != nil {
			return err
		}
	}
	return nil
}

func applyHoldingQuarter(ctx context.Context, db *sql.DB, key storage.AggregateKey, numStates int, state int, startMs int64, endMs int64, loc *time.Location) error {
	return storage.UpdateAggregate(ctx, db, key, numStates, func(data []byte) error {
		b, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		copy(b.Data(), data)

		utcSpans, err := domain.SplitIntervalUTC(startMs, endMs)
		if err != nil {
			return err
		}
		if err := applyHoldingClockSpans(b, numStates, state, domain.ClockUTC, utcSpans); err != nil {
			return err
		}

		localSpans, err := domain.SplitIntervalLocal(startMs, endMs, loc)
		if err != nil {
			return err
		}
		if err := applyHoldingClockSpans(b, numStates, state, domain.ClockLocal, localSpans); err != nil {
			return err
		}

		copy(data, b.Data())
		return nil
	})
}

func applyHoldingClockSpans(b *domain.Blob, numStates int, state int, clock int, spans []domain.BucketSpan) error {
	for _, s := range spans {
		idx, err := domain.HoldIndex(state, clock, s.Bucket, numStates)
		if err != nil {
			return err
		}
		v, err := b.GetU64(idx)
		if err != nil {
			return err
		}
		if s.Millis < 0 {
			return fmt.Errorf("%w: negative holding millis for bucket %d", ErrInvalidInput, s.Bucket)
		}
		if err := b.SetU64(idx, v+uint64(s.Millis)); err != nil {
			return err
		}
	}
	return nil
}
