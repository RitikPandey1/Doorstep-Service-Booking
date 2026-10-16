package services

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

const noSlotsMessage = "No available slots for this date"

type AvailabilityService struct {
	services    ServiceStore
	zones       ZoneStore
	technicians TechnicianStore
	bookings    BookingStore
	now         func() time.Time
}

func NewAvailabilityService(services ServiceStore, zones ZoneStore, technicians TechnicianStore,
	bookings BookingStore, now func() time.Time) *AvailabilityService {
	return &AvailabilityService{services: services, zones: zones, technicians: technicians, bookings: bookings, now: now}
}

// Get lists the bookable start times (HH:MM, IST) for a service at a pincode on a date.
func (s *AvailabilityService) Get(ctx context.Context, serviceID, pincode, date string) (models.AvailabilityResponse, error) {
	id, err := uuid.Parse(serviceID)
	if err != nil {
		return models.AvailabilityResponse{}, utils.ValidationError("serviceId must be a UUID")
	}
	pin, ok := utils.ParsePincode(pincode)
	if !ok {
		return models.AvailabilityResponse{}, utils.ValidationError("pincode must be a 6-digit Indian pincode")
	}
	day, err := utils.ParseDate(date)
	if err != nil {
		return models.AvailabilityResponse{}, utils.ValidationError("date must be YYYY-MM-DD")
	}

	svc, zone, err := lookupServiceAndZone(ctx, s.services, s.zones, id, pin)
	if err != nil {
		return models.AvailabilityResponse{}, err
	}
	techs, err := s.technicians.ListByZoneAndSkill(ctx, zone.PK, svc.RequiredSkill)
	if err != nil {
		return models.AvailabilityResponse{}, err
	}
	days, err := loadTechnicianDays(ctx, s.bookings, techs, day)
	if err != nil {
		return models.AvailabilityResponse{}, err
	}

	resp := models.AvailabilityResponse{Date: date, ServiceID: id.String(), Pincode: pin, Slots: []string{}}
	for _, slot := range AvailableSlots(days, zone.PK, svc.RequiredSkill, svc.Duration(), day, s.now()) {
		resp.Slots = append(resp.Slots, slot.In(utils.IST).Format("15:04"))
	}
	if len(resp.Slots) == 0 {
		resp.Message = noSlotsMessage
	}
	return resp, nil
}
