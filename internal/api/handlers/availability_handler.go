package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/services"
)

type AvailabilityHandler struct {
	availability *services.AvailabilityService
}

func NewAvailabilityHandler(availability *services.AvailabilityService) *AvailabilityHandler {
	return &AvailabilityHandler{availability: availability}
}

// Get handles GET /availability?serviceId=&pincode=&date=.
func (h *AvailabilityHandler) Get(c *gin.Context) {
	resp, err := h.availability.Get(c.Request.Context(), c.Query("serviceId"), c.Query("pincode"), c.Query("date"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
