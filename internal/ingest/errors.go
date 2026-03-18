package ingest

import (
	"errors"

	"home-automation-schedule-analytics-single-bin/internal/domain"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrValidation   = errors.New("validation error")
)

func IsValidationError(err error) bool {
	return errors.Is(err, ErrValidation) ||
		errors.Is(err, ErrInvalidInput) ||
		errors.Is(err, storage.ErrNotFound) ||
		errors.Is(err, domain.ErrInvalidInterval) ||
		errors.Is(err, domain.ErrInvalidTimestamp) ||
		errors.Is(err, domain.ErrInvalidCoordinates)
}
