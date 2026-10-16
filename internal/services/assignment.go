package services

// Pure booking rules: no I/O, no clock. Repositories may prefilter in SQL,
// but these functions are the authoritative check.

import (
	"cmp"
	"slices"
	"time"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

// Job is a booking request reduced to what the assignment rules need.
type Job struct {
	ZonePK   int64
	Skill    string
	Start    time.Time
	Duration time.Duration
}

// Window returns the job itself and the time it blocks (job + travel buffer).
func (j Job) Window() (job, blocked domains.Interval) {
	end := j.Start.Add(j.Duration)
	return domains.Interval{Start: j.Start, End: end},
		domains.Interval{Start: j.Start, End: end.Add(utils.TravelBuffer)}
}

// TechnicianDay is a technician plus the blocked intervals of their live
// (non-cancelled) bookings on one IST day, so len(Blocked) is that day's booking count.
type TechnicianDay struct {
	Technician domains.Technician
	Blocked    []domains.Interval
}

// IsSlotAligned reports whether t starts exactly on a 30-minute IST boundary.
func IsSlotAligned(t time.Time) bool {
	return t.Sub(utils.StartOfDay(t))%utils.SlotStep == 0
}

// InBookingWindow reports whether start lies within [now+2h, now+7d].
func InBookingWindow(start, now time.Time) bool {
	return !start.Before(now.Add(utils.MinLeadTime)) && !start.After(now.Add(utils.MaxLeadTime))
}

// CanCancel reports whether a booking starting at start may still be cancelled at now.
func CanCancel(start, now time.Time) bool {
	return !now.After(start.Add(-utils.CancelCutoff))
}

// FitsShift reports whether the job (buffer excluded) lies inside the technician's shift that day.
func FitsShift(t domains.Technician, job domains.Interval) bool {
	day := utils.StartOfDay(job.Start)
	return !job.Start.Before(day.Add(t.ShiftStart)) && !job.End.After(day.Add(t.ShiftEnd))
}

// CanTake applies every per-technician rule: zone, skill, shift, and no overlap including buffer.
func CanTake(d TechnicianDay, j Job) bool {
	t := d.Technician
	if t.ZonePK != j.ZonePK || !t.HasSkill(j.Skill) {
		return false
	}
	job, blocked := j.Window()
	if !FitsShift(t, job) {
		return false
	}
	return !slices.ContainsFunc(d.Blocked, blocked.Overlaps)
}

// RankCandidates returns the technicians who can take the job, fewest bookings that day first,
// ties broken by lowest internal id.
func RankCandidates(days []TechnicianDay, j Job) []domains.Technician {
	var free []TechnicianDay
	for _, d := range days {
		if CanTake(d, j) {
			free = append(free, d)
		}
	}
	slices.SortFunc(free, func(a, b TechnicianDay) int {
		return cmp.Or(cmp.Compare(len(a.Blocked), len(b.Blocked)), cmp.Compare(a.Technician.PK, b.Technician.PK))
	})
	out := make([]domains.Technician, len(free))
	for i, d := range free {
		out[i] = d.Technician
	}
	return out
}

// AvailableSlots returns the slot starts on date's IST day, inside the booking window,
// that at least one technician can take.
func AvailableSlots(days []TechnicianDay, zonePK int64, skill string, duration time.Duration, date, now time.Time) []time.Time {
	day := utils.StartOfDay(date)
	slots := []time.Time{}
	for s := day; s.Before(day.Add(24 * time.Hour)); s = s.Add(utils.SlotStep) {
		if !InBookingWindow(s, now) {
			continue
		}
		j := Job{ZonePK: zonePK, Skill: skill, Start: s, Duration: duration}
		if slices.ContainsFunc(days, func(d TechnicianDay) bool { return CanTake(d, j) }) {
			slots = append(slots, s)
		}
	}
	return slots
}
