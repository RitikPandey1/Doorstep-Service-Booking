package models

type AvailabilityResponse struct {
	Date      string   `json:"date"`
	ServiceID string   `json:"serviceId"`
	Pincode   int      `json:"pincode"`
	Slots     []string `json:"slots"`
	Message   string   `json:"message,omitempty"`
}

type ScheduleResponse struct {
	Technician TechnicianRef     `json:"technician"`
	Date       string            `json:"date"`
	Bookings   []BookingResponse `json:"bookings"`
}
