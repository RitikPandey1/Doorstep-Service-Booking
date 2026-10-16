package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/api"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
)

// Two requests racing for the last free technician: exactly one wins, the rest get a clear 409.
func TestConcurrentBookingsForLastTechnician(t *testing.T) {
	reset(t)

	// North has two BATTERY_FIT technicians; book Ravi so Amit is the last one free at 10:00.
	first := call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "9000000000"))
	if first.status != http.StatusCreated {
		t.Fatalf("setup booking: %d %s", first.status, first.body)
	}

	const n = 20
	results := make([]response, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", fmt.Sprintf("98%08d", i)))
		}()
	}
	close(start)
	wg.Wait()

	var created, conflicts int
	for _, r := range results {
		switch r.status {
		case http.StatusCreated:
			created++
			var b models.BookingResponse
			r.decode(t, &b)
			if b.Technician.ID != amitID {
				t.Errorf("winner got technician %s, want Amit", b.Technician.ID)
			}
		case http.StatusConflict:
			conflicts++
			var e models.ErrorResponse
			r.decode(t, &e)
			if e.Error.Code != "NO_AVAILABILITY" {
				t.Errorf("conflict code = %s", e.Error.Code)
			}
		default:
			t.Errorf("unexpected %d: %s", r.status, r.body)
		}
	}
	if created != 1 || conflicts != n-1 {
		t.Fatalf("created=%d conflicts=%d, want 1 and %d", created, conflicts, n-1)
	}

	var rows, techs int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*), count(DISTINCT technician_pk) FROM bookings WHERE status <> 'CANCELLED' AND start_time = $1`,
		istTime(t, "10:00")).Scan(&rows, &techs)
	if err != nil || rows != 2 || techs != 2 {
		t.Fatalf("db has %d live bookings on %d technicians at 10:00 (err %v), want 2 on 2", rows, techs, err)
	}
}

// Concurrent retries with one Idempotency-Key create exactly one booking and all see it.
func TestConcurrentRetriesWithSameIdempotencyKey(t *testing.T) {
	reset(t)

	const n = 10
	results := make([]response, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "9876543210"),
				"Idempotency-Key", "race-key")
		}()
	}
	close(start)
	wg.Wait()

	ids := map[string]bool{}
	for _, r := range results {
		if r.status != http.StatusCreated {
			t.Fatalf("status %d: %s", r.status, r.body)
		}
		var b models.BookingResponse
		r.decode(t, &b)
		ids[b.ID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("responses carry %d different booking ids, want 1", len(ids))
	}
	var rows int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM bookings`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("db has %d bookings, want 1", rows)
	}

	// Same key with a different request is rejected.
	r := call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "11:00", "9876543210"), "Idempotency-Key", "race-key")
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("reused key: %d %s", r.status, r.body)
	}
}

func TestBookingLifecycle(t *testing.T) {
	reset(t)
	availability := func() []string {
		r := call(t, "GET", "/availability?serviceId="+batteryID+"&pincode=110001&date="+testDay, nil)
		if r.status != http.StatusOK {
			t.Fatalf("availability: %d %s", r.status, r.body)
		}
		var a models.AvailabilityResponse
		r.decode(t, &a)
		return a.Slots
	}

	if slots := availability(); slots[0] != "08:00" || !slices.Contains(slots, "10:00") {
		t.Fatalf("initial slots = %v", slots)
	}

	// Fill both North BATTERY_FIT technicians at 10:00.
	var ravis models.BookingResponse
	call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "+919876543210")).decode(t, &ravis)
	call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "9876543211"))
	if ravis.Technician.ID != raviID || ravis.Phone != "9876543210" || ravis.EndTime != testDay+"T11:00:00+05:30" {
		t.Fatalf("booking = %+v", ravis)
	}
	slots := availability()
	for _, gone := range []string{"09:00", "09:30", "10:00", "10:30", "11:00"} {
		if slices.Contains(slots, gone) {
			t.Errorf("%s still offered after both technicians were booked: %v", gone, slots)
		}
	}
	if !slices.Contains(slots, "11:30") {
		t.Errorf("11:30 should be free after the buffer: %v", slots)
	}

	var got models.BookingResponse
	call(t, "GET", "/bookings/"+ravis.ID, nil).decode(t, &got)
	if got != ravis {
		t.Fatalf("GET = %+v, want %+v", got, ravis)
	}

	var sched models.ScheduleResponse
	call(t, "GET", "/technicians/"+raviID+"/schedule?date="+testDay, nil).decode(t, &sched)
	if len(sched.Bookings) != 1 || sched.Bookings[0].ID != ravis.ID {
		t.Fatalf("schedule = %+v", sched)
	}

	r := call(t, "POST", "/bookings/"+ravis.ID+"/cancel", nil)
	var cancelled models.BookingResponse
	r.decode(t, &cancelled)
	if r.status != http.StatusOK || cancelled.Status != "CANCELLED" {
		t.Fatalf("cancel: %d %s", r.status, r.body)
	}
	if !slices.Contains(availability(), "10:00") {
		t.Fatal("cancelled slot not offered again")
	}
	call(t, "GET", "/technicians/"+raviID+"/schedule?date="+testDay, nil).decode(t, &sched)
	if len(sched.Bookings) != 0 {
		t.Fatalf("schedule still lists cancelled booking: %+v", sched)
	}
	if r := call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "9876543212")); r.status != http.StatusCreated {
		t.Fatalf("rebook freed slot: %d %s", r.status, r.body)
	}
}

func TestErrorResponses(t *testing.T) {
	reset(t)
	var booked models.BookingResponse
	call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "9876543210")).decode(t, &booked)

	tests := []struct {
		name, method, path string
		body               any
		before             func(t *testing.T)
		status             int
		code               string
	}{
		{name: "malformed json", method: "POST", path: "/bookings", body: `{"pincode":`,
			status: 400, code: "VALIDATION_ERROR"},
		{name: "slot not on boundary", method: "POST", path: "/bookings", body: bookingBody(batteryID, 110001, "10:15", "9876543210"),
			status: 400, code: "VALIDATION_ERROR"},
		{name: "too soon", method: "POST", path: "/bookings", body: bookingBody(batteryID, 110001, "07:30", "9876543210"),
			status: 400, code: "VALIDATION_ERROR"},
		{name: "unserved pincode", method: "POST", path: "/bookings", body: bookingBody(batteryID, 400001, "10:00", "9876543210"),
			status: 400, code: "UNSERVICEABLE_PINCODE"},
		{name: "unknown service", method: "POST", path: "/bookings", body: bookingBody(uuid.NewString(), 110001, "10:00", "9876543210"),
			status: 400, code: "VALIDATION_ERROR"},
		{name: "availability missing params", method: "GET", path: "/availability",
			status: 400, code: "VALIDATION_ERROR"},
		{name: "unknown booking", method: "GET", path: "/bookings/" + uuid.NewString(),
			status: 404, code: "NOT_FOUND"},
		{name: "unknown technician", method: "GET", path: "/technicians/" + uuid.NewString() + "/schedule?date=" + testDay,
			status: 404, code: "NOT_FOUND"},
		{name: "unknown route", method: "GET", path: "/nope",
			status: 404, code: "NOT_FOUND"},
		{name: "cancel under 1h before start", method: "POST", path: "/bookings/" + booked.ID + "/cancel",
			before: func(t *testing.T) { clock.Set(istTime(t, "09:01")) },
			status: 409, code: "CANCELLATION_WINDOW_CLOSED"},
		{name: "cancel completed booking", method: "POST", path: "/bookings/" + booked.ID + "/cancel",
			before: func(t *testing.T) {
				clock.Set(istTime(t, "06:00"))
				pool.Exec(context.Background(), `UPDATE bookings SET status = 'COMPLETED' WHERE booking_id = $1`, booked.ID)
			},
			status: 409, code: "INVALID_STATE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.before != nil {
				tc.before(t)
			}
			r := call(t, tc.method, tc.path, tc.body)
			var e models.ErrorResponse
			r.decode(t, &e)
			if r.status != tc.status || e.Error.Code != tc.code || e.Error.Message == "" {
				t.Fatalf("got %d %s, want %d %s", r.status, r.body, tc.status, tc.code)
			}
		})
	}
}

// The exclusion constraint rejects overlaps even when the application lock is bypassed.
func TestExclusionConstraintBackstop(t *testing.T) {
	reset(t)
	var created models.BookingResponse
	call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "9876543210")).decode(t, &created)

	repo := repository.NewBookingRepo(pool)
	existing, err := repo.GetByBookingID(context.Background(), uuid.MustParse(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	overlap := existing
	overlap.IdempotencyKey, overlap.RequestHash = nil, nil
	overlap.StartTime = existing.EndTime // 11:00: inside the 30-min travel buffer
	overlap.EndTime = overlap.StartTime.Add(time.Hour)
	overlap.BlockedUntil = overlap.EndTime.Add(30 * time.Minute)
	if _, err := repo.Insert(context.Background(), overlap); !errors.Is(err, repository.ErrOverlap) {
		t.Fatalf("err = %v, want ErrOverlap", err)
	}
}

func TestListBookingsByPhone(t *testing.T) {
	reset(t)
	var b1, b2 models.BookingResponse
	call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "10:00", "9876543210")).decode(t, &b1)
	call(t, "POST", "/bookings", bookingBody(jumpID, 110001, "14:00", "+919876543210")).decode(t, &b2)
	call(t, "POST", "/bookings", bookingBody(batteryID, 110001, "12:00", "9000000000")) // another customer
	call(t, "POST", "/bookings/"+b1.ID+"/cancel", nil)

	var page models.BookingListResponse
	r := call(t, "GET", "/bookings?phone=9876543210&size=1", nil)
	r.decode(t, &page)
	if r.status != http.StatusOK || page.Total != 2 || page.Size != 1 || len(page.Items) != 1 || page.Items[0].ID != b2.ID {
		t.Fatalf("page 1: %d %s", r.status, r.body)
	}
	call(t, "GET", "/bookings?phone=9876543210&size=1&page=2", nil).decode(t, &page)
	if len(page.Items) != 1 || page.Items[0].ID != b1.ID {
		t.Fatalf("page 2: %+v", page)
	}
	call(t, "GET", "/bookings?phone=9876543210&status=CANCELLED&from="+testDay+"&to="+testDay, nil).decode(t, &page)
	if page.Total != 1 || page.Items[0].ID != b1.ID {
		t.Fatalf("filtered: %+v", page)
	}
	if r := call(t, "GET", "/bookings", nil); r.status != http.StatusBadRequest {
		t.Fatalf("missing phone: %d", r.status)
	}
}

func TestRequestID(t *testing.T) {
	reset(t)
	r := call(t, "GET", "/bookings/"+uuid.NewString(), nil, "X-Request-Id", "client-abc-123")
	var e models.ErrorResponse
	r.decode(t, &e)
	if r.header.Get("X-Request-Id") != "client-abc-123" || e.Error.RequestID != "client-abc-123" {
		t.Fatalf("header %q body %q, want client id echoed", r.header.Get("X-Request-Id"), e.Error.RequestID)
	}

	r = call(t, "GET", "/bookings/"+uuid.NewString(), nil, "X-Request-Id", "bad id with spaces")
	if id := r.header.Get("X-Request-Id"); uuid.Validate(id) != nil {
		t.Fatalf("invalid client id should be replaced by a UUID, got %q", id)
	}
}

func TestRateLimitOnCreateBooking(t *testing.T) {
	reset(t)
	limited := httptest.NewServer(api.NewRouter(api.Deps{DB: pool, Redis: rdb, Now: clock.Now, BookingRateLimit: 2}))
	defer limited.Close()
	post := func() *http.Response {
		res, err := http.Post(limited.URL+"/bookings", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}

	// The limiter runs before the handler, so invalid bodies still count.
	for i := range 2 {
		if res := post(); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("request %d: %d, want 400 from the handler", i+1, res.StatusCode)
		}
	}
	res := post()
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("third request: %d Retry-After=%q, want 429 with Retry-After", res.StatusCode, res.Header.Get("Retry-After"))
	}
	if res, _ := http.Get(limited.URL + "/bookings/" + uuid.NewString()); res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET must not be rate limited, got %d", res.StatusCode)
	}
}

func TestHealthEndpoints(t *testing.T) {
	reset(t)
	var h models.HealthResponse
	if r := call(t, "GET", "/healthz", nil); r.status != http.StatusOK {
		t.Fatalf("healthz: %d", r.status)
	}
	r := call(t, "GET", "/readyz", nil)
	r.decode(t, &h)
	if r.status != http.StatusOK || h.Status != "UP" || h.Checks["postgres"] != "UP" || h.Checks["redis"] != "UP" {
		t.Fatalf("readyz: %d %s", r.status, r.body)
	}

	deadRedis := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
	defer deadRedis.Close()
	degraded := httptest.NewServer(api.NewRouter(api.Deps{DB: pool, Redis: deadRedis, Now: clock.Now}))
	defer degraded.Close()

	res, err := http.Get(degraded.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	h = models.HealthResponse{}
	json.NewDecoder(res.Body).Decode(&h)
	if res.StatusCode != http.StatusServiceUnavailable || h.Checks["redis"] != "DOWN" || h.Checks["postgres"] != "UP" {
		t.Fatalf("readyz with Redis down: %d %+v", res.StatusCode, h)
	}
	if res, _ := http.Get(degraded.URL + "/healthz"); res.StatusCode != http.StatusOK {
		t.Fatalf("healthz must ignore dependencies, got %d", res.StatusCode)
	}
}
