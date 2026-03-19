package domain

import "time"

type QuarterSpan struct {
	QuarterIndex int
	StartMs      int64
	EndMs        int64
}

// SplitQuarterIntervalUTC breaks an interval anywhere it crosses UTC quarter boundaries.
func SplitQuarterIntervalUTC(startMs, endMs int64) ([]QuarterSpan, error) {
	if endMs <= startMs {
		return nil, ErrInvalidInterval
	}
	var spans []QuarterSpan
	cur := startMs
	for cur < endMs {
		t := time.UnixMilli(cur).UTC()
		quarterIndex := quarterIndexUTC(t)
		nextBoundary := nextQuarterStartUTC(t)
		boundaryMs := nextBoundary.UnixMilli()
		if boundaryMs > endMs {
			boundaryMs = endMs
		}
		spans = append(spans, QuarterSpan{
			QuarterIndex: quarterIndex,
			StartMs:      cur,
			EndMs:        boundaryMs,
		})
		cur = boundaryMs
	}
	return spans, nil
}

// QuarterIndexUTC converts a timestamp into the repository's quarter numbering scheme.
func QuarterIndexUTC(timestampMs int64) int {
	t := time.UnixMilli(timestampMs).UTC()
	return quarterIndexUTC(t)
}

// quarterIndexUTC converts a UTC time to its zero-based quarter index since 1970.
func quarterIndexUTC(t time.Time) int {
	month := int(t.Month())
	quarterNumber := ((month - 1) / 3) + 1
	return (t.Year()-1970)*4 + (quarterNumber - 1)
}

// nextQuarterStartUTC returns the UTC instant at which the following quarter begins.
func nextQuarterStartUTC(t time.Time) time.Time {
	month := int(t.Month())
	quarter := (month-1)/3 + 1
	year := t.Year()
	nextQuarter := quarter + 1
	if nextQuarter == 5 {
		nextQuarter = 1
		year++
	}
	startMonth := time.Month((nextQuarter-1)*3 + 1)
	return time.Date(year, startMonth, 1, 0, 0, 0, 0, time.UTC)
}
