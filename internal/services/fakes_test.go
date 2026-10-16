package services

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

// fakeDB is an in-memory stand-in for all repositories. Insert enforces the same
// overlap and idempotency-key constraints as Postgres.
type fakeDB struct {
	mu       sync.Mutex
	services []domains.Service
	zones    map[int]domains.Zone // by pincode
	techs    []domains.Technician
	bookings []domains.Booking

	// beforeInsert runs before Insert checks constraints; tests use it to simulate a racing writer.
	beforeInsert func(b domains.Booking)
}

func (f *fakeDB) GetByServiceID(_ context.Context, id uuid.UUID) (domains.Service, error) {
	for _, s := range f.services {
		if s.ServiceID == id {
			return s, nil
		}
	}
	return domains.Service{}, repository.ErrNotFound
}

func (f *fakeDB) GetByPincode(_ context.Context, pincode int) (domains.Zone, error) {
	if z, ok := f.zones[pincode]; ok {
		return z, nil
	}
	return domains.Zone{}, repository.ErrNotFound
}

func (f *fakeDB) GetByTechnicianID(_ context.Context, id uuid.UUID) (domains.Technician, error) {
	for _, t := range f.techs {
		if t.TechnicianID == id {
			return t, nil
		}
	}
	return domains.Technician{}, repository.ErrNotFound
}

func (f *fakeDB) ListByZoneAndSkill(_ context.Context, zonePK int64, skill string) ([]domains.Technician, error) {
	var out []domains.Technician
	for _, t := range f.techs {
		if t.ZonePK == zonePK && t.HasSkill(skill) {
			out = append(out, t)
		}
	}
	return out, nil
}

// add stores a booking directly, bypassing constraints (test setup / simulated other writers).
func (f *fakeDB) add(b domains.Booking) domains.Booking {
	f.mu.Lock()
	defer f.mu.Unlock()
	b.PK = int64(len(f.bookings) + 1)
	b.BookingID = uuid.New()
	if b.Status == "" {
		b.Status = utils.StatusConfirmed
	}
	if b.BlockedUntil.IsZero() {
		b.BlockedUntil = b.EndTime.Add(utils.TravelBuffer)
	}
	for _, s := range f.services {
		if s.PK == b.ServicePK {
			b.ServiceID, b.ServiceName = s.ServiceID, s.Name
		}
	}
	for _, t := range f.techs {
		if t.PK == b.TechnicianPK {
			b.TechnicianID, b.TechnicianName = t.TechnicianID, t.Name
		}
	}
	f.bookings = append(f.bookings, b)
	return b
}

func (f *fakeDB) Insert(_ context.Context, b domains.Booking) (domains.Booking, error) {
	if f.beforeInsert != nil {
		f.beforeInsert(b)
	}
	f.mu.Lock()
	for _, x := range f.bookings {
		if b.IdempotencyKey != nil && x.IdempotencyKey != nil && *x.IdempotencyKey == *b.IdempotencyKey {
			f.mu.Unlock()
			return domains.Booking{}, repository.ErrDuplicateIdempotencyKey
		}
		if x.TechnicianPK == b.TechnicianPK && x.Status != utils.StatusCancelled && x.Blocked().Overlaps(b.Blocked()) {
			f.mu.Unlock()
			return domains.Booking{}, repository.ErrOverlap
		}
	}
	f.mu.Unlock()
	return f.add(b), nil
}

func (f *fakeDB) find(match func(domains.Booking) bool) (domains.Booking, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := slices.IndexFunc(f.bookings, match); i >= 0 {
		return f.bookings[i], nil
	}
	return domains.Booking{}, repository.ErrNotFound
}

func (f *fakeDB) GetByBookingID(_ context.Context, id uuid.UUID) (domains.Booking, error) {
	return f.find(func(b domains.Booking) bool { return b.BookingID == id })
}

func (f *fakeDB) GetByIdempotencyKey(_ context.Context, key string) (domains.Booking, error) {
	return f.find(func(b domains.Booking) bool { return b.IdempotencyKey != nil && *b.IdempotencyKey == key })
}

func (f *fakeDB) ListLiveForTechnicians(_ context.Context, pks []int64, from, to time.Time) ([]domains.Booking, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domains.Booking
	for _, b := range f.bookings {
		if slices.Contains(pks, b.TechnicianPK) && b.Status != utils.StatusCancelled &&
			!b.StartTime.Before(from) && b.StartTime.Before(to) {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b domains.Booking) int { return a.StartTime.Compare(b.StartTime) }) // like ORDER BY start_time
	return out, nil
}

func (f *fakeDB) TransitionStatus(_ context.Context, pk int64, from, to string) (domains.Booking, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, b := range f.bookings {
		if b.PK == pk && b.Status == from {
			f.bookings[i].Status = to
			return f.bookings[i], nil
		}
	}
	return domains.Booking{}, repository.ErrNotFound
}

func (f *fakeDB) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bookings)
}

// fakeLocker grants every lock unless err is set; onAcquire simulates work by others before we get the lock.
type fakeLocker struct {
	err       error
	onAcquire func(key string)
}

func (l *fakeLocker) Acquire(_ context.Context, key string) (func(), error) {
	if l.err != nil {
		return nil, l.err
	}
	if l.onAcquire != nil {
		l.onAcquire(key)
	}
	return func() {}, nil
}
