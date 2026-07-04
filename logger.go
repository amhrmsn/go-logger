package logger

import (
	"io"
	"log/slog"
)

// New creates a [*slog.Logger] with the given handler.
//
// This is a thin convenience wrapper around [slog.New]. The handler is typically
// built using the [Builder] or composed manually from handler middleware.
func New(h slog.Handler) *slog.Logger {
	return slog.New(h)
}

// NewJSON creates a [*slog.Logger] with a [slog.JSONHandler] writing to w.
//
// Options are applied to the underlying [slog.HandlerOptions]. If no options are
// provided, the handler uses default settings (Info level, no source).
func NewJSON(w io.Writer, opts ...Option) *slog.Logger {
	o := applyOptions(opts)
	return slog.New(slog.NewJSONHandler(w, o.handlerOptions()))
}

// NewText creates a [*slog.Logger] with a [slog.TextHandler] writing to w.
//
// Options are applied to the underlying [slog.HandlerOptions]. If no options are
// provided, the handler uses default settings (Info level, no source).
func NewText(w io.Writer, opts ...Option) *slog.Logger {
	o := applyOptions(opts)
	return slog.New(slog.NewTextHandler(w, o.handlerOptions()))
}

// SetDefault sets the default [*slog.Logger] used by the top-level functions
// in [log/slog].
//
// This is a convenience wrapper around [slog.SetDefault].
func SetDefault(l *slog.Logger) {
	slog.SetDefault(l)
}

// Default returns the current default [*slog.Logger].
//
// This is a convenience wrapper around [slog.Default].
func Default() *slog.Logger {
	return slog.Default()
}
