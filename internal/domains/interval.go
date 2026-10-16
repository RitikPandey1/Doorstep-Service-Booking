package domains

import "time"

// Interval is a half-open time range [Start, End).
type Interval struct {
	Start, End time.Time
}

// Overlaps reports whether the ranges share any instant. Touching ranges (a.End == b.Start) do not overlap.
func (a Interval) Overlaps(b Interval) bool {
	return a.Start.Before(b.End) && b.Start.Before(a.End)
}
