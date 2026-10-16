package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/lock"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

// errTechnicianTaken means the candidate was booked by someone else after ranking.
var errTechnicianTaken = errors.New("technician no longer free")

type BookingService struct {
	services    ServiceStore
	zones       ZoneStore
	technicians TechnicianStore
	bookings    BookingStore
	locker      lock.Locker
	now         func() time.Time
}

func NewBookingService(services ServiceStore, zones ZoneStore, technicians TechnicianStore,
	bookings BookingStore, locker lock.Locker, now func() time.Time) *BookingService {
	return &BookingService{services: services, zones: zones, technicians: technicians,
		bookings: bookings, locker: locker, now: now}
}

// bookingInput is a parsed, normalised create request.
type bookingInput struct {
	serviceID uuid.UUID
	pincode   int
	start     time.Time
	phone     string
	key       string // Idempotency-Key, "" when absent
	hash      string // fingerprint of the normalised request, compared on key reuse
}

func parseCreate(req models.CreateBookingRequest, key string) (bookingInput, error) {
	serviceID, err := uuid.Parse(req.ServiceID)
	if err != nil {
		return bookingInput{}, utils.ValidationError("serviceId must be a UUID")
	}
	if !utils.ValidPincode(req.Pincode) {
		return bookingInput{}, utils.ValidationError("pincode must be a 6-digit Indian pincode")
	}
	phone, ok := utils.NormalizePhone(req.Phone)
	if !ok {
		return bookingInput{}, utils.ValidationError("phone must be a 10-digit Indian mobile number, optionally prefixed with +91")
	}
	start, err := utils.ParseDateTime(req.StartTime)
	if err != nil {
		return bookingInput{}, utils.ValidationError("startTime must be ISO-8601, e.g. 2026-10-18T10:00:00+05:30")
	}
	if len(key) > utils.MaxIdempotencyKeyLen {
		return bookingInput{}, utils.ValidationError("Idempotency-Key must be at most 255 characters")
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%d|%s|%s", serviceID, req.Pincode, start.UTC().Format(time.RFC3339), phone))
	return bookingInput{serviceID: serviceID, pincode: req.Pincode, start: start, phone: phone,
		key: key, hash: hex.EncodeToString(sum[:])}, nil
}

// Create books the least-loaded free qualified technician. With an Idempotency-Key,
// a repeat of the same request returns the original booking instead of a new one.
func (s *BookingService) Create(ctx context.Context, req models.CreateBookingRequest, key string) (domains.Booking, error) {
	in, err := parseCreate(req, key)
	if err != nil {
		return domains.Booking{}, err
	}
	// Replay before time-based checks: a retry must succeed even after the booking window moved on.
	if b, ok, err := s.replay(ctx, in); err != nil || ok {
		return b, err
	}
	if !IsSlotAligned(in.start) {
		return domains.Booking{}, utils.ValidationError("startTime must be on a 30-minute boundary")
	}
	if !InBookingWindow(in.start, s.now()) {
		return domains.Booking{}, utils.ValidationError("startTime must be between 2 hours and 7 days from now")
	}

	svc, zone, err := lookupServiceAndZone(ctx, s.services, s.zones, in.serviceID, in.pincode)
	if err != nil {
		return domains.Booking{}, err
	}
	job := Job{ZonePK: zone.PK, Skill: svc.RequiredSkill, Start: in.start, Duration: svc.Duration()}
	techs, err := s.technicians.ListByZoneAndSkill(ctx, zone.PK, svc.RequiredSkill)
	if err != nil {
		return domains.Booking{}, err
	}
	days, err := loadTechnicianDays(ctx, s.bookings, techs, in.start)
	if err != nil {
		return domains.Booking{}, err
	}

	for _, tech := range RankCandidates(days, job) {
		b, err := s.tryBook(ctx, tech, svc, job, in)
		if errors.Is(err, errTechnicianTaken) {
			continue
		}
		if errors.Is(err, repository.ErrDuplicateIdempotencyKey) {
			break // a concurrent request with our key won; replay it below
		}
		if err != nil {
			return domains.Booking{}, lockError(err)
		}
		return b, nil
	}

	// A concurrent request with the same key may have taken the last free technician.
	if b, ok, err := s.replay(ctx, in); err != nil || ok {
		return b, err
	}
	return domains.Booking{}, utils.NewAppError(http.StatusConflict, utils.CodeNoAvailability,
		"no qualified technician is free at that time")
}

// tryBook books tech while holding its technician-day lock, re-checking availability under the lock.
// The DB exclusion constraint backs this up if the lock ever fails (e.g. TTL expiry).
func (s *BookingService) tryBook(ctx context.Context, tech domains.Technician, svc domains.Service,
	job Job, in bookingInput) (domains.Booking, error) {
	release, err := s.locker.Acquire(ctx, technicianDayLockKey(tech, job.Start))
	if err != nil {
		return domains.Booking{}, err
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, utils.LockedWorkTimeout)
	defer cancel()

	days, err := loadTechnicianDays(ctx, s.bookings, []domains.Technician{tech}, job.Start)
	if err != nil {
		return domains.Booking{}, err
	}
	if !CanTake(days[0], job) {
		return domains.Booking{}, errTechnicianTaken
	}

	jobIv, blocked := job.Window()
	b := domains.Booking{
		CustomerPhone: in.phone,
		ServicePK:     svc.PK,
		Pincode:       in.pincode,
		TechnicianPK:  tech.PK,
		StartTime:     jobIv.Start,
		EndTime:       jobIv.End,
		BlockedUntil:  blocked.End,
		Status:        utils.StatusConfirmed,
	}
	if in.key != "" {
		b.IdempotencyKey, b.RequestHash = &in.key, &in.hash
	}
	b, err = s.bookings.Insert(ctx, b)
	if errors.Is(err, repository.ErrOverlap) {
		return domains.Booking{}, errTechnicianTaken
	}
	return b, err
}

// replay returns the booking already created with in's Idempotency-Key, if any.
func (s *BookingService) replay(ctx context.Context, in bookingInput) (domains.Booking, bool, error) {
	if in.key == "" {
		return domains.Booking{}, false, nil
	}
	b, err := s.bookings.GetByIdempotencyKey(ctx, in.key)
	if errors.Is(err, repository.ErrNotFound) {
		return domains.Booking{}, false, nil
	}
	if err != nil {
		return domains.Booking{}, false, err
	}
	if b.RequestHash == nil || *b.RequestHash != in.hash {
		return domains.Booking{}, false, utils.NewAppError(http.StatusUnprocessableEntity, utils.CodeIdempotencyKeyReused,
			"Idempotency-Key was already used with a different request")
	}
	return b, true, nil
}

func (s *BookingService) Get(ctx context.Context, id string) (domains.Booking, error) {
	bookingID, err := uuid.Parse(id)
	if err != nil {
		return domains.Booking{}, utils.NotFoundError("booking not found")
	}
	b, err := s.bookings.GetByBookingID(ctx, bookingID)
	if errors.Is(err, repository.ErrNotFound) {
		return domains.Booking{}, utils.NotFoundError("booking not found")
	}
	return b, err
}

// Cancel cancels a confirmed booking up to 1 hour before start. Cancelling twice is a no-op.
func (s *BookingService) Cancel(ctx context.Context, id string) (domains.Booking, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return domains.Booking{}, err
	}
	switch b.Status {
	case utils.StatusCancelled:
		return b, nil
	case utils.StatusCompleted:
		return domains.Booking{}, utils.NewAppError(http.StatusConflict, utils.CodeInvalidState,
			"completed bookings cannot be cancelled")
	}
	if !CanCancel(b.StartTime, s.now()) {
		return domains.Booking{}, utils.NewAppError(http.StatusConflict, utils.CodeCancellationClosed,
			"bookings can only be cancelled up to 1 hour before start")
	}
	out, err := s.bookings.TransitionStatus(ctx, b.PK, utils.StatusConfirmed, utils.StatusCancelled)
	if errors.Is(err, repository.ErrNotFound) {
		return s.Get(ctx, id) // status changed concurrently; report the current state
	}
	return out, err
}

// ListQuery holds the raw query params of GET /bookings.
type ListQuery struct {
	Phone, Status, From, To, Page, Size string
}

// List returns one page of a customer's bookings, newest first. from/to are inclusive IST dates.
func (s *BookingService) List(ctx context.Context, q ListQuery) (models.BookingListResponse, error) {
	phone, ok := utils.NormalizePhone(q.Phone)
	if !ok {
		return models.BookingListResponse{}, utils.ValidationError("phone is required and must be a 10-digit Indian mobile number")
	}
	f := repository.BookingFilter{Phone: phone}
	if q.Status != "" {
		if q.Status != utils.StatusConfirmed && q.Status != utils.StatusCancelled && q.Status != utils.StatusCompleted {
			return models.BookingListResponse{}, utils.ValidationError("status must be CONFIRMED, CANCELLED or COMPLETED")
		}
		f.Status = &q.Status
	}
	if q.From != "" {
		from, err := utils.ParseDate(q.From)
		if err != nil {
			return models.BookingListResponse{}, utils.ValidationError("from must be YYYY-MM-DD")
		}
		f.From = &from
	}
	if q.To != "" {
		to, err := utils.ParseDate(q.To)
		if err != nil {
			return models.BookingListResponse{}, utils.ValidationError("to must be YYYY-MM-DD")
		}
		to = to.Add(24 * time.Hour) // inclusive end date
		f.To = &to
	}
	page, err := intParam(q.Page, 1, 1, math.MaxInt32)
	if err != nil {
		return models.BookingListResponse{}, utils.ValidationError("page must be a positive integer")
	}
	size, err := intParam(q.Size, utils.DefaultPageSize, 1, utils.MaxPageSize)
	if err != nil {
		return models.BookingListResponse{}, utils.ValidationError(fmt.Sprintf("size must be between 1 and %d", utils.MaxPageSize))
	}
	f.Limit, f.Offset = size, (page-1)*size

	items, total, err := s.bookings.ListByPhone(ctx, f)
	if err != nil {
		return models.BookingListResponse{}, err
	}
	resp := models.BookingListResponse{Items: []models.BookingResponse{}, Page: page, Size: size, Total: total}
	for _, b := range items {
		resp.Items = append(resp.Items, b.ToResponse())
	}
	return resp, nil
}

// intParam parses an optional integer query param within [lo, hi].
func intParam(s string, def, lo, hi int) (int, error) {
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, errors.New("out of range")
	}
	return n, nil
}

// lookupServiceAndZone resolves the public service id and the pincode's zone.
func lookupServiceAndZone(ctx context.Context, services ServiceStore, zones ZoneStore,
	serviceID uuid.UUID, pincode int) (domains.Service, domains.Zone, error) {
	svc, err := services.GetByServiceID(ctx, serviceID)
	if errors.Is(err, repository.ErrNotFound) {
		return svc, domains.Zone{}, utils.ValidationError("unknown serviceId")
	}
	if err != nil {
		return svc, domains.Zone{}, err
	}
	zone, err := zones.GetByPincode(ctx, pincode)
	if errors.Is(err, repository.ErrNotFound) {
		return svc, zone, utils.NewAppError(http.StatusBadRequest, utils.CodeUnserviceablePincode,
			fmt.Sprintf("pincode %d is not served by any zone", pincode))
	}
	return svc, zone, err
}

// loadTechnicianDays attaches each technician's live bookings on date's IST day.
func loadTechnicianDays(ctx context.Context, bookings BookingStore, techs []domains.Technician,
	date time.Time) ([]TechnicianDay, error) {
	day := utils.StartOfDay(date)
	pks := make([]int64, len(techs))
	for i, t := range techs {
		pks[i] = t.PK
	}
	live, err := bookings.ListLiveForTechnicians(ctx, pks, day, day.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	blocked := map[int64][]domains.Interval{}
	for _, b := range live {
		blocked[b.TechnicianPK] = append(blocked[b.TechnicianPK], b.Blocked())
	}
	days := make([]TechnicianDay, len(techs))
	for i, t := range techs {
		days[i] = TechnicianDay{Technician: t, Blocked: blocked[t.PK]}
	}
	return days, nil
}

// technicianDayLockKey: per technician per day, because overlapping jobs can start at different
// slots (10:00 vs 10:30) while different days never interact (shift + buffer ends before midnight).
func technicianDayLockKey(t domains.Technician, start time.Time) string {
	return fmt.Sprintf("lock:tech:%s:%s", t.TechnicianID, start.In(utils.IST).Format(time.DateOnly))
}

func lockError(err error) error {
	switch {
	case errors.Is(err, lock.ErrTimeout):
		return utils.NewAppError(http.StatusServiceUnavailable, utils.CodeBookingBusy,
			"technician schedule is busy, please retry")
	case errors.Is(err, lock.ErrUnavailable):
		return utils.NewAppError(http.StatusServiceUnavailable, utils.CodeDependencyUnavailable,
			"booking is temporarily unavailable, please retry")
	}
	return err
}
