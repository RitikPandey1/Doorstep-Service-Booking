package utils

import "time"

// Booking statuses.
const (
	StatusConfirmed = "CONFIRMED"
	StatusCancelled = "CANCELLED"
	StatusCompleted = "COMPLETED"
)

// Technician skills / service required skills.
const (
	SkillBatteryFit  = "BATTERY_FIT"
	SkillJumpstart   = "JUMPSTART"
	SkillDiagnostics = "DIAGNOSTICS"
)

// Booking rules.
const (
	SlotStep     = 30 * time.Minute   // slots start on 30-minute boundaries
	TravelBuffer = 30 * time.Minute   // gap a technician needs after each job
	MinLeadTime  = 2 * time.Hour      // earliest booking start from now
	MaxLeadTime  = 7 * 24 * time.Hour // latest booking start from now
	CancelCutoff = time.Hour          // cancellation allowed until start - CancelCutoff
)
