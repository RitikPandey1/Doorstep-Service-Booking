package services

import (
	"testing"
	"time"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

const (
	zoneNorth int64 = 1
	zoneSouth int64 = 2
	hour            = time.Hour
	min30           = 30 * time.Minute
)

var (
	allSkills = []string{utils.SkillBatteryFit, utils.SkillJumpstart, utils.SkillDiagnostics}
	ravi      = domains.Technician{PK: 1, Name: "Ravi", ZonePK: zoneNorth, Skills: allSkills, ShiftStart: 8 * hour, ShiftEnd: 20 * hour}
	amit      = domains.Technician{PK: 2, Name: "Amit", ZonePK: zoneNorth, Skills: []string{utils.SkillBatteryFit}, ShiftStart: 8 * hour, ShiftEnd: 20 * hour}
	suresh    = domains.Technician{PK: 3, Name: "Suresh", ZonePK: zoneNorth, Skills: []string{utils.SkillJumpstart, utils.SkillDiagnostics}, ShiftStart: 8 * hour, ShiftEnd: 20 * hour}
)

// at returns hh:mm IST on 2026-10-18.
func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-18 "+hhmm, utils.IST)
	if err != nil {
		panic(err)
	}
	return t
}

// booked is the blocked interval of an existing job (job + 30m buffer).
func booked(start string, d time.Duration) domains.Interval {
	return domains.Interval{Start: at(start), End: at(start).Add(d + utils.TravelBuffer)}
}

func battery(start string) Job {
	return Job{ZonePK: zoneNorth, Skill: utils.SkillBatteryFit, Start: at(start), Duration: hour}
}

func TestIntervalOverlaps(t *testing.T) {
	iv := func(a, b string) domains.Interval { return domains.Interval{Start: at(a), End: at(b)} }
	tests := []struct {
		name string
		a, b domains.Interval
		want bool
	}{
		{"disjoint", iv("08:00", "09:00"), iv("10:00", "11:00"), false},
		{"touching is not overlap", iv("08:00", "10:00"), iv("10:00", "11:00"), false},
		{"partial overlap", iv("08:00", "10:30"), iv("10:00", "11:00"), true},
		{"contained", iv("10:15", "10:45"), iv("10:00", "11:00"), true},
		{"identical", iv("10:00", "11:00"), iv("10:00", "11:00"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Overlaps(tc.b); got != tc.want {
				t.Errorf("a.Overlaps(b) = %v, want %v", got, tc.want)
			}
			if got := tc.b.Overlaps(tc.a); got != tc.want {
				t.Errorf("b.Overlaps(a) = %v, want %v (must be symmetric)", got, tc.want)
			}
		})
	}
}

func TestCanTake(t *testing.T) {
	// Existing 60-min job 10:00-11:00 blocks Ravi until 11:30.
	busy := TechnicianDay{Technician: ravi, Blocked: []domains.Interval{booked("10:00", hour)}}
	free := TechnicianDay{Technician: ravi}

	tests := []struct {
		name string
		day  TechnicianDay
		job  Job
		want bool
	}{
		// zone and skill
		{"wrong zone", free, Job{ZonePK: zoneSouth, Skill: utils.SkillBatteryFit, Start: at("10:00"), Duration: hour}, false},
		{"missing skill", TechnicianDay{Technician: amit}, Job{ZonePK: zoneNorth, Skill: utils.SkillJumpstart, Start: at("10:00"), Duration: min30}, false},
		{"matching skill", TechnicianDay{Technician: suresh}, Job{ZonePK: zoneNorth, Skill: utils.SkillJumpstart, Start: at("10:00"), Duration: min30}, true},

		// shift edges
		{"first job at shift start, no buffer before", free, battery("08:00"), true},
		{"starts before shift", free, battery("07:30"), false},
		{"ends exactly at shift end, buffer spills past", free, battery("19:00"), true},
		{"ends after shift end", free, battery("19:30"), false},
		{"30-min job in last slot", free, Job{ZonePK: zoneNorth, Skill: utils.SkillJumpstart, Start: at("19:30"), Duration: min30}, true},

		// overlap and buffer after an existing job (10:00-11:00, blocked to 11:30)
		{"same start", busy, battery("10:00"), false},
		{"starts during existing job", busy, battery("10:30"), false},
		{"starts inside existing buffer", busy, battery("11:00"), false},
		{"starts when buffer ends", busy, battery("11:30"), true},

		// buffer before an existing job: new job's own buffer must end by 10:00
		{"new buffer runs into existing job", busy, battery("09:00"), false},
		{"new buffer ends exactly at existing start", busy, battery("08:30"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanTake(tc.day, tc.job); got != tc.want {
				t.Errorf("CanTake = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsSlotAligned(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"on the hour", at("10:00"), true},
		{"half past", at("10:30"), true},
		{"quarter past", at("10:15"), false},
		{"one second off", at("10:00").Add(time.Second), false},
		{"UTC input on IST boundary", time.Date(2026, 10, 18, 4, 30, 0, 0, time.UTC), true}, // 10:00 IST
		{"UTC hour that is :30 IST", time.Date(2026, 10, 18, 5, 0, 0, 0, time.UTC), true},   // 10:30 IST
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSlotAligned(tc.t); got != tc.want {
				t.Errorf("IsSlotAligned(%v) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func TestInBookingWindow(t *testing.T) {
	now := at("08:00")
	tests := []struct {
		name  string
		start time.Time
		want  bool
	}{
		{"exactly 2h ahead", now.Add(2 * hour), true},
		{"just under 2h", now.Add(2*hour - time.Minute), false},
		{"in the past", now.Add(-hour), false},
		{"exactly 7 days ahead", now.Add(7 * 24 * hour), true},
		{"just over 7 days", now.Add(7*24*hour + time.Minute), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := InBookingWindow(tc.start, now); got != tc.want {
				t.Errorf("InBookingWindow = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanCancel(t *testing.T) {
	start := at("12:00")
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"well before", at("09:00"), true},
		{"exactly 1h before", at("11:00"), true},
		{"59 minutes before", at("11:01"), false},
		{"after start", at("12:30"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanCancel(start, tc.now); got != tc.want {
				t.Errorf("CanCancel = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRankCandidates(t *testing.T) {
	job := battery("15:00")
	tests := []struct {
		name string
		days []TechnicianDay
		want []int64
	}{
		{
			name: "fewest bookings first",
			days: []TechnicianDay{
				{Technician: ravi, Blocked: []domains.Interval{booked("08:00", hour), booked("10:00", hour)}},
				{Technician: amit, Blocked: []domains.Interval{booked("08:00", hour)}},
			},
			want: []int64{2, 1},
		},
		{
			name: "tie broken by lowest id",
			days: []TechnicianDay{{Technician: amit}, {Technician: ravi}},
			want: []int64{1, 2},
		},
		{
			name: "busy and unqualified technicians excluded",
			days: []TechnicianDay{
				{Technician: ravi, Blocked: []domains.Interval{booked("15:00", hour)}},
				{Technician: amit},
				{Technician: suresh}, // no BATTERY_FIT
			},
			want: []int64{2},
		},
		{
			name: "nobody free",
			days: []TechnicianDay{{Technician: ravi, Blocked: []domains.Interval{booked("14:30", hour)}}},
			want: []int64{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RankCandidates(tc.days, job)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d candidates, want %d", len(got), len(tc.want))
			}
			for i, tech := range got {
				if tech.PK != tc.want[i] {
					t.Errorf("candidate[%d] = %d, want %d", i, tech.PK, tc.want[i])
				}
			}
		})
	}
}

func TestAvailableSlots(t *testing.T) {
	days := []TechnicianDay{{Technician: ravi, Blocked: []domains.Interval{booked("10:00", hour)}}}
	format := func(ts []time.Time) []string {
		out := make([]string, len(ts))
		for i, s := range ts {
			out[i] = s.In(utils.IST).Format("15:04")
		}
		return out
	}

	t.Run("window, shift, overlap and buffer applied", func(t *testing.T) {
		now := at("07:00") // earliest bookable start is 09:00
		got := format(AvailableSlots(days, zoneNorth, utils.SkillBatteryFit, hour, at("00:00"), now))
		// 09:00 and 09:30 clash with the 10:00 job, 10:00-11:00 are taken/buffer, last 60-min slot is 19:00.
		want := []string{"11:30", "12:00", "12:30", "13:00", "13:30", "14:00", "14:30", "15:00",
			"15:30", "16:00", "16:30", "17:00", "17:30", "18:00", "18:30", "19:00"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	})

	t.Run("date beyond 7 days is empty", func(t *testing.T) {
		now := at("07:00").Add(-8 * 24 * hour)
		if got := AvailableSlots(days, zoneNorth, utils.SkillBatteryFit, hour, at("00:00"), now); len(got) != 0 {
			t.Errorf("got %v, want none", format(got))
		}
	})

	t.Run("no qualified technician is empty", func(t *testing.T) {
		now := at("07:00")
		if got := AvailableSlots(days, zoneSouth, utils.SkillBatteryFit, hour, at("00:00"), now); len(got) != 0 {
			t.Errorf("got %v, want none", format(got))
		}
	})
}
