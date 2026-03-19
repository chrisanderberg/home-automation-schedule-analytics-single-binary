package domain

import (
	"errors"
	"math"
	"time"
)

// durationFromOffsetMinutes converts a floating-point minute offset into a time.Duration.
func durationFromOffsetMinutes(offsetMinutes float64) time.Duration {
	return time.Duration(offsetMinutes * float64(time.Minute))
}

// validateCoordinates rejects coordinates that are out of range or non-finite.
func validateCoordinates(latitude, longitude float64) error {
	if math.IsNaN(latitude) || math.IsNaN(longitude) || math.IsInf(latitude, 0) || math.IsInf(longitude, 0) {
		return ErrInvalidCoordinates
	}
	if latitude > 90 || latitude < -90 || longitude > 180 || longitude < -180 {
		return ErrInvalidCoordinates
	}
	return nil
}

// BucketAtMeanSolar maps a timestamp to a bucket using longitude-adjusted mean solar time.
func BucketAtMeanSolar(timestampMs int64, latitude, longitude float64) (int, error) {
	if err := validateCoordinates(latitude, longitude); err != nil {
		return 0, err
	}
	offsetMinutes := longitude * 4
	adj := time.UnixMilli(timestampMs).UTC().Add(durationFromOffsetMinutes(offsetMinutes))
	return bucketFromTime(adj), nil
}

// BucketAtApparentSolar maps a timestamp to a bucket using apparent solar time.
func BucketAtApparentSolar(timestampMs int64, latitude, longitude float64) (int, error) {
	if err := validateCoordinates(latitude, longitude); err != nil {
		return 0, err
	}
	offsetMinutes := longitude*4 + equationOfTimeMinutes(time.UnixMilli(timestampMs).UTC())
	adj := time.UnixMilli(timestampMs).UTC().Add(durationFromOffsetMinutes(offsetMinutes))
	return bucketFromTime(adj), nil
}

// BucketAtUnequalHours maps a timestamp to a bucket on the unequal-hours clock.
func BucketAtUnequalHours(timestampMs int64, latitude, longitude float64) (int, error) {
	if err := validateCoordinates(latitude, longitude); err != nil {
		return 0, err
	}

	eqTime := equationOfTimeMinutes(time.UnixMilli(timestampMs).UTC())
	offsetMinutes := longitude*4 + eqTime

	adj := time.UnixMilli(timestampMs).UTC().Add(durationFromOffsetMinutes(offsetMinutes))
	dayTime := time.Date(adj.Year(), adj.Month(), adj.Day(), 0, 0, 0, 0, time.UTC)
	solarMinutes := (adj.Sub(dayTime)).Minutes()
	if solarMinutes < 0 {
		solarMinutes += 1440
	}

	sunrise, sunset, err := sunriseSunsetSolarMinutes(dayTime, latitude)
	if err != nil {
		return 0, err
	}
	if solarMinutes < 0 || solarMinutes >= 1440 {
		return 0, ErrInvalidTimestamp
	}

	dayLength := sunset - sunrise
	nightLength := 1440 - dayLength
	if dayLength <= 0 || nightLength <= 0 {
		return 0, ErrUndefinedClock
	}

	var pseudoMinutes float64
	if solarMinutes >= sunrise && solarMinutes < sunset {
		dayFraction := (solarMinutes - sunrise) / dayLength
		pseudoMinutes = 360 + dayFraction*720
	} else {
		var nightFraction float64
		if solarMinutes >= sunset {
			nightFraction = (solarMinutes - sunset) / nightLength
		} else {
			nightFraction = (solarMinutes + 1440 - sunset) / nightLength
		}
		pseudoMinutes = 1080 + nightFraction*720
		if pseudoMinutes >= 1440 {
			pseudoMinutes -= 1440
		}
	}

	bucketWithinDay := int(pseudoMinutes) / 5
	dayIndex := (int(adj.Weekday()) + 6) % 7
	return dayIndex*288 + bucketWithinDay, nil
}

// SplitIntervalMeanSolar breaks an interval into mean-solar bucket spans.
func SplitIntervalMeanSolar(startMs, endMs int64, latitude, longitude float64) ([]BucketSpan, error) {
	if err := validateCoordinates(latitude, longitude); err != nil {
		return nil, err
	}
	return splitIntervalWithOffset(startMs, endMs, func(ts int64) float64 {
		return longitude * 4
	})
}

// SplitIntervalApparentSolar breaks an interval into apparent-solar bucket spans.
func SplitIntervalApparentSolar(startMs, endMs int64, latitude, longitude float64) ([]BucketSpan, error) {
	if err := validateCoordinates(latitude, longitude); err != nil {
		return nil, err
	}
	return splitIntervalWithOffset(startMs, endMs, func(ts int64) float64 {
		return longitude*4 + equationOfTimeMinutes(time.UnixMilli(ts).UTC())
	})
}

// SplitIntervalUnequalHours breaks an interval into unequal-hours bucket spans.
func SplitIntervalUnequalHours(startMs, endMs int64, latitude, longitude float64) ([]BucketSpan, error) {
	if err := validateCoordinates(latitude, longitude); err != nil {
		return nil, err
	}
	if endMs <= startMs {
		return nil, ErrInvalidInterval
	}
	var spans []BucketSpan
	cur := startMs
	for cur < endMs {
		bucket, err := BucketAtUnequalHours(cur, latitude, longitude)
		if err != nil {
			if errors.Is(err, ErrUndefinedClock) {
				return nil, ErrUndefinedClock
			}
			return nil, err
		}
		boundary, err := nextUnequalBoundary(cur, latitude, longitude)
		if err != nil {
			return nil, err
		}
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

// unequalHoursBoundary finds the first instant after t that leaves the current unequal-hours bucket.
func unequalHoursBoundary(t time.Time, latitude, longitude float64) (time.Time, error) {
	bucket, err := BucketAtUnequalHours(t.UnixMilli(), latitude, longitude)
	if err != nil {
		return time.Time{}, err
	}
	lastSameBucket := t
	nextBucketStart := t.Add(1 * time.Minute)
	for i := 0; i < 400; i++ {
		b, err := BucketAtUnequalHours(nextBucketStart.UnixMilli(), latitude, longitude)
		if err != nil {
			return time.Time{}, err
		}
		if b != bucket {
			low := lastSameBucket
			high := nextBucketStart
			for high.Sub(low) > time.Millisecond {
				mid := low.Add(high.Sub(low) / 2)
				midBucket, err := BucketAtUnequalHours(mid.UnixMilli(), latitude, longitude)
				if err != nil {
					return time.Time{}, err
				}
				if midBucket == bucket {
					low = mid
					continue
				}
				high = mid
			}
			if !high.After(t) {
				return time.Time{}, ErrInvalidInterval
			}
			return high, nil
		}
		lastSameBucket = nextBucketStart
		nextBucketStart = nextBucketStart.Add(1 * time.Minute)
	}
	return time.Time{}, ErrInvalidInterval
}

// nextUnequalBoundary returns the next unequal-hours bucket boundary after a timestamp.
func nextUnequalBoundary(timestampMs int64, latitude, longitude float64) (int64, error) {
	cur := time.UnixMilli(timestampMs).UTC()
	boundary, err := unequalHoursBoundary(cur, latitude, longitude)
	if err != nil {
		return 0, err
	}
	if boundary.UnixMilli() <= timestampMs {
		return timestampMs + int64(5*60*1000), nil
	}
	return boundary.UnixMilli(), nil
}

// splitIntervalWithOffset breaks an interval using a time-varying offset applied before bucket lookup.
func splitIntervalWithOffset(startMs, endMs int64, offsetMinutes func(int64) float64) ([]BucketSpan, error) {
	if endMs <= startMs {
		return nil, ErrInvalidInterval
	}
	var spans []BucketSpan
	cur := startMs
	for cur < endMs {
		offset := offsetMinutes(cur)
		adj := time.UnixMilli(cur).UTC().Add(durationFromOffsetMinutes(offset))
		bucket := bucketFromTime(adj)
		adjBoundary := nextBoundaryUTC(adj.UnixMilli())
		boundaryUTC := time.UnixMilli(adjBoundary).Add(-durationFromOffsetMinutes(offset)).UnixMilli()
		if boundaryUTC <= cur {
			boundaryUTC = cur + int64(5*60*1000)
		}
		if boundaryUTC > endMs {
			boundaryUTC = endMs
		}
		millis := boundaryUTC - cur
		if millis <= 0 {
			return nil, ErrInvalidInterval
		}
		spans = append(spans, BucketSpan{Bucket: bucket, Millis: millis})
		cur = boundaryUTC
	}
	return spans, nil
}

// equationOfTimeMinutes approximates the apparent-minus-mean-solar time offset for a day.
func equationOfTimeMinutes(day time.Time) float64 {
	gamma := fractionalYear(day)
	return 229.18 * (0.000075 + 0.001868*math.Cos(gamma) - 0.032077*math.Sin(gamma) - 0.014615*math.Cos(2*gamma) - 0.040849*math.Sin(2*gamma))
}

// solarDeclination approximates the sun's declination angle for a day of year.
func solarDeclination(day time.Time) float64 {
	gamma := fractionalYear(day)
	return 0.006918 - 0.399912*math.Cos(gamma) + 0.070257*math.Sin(gamma) - 0.006758*math.Cos(2*gamma) + 0.000907*math.Sin(2*gamma) - 0.002697*math.Cos(3*gamma) + 0.00148*math.Sin(3*gamma)
}

// fractionalYear converts a date into the normalized annual angle used by the solar formulas.
func fractionalYear(day time.Time) float64 {
	yday := day.YearDay()
	return 2 * math.Pi / 365 * (float64(yday-1) + (float64(day.Hour())-12)/24)
}

// sunriseSunsetSolarMinutes estimates sunrise and sunset in solar minutes for the given day and latitude.
func sunriseSunsetSolarMinutes(day time.Time, latitude float64) (float64, float64, error) {
	decl := solarDeclination(day)
	latRad := latitude * math.Pi / 180
	solarZenith := 90.833 * math.Pi / 180

	cosH := (math.Cos(solarZenith)/(math.Cos(latRad)*math.Cos(decl)) - math.Tan(latRad)*math.Tan(decl))
	if cosH > 1 || cosH < -1 {
		return 0, 0, ErrUndefinedClock
	}
	H := math.Acos(cosH)
	Hdeg := H * 180 / math.Pi
	sunrise := 720 - 4*Hdeg
	sunset := 720 + 4*Hdeg
	return sunrise, sunset, nil
}
