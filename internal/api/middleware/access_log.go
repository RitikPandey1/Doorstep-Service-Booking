package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog writes one structured line per request. Health probes are skipped to keep logs readable.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/healthz" || path == "/readyz" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		slog.InfoContext(c.Request.Context(), "request",
			"method", c.Request.Method,
			"path", path,
			"route", c.FullPath(),
			"status", c.Writer.Status(),
			"latencyMs", time.Since(start).Milliseconds(),
			"clientIp", c.ClientIP(),
		)
	}
}
