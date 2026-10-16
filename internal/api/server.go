package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the Gin engine with all middleware and routes.
func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	return r
}

// NewServer wraps the router in an http.Server with sane timeouts.
func NewServer(addr string, router http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
