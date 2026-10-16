// Package models holds API request/response shapes. It imports no other internal package.
package models

type CreateBookingRequest struct {
	ServiceID string `json:"serviceId" binding:"required"`
	Pincode   int    `json:"pincode" binding:"required"`
	StartTime string `json:"startTime" binding:"required"`
	Phone     string `json:"phone" binding:"required"`
}

type TechnicianRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type BookingResponse struct {
	ID          string        `json:"id"`
	ServiceID   string        `json:"serviceId"`
	ServiceName string        `json:"serviceName"`
	Pincode     int           `json:"pincode"`
	Phone       string        `json:"phone"`
	StartTime   string        `json:"startTime"`
	EndTime     string        `json:"endTime"`
	Status      string        `json:"status"`
	Technician  TechnicianRef `json:"technician"`
	CreatedAt   string        `json:"createdAt"`
}

type BookingListResponse struct {
	Items []BookingResponse `json:"items"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
	Total int               `json:"total"`
}
