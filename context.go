package logger

import (
	"context"
	"log/slog"
)

// ctxKey is the private context key for the logger value.
type ctxKey struct{}

// NewContext returns a copy of ctx that carries log.
//
// This enables the common request-scoped logger pattern: attach a logger
// enriched with request attributes once, then retrieve it anywhere below
// with [FromContext].
//
//	log := logger.FromContext(ctx).With("request_id", id)
//	ctx = logger.NewContext(ctx, log)
func NewContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, log)
}

// FromContext returns the [*slog.Logger] stored in ctx by [NewContext].
//
// If ctx is nil or carries no logger, [slog.Default] is returned, so the
// result is always safe to use.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}
	if log, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return log
	}
	return slog.Default()
}
