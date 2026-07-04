package logger

import (
	"context"
	"errors"
	"log/slog"
)

// Closer represents a [slog.Handler] that holds resources requiring cleanup.
//
// Handlers that open files, network connections, or background goroutines
// should implement this interface to support graceful shutdown.
type Closer interface {
	Close() error
}

// ContextCloser is like [Closer] but accepts a context for deadline and
// cancellation support. Handlers should prefer implementing this interface
// to allow callers to bound shutdown time.
type ContextCloser interface {
	CloseContext(ctx context.Context) error
}

// Flusher represents a [slog.Handler] that buffers data internally.
//
// Handlers that buffer log records (such as [AsyncHandler]) should implement
// this interface to allow callers to ensure all buffered records are written
// before inspecting output or shutting down.
type Flusher interface {
	Flush() error
}

// ContextFlusher is like [Flusher] but accepts a context for deadline and
// cancellation support.
type ContextFlusher interface {
	FlushContext(ctx context.Context) error
}

// Unwrapper is implemented by middleware handlers that wrap an inner handler.
//
// Implementing Unwrapper lets [Close], [CloseContext], [Flush], and
// [FlushContext] traverse past a handler to reach lifecycle-aware handlers
// (such as [handler.AsyncHandler]) deeper in the chain. All middleware in
// this library implements it; third-party middleware that wraps another
// handler should too, otherwise the chain traversal stops at that handler
// and inner resources are never flushed or closed.
type Unwrapper interface {
	Unwrap() slog.Handler
}

// Close attempts to close the given handler by checking if it implements
// [Closer]. If it does not, Close recursively unwraps the handler chain
// (via the Unwrap() method) to find a Closer in the middleware stack.
//
// Usage in application shutdown:
//
//	h := log.Handler()
//	if err := logger.Close(h); err != nil {
//	    fmt.Fprintf(os.Stderr, "logger close: %v\n", err)
//	}
func Close(h slog.Handler) error {
	return CloseContext(context.Background(), h)
}

// CloseContext is like [Close] but accepts a context for deadline and
// cancellation support. It prefers [ContextCloser] if implemented,
// falling back to [Closer].
//
// CloseContext propagates through the entire middleware chain: after calling
// a handler's lifecycle method, it continues unwrapping to close inner
// handlers as well. This ensures that all handlers in the chain (including
// MultiHandler children) receive the close signal.
//
// Errors from multiple handlers are aggregated using [errors.Join].
func CloseContext(ctx context.Context, h slog.Handler) error {
	var errs []error
	for h != nil {
		if c, ok := h.(ContextCloser); ok {
			if err := c.CloseContext(ctx); err != nil {
				errs = append(errs, err)
			}
			// Continue to inner handler to propagate lifecycle.
		} else if c, ok := h.(Closer); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if u, ok := h.(Unwrapper); ok {
			h = u.Unwrap()
		} else {
			break
		}
	}
	return errors.Join(errs...)
}

// Flush attempts to flush the given handler by checking if it implements
// [Flusher]. If it does not, Flush recursively unwraps the handler chain
// (via the Unwrap() method) to find a Flusher in the middleware stack.
//
// After Flush returns, all records submitted before the Flush call are
// guaranteed to have been written to the underlying output.
func Flush(h slog.Handler) error {
	return FlushContext(context.Background(), h)
}

// FlushContext is like [Flush] but accepts a context for deadline and
// cancellation support. It prefers [ContextFlusher] if implemented,
// falling back to [Flusher].
//
// FlushContext propagates through the entire middleware chain: after calling
// a handler's lifecycle method, it continues unwrapping to flush inner
// handlers as well.
//
// Errors from multiple handlers are aggregated using [errors.Join].
func FlushContext(ctx context.Context, h slog.Handler) error {
	var errs []error
	for h != nil {
		if f, ok := h.(ContextFlusher); ok {
			if err := f.FlushContext(ctx); err != nil {
				errs = append(errs, err)
			}
		} else if f, ok := h.(Flusher); ok {
			if err := f.Flush(); err != nil {
				errs = append(errs, err)
			}
		}
		if u, ok := h.(Unwrapper); ok {
			h = u.Unwrap()
		} else {
			break
		}
	}
	return errors.Join(errs...)
}
