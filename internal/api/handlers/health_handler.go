package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
)

const (
	statusUp   = "UP"
	statusDown = "DOWN"
)

// HealthHandler serves liveness and readiness. checks maps a dependency name to its ping.
type HealthHandler struct {
	checks map[string]func(context.Context) error
}

func NewHealthHandler(checks map[string]func(context.Context) error) *HealthHandler {
	return &HealthHandler{checks: checks}
}

// Live handles GET /healthz: the process is up. It never touches dependencies,
// so a database blip does not get healthy instances restarted.
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, models.HealthResponse{Status: statusUp})
}

// Ready handles GET /readyz: 200 only when every dependency answers its ping.
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	resp := models.HealthResponse{Status: statusUp, Checks: map[string]string{}}
	code := http.StatusOK
	for name, ping := range h.checks {
		resp.Checks[name] = statusUp
		if err := ping(ctx); err != nil {
			slog.WarnContext(ctx, "readiness check failed", "dependency", name, "error", err)
			resp.Checks[name] = statusDown
			resp.Status, code = statusDown, http.StatusServiceUnavailable
		}
	}
	c.JSON(code, resp)
}
