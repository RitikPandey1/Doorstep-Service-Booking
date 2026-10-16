package services

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

func (e *env) availability() *AvailabilityService {
	return NewAvailabilityService(e.db, e.db, e.db, e.db, func() time.Time { return e.now })
}

func TestAvailability(t *testing.T) {
	ctx := context.Background()
	battery := batterySvc.ServiceID.String()

	t.Run("slot listed if any technician is free", func(t *testing.T) {
		e := newEnv() // now 07:00 → bookable from 09:00
		e.bookRavi("10:00")
		got, err := e.availability().Get(ctx, battery, "110001", "2026-10-18")
		if err != nil {
			t.Fatal(err)
		}
		// Amit is free all day, so every 60-min slot 09:00..19:00 is listed.
		if len(got.Slots) != 21 || got.Slots[0] != "09:00" || got.Slots[20] != "19:00" || got.Message != "" {
			t.Fatalf("slots = %v, message = %q", got.Slots, got.Message)
		}
		if got.ServiceID != battery || got.Pincode != 110001 || got.Date != "2026-10-18" {
			t.Fatalf("echo fields wrong: %+v", got)
		}
	})

	t.Run("slots blocked by the only technician's job and buffer are hidden", func(t *testing.T) {
		e := newEnv()
		e.db.techs = e.db.techs[:1] // Ravi only
		e.bookRavi("10:00")
		got, _ := e.availability().Get(ctx, battery, "110001", "2026-10-18")
		for _, hidden := range []string{"09:00", "09:30", "10:00", "10:30", "11:00"} {
			if slices.Contains(got.Slots, hidden) {
				t.Errorf("%s should be hidden, got %v", hidden, got.Slots)
			}
		}
		if got.Slots[0] != "11:30" {
			t.Fatalf("first slot = %s, want 11:30", got.Slots[0])
		}
	})

	t.Run("cancelled booking frees its slots", func(t *testing.T) {
		e := newEnv()
		e.db.techs = e.db.techs[:1]
		b := e.bookRavi("10:00")
		e.db.TransitionStatus(ctx, b.PK, utils.StatusConfirmed, utils.StatusCancelled)
		got, _ := e.availability().Get(ctx, battery, "110001", "2026-10-18")
		if !slices.Contains(got.Slots, "10:00") {
			t.Fatalf("10:00 should be free again, got %v", got.Slots)
		}
	})

	t.Run("only technicians with the skill count", func(t *testing.T) {
		e := newEnv()
		e.bookRavi("10:00") // Ravi is the only JUMPSTART technician
		got, _ := e.availability().Get(ctx, jumpSvc.ServiceID.String(), "110001", "2026-10-18")
		if slices.Contains(got.Slots, "10:30") || !slices.Contains(got.Slots, "11:30") {
			t.Fatalf("slots = %v", got.Slots)
		}
	})

	t.Run("no slots returns empty list with message", func(t *testing.T) {
		e := newEnv()
		got, err := e.availability().Get(ctx, battery, "110001", "2026-11-30") // beyond 7 days
		if err != nil {
			t.Fatal(err)
		}
		if got.Slots == nil || len(got.Slots) != 0 || got.Message != noSlotsMessage {
			t.Fatalf("got %+v", got)
		}
	})

	errTests := []struct {
		name, serviceID, pincode, date, code string
	}{
		{"bad service id", "x", "110001", "2026-10-18", utils.CodeValidation},
		{"unknown service", uuid.NewString(), "110001", "2026-10-18", utils.CodeValidation},
		{"bad pincode", battery, "11001", "2026-10-18", utils.CodeValidation},
		{"unserved pincode", battery, "400001", "2026-10-18", utils.CodeUnserviceablePincode},
		{"bad date", battery, "110001", "18-10-2026", utils.CodeValidation},
		{"missing date", battery, "110001", "", utils.CodeValidation},
	}
	for _, tc := range errTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newEnv().availability().Get(ctx, tc.serviceID, tc.pincode, tc.date)
			wantAppError(t, err, http.StatusBadRequest, tc.code)
		})
	}
}

func TestSchedule(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	svc := NewTechnicianService(e.db, e.db)
	raviID := e.db.techs[0].TechnicianID.String()

	late := e.bookRavi("15:00")
	early := e.bookRavi("09:00")
	cancelled := e.bookRavi("12:00")
	e.db.TransitionStatus(ctx, cancelled.PK, utils.StatusConfirmed, utils.StatusCancelled)
	e.db.add(domains.Booking{ServicePK: batterySvc.PK, TechnicianPK: ravi.PK, // next day
		StartTime: at("10:00").Add(24 * time.Hour), EndTime: at("11:00").Add(24 * time.Hour)})
	e.db.add(domains.Booking{ServicePK: batterySvc.PK, TechnicianPK: amit.PK, StartTime: at("10:00"), EndTime: at("11:00")})

	got, err := svc.Schedule(ctx, raviID, "2026-10-18")
	if err != nil {
		t.Fatal(err)
	}
	if got.Technician.ID != raviID || len(got.Bookings) != 2 ||
		got.Bookings[0].ID != early.BookingID.String() || got.Bookings[1].ID != late.BookingID.String() {
		t.Fatalf("got %+v", got)
	}

	empty, err := svc.Schedule(ctx, raviID, "2026-10-20")
	if err != nil || empty.Bookings == nil || len(empty.Bookings) != 0 {
		t.Fatalf("empty day: %+v, %v", empty, err)
	}

	for _, id := range []string{"nope", uuid.NewString()} {
		_, err := svc.Schedule(ctx, id, "2026-10-18")
		wantAppError(t, err, http.StatusNotFound, utils.CodeNotFound)
	}
	_, err = svc.Schedule(ctx, raviID, "bad")
	wantAppError(t, err, http.StatusBadRequest, utils.CodeValidation)
}
