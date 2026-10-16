package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

type TechnicianService struct {
	technicians TechnicianStore
	bookings    BookingStore
}

func NewTechnicianService(technicians TechnicianStore, bookings BookingStore) *TechnicianService {
	return &TechnicianService{technicians: technicians, bookings: bookings}
}

// Schedule returns a technician's non-cancelled bookings on an IST date, in start order.
func (s *TechnicianService) Schedule(ctx context.Context, technicianID, date string) (models.ScheduleResponse, error) {
	tech, err := s.get(ctx, technicianID)
	if err != nil {
		return models.ScheduleResponse{}, err
	}
	day, err := utils.ParseDate(date)
	if err != nil {
		return models.ScheduleResponse{}, utils.ValidationError("date must be YYYY-MM-DD")
	}
	bookings, err := s.bookings.ListLiveForTechnicians(ctx, []int64{tech.PK}, day, day.Add(24*time.Hour))
	if err != nil {
		return models.ScheduleResponse{}, err
	}
	resp := models.ScheduleResponse{Technician: tech.ToRef(), Date: date, Bookings: []models.BookingResponse{}}
	for _, b := range bookings {
		resp.Bookings = append(resp.Bookings, b.ToResponse())
	}
	return resp, nil
}

func (s *TechnicianService) get(ctx context.Context, id string) (domains.Technician, error) {
	techID, err := uuid.Parse(id)
	if err != nil {
		return domains.Technician{}, utils.NotFoundError("technician not found")
	}
	tech, err := s.technicians.GetByTechnicianID(ctx, techID)
	if errors.Is(err, repository.ErrNotFound) {
		return tech, utils.NotFoundError("technician not found")
	}
	return tech, err
}
