package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

// ValidateHolding rejects holding payloads that cannot be represented as aggregate updates.
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

// IngestHolding splits one holding interval into quarter-scoped aggregate updates.
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

	// Aggregate rows are quarter-scoped, so longer holding intervals are applied
	// to each affected quarter independently.
	for _, span := range quarterSpans {
		key := storage.AggregateKey{ControlID: input.ControlID, ModelID: input.ModelID, QuarterIndex: span.QuarterIndex}
		if err := applyHoldingQuarter(ctx, db, key, control.NumStates, input.State, span.StartMs, span.EndMs, loc, cfg.Latitude, cfg.Longitude); err != nil {
			return err
		}
	}
	return nil
}

// applyHoldingQuarter adds one quarter-bounded holding interval into every clock view of an aggregate.
func applyHoldingQuarter(ctx context.Context, db *sql.DB, key storage.AggregateKey, numStates int, state int, startMs int64, endMs int64, loc *time.Location, latitude float64, longitude float64) error {
	return storage.UpdateAggregate(ctx, db, key, numStates, func(data []byte) error {
		b, err := domain.NewBlob(numStates)
		if err != nil {
			return err
		}
		copy(b.Data(), data)

		// Each ingest event fans out into every supported clock representation, but
		// all of those counters live in the same quarter-scoped aggregate blob.
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

		meanSolarSpans, err := domain.SplitIntervalMeanSolar(startMs, endMs, latitude, longitude)
		if err != nil {
			return err
		}
		if err := applyHoldingClockSpans(b, numStates, state, domain.ClockMeanSolar, meanSolarSpans); err != nil {
			return err
		}

		apparentSolarSpans, err := domain.SplitIntervalApparentSolar(startMs, endMs, latitude, longitude)
		if err != nil {
			return err
		}
		if err := applyHoldingClockSpans(b, numStates, state, domain.ClockApparentSolar, apparentSolarSpans); err != nil {
			return err
		}

		unequalHoursSpans, err := domain.SplitIntervalUnequalHours(startMs, endMs, latitude, longitude)
		if err != nil {
			if errors.Is(err, domain.ErrUndefinedClock) {
				// Polar-day or polar-night conditions only invalidate the unequal-hours
				// clock; the other clock projections are still preserved.
				logHoldingUndefinedClock(startMs, endMs, latitude, longitude)
			} else {
				return err
			}
		} else if err := applyHoldingClockSpans(b, numStates, state, domain.ClockUnequalHours, unequalHoursSpans); err != nil {
			return err
		}

		copy(data, b.Data())
		return nil
	})
}

// logHoldingUndefinedClock records intervals that cannot be placed on the unequal-hours clock.
func logHoldingUndefinedClock(startMs, endMs int64, latitude, longitude float64) {
	log.Printf(
		"holding ingest skipped unequal-hours spans for undefined clock: startMs=%d endMs=%d latitude=%f longitude=%f clock=%d",
		startMs, endMs, latitude, longitude, domain.ClockUnequalHours,
	)
}

// applyHoldingClockSpans adds a list of bucket durations to one state's counters for a single clock.
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
		// Holding counters saturate instead of overflowing so a bad or repeated
		// ingest event cannot wrap persisted time backward.
		add := uint64(s.Millis)
		if add > math.MaxUint64-v {
			add = math.MaxUint64 - v
		}
		if err := b.SetU64(idx, v+add); err != nil {
			return err
		}
	}
	return nil
}
