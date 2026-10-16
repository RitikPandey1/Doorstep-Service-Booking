package utils

import "net/http"

// Error codes returned in the API error body.
const (
	CodeValidation            = "VALIDATION_ERROR"
	CodeUnserviceablePincode  = "UNSERVICEABLE_PINCODE"
	CodeNotFound              = "NOT_FOUND"
	CodeNoAvailability        = "NO_AVAILABILITY"
	CodeCancellationClosed    = "CANCELLATION_WINDOW_CLOSED"
	CodeInvalidState          = "INVALID_STATE"
	CodeIdempotencyKeyReused  = "IDEMPOTENCY_KEY_REUSED"
	CodeRateLimited           = "RATE_LIMITED"
	CodeBookingBusy           = "BOOKING_BUSY"
	CodeDependencyUnavailable = "DEPENDENCY_UNAVAILABLE"
	CodeInternal              = "INTERNAL_ERROR"
)

// AppError is an error that knows its HTTP status and API error code.
type AppError struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *AppError) Error() string { return e.Code + ": " + e.Message }

func NewAppError(status int, code, message string) *AppError {
	return &AppError{Status: status, Code: code, Message: message}
}

func ValidationError(message string) *AppError {
	return NewAppError(http.StatusBadRequest, CodeValidation, message)
}

func NotFoundError(message string) *AppError {
	return NewAppError(http.StatusNotFound, CodeNotFound, message)
}
