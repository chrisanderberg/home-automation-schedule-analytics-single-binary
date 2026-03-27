package domain

import "time"

// BucketSpan describes how much of an interval belongs to one weekly bucket.
type BucketSpan struct {
	Bucket int
	Millis int64
}

// BucketAtUTC maps a UTC timestamp to its weekly five-minute bucket.
func BucketAtUTC(timestampMs int64) (int, error) {
	t := time.UnixMilli(timestampMs).UTC()
	return bucketFromTime(t), nil
}

// BucketAtLocal maps a timestamp to its weekly five-minute bucket in a specific time zone.
func BucketAtLocal(timestampMs int64, loc *time.Location) (int, error) {
	if loc == nil {
		return 0, ErrNilLocation
	}
	t := time.UnixMilli(timestampMs).In(loc)
	return bucketFromTime(t), nil
}

// SplitIntervalUTC breaks a UTC interval into contiguous five-minute bucket spans.
func SplitIntervalUTC(startMs, endMs int64) ([]BucketSpan, error) {
	if endMs <= startMs {
		return nil, ErrInvalidInterval
	}
	var spans []BucketSpan
	cur := startMs
	for cur < endMs {
		bucket, err := BucketAtUTC(cur)
		if err != nil {
			return nil, err
		}
		boundary := nextBoundaryUTC(cur)
		if boundary > endMs {
			boundary = endMs
		}
		millis := boundary - cur
		if millis <= 0 {
			return nil, ErrInvalidInterval
		}
		spans = append(spans, BucketSpan{Bucket: bucket, Millis: millis})
		cur = boundary
	}
	return spans, nil
}

// SplitIntervalLocal breaks a local-time interval into contiguous five-minute bucket spans.
func SplitIntervalLocal(startMs, endMs int64, loc *time.Location) ([]BucketSpan, error) {
	if loc == nil {
		return nil, ErrNilLocation
	}
	if endMs <= startMs {
		return nil, ErrInvalidInterval
	}
	var spans []BucketSpan
	cur := startMs
	for cur < endMs {
		bucket, err := BucketAtLocal(cur, loc)
		if err != nil {
			return nil, err
		}
		boundary := nextBoundaryLocal(cur, loc)
		if boundary > endMs {
			boundary = endMs
		}
		millis := boundary - cur
		if millis <= 0 {
			return nil, ErrInvalidInterval
		}
		// Local bucket spans preserve real DST fold behavior, so repeated local
		// clock labels are still tracked as distinct UTC-backed intervals.
		spans = append(spans, BucketSpan{Bucket: bucket, Millis: millis})
		cur = boundary
	}
	return spans, nil
}

// nextBoundaryUTC returns the next UTC five-minute boundary after a timestamp.
func nextBoundaryUTC(timestampMs int64) int64 {
	t := time.UnixMilli(timestampMs).UTC()
	minute := (t.Minute()/5 + 1) * 5
	hour := t.Hour()
	day := t.Day()
	month := t.Month()
	year := t.Year()
	if minute >= 60 {
		minute = 0
		hour++
		// nextBoundaryUTC relies on time.Date to normalize any hour >= 24 into the next day.
	}
	boundary := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	return boundary.UnixMilli()
}

// nextBoundaryLocal returns the earliest instant after a timestamp that lands in
// a different local-time bucket. This preserves exact local bucket semantics
// across DST folds, where the next bucket can revisit an earlier wall-clock hour.
func nextBoundaryLocal(timestampMs int64, loc *time.Location) int64 {
	bucket, err := BucketAtLocal(timestampMs, loc)
	if err != nil {
		return timestampMs
	}

	low := time.UnixMilli(timestampMs)
	high := low.Add(time.Minute)
	for i := 0; i < 10; i++ {
		nextBucket, err := BucketAtLocal(high.UnixMilli(), loc)
		if err != nil {
			return timestampMs
		}
		if nextBucket != bucket {
			// The boundary search returns the earliest UTC instant that changes the
			// local bucket, which is what preserves exact fold semantics.
			for high.Sub(low) > time.Millisecond {
				mid := low.Add(high.Sub(low) / 2)
				midBucket, err := BucketAtLocal(mid.UnixMilli(), loc)
				if err != nil {
					return timestampMs
				}
				if midBucket == bucket {
					low = mid
					continue
				}
				high = mid
			}
			return high.UnixMilli()
		}
		high = high.Add(time.Minute)
	}
	return timestampMs + int64(5*60*1000)
}

// bucketFromTime converts a wall-clock time into the repository's Monday-based weekly bucket index.
func bucketFromTime(t time.Time) int {
	dayIndex := (int(t.Weekday()) + 6) % 7
	bucketWithinDay := t.Hour()*12 + (t.Minute() / 5)
	return dayIndex*288 + bucketWithinDay
}
