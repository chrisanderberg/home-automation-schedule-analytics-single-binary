package quarter

import "time"

type Segment struct {
	QuarterIndex int
	Start        time.Time
	End          time.Time
}

func Index(ts time.Time) int {
	utc := ts.UTC()
	quarter := int((utc.Month() - 1) / 3)
	return (utc.Year()-1970)*4 + quarter
}

func Split(start, end time.Time) []Segment {
	if !start.Before(end) {
		return nil
	}

	var segments []Segment
	cursor := start
	for cursor.Before(end) {
		next := quarterEnd(cursor)
		if next.After(end) {
			next = end
		}
		segments = append(segments, Segment{
			QuarterIndex: Index(cursor),
			Start:        cursor,
			End:          next,
		})
		cursor = next
	}
	return segments
}

func quarterEnd(ts time.Time) time.Time {
	utc := ts.UTC()
	year := utc.Year()
	month := utc.Month()
	nextMonth := ((int(month)-1)/3)*3 + 4
	if nextMonth > 12 {
		year++
		nextMonth = 1
	}
	return time.Date(year, time.Month(nextMonth), 1, 0, 0, 0, 0, time.UTC)
}
