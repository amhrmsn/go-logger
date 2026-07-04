package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

// --- slogtest compliance ---

func TestMultiHandler_SlogtestCompliance(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	h := NewMultiHandler(inner)

	err := slogtest.TestHandler(h, func() []map[string]any {
		var results []map[string]any
		for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(line, &m); err == nil {
				results = append(results, m)
			}
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMultiHandler_SlogtestCompliance_TwoHandlers(t *testing.T) {
	t.Parallel()

	// Both handlers write to the same buffer — slogtest reads from it.
	// We only parse the first handler's output for compliance; the second
	// writes to a separate buffer just to verify fan-out.
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewJSONHandler(&buf1, nil)
	h2 := slog.NewJSONHandler(&buf2, nil)
	h := NewMultiHandler(h1, h2)

	err := slogtest.TestHandler(h, func() []map[string]any {
		var results []map[string]any
		for _, line := range bytes.Split(buf1.Bytes(), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(line, &m); err == nil {
				results = append(results, m)
			}
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}

// --- Enabled OR semantics ---

func TestMultiHandler_Enabled_ORSemantics(t *testing.T) {
	t.Parallel()

	// h1 accepts Info+, h2 accepts Warn+
	h1 := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})
	h2 := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewMultiHandler(h1, h2)

	ctx := context.Background()

	// Debug: neither accepts
	if h.Enabled(ctx, slog.LevelDebug) {
		t.Error("expected Enabled=false for Debug (neither handler accepts)")
	}

	// Info: h1 accepts, h2 doesn't → true (OR)
	if !h.Enabled(ctx, slog.LevelInfo) {
		t.Error("expected Enabled=true for Info (h1 accepts)")
	}

	// Warn: both accept → true
	if !h.Enabled(ctx, slog.LevelWarn) {
		t.Error("expected Enabled=true for Warn (both accept)")
	}

	// Error: both accept → true
	if !h.Enabled(ctx, slog.LevelError) {
		t.Error("expected Enabled=true for Error (both accept)")
	}
}

func TestMultiHandler_Enabled_NoHandlers(t *testing.T) {
	t.Parallel()

	h := NewMultiHandler()
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Enabled=false for zero handlers")
	}
}

func TestMultiHandler_Enabled_AllDisabled(t *testing.T) {
	t.Parallel()

	h1 := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})
	h2 := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})
	h := NewMultiHandler(h1, h2)

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Enabled=false when all handlers reject Debug")
	}
}

// --- Handle fan-out ---

func TestMultiHandler_Handle_FanOut(t *testing.T) {
	t.Parallel()

	var buf1, buf2 bytes.Buffer
	h1 := slog.NewJSONHandler(&buf1, nil)
	h2 := slog.NewJSONHandler(&buf2, nil)
	h := NewMultiHandler(h1, h2)

	log := slog.New(h)
	log.Info("hello", "key", "value")

	// Both buffers should have output
	var r1, r2 map[string]any
	if err := json.Unmarshal(buf1.Bytes(), &r1); err != nil {
		t.Fatalf("buf1: %v", err)
	}
	if err := json.Unmarshal(buf2.Bytes(), &r2); err != nil {
		t.Fatalf("buf2: %v", err)
	}

	if r1["msg"] != "hello" {
		t.Errorf("buf1: expected msg 'hello', got %v", r1["msg"])
	}
	if r2["msg"] != "hello" {
		t.Errorf("buf2: expected msg 'hello', got %v", r2["msg"])
	}
	if r1["key"] != "value" {
		t.Errorf("buf1: expected key 'value', got %v", r1["key"])
	}
	if r2["key"] != "value" {
		t.Errorf("buf2: expected key 'value', got %v", r2["key"])
	}
}

func TestMultiHandler_Handle_RespectLevels(t *testing.T) {
	t.Parallel()

	var buf1, buf2 bytes.Buffer
	h1 := slog.NewJSONHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelInfo})
	h2 := slog.NewJSONHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelError})
	h := NewMultiHandler(h1, h2)

	log := slog.New(h)
	log.Info("info only")

	// buf1 should have the message (Info level accepted)
	if buf1.Len() == 0 {
		t.Error("buf1 (Info handler) should have output")
	}

	// buf2 should be empty (Error handler rejects Info)
	if buf2.Len() != 0 {
		t.Error("buf2 (Error handler) should not have output for Info message")
	}

	// Now send an Error
	buf1.Reset()
	log.Error("error for both")

	if buf1.Len() == 0 {
		t.Error("buf1 should have error output")
	}
	if buf2.Len() == 0 {
		t.Error("buf2 should have error output")
	}
}

func TestMultiHandler_Handle_ThreeHandlers(t *testing.T) {
	t.Parallel()

	var buf1, buf2, buf3 bytes.Buffer
	h := NewMultiHandler(
		slog.NewJSONHandler(&buf1, nil),
		slog.NewJSONHandler(&buf2, nil),
		slog.NewJSONHandler(&buf3, nil),
	)

	log := slog.New(h)
	log.Info("three-way")

	for i, buf := range []*bytes.Buffer{&buf1, &buf2, &buf3} {
		if buf.Len() == 0 {
			t.Errorf("buf%d should have output", i+1)
		}
	}
}

// --- Error aggregation ---

// errorHandler is a handler that always returns an error from Handle.
type errorHandler struct {
	slog.Handler
	handleErr error
}

func (e *errorHandler) Handle(_ context.Context, _ slog.Record) error {
	return e.handleErr
}

func (e *errorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &errorHandler{Handler: e.Handler.WithAttrs(attrs), handleErr: e.handleErr}
}

func (e *errorHandler) WithGroup(name string) slog.Handler {
	return &errorHandler{Handler: e.Handler.WithGroup(name), handleErr: e.handleErr}
}

func TestMultiHandler_Handle_ErrorAggregation(t *testing.T) {
	t.Parallel()

	err1 := errors.New("handler1 failed")
	err2 := errors.New("handler2 failed")

	h := NewMultiHandler(
		&errorHandler{Handler: slog.NewJSONHandler(io.Discard, nil), handleErr: err1},
		&errorHandler{Handler: slog.NewJSONHandler(io.Discard, nil), handleErr: err2},
	)

	ctx := context.Background()
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	err := h.Handle(ctx, record)

	if err == nil {
		t.Fatal("expected error from Handle")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "handler1 failed") {
		t.Errorf("expected error to contain 'handler1 failed', got %q", errStr)
	}
	if !strings.Contains(errStr, "handler2 failed") {
		t.Errorf("expected error to contain 'handler2 failed', got %q", errStr)
	}
}

func TestMultiHandler_Handle_PartialError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewMultiHandler(
		slog.NewJSONHandler(&buf, nil), // succeeds
		&errorHandler{Handler: slog.NewJSONHandler(io.Discard, nil), handleErr: errors.New("fail")}, // fails
	)

	ctx := context.Background()
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "partial", 0)
	err := h.Handle(ctx, record)

	// Should still have written to buf (first handler succeeded)
	if buf.Len() == 0 {
		t.Error("successful handler should still write even if another fails")
	}

	// Error should come from the failing handler
	if err == nil {
		t.Fatal("expected error from failing handler")
	}
	if !strings.Contains(err.Error(), "fail") {
		t.Errorf("expected error 'fail', got %q", err.Error())
	}
}

func TestMultiHandler_Handle_NoErrors(t *testing.T) {
	t.Parallel()

	h := NewMultiHandler(
		slog.NewJSONHandler(io.Discard, nil),
		slog.NewJSONHandler(io.Discard, nil),
	)

	log := slog.New(h)
	// If Handle returns an error, slog will call the default handler
	// Just verify no panic occurs
	log.Info("no errors")
}

// --- WithAttrs / WithGroup cloning ---

func TestMultiHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	var buf1, buf2 bytes.Buffer
	h := NewMultiHandler(
		slog.NewJSONHandler(&buf1, nil),
		slog.NewJSONHandler(&buf2, nil),
	)

	// Create a child with attrs
	child := h.WithAttrs([]slog.Attr{slog.String("env", "prod")})
	log := slog.New(child)
	log.Info("with attrs", "extra", "data")

	for i, buf := range []*bytes.Buffer{&buf1, &buf2} {
		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("buf%d: %v", i+1, err)
		}
		if result["env"] != "prod" {
			t.Errorf("buf%d: expected env='prod', got %v", i+1, result["env"])
		}
		if result["extra"] != "data" {
			t.Errorf("buf%d: expected extra='data', got %v", i+1, result["extra"])
		}
	}

	// Original should NOT have the attrs
	buf1.Reset()
	buf2.Reset()
	origLog := slog.New(h)
	origLog.Info("no attrs")

	var r1 map[string]any
	if err := json.Unmarshal(buf1.Bytes(), &r1); err != nil {
		t.Fatalf("buf1 original: %v", err)
	}
	if _, exists := r1["env"]; exists {
		t.Error("original handler should not have 'env' attr")
	}
}

func TestMultiHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf1, buf2 bytes.Buffer
	h := NewMultiHandler(
		slog.NewJSONHandler(&buf1, nil),
		slog.NewJSONHandler(&buf2, nil),
	)

	child := h.WithGroup("request")
	log := slog.New(child)
	log.Info("grouped", "method", "GET", "path", "/api")

	for i, buf := range []*bytes.Buffer{&buf1, &buf2} {
		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("buf%d: %v", i+1, err)
		}
		req, ok := result["request"].(map[string]any)
		if !ok {
			t.Fatalf("buf%d: expected 'request' group, got %T", i+1, result["request"])
		}
		if req["method"] != "GET" {
			t.Errorf("buf%d: expected method='GET', got %v", i+1, req["method"])
		}
		if req["path"] != "/api" {
			t.Errorf("buf%d: expected path='/api', got %v", i+1, req["path"])
		}
	}
}

func TestMultiHandler_WithGroup_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewMultiHandler(slog.NewJSONHandler(&buf, nil))

	// Empty group name should return the same handler
	child := h.WithGroup("")
	if child != h {
		t.Error("WithGroup('') should return the same handler")
	}
}

func TestMultiHandler_WithAttrs_ThenWithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewMultiHandler(slog.NewJSONHandler(&buf, nil))

	child := h.WithAttrs([]slog.Attr{slog.String("service", "api")})
	grouped := child.WithGroup("req")
	log := slog.New(grouped)
	log.Info("chained", "method", "POST")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result["service"] != "api" {
		t.Errorf("expected service='api', got %v", result["service"])
	}
	req, ok := result["req"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'req' group, got %T", result["req"])
	}
	if req["method"] != "POST" {
		t.Errorf("expected method='POST', got %v", req["method"])
	}
}

// --- Concurrency ---

func TestMultiHandler_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	var buf1, buf2 bytes.Buffer
	var mu1, mu2 sync.Mutex
	w1 := &syncWriter{buf: &buf1, mu: &mu1}
	w2 := &syncWriter{buf: &buf2, mu: &mu2}

	h := NewMultiHandler(
		slog.NewJSONHandler(w1, nil),
		slog.NewJSONHandler(w2, nil),
	)

	log := slog.New(h)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				log.Info("concurrent", "goroutine", n, "iteration", j)
			}
		}(i)
	}
	wg.Wait()

	// Count lines in each buffer
	expected := goroutines * iterations
	for i, w := range []*syncWriter{w1, w2} {
		w.mu.Lock()
		lines := countJSONLines(w.buf.Bytes())
		w.mu.Unlock()
		if lines != expected {
			t.Errorf("buf%d: expected %d lines, got %d", i+1, expected, lines)
		}
	}
}

// --- Lifecycle tests ---

type lifecycleMockHandler struct {
	closed  bool
	flushed bool
}

func (m *lifecycleMockHandler) Enabled(_ context.Context, _ slog.Level) bool  { return true }
func (m *lifecycleMockHandler) Handle(_ context.Context, _ slog.Record) error { return nil }
func (m *lifecycleMockHandler) WithAttrs(_ []slog.Attr) slog.Handler          { return m }
func (m *lifecycleMockHandler) WithGroup(_ string) slog.Handler               { return m }

func (m *lifecycleMockHandler) CloseContext(_ context.Context) error {
	m.closed = true
	return nil
}

func (m *lifecycleMockHandler) FlushContext(_ context.Context) error {
	m.flushed = true
	return nil
}

func TestMultiHandler_CloseContext(t *testing.T) {
	t.Parallel()

	h1 := &lifecycleMockHandler{}
	h2 := &lifecycleMockHandler{}
	multi := NewMultiHandler(h1, h2)

	err := multi.CloseContext(context.Background())
	if err != nil {
		t.Errorf("CloseContext error: %v", err)
	}

	if !h1.closed || !h2.closed {
		t.Error("MultiHandler did not close all children")
	}
}

func TestMultiHandler_FlushContext(t *testing.T) {
	t.Parallel()

	h1 := &lifecycleMockHandler{}
	h2 := &lifecycleMockHandler{}
	multi := NewMultiHandler(h1, h2)

	err := multi.FlushContext(context.Background())
	if err != nil {
		t.Errorf("FlushContext error: %v", err)
	}

	if !h1.flushed || !h2.flushed {
		t.Error("MultiHandler did not flush all children")
	}
}

// Regression: CloseContext must traverse each child's Unwrap chain so that
// closeable handlers wrapped by pass-through middleware are closed too.
func TestMultiHandler_CloseContext_UnwrapsWrappedChildren(t *testing.T) {
	t.Parallel()

	async := NewAsyncHandler(slog.NewJSONHandler(io.Discard, nil))
	redact := NewRedactionHandler(async, WithRedactKeys("password"))
	multi := NewMultiHandler(redact, slog.NewTextHandler(io.Discard, nil))

	if err := multi.CloseContext(context.Background()); err != nil {
		t.Fatalf("CloseContext error: %v", err)
	}

	err := async.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "after close", 0))
	if !errors.Is(err, ErrHandlerClosed) {
		t.Errorf("wrapped AsyncHandler was not closed by MultiHandler.CloseContext, Handle returned %v", err)
	}
}

// Regression: FlushContext must traverse each child's Unwrap chain so that
// flushable handlers wrapped by pass-through middleware are flushed too.
func TestMultiHandler_FlushContext_UnwrapsWrappedChildren(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var buf bytes.Buffer
	w := &syncWriter{buf: &buf, mu: &mu}

	async := NewAsyncHandler(slog.NewJSONHandler(w, nil))
	defer func() { _ = async.Close() }()
	redact := NewRedactionHandler(async, WithRedactKeys("password"))
	multi := NewMultiHandler(redact)

	slog.New(multi).Info("buffered record")

	if err := multi.FlushContext(context.Background()); err != nil {
		t.Fatalf("FlushContext error: %v", err)
	}

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()
	if lines != 1 {
		t.Errorf("expected 1 record written after flush, got %d", lines)
	}
}

// --- helpers ---

// syncWriter wraps a bytes.Buffer with a mutex for concurrent writes.
type syncWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w *syncWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func countJSONLines(data []byte) int {
	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) > 0 {
			count++
		}
	}
	return count
}
