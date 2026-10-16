package domains

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
)

type Service struct {
	PK            int64
	ServiceID     uuid.UUID
	Name          string
	DurationMin   int
	RequiredSkill string
}

func (s Service) Duration() time.Duration {
	return time.Duration(s.DurationMin) * time.Minute
}

type Zone struct {
	PK     int64
	ZoneID uuid.UUID
	Name   string
}

type Technician struct {
	PK           int64
	TechnicianID uuid.UUID
	Name         string
	ZonePK       int64
	Skills       []string
	// Shift bounds as offsets from midnight IST.
	ShiftStart time.Duration
	ShiftEnd   time.Duration
}

func (t Technician) HasSkill(skill string) bool {
	return slices.Contains(t.Skills, skill)
}

func (t Technician) ToRef() models.TechnicianRef {
	return models.TechnicianRef{ID: t.TechnicianID.String(), Name: t.Name}
}
