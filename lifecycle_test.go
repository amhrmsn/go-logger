package logger

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/amhrmsn/go-logger/handler"
)

// --- Close ---

func TestClose_WithCloser(t *testing.T) {
	t.Parallel()

	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)

	err := Close(h)
	if err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestClose_NilHandler(t *testing.T) {
	t.Parallel()

	err := Close(nil)
	if err != nil {
		t.Errorf("Close(nil) should return nil, got %v", err)
	}
}

func TestClose_NonCloser(t *testing.T) {
	t.Parallel()

	h := slog.NewJSONHandler(io.Discard, nil)
	err := Close(h)
	if err != nil {
		t.Errorf("Close() on non-Closer should return nil, got %v", err)
	}
}

func TestClose_TraversesChain(t *testing.T) {
	t.Parallel()

	// Build a chain: Redaction → Async → JSON
	// Close should traverse through Redaction to find Async.
	async := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)
	h := handler.NewRedactionHandler(async, handler.WithRedactKeys("pw"))

	err := Close(h)
	if err != nil {
		t.Errorf("Close() via chain traversal error: %v", err)
	}
}

// --- CloseContext ---

func TestCloseContext_WithTimeout(t *testing.T) {
	t.Parallel()

	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := CloseContext(ctx, h)
	if err != nil {
		t.Errorf("CloseContext() error: %v", err)
	}
}

func TestCloseContext_NilHandler(t *testing.T) {
	t.Parallel()

	err := CloseContext(context.Background(), nil)
	if err != nil {
		t.Errorf("CloseContext(nil) should return nil, got %v", err)
	}
}

// --- Flush ---

func TestFlush_WithFlusher(t *testing.T) {
	t.Parallel()

	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)
	defer h.Close()

	err := Flush(h)
	if err != nil {
		t.Errorf("Flush() error: %v", err)
	}
}

func TestFlush_NilHandler(t *testing.T) {
	t.Parallel()

	err := Flush(nil)
	if err != nil {
		t.Errorf("Flush(nil) should return nil, got %v", err)
	}
}

func TestFlush_NonFlusher(t *testing.T) {
	t.Parallel()

	h := slog.NewJSONHandler(io.Discard, nil)
	err := Flush(h)
	if err != nil {
		t.Errorf("Flush() on non-Flusher should return nil, got %v", err)
	}
}

func TestFlush_TraversesChain(t *testing.T) {
	t.Parallel()

	async := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)
	h := handler.NewRedactionHandler(async, handler.WithRedactKeys("pw"))

	slog.New(h).Info("test")

	err := Flush(h)
	if err != nil {
		t.Errorf("Flush() via chain traversal error: %v", err)
	}

	_ = Close(h)
}

// --- FlushContext ---

func TestFlushContext_WithTimeout(t *testing.T) {
	t.Parallel()

	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := FlushContext(ctx, h)
	if err != nil {
		t.Errorf("FlushContext() error: %v", err)
	}
}

func TestFlushContext_NilHandler(t *testing.T) {
	t.Parallel()

	err := FlushContext(context.Background(), nil)
	if err != nil {
		t.Errorf("FlushContext(nil) should return nil, got %v", err)
	}
}

// --- Interface satisfaction ---

func TestAsyncHandler_ImplementsContextCloser(t *testing.T) {
	t.Parallel()

	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)

	var cc ContextCloser = h
	if err := cc.CloseContext(context.Background()); err != nil {
		t.Errorf("CloseContext() error: %v", err)
	}
}

func TestAsyncHandler_ImplementsContextFlusher(t *testing.T) {
	t.Parallel()

	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(16),
	)
	defer h.Close()

	var cf ContextFlusher = h
	if err := cf.FlushContext(context.Background()); err != nil {
		t.Errorf("FlushContext() error: %v", err)
	}
}

// --- Fix 5 regression: Lifecycle propagation ---

type testCloser struct {
	inner  slog.Handler
	closed bool
}

func (c *testCloser) Enabled(ctx context.Context, level slog.Level) bool   { return true }
func (c *testCloser) Handle(ctx context.Context, record slog.Record) error { return nil }
func (c *testCloser) WithAttrs(attrs []slog.Attr) slog.Handler             { return c }
func (c *testCloser) WithGroup(name string) slog.Handler                   { return c }
func (c *testCloser) Unwrap() slog.Handler                                 { return c.inner }
func (c *testCloser) CloseContext(ctx context.Context) error {
	c.closed = true
	return nil
}

func TestCloseContext_PropagatesThroughChain(t *testing.T) {
	t.Parallel()

	innerCloser := &testCloser{}
	outerCloser := &testCloser{inner: innerCloser}

	err := CloseContext(context.Background(), outerCloser)
	if err != nil {
		t.Errorf("CloseContext error: %v", err)
	}

	if !outerCloser.closed {
		t.Error("outer closer was not closed")
	}
	if !innerCloser.closed {
		t.Error("inner closer was not closed (lifecycle propagation failed)")
	}
}

func TestFlushContext_PropagatesThroughChain(t *testing.T) {
	t.Parallel()

	// Same pattern for flusher
	innerFlusher := &testFlusher{}
	outerFlusher := &testFlusher{inner: innerFlusher}

	err := FlushContext(context.Background(), outerFlusher)
	if err != nil {
		t.Errorf("FlushContext error: %v", err)
	}

	if !outerFlusher.flushed {
		t.Error("outer flusher was not flushed")
	}
	if !innerFlusher.flushed {
		t.Error("inner flusher was not flushed (lifecycle propagation failed)")
	}
}

type testFlusher struct {
	inner   slog.Handler
	flushed bool
}

func (f *testFlusher) Enabled(ctx context.Context, level slog.Level) bool   { return true }
func (f *testFlusher) Handle(ctx context.Context, record slog.Record) error { return nil }
func (f *testFlusher) WithAttrs(attrs []slog.Attr) slog.Handler             { return f }
func (f *testFlusher) WithGroup(name string) slog.Handler                   { return f }
func (f *testFlusher) Unwrap() slog.Handler                                 { return f.inner }
func (f *testFlusher) FlushContext(ctx context.Context) error {
	f.flushed = true
	return nil
}
