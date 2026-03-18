package domain

import "errors"

var (
	ErrInvalidTimestamp   = errors.New("invalid timestamp")
	ErrNilLocation       = errors.New("location is nil")
	ErrInvalidCoordinates = errors.New("invalid coordinates")
	ErrInvalidInterval   = errors.New("invalid interval")
	ErrInvalidBucket     = errors.New("invalid bucket")
	ErrUndefinedClock    = errors.New("clock mapping undefined")
)
