package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/models"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/services"
	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

type BookingHandler struct {
	bookings *services.BookingService
}

func NewBookingHandler(bookings *services.BookingService) *BookingHandler {
	return &BookingHandler{bookings: bookings}
}

// Create handles POST /bookings. Field validation lives in the service; this only rejects malformed JSON.
func (h *BookingHandler) Create(c *gin.Context) {
	var req models.CreateBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, utils.ValidationError("request body must be JSON with serviceId, pincode (number), startTime and phone"))
		return
	}
	b, err := h.bookings.Create(c.Request.Context(), req, c.GetHeader("Idempotency-Key"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, b.ToResponse())
}

// List handles GET /bookings?phone=&status=&from=&to=&page=&size=.
func (h *BookingHandler) List(c *gin.Context) {
	resp, err := h.bookings.List(c.Request.Context(), services.ListQuery{
		Phone: c.Query("phone"), Status: c.Query("status"), From: c.Query("from"), To: c.Query("to"),
		Page: c.Query("page"), Size: c.Query("size"),
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Get handles GET /bookings/:id.
func (h *BookingHandler) Get(c *gin.Context) {
	b, err := h.bookings.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, b.ToResponse())
}

// Cancel handles POST /bookings/:id/cancel.
func (h *BookingHandler) Cancel(c *gin.Context) {
	b, err := h.bookings.Cancel(c.Request.Context(), c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, b.ToResponse())
}
