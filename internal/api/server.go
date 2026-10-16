package api

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/api/handlers"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/api/middleware"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/lock"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/services"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

// Deps are the external resources the API needs. Tests pass their own.
type Deps struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
	Now   func() time.Time
	// BookingRateLimit is POST /bookings requests allowed per client IP per BookingRateWindow; 0 disables it.
	BookingRateLimit  int
	BookingRateWindow time.Duration // defaults to 1 minute
}

// NewRouter wires repositories → services → handlers and registers all routes.
func NewRouter(d Deps) *gin.Engine {
	serviceRepo := repository.NewServiceRepo(d.DB)
	zoneRepo := repository.NewZoneRepo(d.DB)
	techRepo := repository.NewTechnicianRepo(d.DB)
	bookingRepo := repository.NewBookingRepo(d.DB)
	locker := lock.NewRedisLocker(d.Redis, utils.LockTTL, utils.LockWait)

	bookingH := handlers.NewBookingHandler(
		services.NewBookingService(serviceRepo, zoneRepo, techRepo, bookingRepo, locker, d.Now))
	availabilityH := handlers.NewAvailabilityHandler(
		services.NewAvailabilityService(serviceRepo, zoneRepo, techRepo, bookingRepo, d.Now))
	technicianH := handlers.NewTechnicianHandler(services.NewTechnicianService(techRepo, bookingRepo))
	healthH := handlers.NewHealthHandler(map[string]func(context.Context) error{
		"postgres": d.DB.Ping,
		"redis":    func(ctx context.Context) error { return d.Redis.Ping(ctx).Err() },
	})

	r := gin.New()
	// Use the socket peer as client IP; trusting X-Forwarded-For from anyone would let clients dodge the rate limit.
	_ = r.SetTrustedProxies(nil)
	r.Use(
		middleware.RequestID(),
		middleware.AccessLog(),
		gin.CustomRecovery(func(c *gin.Context, err any) {
			handlers.RespondError(c, fmt.Errorf("panic: %v", err))
		}),
	)
	r.NoRoute(func(c *gin.Context) {
		handlers.RespondError(c, utils.NotFoundError("route not found"))
	})

	r.GET("/healthz", healthH.Live)
	r.GET("/readyz", healthH.Ready)
	r.GET("/availability", availabilityH.Get)
	createBooking := []gin.HandlerFunc{bookingH.Create}
	if d.BookingRateLimit > 0 {
		createBooking = append([]gin.HandlerFunc{middleware.RateLimit(d.Redis, d.BookingRateLimit, cmp.Or(d.BookingRateWindow, time.Minute))}, createBooking...)
	}
	r.POST("/bookings", createBooking...)
	r.GET("/bookings", bookingH.List)
	r.GET("/bookings/:id", bookingH.Get)
	r.POST("/bookings/:id/cancel", bookingH.Cancel)
	r.GET("/technicians/:id/schedule", technicianH.Schedule)
	return r
}

// NewServer wraps the router in an http.Server with sane timeouts.
func NewServer(addr string, router http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
