package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

// --- slogtest compliance ---

func TestAsyncHandler_SlogtestCompliance(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	inner := slog.NewJSONHandler(w, nil)
	h := NewAsyncHandler(inner,
		WithBufferSize(1024),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)), // disable bypass for compliance test
	)
	defer h.Close()

	err := slogtest.TestHandler(h, func() []map[string]any {
		// Flush before reading to ensure all records are written.
		if ferr := h.Flush(); ferr != nil {
			t.Fatalf("flush error: %v", ferr)
		}

		mu.Lock()
		data := make([]byte, buf.Len())
		_ = copy(data, buf.Bytes())
		mu.Unlock()

		var results []map[string]any
		for _, line := range bytes.Split(data, []byte("\n")) {
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

// --- DropNewest policy ---

func TestAsyncHandler_DropNewest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(2), // tiny buffer
		WithDropPolicy(DropNewest),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	// Fill the buffer and overflow.
	for i := 0; i < 100; i++ {
		slog.New(h).Info("test", "i", i)
	}

	if err := h.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}
	_ = h.Close()

	dropped := h.DroppedCount()
	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	// Some records should be dropped, some should be written.
	total := int(dropped) + lines
	if total != 100 {
		t.Errorf("dropped(%d) + written(%d) = %d, expected 100", dropped, lines, total)
	}
	if dropped == 0 {
		t.Error("expected some records to be dropped with tiny buffer")
	}
}

// --- Block policy ---

func TestAsyncHandler_Block(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(4),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	const n = 50
	for i := 0; i < n; i++ {
		slog.New(h).Info("block test", "i", i)
	}

	_ = h.Flush()
	_ = h.Close()

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	// Block policy: all records should be written (no drops).
	if lines != n {
		t.Errorf("expected %d lines with Block policy, got %d", n, lines)
	}
	if h.DroppedCount() != 0 {
		t.Errorf("expected 0 dropped with Block policy, got %d", h.DroppedCount())
	}
}

// --- SyncFallback policy ---

func TestAsyncHandler_SyncFallback(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(2),
		WithDropPolicy(SyncFallback),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	const n = 50
	for i := 0; i < n; i++ {
		slog.New(h).Info("sync fallback test", "i", i)
	}

	_ = h.Flush()
	_ = h.Close()

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	// SyncFallback: all records should be written (some sync, some async).
	if lines != n {
		t.Errorf("expected %d lines with SyncFallback policy, got %d", n, lines)
	}
	if h.DroppedCount() != 0 {
		t.Errorf("expected 0 dropped with SyncFallback, got %d", h.DroppedCount())
	}
}

// --- Bypass level ---

func TestAsyncHandler_BypassLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(1024),
		WithDropPolicy(DropNewest),
		// Default bypass level: Error
	)

	log := slog.New(h)

	// Error should be written synchronously.
	log.Error("sync error")

	// No need to flush — bypass writes are synchronous.
	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	if lines != 1 {
		t.Errorf("expected 1 line from bypass sync write, got %d", lines)
	}

	_ = h.Close()
}

// --- Close idempotency ---

func TestAsyncHandler_Close_Idempotent(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(16),
	)

	// Close multiple times — should not panic or deadlock.
	for i := 0; i < 5; i++ {
		err := h.Close()
		if err != nil {
			t.Errorf("Close() call %d returned error: %v", i+1, err)
		}
	}
}

// --- Flush determinism ---

func TestAsyncHandler_Flush_Deterministic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(1024),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	log := slog.New(h)

	const n = 100
	for i := 0; i < n; i++ {
		log.Info("before flush", "i", i)
	}

	// After Flush, all records must be written.
	if err := h.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	if lines != n {
		t.Errorf("expected %d lines after Flush, got %d", n, lines)
	}

	_ = h.Close()
}

func TestAsyncHandler_Flush_MultipleFlushes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(1024),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	log := slog.New(h)

	log.Info("batch 1")
	_ = h.Flush()

	mu.Lock()
	lines1 := countJSONLines(buf.Bytes())
	mu.Unlock()

	log.Info("batch 2")
	_ = h.Flush()

	mu.Lock()
	lines2 := countJSONLines(buf.Bytes())
	mu.Unlock()

	if lines1 != 1 {
		t.Errorf("after first flush: expected 1, got %d", lines1)
	}
	if lines2 != 2 {
		t.Errorf("after second flush: expected 2, got %d", lines2)
	}

	_ = h.Close()
}

// --- DroppedCount accuracy ---

func TestAsyncHandler_DroppedCount(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(1),
		WithDropPolicy(DropNewest),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	// Initial count should be 0.
	if h.DroppedCount() != 0 {
		t.Errorf("initial dropped count should be 0, got %d", h.DroppedCount())
	}

	_ = h.Close()

	// After close, count should still be accessible.
	_ = h.DroppedCount()
}

// --- Handle after Close ---

func TestAsyncHandler_Handle_AfterClose(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(16),
	)

	_ = h.Close()

	log := slog.New(h)
	// Logging after close should not panic. slog swallows the error.
	log.Info("after close")
}

// --- WithAttrs / WithGroup ---

func TestAsyncHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(1024),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	child := h.WithAttrs([]slog.Attr{slog.String("env", "prod")})
	log := slog.New(child)
	log.Info("with attrs")

	_ = h.Flush()

	mu.Lock()
	result := parseJSON(t, buf.Bytes())
	mu.Unlock()

	assertEqual(t, result["env"], "prod")
	assertEqual(t, result["msg"], "with attrs")

	_ = h.Close()
}

func TestAsyncHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(1024),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	child := h.WithGroup("request")
	log := slog.New(child)
	log.Info("grouped", "method", "GET")

	_ = h.Flush()

	mu.Lock()
	result := parseJSON(t, buf.Bytes())
	mu.Unlock()

	req := result["request"].(map[string]any)
	assertEqual(t, req["method"], "GET")

	_ = h.Close()
}

func TestAsyncHandler_WithGroup_Empty(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(slog.NewJSONHandler(io.Discard, nil), WithBufferSize(16))
	defer h.Close()

	child := h.WithGroup("")
	if child != h {
		t.Error("WithGroup('') should return the same handler")
	}
}

// --- Concurrency test: 100 goroutines × 100 writes ---

func TestAsyncHandler_ConcurrentWrites_100x100(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(4096),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	log := slog.New(h)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 100

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

	_ = h.Flush()
	_ = h.Close()

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	expected := goroutines * iterations
	if lines != expected {
		t.Errorf("expected %d lines, got %d", expected, lines)
	}
}

func TestAsyncHandler_ConcurrentWrites_WithDropNewest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(
		slog.NewJSONHandler(w, nil),
		WithBufferSize(64),
		WithDropPolicy(DropNewest),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	log := slog.New(h)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				log.Info("concurrent drop", "goroutine", n)
			}
		}(i)
	}
	wg.Wait()

	_ = h.Flush()
	_ = h.Close()

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	dropped := h.DroppedCount()
	total := int(dropped) + lines
	expected := goroutines * iterations

	if total != expected {
		t.Errorf("dropped(%d) + written(%d) = %d, expected %d", dropped, lines, total, expected)
	}
}

// --- Lifecycle: Closer and Flusher interfaces ---

func TestAsyncHandler_ImplementsCloser(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(slog.NewJSONHandler(io.Discard, nil), WithBufferSize(16))

	// Verify it satisfies the Closer interface from lifecycle.go
	var c interface{ Close() error } = h
	if err := c.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestAsyncHandler_ImplementsFlusher(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(slog.NewJSONHandler(io.Discard, nil), WithBufferSize(16))
	defer h.Close()

	// Verify it satisfies the Flusher interface from lifecycle.go
	var f interface{ Flush() error } = h
	if err := f.Flush(); err != nil {
		t.Errorf("Flush() error: %v", err)
	}
}

// --- Default options ---

func TestAsyncHandler_DefaultOptions(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	// No options — should use defaults.
	h := NewAsyncHandler(slog.NewJSONHandler(w, nil))

	log := slog.New(h)
	log.Info("default options")

	_ = h.Flush()
	_ = h.Close()

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	if lines != 1 {
		t.Errorf("expected 1 line, got %d", lines)
	}
}

// --- Fix 2 regression: FlushContext barrier deadlock ---

func TestAsyncHandler_FlushContext_TimeoutNoDeadlock(t *testing.T) {
	t.Parallel()

	// Create a handler with a slow inner handler that blocks writes.
	blockCh := make(chan struct{})
	inner := &blockingHandler{blockCh: blockCh}

	h := NewAsyncHandler(inner,
		WithBufferSize(4),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	// Enqueue a record that will block the worker.
	slog.New(h).Info("will block")

	// FlushContext with a very short timeout: should return deadline exceeded,
	// NOT deadlock.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := h.FlushContext(ctx)
	if err == nil {
		t.Error("expected context deadline exceeded, got nil")
	}

	// Unblock the inner handler.
	close(blockCh)

	// Worker should still be alive. CloseContext must complete.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	if err := h.CloseContext(closeCtx); err != nil {
		t.Errorf("CloseContext after unblock should succeed, got %v", err)
	}
}

// Regression: a FlushContext that races with Close could enqueue its barrier
// after the worker exited; the barrier was never acknowledged and Flush()
// (background context) hung forever. FlushContext must now observe the worker
// exit and return promptly — nil or ErrHandlerClosed, never a deadline error.
func TestAsyncHandler_FlushVsClose_Race_NoHang(t *testing.T) {
	t.Parallel()

	const iterations = 2000
	const flushers = 4

	for i := 0; i < iterations; i++ {
		h := NewAsyncHandler(slog.NewJSONHandler(io.Discard, nil))

		res := make(chan error, flushers)
		for f := 0; f < flushers; f++ {
			go func() {
				var bad error
				for j := 0; j < 25; j++ {
					ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
					err := h.FlushContext(ctx)
					cancel()
					if errors.Is(err, context.DeadlineExceeded) {
						bad = err
						break
					}
					if err != nil {
						break // ErrHandlerClosed: expected once Close wins.
					}
				}
				res <- bad
			}()
		}

		_ = h.Close()

		for f := 0; f < flushers; f++ {
			if err := <-res; err != nil {
				t.Fatalf("iteration %d: FlushContext hung until deadline: %v", i, err)
			}
		}
	}
}

// Regression: flush barriers used to be marked by a magic record message, so
// a user record with that exact message was swallowed as a barrier. Barriers
// are now flagged on the queue item itself; any record message must be logged.
func TestAsyncHandler_BarrierLikeMessage_IsLogged(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var buf bytes.Buffer
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewAsyncHandler(slog.NewJSONHandler(w, nil))
	defer func() { _ = h.Close() }()

	slog.New(h).Info("\x00__go_logger_barrier__\x00")

	if err := h.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()
	if lines != 1 {
		t.Errorf("expected barrier-like message to be logged, got %d records", lines)
	}
}

// --- Fix 3 regression: Handle vs Close lifecycle race ---

func TestAsyncHandler_HandleVsClose_Race(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(64),
		WithDropPolicy(DropNewest),
	)

	log := slog.New(h)

	// Start many goroutines logging concurrently.
	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100
	started := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-started
			for j := 0; j < iterations; j++ {
				log.Info("concurrent with close")
			}
		}()
	}

	// Unblock all goroutines simultaneously.
	close(started)

	// Close while writes are in-flight.
	time.Sleep(1 * time.Millisecond)
	_ = h.CloseContext(context.Background())

	wg.Wait()

	// After close, all Handle calls must return ErrHandlerClosed.
	err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "after close", 0))
	if err != ErrHandlerClosed {
		t.Errorf("Handle after close should return ErrHandlerClosed, got %v", err)
	}
}

// --- Fix 4 regression: context preservation ---

func TestAsyncHandler_ContextPreservation(t *testing.T) {
	t.Parallel()

	type ctxKey string
	const key ctxKey = "request_id"

	var gotValue string
	var gotMu sync.Mutex

	inner := &contextCapturingHandler{
		onHandle: func(ctx context.Context) {
			gotMu.Lock()
			defer gotMu.Unlock()
			if v, ok := ctx.Value(key).(string); ok {
				gotValue = v
			}
		},
	}

	h := NewAsyncHandler(inner,
		WithBufferSize(64),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)), // disable bypass
	)

	ctx := context.WithValue(context.Background(), key, "req-12345")
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	_ = h.Handle(ctx, r)
	_ = h.Flush()
	_ = h.Close()

	gotMu.Lock()
	defer gotMu.Unlock()
	if gotValue != "req-12345" {
		t.Errorf("inner handler should see context value 'req-12345', got %q", gotValue)
	}
}

// --- Fix 6 regression: stats consistency ---

func TestAsyncHandler_Stats_BypassWrittenCounted(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(64),
		// Default bypass level: Error
	)

	log := slog.New(h)

	// Bypass-level write (synchronous).
	log.Error("bypass write")

	stats := h.Stats()
	if stats.Written != 1 {
		t.Errorf("bypass write should increment Written, got %d", stats.Written)
	}

	_ = h.Close()
}

func TestAsyncHandler_Stats_SyncFallbackCounted(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(1),
		WithDropPolicy(SyncFallback),
		WithAsyncBypassLevel(slog.Level(100)), // disable bypass
	)

	log := slog.New(h)

	// Overwhelm the buffer to trigger SyncFallback.
	for i := 0; i < 50; i++ {
		log.Info("sync fallback test", "i", i)
	}

	_ = h.Flush()

	stats := h.Stats()
	if stats.Written == 0 {
		t.Error("SyncFallback writes should be counted in Written")
	}
	if stats.Dropped != 0 {
		t.Errorf("SyncFallback should not drop records, got Dropped=%d", stats.Dropped)
	}

	_ = h.Close()
}

func TestAsyncHandler_Stats_WorkerWrittenCounted(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(64),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)), // disable bypass
	)

	log := slog.New(h)
	const n = 10
	for i := 0; i < n; i++ {
		log.Info("worker test", "i", i)
	}

	_ = h.Flush()

	stats := h.Stats()
	if stats.Written != uint64(n) {
		t.Errorf("worker writes: expected Written=%d, got %d", n, stats.Written)
	}

	_ = h.Close()
}

// --- Helpers for async regression tests ---

// blockingHandler blocks Handle() until blockCh is closed.
type blockingHandler struct {
	blockCh chan struct{}
}

func (h *blockingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *blockingHandler) Handle(_ context.Context, _ slog.Record) error {
	<-h.blockCh
	return nil
}
func (h *blockingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *blockingHandler) WithGroup(_ string) slog.Handler      { return h }

// contextCapturingHandler captures the context passed to Handle.
type contextCapturingHandler struct {
	onHandle func(ctx context.Context)
}

func (h *contextCapturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *contextCapturingHandler) Handle(ctx context.Context, _ slog.Record) error {
	if h.onHandle != nil {
		h.onHandle(ctx)
	}
	return nil
}
func (h *contextCapturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *contextCapturingHandler) WithGroup(_ string) slog.Handler      { return h }
