package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/repository"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

// RespondError writes err in the standard error body. A database outage becomes a retryable 503;
// any other unknown error becomes a logged 500.
func RespondError(c *gin.Context, err error) {
	var appErr *utils.AppError
	switch {
	case errors.As(err, &appErr):
	case errors.Is(err, repository.ErrUnavailable):
		slog.WarnContext(c.Request.Context(), "database unavailable", "error", err)
		appErr = utils.NewAppError(http.StatusServiceUnavailable, utils.CodeDependencyUnavailable,
			"service is temporarily unavailable, please retry")
	default:
		slog.ErrorContext(c.Request.Context(), "unhandled error", "error", err,
			"method", c.Request.Method, "path", c.FullPath())
		appErr = utils.NewAppError(http.StatusInternalServerError, utils.CodeInternal, "internal server error")
	}
	if appErr.Status == http.StatusServiceUnavailable {
		c.Header("Retry-After", "1") // BOOKING_BUSY / DEPENDENCY_UNAVAILABLE are safe to retry
	}
	c.AbortWithStatusJSON(appErr.Status, models.ErrorResponse{Error: models.ErrorBody{
		Code:      appErr.Code,
		Message:   appErr.Message,
		Details:   appErr.Details,
		RequestID: utils.RequestIDFrom(c.Request.Context()),
	}})
}
