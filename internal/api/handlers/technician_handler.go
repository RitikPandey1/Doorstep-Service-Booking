package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/services"
)

type TechnicianHandler struct {
	technicians *services.TechnicianService
}

func NewTechnicianHandler(technicians *services.TechnicianService) *TechnicianHandler {
	return &TechnicianHandler{technicians: technicians}
}

// Schedule handles GET /technicians/:id/schedule?date=.
func (h *TechnicianHandler) Schedule(c *gin.Context) {
	resp, err := h.technicians.Schedule(c.Request.Context(), c.Param("id"), c.Query("date"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
