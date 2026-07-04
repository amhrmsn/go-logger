package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
)

// --- MultiHandler: record clone per child ---

func TestMultiHandler_RecordClonePerChild(t *testing.T) {
	t.Parallel()

	// Create two handlers that both try to read attrs from the same record.
	// Without cloning, concurrent attr iteration could cause issues.
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
	const goroutines = 100
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				log.Info("clone test",
					"goroutine", n,
					"iteration", j,
					"data", "some-value",
				)
			}
		}(i)
	}
	wg.Wait()

	expected := goroutines * iterations

	mu1.Lock()
	lines1 := countJSONLines(buf1.Bytes())
	mu1.Unlock()

	mu2.Lock()
	lines2 := countJSONLines(buf2.Bytes())
	mu2.Unlock()

	if lines1 != expected {
		t.Errorf("buf1: expected %d lines, got %d", expected, lines1)
	}
	if lines2 != expected {
		t.Errorf("buf2: expected %d lines, got %d", expected, lines2)
	}
}

// --- AsyncHandler: stats counters ---

func TestAsyncHandler_Stats_WrittenCounter(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(256),
		WithDropPolicy(Block),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	log := slog.New(h)
	const n = 50
	for i := 0; i < n; i++ {
		log.Info("test", "i", i)
	}

	_ = h.Flush()

	stats := h.Stats()
	if stats.Written != uint64(n) {
		t.Errorf("expected Written=%d, got %d", n, stats.Written)
	}
	if stats.Errors != 0 {
		t.Errorf("expected Errors=0, got %d", stats.Errors)
	}

	_ = h.Close()
}

func TestAsyncHandler_Stats_DroppedCounter(t *testing.T) {
	t.Parallel()

	h := NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBufferSize(1),
		WithDropPolicy(DropNewest),
		WithAsyncBypassLevel(slog.Level(100)),
	)

	for i := 0; i < 100; i++ {
		slog.New(h).Info("test", "i", i)
	}

	_ = h.Flush()
	_ = h.Close()

	stats := h.Stats()
	if stats.Written+stats.Dropped != 100 {
		t.Errorf("Written(%d) + Dropped(%d) should = 100", stats.Written, stats.Dropped)
	}
	if stats.Dropped == 0 {
		t.Error("expected some records to be dropped with tiny buffer")
	}
}

// --- SamplingHandler: stats counters ---

func TestSamplingHandler_Stats_AllPassed(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithSampleRate(1.0),
	)
	log := slog.New(h)

	const n = 50
	for i := 0; i < n; i++ {
		log.Info("test", "i", i)
	}

	stats := h.Stats()
	if stats.Passed != uint64(n) {
		t.Errorf("expected Passed=%d, got %d", n, stats.Passed)
	}
	if stats.Dropped != 0 {
		t.Errorf("expected Dropped=0, got %d", stats.Dropped)
	}
}

func TestSamplingHandler_Stats_AllDropped(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithSampleRate(0.0),
		WithSampleBypassLevel(slog.Level(100)),
	)
	log := slog.New(h)

	const n = 50
	for i := 0; i < n; i++ {
		log.Info("test", "i", i)
	}

	stats := h.Stats()
	if stats.Dropped != uint64(n) {
		t.Errorf("expected Dropped=%d, got %d", n, stats.Dropped)
	}
	if stats.Passed != 0 {
		t.Errorf("expected Passed=0, got %d", stats.Passed)
	}
}

func TestSamplingHandler_Stats_BypassCountsAsPassed(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithSampleRate(0.0), // drop all non-error
		// Default bypass: Error
	)
	log := slog.New(h)

	log.Error("error passes bypass")

	stats := h.Stats()
	if stats.Passed != 1 {
		t.Errorf("expected Passed=1 (bypass), got %d", stats.Passed)
	}
}

func TestSamplingHandler_Stats_SharedAcrossClones(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithSampleRate(1.0),
	)

	child := h.WithAttrs([]slog.Attr{slog.String("env", "test")})
	log := slog.New(child)
	log.Info("from child")

	stats := h.Stats()
	if stats.Passed != 1 {
		t.Errorf("parent Stats should see child's records; got Passed=%d", stats.Passed)
	}
}

// --- Cross-handler concurrent race test ---

func TestFullChain_ConcurrentRace(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	config := NewModuleConfig(slog.LevelDebug)

	inner := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})

	h := NewModuleHandler(
		NewSamplingHandler(
			NewRedactionHandler(
				NewAsyncHandler(inner,
					WithBufferSize(4096),
					WithDropPolicy(Block),
					WithAsyncBypassLevel(slog.Level(100)),
				),
				WithRedactKeys("password"),
			),
			WithSampleRate(1.0),
		),
		config,
	)

	log := slog.New(h)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				log.Info("race test",
					"goroutine", n,
					"iteration", j,
					"password", "secret",
					"component", "api",
				)
			}
		}(i)
	}
	wg.Wait()

	// Find the async handler to flush.
	findAsync := func(hh slog.Handler) *AsyncHandler {
		type unwrap interface{ Unwrap() slog.Handler }
		for hh != nil {
			if a, ok := hh.(*AsyncHandler); ok {
				return a
			}
			if u, ok := hh.(unwrap); ok {
				hh = u.Unwrap()
			} else {
				break
			}
		}
		return nil
	}

	async := findAsync(h)
	if async == nil {
		t.Fatal("could not find AsyncHandler in chain")
	}
	_ = async.Flush()
	_ = async.Close()

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	data := make([]byte, buf.Len())
	_ = copy(data, buf.Bytes())
	mu.Unlock()

	expected := goroutines * iterations
	if lines != expected {
		t.Errorf("expected %d lines, got %d", expected, lines)
	}

	// Verify redaction on a sample of lines.
	allLines := bytes.Split(data, []byte("\n"))
	checked := 0
	for _, line := range allLines {
		if len(line) == 0 {
			continue
		}
		var result map[string]any
		if err := json.Unmarshal(line, &result); err != nil {
			continue
		}
		if result["password"] != "[REDACTED]" {
			t.Errorf("password should be redacted, got %v", result["password"])
		}
		checked++
		if checked >= 10 {
			break // Spot check is enough.
		}
	}
}
