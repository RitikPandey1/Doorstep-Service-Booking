package middleware

import (
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

const RequestIDHeader = "X-Request-Id"

// Client ids are echoed into logs and headers, so only accept short, plain tokens.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID reuses a well-formed incoming X-Request-Id or generates one, stores it
// in the gin context and echoes it on the response.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		c.Set(utils.RequestIDKey, id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}
