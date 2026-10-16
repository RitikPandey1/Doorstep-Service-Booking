package utils

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"9876543210", "9876543210", true},
		{"+919876543210", "9876543210", true},
		{"5876543210", "", false},   // must start with 6-9
		{"987654321", "", false},    // 9 digits
		{"98765432101", "", false},  // 11 digits
		{"919876543210", "", false}, // country code needs +
		{"98765 43210", "", false},
	}
	for _, tc := range tests {
		got, ok := NormalizePhone(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParsePincode(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"110001", 110001, true},
		{"011000", 0, false},
		{"11000", 0, false},
		{"1100011", 0, false},
		{"+11000", 0, false},
		{"abcdef", 0, false},
	}
	for _, tc := range tests {
		got, ok := ParsePincode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParsePincode(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseDateTime(t *testing.T) {
	want := time.Date(2026, 10, 18, 10, 0, 0, 0, IST)
	tests := []struct {
		in string
		ok bool
	}{
		{"2026-10-18T10:00:00+05:30", true},
		{"2026-10-18T04:30:00Z", true},
		{"2026-10-18T10:00:00", true}, // no offset → IST
		{"2026-10-18 10:00", false},
		{"tomorrow", false},
	}
	for _, tc := range tests {
		got, err := ParseDateTime(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("ParseDateTime(%q) err = %v, want ok=%v", tc.in, err, tc.ok)
			continue
		}
		if tc.ok && !got.Equal(want) {
			t.Errorf("ParseDateTime(%q) = %v, want %v", tc.in, got, want)
		}
	}
}

func TestStartOfDayUsesISTCalendar(t *testing.T) {
	// 2026-10-17 20:00 UTC is 2026-10-18 01:30 IST.
	got := StartOfDay(time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 10, 18, 0, 0, 0, 0, IST); !got.Equal(want) {
		t.Errorf("StartOfDay = %v, want %v", got, want)
	}
}

func TestLogHandlerAddsRequestID(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewLogHandler(slog.NewJSONHandler(&buf, nil)))

	log.InfoContext(WithRequestID(context.Background(), "req-42"), "with id")
	log.InfoContext(context.Background(), "without id")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"requestId":"req-42"`) || strings.Contains(lines[1], "requestId") {
		t.Fatalf("log output:\n%s", buf.String())
	}
}
