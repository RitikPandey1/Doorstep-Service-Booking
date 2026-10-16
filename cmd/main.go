package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/api"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	gin.SetMode(gin.ReleaseMode)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := repository.NewPool(ctx, utils.GetEnv("DATABASE_URL", "postgres://booking:booking@localhost:5432/booking?sslmode=disable"))
	if err != nil {
		fatal("connect postgres", err)
	}
	defer pool.Close()

	redisOpts, err := redis.ParseURL(utils.GetEnv("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		fatal("parse REDIS_URL", err)
	}
	// No startup ping: Redis being down only fails POST /bookings (fail closed), not the whole API.
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	rateLimit, err := strconv.Atoi(utils.GetEnv("RATE_LIMIT_PER_MINUTE", "10"))
	if err != nil || rateLimit < 0 {
		fatal("parse RATE_LIMIT_PER_MINUTE", fmt.Errorf("want a non-negative integer, got %q", os.Getenv("RATE_LIMIT_PER_MINUTE")))
	}

	router := api.NewRouter(api.Deps{DB: pool, Redis: rdb, Now: time.Now, BookingRateLimit: rateLimit})
	srv := api.NewServer(":"+utils.GetEnv("PORT", "8080"), router)

	go func() {
		slog.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("server failed", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
