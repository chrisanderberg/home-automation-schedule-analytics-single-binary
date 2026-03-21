package bucketing

import (
	"fmt"
	"math"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/domain/blob"
)

type Clock int

const (
	ClockUTC Clock = iota
	ClockLocal
	ClockMeanSolar
	ClockApparentSolar
	ClockUnequalHours
)

type Config struct {
	Location  *time.Location
	Latitude  float64
	Longitude float64
}

type Segment struct {
	Bucket int
	Span   time.Duration
}

type Engine struct {
	cfg Config
}

func New(cfg Config) (*Engine, error) {
	if cfg.Location == nil {
		return nil, fmt.Errorf("location is required")
	}
	if cfg.Latitude < -90 || cfg.Latitude > 90 {
		return nil, fmt.Errorf("latitude out of range")
	}
	if cfg.Longitude < -180 || cfg.Longitude > 180 {
		return nil, fmt.Errorf("longitude out of range")
	}
	return &Engine{cfg: cfg}, nil
}

func (e *Engine) HoldingSegments(clock Clock, start, end time.Time) ([]Segment, error) {
	if !start.Before(end) {
		return nil, fmt.Errorf("start must be before end")
	}
	switch clock {
	case ClockUTC, ClockLocal, ClockMeanSolar, ClockApparentSolar:
		return e.fixedSegments(clock, start, end)
	case ClockUnequalHours:
		return e.unequalSegments(start, end)
	default:
		return nil, fmt.Errorf("unknown clock")
	}
}

func (e *Engine) TransitionBucket(clock Clock, ts time.Time) (int, error) {
	switch clock {
	case ClockUTC, ClockLocal, ClockMeanSolar, ClockApparentSolar:
		shifted := e.clockTime(clock, ts)
		return weekBucket(shifted), nil
	case ClockUnequalHours:
		return e.unequalBucket(ts)
	default:
		return 0, fmt.Errorf("unknown clock")
	}
}

func (e *Engine) fixedSegments(clock Clock, start, end time.Time) ([]Segment, error) {
	cursor := start
	var out []Segment
	for cursor.Before(end) {
		shifted := e.clockTime(clock, cursor)
		boundary := fixedBucketBoundary(shifted).Add(5 * time.Minute)
		step := boundary.Sub(shifted)
		if step <= 0 {
			step = time.Millisecond
		}
		next := cursor.Add(step)
		if next.After(end) {
			next = end
		}
		out = append(out, Segment{
			Bucket: weekBucket(shifted),
			Span:   next.Sub(cursor),
		})
		cursor = next
	}
	return out, nil
}

func (e *Engine) unequalSegments(start, end time.Time) ([]Segment, error) {
	cursor := start
	var out []Segment
	for cursor.Before(end) {
		bucket, boundary, err := e.unequalBucketAndBoundary(cursor)
		if err != nil {
			return nil, err
		}
		next := boundary
		if next.After(end) {
			next = end
		}
		out = append(out, Segment{
			Bucket: bucket,
			Span:   next.Sub(cursor),
		})
		cursor = next
	}
	return out, nil
}

func fixedBucketBoundary(ts time.Time) time.Time {
	return ts.Truncate(5 * time.Minute)
}

func weekBucket(ts time.Time) int {
	weekday := (int(ts.Weekday()) + 6) % 7
	return weekday*blob.BucketsPerDay + (ts.Hour()*12 + ts.Minute()/5)
}

func (e *Engine) clockTime(clock Clock, ts time.Time) time.Time {
	switch clock {
	case ClockUTC:
		return ts.UTC()
	case ClockLocal:
		return ts.In(e.cfg.Location)
	case ClockMeanSolar:
		return ts.UTC().Add(meanSolarOffset(e.cfg.Longitude))
	case ClockApparentSolar:
		utc := ts.UTC()
		return utc.Add(meanSolarOffset(e.cfg.Longitude)).Add(equationOfTimeDuration(utc))
	default:
		return ts
	}
}

func meanSolarOffset(longitude float64) time.Duration {
	return time.Duration(longitude*240) * time.Second
}

// NOAA's solar calculator uses this equation-of-time approximation; we keep the
// result as a duration so downstream sunrise/sunset math stays in Go time units.
func equationOfTimeDuration(ts time.Time) time.Duration {
	gamma := fractionalYear(ts)
	minutes := 229.18 * (0.000075 + 0.001868*math.Cos(gamma) - 0.032077*math.Sin(gamma) - 0.014615*math.Cos(2*gamma) - 0.040849*math.Sin(2*gamma))
	return time.Duration(minutes * float64(time.Minute))
}

func solarDeclination(ts time.Time) float64 {
	gamma := fractionalYear(ts)
	return 0.006918 - 0.399912*math.Cos(gamma) + 0.070257*math.Sin(gamma) - 0.006758*math.Cos(2*gamma) + 0.000907*math.Sin(2*gamma) - 0.002697*math.Cos(3*gamma) + 0.00148*math.Sin(3*gamma)
}

func fractionalYear(ts time.Time) float64 {
	utc := ts.UTC()
	day := float64(utc.YearDay() - 1)
	hour := float64(utc.Hour()) + float64(utc.Minute())/60 + float64(utc.Second())/3600
	return 2 * math.Pi / 365 * (day + (hour-12)/24)
}

func (e *Engine) unequalBucket(ts time.Time) (int, error) {
	bucket, _, err := e.unequalBucketAndBoundary(ts)
	return bucket, err
}

func (e *Engine) unequalBucketAndBoundary(ts time.Time) (int, time.Time, error) {
	local := ts.In(e.cfg.Location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, e.cfg.Location)

	sunrise, sunset, err := e.sunTimes(dayStart)
	if err != nil {
		return 0, time.Time{}, err
	}
	prevStart := dayStart.AddDate(0, 0, -1)
	nextStart := dayStart.AddDate(0, 0, 1)
	_, prevSunset, err := e.sunTimes(prevStart)
	if err != nil {
		return 0, time.Time{}, err
	}
	nextSunrise, _, err := e.sunTimes(nextStart)
	if err != nil {
		return 0, time.Time{}, err
	}

	weekday := (int(local.Weekday()) + 6) % 7

	if !local.Before(sunrise) && local.Before(sunset) {
		dayStep := sunset.Sub(sunrise) / 144
		index := int(local.Sub(sunrise) / dayStep)
		if index >= 144 {
			index = 143
		}
		boundary := sunrise.Add(time.Duration(index+1) * dayStep)
		return weekday*blob.BucketsPerDay + index, boundary.UTC(), nil
	}

	var nightStart time.Time
	var nextBoundaryBase time.Time
	if local.Before(sunrise) {
		nightStart = prevSunset
		nextBoundaryBase = sunrise
	} else {
		nightStart = sunset
		nextBoundaryBase = nextSunrise
	}

	nightStep := nextBoundaryBase.Sub(nightStart) / 144
	index := int(local.Sub(nightStart) / nightStep)
	if index < 0 {
		index = 0
	}
	if index >= 144 {
		index = 143
	}
	boundary := nightStart.Add(time.Duration(index+1) * nightStep)
	bucket := 144 + index
	return weekday*blob.BucketsPerDay + bucket, boundary.UTC(), nil
}

func (e *Engine) sunTimes(dayStart time.Time) (time.Time, time.Time, error) {
	noon := dayStart.Add(12 * time.Hour)
	decl := solarDeclination(noon)
	latRad := e.cfg.Latitude * math.Pi / 180
	cosZenith := math.Cos(90.833 * math.Pi / 180)
	cosH := (cosZenith / (math.Cos(latRad) * math.Cos(decl))) - math.Tan(latRad)*math.Tan(decl)
	if cosH < -1 {
		cosH = -1
	}
	if cosH > 1 {
		cosH = 1
	}
	hourAngle := math.Acos(cosH) * 180 / math.Pi
	_, offsetSeconds := noon.Zone()
	offsetMinutes := float64(offsetSeconds) / 60
	eqMinutes := equationOfTimeDuration(noon).Minutes()
	solarNoonMinutes := 720 - 4*e.cfg.Longitude - eqMinutes + offsetMinutes
	sunrise := dayStart.Add(time.Duration((solarNoonMinutes - hourAngle*4) * float64(time.Minute)))
	sunset := dayStart.Add(time.Duration((solarNoonMinutes + hourAngle*4) * float64(time.Minute)))
	if !sunrise.Before(sunset) {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid sunrise/sunset for date %s", dayStart.Format("2006-01-02"))
	}
	return sunrise, sunset, nil
}
