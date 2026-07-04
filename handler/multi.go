package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/amhrmsn/go-logger/internal/record"
)

// MultiHandler fans out log records to multiple [slog.Handler] implementations
// simultaneously.
//
// Each record is dispatched to every child handler whose [slog.Handler.Enabled]
// method returns true for the record's level. Errors from individual handlers
// are aggregated using [errors.Join].
//
// MultiHandler follows the immutable clone pattern: [MultiHandler.WithAttrs]
// and [MultiHandler.WithGroup] return new instances wrapping cloned children,
// ensuring concurrency safety.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler creates a [MultiHandler] that fans out to the given handlers.
//
// At least one handler should be provided. If zero handlers are given, the
// resulting handler will silently discard all records.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	// Copy the slice to prevent external mutation.
	h := make([]slog.Handler, len(handlers))
	_ = copy(h, handlers)
	return &MultiHandler{handlers: h}
}

// Enabled reports whether ANY child handler is enabled for the given level.
//
// This uses OR semantics: if at least one child handler would accept a record
// at this level, Enabled returns true. This ensures no records are dropped
// prematurely — individual handlers perform their own level checks in Handle.
func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle dispatches the record to every child handler that is enabled for the
// record's level.
//
// Each child receives its own copy of the record so that no mutable state is
// shared between children. The last enabled child receives the original
// record, since ownership of r ends here; with a single enabled child no
// clone is made at all.
//
// Errors from individual handlers are collected and returned as a single error
// using [errors.Join]. A failure in one handler does not prevent dispatch to
// other handlers.
func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	// Dispatch lags one handler behind discovery: when a further enabled
	// child is found, the pending one gets a clone; the final pending child
	// gets the original record.
	var pending slog.Handler
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if pending != nil {
			if err := pending.Handle(ctx, record.CloneRecord(r)); err != nil {
				errs = append(errs, err)
			}
		}
		pending = h
	}
	if pending != nil {
		if err := pending.Handle(ctx, r); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a new [MultiHandler] where each child handler has been
// cloned with the given attributes.
//
// The original MultiHandler and its children are not modified.
func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: handlers}
}

// WithGroup returns a new [MultiHandler] where each child handler has been
// cloned with the given group name.
//
// The original MultiHandler and its children are not modified.
func (m *MultiHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return m
	}
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: handlers}
}

// CloseContext closes all child handlers that implement lifecycle interfaces.
//
// Each child's middleware chain is traversed via Unwrap(), so closeable
// handlers wrapped by pass-through middleware (e.g., a RedactionHandler
// wrapping an AsyncHandler) are closed as well. At each level, ContextCloser
// is preferred over Closer. Errors are aggregated using [errors.Join]; a
// failure in one child does not prevent other children from being closed.
func (m *MultiHandler) CloseContext(ctx context.Context) error {
	var errs []error
	for _, h := range m.handlers {
		for cur := h; cur != nil; {
			if c, ok := cur.(interface{ CloseContext(context.Context) error }); ok {
				if err := c.CloseContext(ctx); err != nil {
					errs = append(errs, err)
				}
			} else if c, ok := cur.(interface{ Close() error }); ok {
				if err := c.Close(); err != nil {
					errs = append(errs, err)
				}
			}
			u, ok := cur.(interface{ Unwrap() slog.Handler })
			if !ok {
				break
			}
			cur = u.Unwrap()
		}
	}
	return errors.Join(errs...)
}

// FlushContext flushes all child handlers that implement lifecycle interfaces.
//
// Each child's middleware chain is traversed via Unwrap(), so flushable
// handlers wrapped by pass-through middleware are flushed as well. At each
// level, ContextFlusher is preferred over Flusher. Errors are aggregated using
// [errors.Join]; a failure in one child does not prevent other children from
// being flushed.
func (m *MultiHandler) FlushContext(ctx context.Context) error {
	var errs []error
	for _, h := range m.handlers {
		for cur := h; cur != nil; {
			if f, ok := cur.(interface{ FlushContext(context.Context) error }); ok {
				if err := f.FlushContext(ctx); err != nil {
					errs = append(errs, err)
				}
			} else if f, ok := cur.(interface{ Flush() error }); ok {
				if err := f.Flush(); err != nil {
					errs = append(errs, err)
				}
			}
			u, ok := cur.(interface{ Unwrap() slog.Handler })
			if !ok {
				break
			}
			cur = u.Unwrap()
		}
	}
	return errors.Join(errs...)
}
