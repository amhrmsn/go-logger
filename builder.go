package logger

import (
	"log/slog"

	"github.com/amhrmsn/go-logger/handler"
)

// Builder provides a fluent API for composing [slog.Handler] middleware chains.
//
// The builder records configurations for each middleware layer and composes
// them in a fixed order when [Build] or [BuildLogger] is called.
//
// Composition order (innermost → outermost):
//
//	base → AsyncHandler → RedactionHandler → SamplingHandler → ModuleHandler → custom middleware
//
// This means ModuleHandler is checked first at call time (outermost), followed
// by SamplingHandler, RedactionHandler, and finally AsyncHandler wraps the
// base handler directly.
//
// Example:
//
//	log := logger.NewBuilder(slog.NewJSONHandler(os.Stdout, nil)).
//	    WithRedaction(handler.WithRedactKeys("password", "token")).
//	    WithAsync(handler.WithBufferSize(4096)).
//	    BuildLogger()
//	defer logger.Close(log.Handler())
type Builder struct {
	base         slog.Handler
	asyncOpts    []handler.AsyncOption
	redactOpts   []handler.RedactOption
	sampleOpts   []handler.SampleOption
	moduleConfig *handler.ModuleConfig
	middlewares  []func(slog.Handler) slog.Handler
	useAsync     bool
	useRedact    bool
	useSample    bool
	useModule    bool
}

// NewBuilder creates a [Builder] with the given base handler.
//
// The base handler is the innermost handler in the chain — typically a
// [slog.JSONHandler] or [slog.TextHandler].
func NewBuilder(base slog.Handler) *Builder {
	return &Builder{base: base}
}

// WithAsync enables the [handler.AsyncHandler] middleware with the given options.
//
// The async handler wraps the base handler directly (innermost middleware),
// buffering records in a channel for background processing.
func (b *Builder) WithAsync(opts ...handler.AsyncOption) *Builder {
	b.asyncOpts = opts
	b.useAsync = true
	return b
}

// WithRedaction enables the [handler.RedactionHandler] middleware with the
// given options.
//
// The redaction handler inspects and redacts sensitive attributes before
// they reach the base handler (or async handler).
func (b *Builder) WithRedaction(opts ...handler.RedactOption) *Builder {
	b.redactOpts = opts
	b.useRedact = true
	return b
}

// WithSampling enables the [handler.SamplingHandler] middleware with the
// given options.
//
// The sampling handler applies probabilistic filtering to reduce log volume.
func (b *Builder) WithSampling(opts ...handler.SampleOption) *Builder {
	b.sampleOpts = opts
	b.useSample = true
	return b
}

// WithModuleFilter enables the [handler.ModuleHandler] middleware with the
// given configuration.
//
// The module handler applies per-component log level filtering. It is the
// outermost built-in middleware, so it is checked first.
func (b *Builder) WithModuleFilter(cfg *handler.ModuleConfig) *Builder {
	b.moduleConfig = cfg
	b.useModule = true
	return b
}

// WithMiddleware adds a custom middleware function to the chain.
//
// Custom middleware is applied after all built-in middleware (outermost layer).
// Multiple calls to WithMiddleware append to the chain in registration order:
// the first registered middleware wraps the built-in chain, the second wraps
// the first, and so on. Therefore, the LAST registered middleware becomes
// the outermost handler and is executed FIRST at log time.
//
// Example with two custom middlewares:
//
//	builder.WithMiddleware(mwA).WithMiddleware(mwB).Build()
//	// Composition: base → ... → ModuleHandler → mwA → mwB
//	// Execution:   mwB → mwA → ModuleHandler → ... → base
func (b *Builder) WithMiddleware(mw func(slog.Handler) slog.Handler) *Builder {
	b.middlewares = append(b.middlewares, mw)
	return b
}

// Build composes the handler chain and returns the outermost handler.
//
// Composition order (innermost → outermost):
//
//	base → AsyncHandler → RedactionHandler → SamplingHandler → ModuleHandler → custom middleware
//
// Each middleware is only included if it was configured via the corresponding
// With*() method.
//
// Call Build (or [Builder.BuildLogger]) at most once per Builder. Every call
// composes a fresh chain around the same base handler; with [Builder.WithAsync]
// configured, each call starts its own background worker goroutine that must
// be closed independently.
func (b *Builder) Build() slog.Handler {
	h := b.base

	// 1. AsyncHandler wraps base directly (innermost).
	if b.useAsync {
		h = handler.NewAsyncHandler(h, b.asyncOpts...)
	}

	// 2. RedactionHandler.
	if b.useRedact {
		h = handler.NewRedactionHandler(h, b.redactOpts...)
	}

	// 3. SamplingHandler.
	if b.useSample {
		h = handler.NewSamplingHandler(h, b.sampleOpts...)
	}

	// 4. ModuleHandler (outermost built-in).
	if b.useModule {
		h = handler.NewModuleHandler(h, b.moduleConfig)
	}

	// 5. Custom middleware (outermost).
	for _, mw := range b.middlewares {
		h = mw(h)
	}

	return h
}

// BuildLogger composes the handler chain and returns a [*slog.Logger].
//
// This is equivalent to calling slog.New(b.Build()).
func (b *Builder) BuildLogger() *slog.Logger {
	return slog.New(b.Build())
}
