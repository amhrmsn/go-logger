package logger

import "log/slog"

// Option configures the base [slog.Handler] created by [NewJSON] and [NewText].
type Option func(*options)

// options holds the collected configuration for handler construction.
type options struct {
	level       slog.Leveler
	addSource   bool
	replaceAttr func(groups []string, a slog.Attr) slog.Attr
}

// handlerOptions converts the collected options into a [*slog.HandlerOptions]
// suitable for passing to [slog.NewJSONHandler] or [slog.NewTextHandler].
func (o *options) handlerOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level:       o.level,
		AddSource:   o.addSource,
		ReplaceAttr: o.replaceAttr,
	}
}

// applyOptions creates an [options] with defaults and applies all provided
// [Option] functions.
func applyOptions(opts []Option) *options {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithLevel sets the minimum log level for the handler.
//
// Any [slog.Leveler] can be used, including a [*slog.LevelVar] for dynamic
// runtime level changes.
func WithLevel(level slog.Leveler) Option {
	return func(o *options) {
		o.level = level
	}
}

// WithLevelVar sets a dynamic log level that can be changed at runtime
// without restarting the application.
//
// This is equivalent to calling [WithLevel] with the [*slog.LevelVar], but
// communicates the intent more clearly.
//
// The [slog.LevelVar] is safe for concurrent use by multiple goroutines.
func WithLevelVar(lv *slog.LevelVar) Option {
	return func(o *options) {
		o.level = lv
	}
}

// WithSource enables or disables source code location (file, line, function)
// in log output.
//
// Enabling source location has a performance cost due to [runtime.Caller]
// stack introspection. Consider enabling it only in development or for
// specific debugging scenarios.
func WithSource(enabled bool) Option {
	return func(o *options) {
		o.addSource = enabled
	}
}

// WithReplaceAttr sets a function that is called for each non-group [slog.Attr]
// before it is logged. The function can modify, replace, or remove attributes.
//
// This is useful for:
//   - Redacting sensitive fields by key name
//   - Customizing timestamp or source location formatting
//   - Removing unwanted built-in attributes
//
// See [slog.HandlerOptions.ReplaceAttr] for details on the function signature.
func WithReplaceAttr(fn func(groups []string, a slog.Attr) slog.Attr) Option {
	return func(o *options) {
		o.replaceAttr = fn
	}
}
