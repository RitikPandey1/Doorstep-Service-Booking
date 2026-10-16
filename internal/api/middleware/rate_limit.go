package middleware

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/api/handlers"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

// RateLimit allows limit requests per client IP per fixed window, counted in Redis so
// all API replicas share one budget for now (we can extented it per api path rate limiting on cloudflare or dns level or we can implement that here too when system scale). It fails open: a limiter outage must not block bookings.
func RateLimit(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		now := time.Now()
		slot := now.UnixNano() / int64(window)
		key := fmt.Sprintf("rl:%s:%d", c.ClientIP(), slot)

		var count *redis.IntCmd
		_, err := rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
			count = p.Incr(ctx, key)
			p.Expire(ctx, key, window)
			return nil
		})
		if err != nil {
			slog.WarnContext(ctx, "rate limiter unavailable, allowing request",
				"error", err, "requestId", c.GetString(utils.RequestIDKey))
			c.Next()
			return
		}
		if count.Val() > int64(limit) {
			windowEnd := time.Unix(0, (slot+1)*int64(window))
			wait := int(math.Ceil(windowEnd.Sub(now).Seconds()))
			c.Header("Retry-After", strconv.Itoa(wait))
			appErr := utils.NewAppError(http.StatusTooManyRequests, utils.CodeRateLimited,
				fmt.Sprintf("too many booking requests, retry in %d seconds", wait))
			// Only the wait is exposed; the limit and window stay server-side.
			appErr.Details = map[string]any{"retryAfterSeconds": wait}
			handlers.RespondError(c, appErr)
			return
		}
		c.Next()
	}
}
