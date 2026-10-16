package utils

import "time"

// IST is the single time zone of the system. A fixed zone avoids depending on tzdata in the image.
var IST = time.FixedZone("IST", 5*60*60+30*60)

// ParseDate parses YYYY-MM-DD as midnight IST.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation(time.DateOnly, s, IST)
}

// ParseDateTime parses ISO-8601 with an offset, or without one (treated as IST). Result is in IST.
func ParseDateTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(IST), nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05", s, IST)
}

// FormatDateTime renders t as RFC 3339 in IST, e.g. 2026-10-18T10:00:00+05:30.
func FormatDateTime(t time.Time) string {
	return t.In(IST).Format(time.RFC3339)
}

// StartOfDay returns midnight IST of the IST calendar day containing t.
func StartOfDay(t time.Time) time.Time {
	y, m, d := t.In(IST).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, IST)
}
