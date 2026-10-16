package api_test

// Integration harness: real Postgres + Redis in containers, real migrations and seed,
// the real router behind an httptest server. Requires Docker; skipped with -short.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/api"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

// Seed ids (see cmd/seed).
const (
	batteryID = "5e000000-0000-0000-0000-000000000001"
	jumpID    = "5e000000-0000-0000-0000-000000000002"
	raviID    = "7e000000-0000-0000-0000-000000000001" // North, all skills
	amitID    = "7e000000-0000-0000-0000-000000000002" // North, BATTERY_FIT
)

// testDay is the IST date most tests book on; the clock starts at 06:00 that day,
// so 08:00 is the first bookable slot.
const testDay = "2026-11-02"

var (
	server *httptest.Server
	pool   *pgxpool.Pool
	rdb    *redis.Client
	clock  = &fakeClock{}
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run()) // integration tests skip themselves
	}
	teardown, err := setup()
	if err != nil {
		log.Fatalf("integration setup: %v", err)
	}
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() (func(), error) {
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("booking"), tcpostgres.WithUsername("booking"), tcpostgres.WithPassword("booking"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	rc, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		return nil, fmt.Errorf("start redis: %w", err)
	}
	dbURL, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, err
	}
	redisURL, err := rc.ConnectionString(ctx)
	if err != nil {
		return nil, err
	}

	if pool, err = repository.NewPool(ctx, dbURL); err != nil {
		return nil, err
	}
	if err := migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	seed := exec.Command("go", "run", "./cmd/seed")
	seed.Dir = "../.."
	seed.Env = append(os.Environ(), "DATABASE_URL="+dbURL)
	if out, err := seed.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("seed: %v: %s", err, out)
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	rdb = redis.NewClient(opts)
	gin.SetMode(gin.TestMode)
	// Rate limiting off here: the race tests fire many POSTs from one IP. TestRateLimit enables it.
	server = httptest.NewServer(api.NewRouter(api.Deps{DB: pool, Redis: rdb, Now: clock.Now}))

	return func() {
		server.Close()
		rdb.Close()
		pool.Close()
		_ = rc.Terminate(ctx)
		_ = pg.Terminate(ctx)
	}, nil
}

// migrate applies the same *.up.sql files the migrate container uses, in order.
func migrate(ctx context.Context) error {
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}

// reset skips in -short mode, clears bookings and Redis, and puts the clock at 06:00 IST on testDay.
func reset(t *testing.T) {
	t.Helper()
	if server == nil {
		t.Skip("integration test: needs Docker")
	}
	if _, err := pool.Exec(context.Background(), "TRUNCATE bookings"); err != nil {
		t.Fatal(err)
	}
	if err := rdb.FlushAll(context.Background()).Err(); err != nil { // locks and rate-limit counters
		t.Fatal(err)
	}
	clock.Set(istTime(t, "06:00"))
}

func istTime(t *testing.T, hhmm string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", testDay+" "+hhmm, utils.IST)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

// call sends a request; body may be nil, a string (sent raw) or a value (sent as JSON).
// It reports failures with t.Errorf so it is safe to use from goroutines.
func call(t *testing.T, method, path string, body any, headers ...string) response {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = bytes.NewBufferString(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, server.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("%s %s: %v", method, path, err)
		return response{}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, header: res.Header, body: raw}
}

func bookingBody(serviceID string, pincode int, hhmm, phone string) map[string]any {
	return map[string]any{"serviceId": serviceID, "pincode": pincode,
		"startTime": testDay + "T" + hhmm + ":00+05:30", "phone": phone}
}
