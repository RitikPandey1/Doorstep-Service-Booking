package services

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
)

// Storage the services depend on; implemented by the repository package and by test fakes.

type ServiceStore interface {
	GetByServiceID(ctx context.Context, id uuid.UUID) (domains.Service, error)
}

type ZoneStore interface {
	GetByPincode(ctx context.Context, pincode int) (domains.Zone, error)
}

type TechnicianStore interface {
	GetByTechnicianID(ctx context.Context, id uuid.UUID) (domains.Technician, error)
	ListByZoneAndSkill(ctx context.Context, zonePK int64, skill string) ([]domains.Technician, error)
}

type BookingStore interface {
	Insert(ctx context.Context, b domains.Booking) (domains.Booking, error)
	GetByBookingID(ctx context.Context, id uuid.UUID) (domains.Booking, error)
	GetByIdempotencyKey(ctx context.Context, key string) (domains.Booking, error)
	ListLiveForTechnicians(ctx context.Context, technicianPKs []int64, from, to time.Time) ([]domains.Booking, error)
	TransitionStatus(ctx context.Context, bookingPK int64, from, to string) (domains.Booking, error)
}
