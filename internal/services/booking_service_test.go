package services

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/lock"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

var (
	batterySvc = domains.Service{PK: 1, ServiceID: uuid.New(), Name: "Battery replacement", DurationMin: 60, RequiredSkill: utils.SkillBatteryFit}
	jumpSvc    = domains.Service{PK: 2, ServiceID: uuid.New(), Name: "Jump-start", DurationMin: 30, RequiredSkill: utils.SkillJumpstart}
)

type env struct {
	svc    *BookingService
	db     *fakeDB
	locker *fakeLocker
	now    time.Time
}

func withID(t domains.Technician) domains.Technician {
	t.TechnicianID = uuid.New()
	return t
}

// newEnv: North zone (110001) with Ravi (all skills, PK 1) and Amit (BATTERY_FIT, PK 2); now is 07:00 on 2026-10-18.
func newEnv() *env {
	e := &env{
		db: &fakeDB{
			services: []domains.Service{batterySvc, jumpSvc},
			zones:    map[int]domains.Zone{110001: {PK: zoneNorth, ZoneID: uuid.New(), Name: "North"}},
			techs:    []domains.Technician{withID(ravi), withID(amit)},
		},
		locker: &fakeLocker{},
		now:    at("07:00"),
	}
	e.svc = NewBookingService(e.db, e.db, e.db, e.db, e.locker, func() time.Time { return e.now })
	return e
}

func bookReq(hhmm string) models.CreateBookingRequest {
	return models.CreateBookingRequest{
		ServiceID: batterySvc.ServiceID.String(),
		Pincode:   110001,
		StartTime: "2026-10-18T" + hhmm + ":00+05:30",
		Phone:     "9876543210",
	}
}

func (e *env) bookRavi(hhmm string) domains.Booking {
	return e.db.add(domains.Booking{CustomerPhone: "9000000000", ServicePK: batterySvc.PK, Pincode: 110001,
		TechnicianPK: ravi.PK, StartTime: at(hhmm), EndTime: at(hhmm).Add(hour)})
}

func wantAppError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var appErr *utils.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("err = %v, want AppError %s", err, code)
	}
	if appErr.Status != status || appErr.Code != code {
		t.Fatalf("got %d %s (%s), want %d %s", appErr.Status, appErr.Code, appErr.Message, status, code)
	}
}

func TestCreateAssignsLeastLoadedTechnician(t *testing.T) {
	ctx := context.Background()

	t.Run("tie goes to lowest id", func(t *testing.T) {
		e := newEnv()
		b, err := e.svc.Create(ctx, bookReq("15:00"), "")
		if err != nil {
			t.Fatal(err)
		}
		if b.TechnicianPK != ravi.PK || b.Status != utils.StatusConfirmed {
			t.Fatalf("got tech %d status %s, want Ravi CONFIRMED", b.TechnicianPK, b.Status)
		}
		if !b.EndTime.Equal(at("16:00")) || !b.BlockedUntil.Equal(at("16:30")) {
			t.Fatalf("end %v blocked %v, want 16:00 / 16:30", b.EndTime, b.BlockedUntil)
		}
	})

	t.Run("fewer bookings that day wins", func(t *testing.T) {
		e := newEnv()
		e.bookRavi("09:00")
		b, err := e.svc.Create(ctx, bookReq("15:00"), "")
		if err != nil {
			t.Fatal(err)
		}
		if b.TechnicianPK != amit.PK {
			t.Fatalf("got tech %d, want Amit (fewer bookings)", b.TechnicianPK)
		}
	})

	t.Run("phone with +91 is normalised", func(t *testing.T) {
		e := newEnv()
		req := bookReq("15:00")
		req.Phone = "+919876543210"
		b, err := e.svc.Create(ctx, req, "")
		if err != nil {
			t.Fatal(err)
		}
		if b.CustomerPhone != "9876543210" {
			t.Fatalf("phone = %s", b.CustomerPhone)
		}
	})
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.CreateBookingRequest)
		code   string
	}{
		{"bad service id", func(r *models.CreateBookingRequest) { r.ServiceID = "nope" }, utils.CodeValidation},
		{"unknown service", func(r *models.CreateBookingRequest) { r.ServiceID = uuid.NewString() }, utils.CodeValidation},
		{"bad pincode", func(r *models.CreateBookingRequest) { r.Pincode = 12345 }, utils.CodeValidation},
		{"unserved pincode", func(r *models.CreateBookingRequest) { r.Pincode = 400001 }, utils.CodeUnserviceablePincode},
		{"bad phone", func(r *models.CreateBookingRequest) { r.Phone = "12345" }, utils.CodeValidation},
		{"bad start time", func(r *models.CreateBookingRequest) { r.StartTime = "tomorrow" }, utils.CodeValidation},
		{"not on 30-min boundary", func(r *models.CreateBookingRequest) { r.StartTime = "2026-10-18T15:15:00+05:30" }, utils.CodeValidation},
		{"under 2h ahead", func(r *models.CreateBookingRequest) { r.StartTime = "2026-10-18T08:30:00+05:30" }, utils.CodeValidation},
		{"over 7 days ahead", func(r *models.CreateBookingRequest) { r.StartTime = "2026-10-26T10:00:00+05:30" }, utils.CodeValidation},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv()
			req := bookReq("15:00")
			tc.mutate(&req)
			_, err := e.svc.Create(context.Background(), req, "")
			wantAppError(t, err, http.StatusBadRequest, tc.code)
			if e.db.count() != 0 {
				t.Fatal("no booking may be stored")
			}
		})
	}
}

func TestCreateNoAvailability(t *testing.T) {
	e := newEnv()
	e.db.techs = e.db.techs[:1] // Ravi only
	e.bookRavi("15:00")
	_, err := e.svc.Create(context.Background(), bookReq("15:30"), "")
	wantAppError(t, err, http.StatusConflict, utils.CodeNoAvailability)
}

func TestCreateFallsBackWhenFirstChoiceIsTakenConcurrently(t *testing.T) {
	ctx := context.Background()

	t.Run("re-check under lock sees the new booking", func(t *testing.T) {
		e := newEnv()
		first := true
		e.locker.onAcquire = func(string) {
			if first { // another instance booked Ravi between our ranking and our lock
				first = false
				e.bookRavi("15:00")
			}
		}
		b, err := e.svc.Create(ctx, bookReq("15:00"), "")
		if err != nil {
			t.Fatal(err)
		}
		if b.TechnicianPK != amit.PK {
			t.Fatalf("got tech %d, want fallback to Amit", b.TechnicianPK)
		}
	})

	t.Run("DB backstop rejects the insert", func(t *testing.T) {
		e := newEnv()
		first := true
		e.db.beforeInsert = func(domains.Booking) {
			if first { // lock was bypassed (e.g. TTL expired) and someone else wrote first
				first = false
				e.bookRavi("15:00")
			}
		}
		b, err := e.svc.Create(ctx, bookReq("15:00"), "")
		if err != nil {
			t.Fatal(err)
		}
		if b.TechnicianPK != amit.PK {
			t.Fatalf("got tech %d, want fallback to Amit", b.TechnicianPK)
		}
		if e.db.count() != 2 {
			t.Fatalf("bookings = %d, want 2", e.db.count())
		}
	})
}

func TestCreateLockFailures(t *testing.T) {
	tests := []struct {
		lockErr error
		code    string
	}{
		{lock.ErrTimeout, utils.CodeBookingBusy},
		{lock.ErrUnavailable, utils.CodeDependencyUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			e := newEnv()
			e.locker.err = tc.lockErr
			_, err := e.svc.Create(context.Background(), bookReq("15:00"), "")
			wantAppError(t, err, http.StatusServiceUnavailable, tc.code)
		})
	}
}

func TestCreateIdempotency(t *testing.T) {
	ctx := context.Background()

	t.Run("same key and body returns the original booking", func(t *testing.T) {
		e := newEnv()
		first, err := e.svc.Create(ctx, bookReq("15:00"), "key-1")
		if err != nil {
			t.Fatal(err)
		}
		again, err := e.svc.Create(ctx, bookReq("15:00"), "key-1")
		if err != nil {
			t.Fatal(err)
		}
		if again.BookingID != first.BookingID || e.db.count() != 1 {
			t.Fatalf("replay created a new booking (%d stored)", e.db.count())
		}
	})

	t.Run("replay still works after the booking window has passed", func(t *testing.T) {
		e := newEnv()
		first, _ := e.svc.Create(ctx, bookReq("15:00"), "key-1")
		e.now = at("14:30") // start is now < 2h away
		again, err := e.svc.Create(ctx, bookReq("15:00"), "key-1")
		if err != nil || again.BookingID != first.BookingID {
			t.Fatalf("err = %v, want replay of %s", err, first.BookingID)
		}
	})

	t.Run("same key with a different body is rejected", func(t *testing.T) {
		e := newEnv()
		if _, err := e.svc.Create(ctx, bookReq("15:00"), "key-1"); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.Create(ctx, bookReq("16:00"), "key-1")
		wantAppError(t, err, http.StatusUnprocessableEntity, utils.CodeIdempotencyKeyReused)
	})

	t.Run("failed attempt does not burn the key", func(t *testing.T) {
		e := newEnv()
		e.locker.err = lock.ErrTimeout
		if _, err := e.svc.Create(ctx, bookReq("15:00"), "key-1"); err == nil {
			t.Fatal("want failure")
		}
		e.locker.err = nil
		if _, err := e.svc.Create(ctx, bookReq("15:00"), "key-1"); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("without a key every request books", func(t *testing.T) {
		e := newEnv()
		e.svc.Create(ctx, bookReq("15:00"), "")
		e.svc.Create(ctx, bookReq("15:00"), "")
		if e.db.count() != 2 {
			t.Fatalf("bookings = %d, want 2 (Ravi and Amit)", e.db.count())
		}
	})

	t.Run("overlong key is rejected", func(t *testing.T) {
		e := newEnv()
		long := make([]byte, utils.MaxIdempotencyKeyLen+1)
		for i := range long {
			long[i] = 'k'
		}
		_, err := e.svc.Create(ctx, bookReq("15:00"), string(long))
		wantAppError(t, err, http.StatusBadRequest, utils.CodeValidation)
	})
}

func TestGet(t *testing.T) {
	e := newEnv()
	b := e.bookRavi("15:00")
	got, err := e.svc.Get(context.Background(), b.BookingID.String())
	if err != nil || got.BookingID != b.BookingID {
		t.Fatalf("got %v, %v", got.BookingID, err)
	}
	for _, id := range []string{"not-a-uuid", uuid.NewString()} {
		_, err := e.svc.Get(context.Background(), id)
		wantAppError(t, err, http.StatusNotFound, utils.CodeNotFound)
	}
}

func TestCancel(t *testing.T) {
	ctx := context.Background()

	t.Run("cancels and frees the slot", func(t *testing.T) {
		e := newEnv()
		e.db.techs = e.db.techs[:1]
		b := e.bookRavi("15:00")
		got, err := e.svc.Cancel(ctx, b.BookingID.String())
		if err != nil || got.Status != utils.StatusCancelled {
			t.Fatalf("status %s, err %v", got.Status, err)
		}
		if _, err := e.svc.Create(ctx, bookReq("15:00"), ""); err != nil {
			t.Fatalf("slot not freed: %v", err)
		}
	})

	t.Run("exactly 1 hour before is allowed", func(t *testing.T) {
		e := newEnv()
		b := e.bookRavi("15:00")
		e.now = at("14:00")
		if _, err := e.svc.Cancel(ctx, b.BookingID.String()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("less than 1 hour before is rejected", func(t *testing.T) {
		e := newEnv()
		b := e.bookRavi("15:00")
		e.now = at("14:01")
		_, err := e.svc.Cancel(ctx, b.BookingID.String())
		wantAppError(t, err, http.StatusConflict, utils.CodeCancellationClosed)
	})

	t.Run("cancelling twice returns the booking unchanged", func(t *testing.T) {
		e := newEnv()
		b := e.bookRavi("15:00")
		e.svc.Cancel(ctx, b.BookingID.String())
		e.now = at("14:59") // even past the cutoff
		got, err := e.svc.Cancel(ctx, b.BookingID.String())
		if err != nil || got.Status != utils.StatusCancelled {
			t.Fatalf("status %s, err %v", got.Status, err)
		}
	})

	t.Run("completed booking cannot be cancelled", func(t *testing.T) {
		e := newEnv()
		b := e.db.add(domains.Booking{ServicePK: batterySvc.PK, TechnicianPK: ravi.PK,
			StartTime: at("15:00"), EndTime: at("16:00"), Status: utils.StatusCompleted})
		_, err := e.svc.Cancel(ctx, b.BookingID.String())
		wantAppError(t, err, http.StatusConflict, utils.CodeInvalidState)
	})

	t.Run("unknown booking", func(t *testing.T) {
		_, err := newEnv().svc.Cancel(ctx, uuid.NewString())
		wantAppError(t, err, http.StatusNotFound, utils.CodeNotFound)
	})
}

func TestList(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	mine := func(start time.Time, status string) domains.Booking {
		return e.db.add(domains.Booking{CustomerPhone: "9876543210", ServicePK: batterySvc.PK, TechnicianPK: ravi.PK,
			StartTime: start, EndTime: start.Add(hour), Status: status})
	}
	d1 := mine(at("10:00"), utils.StatusConfirmed)
	d2 := mine(at("10:00").Add(24*hour), utils.StatusCancelled)
	d3 := mine(at("10:00").Add(48*hour), utils.StatusConfirmed)
	e.db.add(domains.Booking{CustomerPhone: "9000000000", ServicePK: batterySvc.PK, TechnicianPK: amit.PK,
		StartTime: at("10:00"), EndTime: at("11:00")}) // someone else's

	ids := func(r models.BookingListResponse) []string {
		out := make([]string, len(r.Items))
		for i, b := range r.Items {
			out[i] = b.ID
		}
		return out
	}
	tests := []struct {
		name  string
		q     ListQuery
		want  []domains.Booking
		total int
	}{
		{"all mine, newest first", ListQuery{Phone: "+919876543210"}, []domains.Booking{d3, d2, d1}, 3},
		{"status filter", ListQuery{Phone: "9876543210", Status: "CONFIRMED"}, []domains.Booking{d3, d1}, 2},
		{"inclusive date range", ListQuery{Phone: "9876543210", From: "2026-10-19", To: "2026-10-19"}, []domains.Booking{d2}, 1},
		{"page 2 of size 2", ListQuery{Phone: "9876543210", Page: "2", Size: "2"}, []domains.Booking{d1}, 3},
		{"page past the end", ListQuery{Phone: "9876543210", Page: "5"}, nil, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.svc.List(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			want := make([]string, len(tc.want))
			for i, b := range tc.want {
				want[i] = b.BookingID.String()
			}
			if got.Total != tc.total || got.Items == nil || !slices.Equal(ids(got), want) {
				t.Fatalf("got %v total %d, want %v total %d", ids(got), got.Total, want, tc.total)
			}
		})
	}

	for _, q := range []ListQuery{
		{},
		{Phone: "123"},
		{Phone: "9876543210", Status: "DONE"},
		{Phone: "9876543210", From: "yesterday"},
		{Phone: "9876543210", Page: "0"},
		{Phone: "9876543210", Size: "101"},
		{Phone: "9876543210", Size: "abc"},
	} {
		_, err := e.svc.List(ctx, q)
		wantAppError(t, err, http.StatusBadRequest, utils.CodeValidation)
	}
}
