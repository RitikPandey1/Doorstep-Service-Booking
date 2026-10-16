package utils

import (
	"context"
	"log/slog"
)

type requestIDKey struct{}

// WithRequestID returns ctx carrying the request id, so logs and errors further down can read it.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFrom returns the request id stored in ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// LogHandler adds the request id from the context to every record logged with a *Context call.
type LogHandler struct {
	slog.Handler
}

func NewLogHandler(h slog.Handler) LogHandler {
	return LogHandler{Handler: h}
}

func (h LogHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDFrom(ctx); id != "" {
		r.AddAttrs(slog.String("requestId", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return LogHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h LogHandler) WithGroup(name string) slog.Handler {
	return LogHandler{Handler: h.Handler.WithGroup(name)}
}
