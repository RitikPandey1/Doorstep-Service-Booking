package domains

import (
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

type Booking struct {
	PK             int64
	BookingID      uuid.UUID
	CustomerPhone  string
	ServicePK      int64
	Pincode        int
	TechnicianPK   int64
	StartTime      time.Time
	EndTime        time.Time
	BlockedUntil   time.Time
	Status         string
	IdempotencyKey *string
	RequestHash    *string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// Read-only fields joined from services/technicians for responses.
	ServiceID      uuid.UUID
	ServiceName    string
	TechnicianID   uuid.UUID
	TechnicianName string
}

// Blocked is the time the booking takes from its technician: the job plus the travel buffer.
func (b Booking) Blocked() Interval {
	return Interval{Start: b.StartTime, End: b.BlockedUntil}
}

func (b Booking) ToResponse() models.BookingResponse {
	return models.BookingResponse{
		ID:          b.BookingID.String(),
		ServiceID:   b.ServiceID.String(),
		ServiceName: b.ServiceName,
		Pincode:     b.Pincode,
		Phone:       b.CustomerPhone,
		StartTime:   utils.FormatDateTime(b.StartTime),
		EndTime:     utils.FormatDateTime(b.EndTime),
		Status:      b.Status,
		Technician:  models.TechnicianRef{ID: b.TechnicianID.String(), Name: b.TechnicianName},
		CreatedAt:   utils.FormatDateTime(b.CreatedAt),
	}
}
