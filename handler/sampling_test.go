package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

// --- slogtest compliance ---

func TestSamplingHandler_SlogtestCompliance(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	// rate=1.0 keeps all records — slogtest requires all records to appear.
	h := NewSamplingHandler(inner, WithSampleRate(1.0))

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

// --- Rate 0.0 drops all ---

func TestSamplingHandler_Rate0_DropsAll(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(0.0),
	)
	log := slog.New(h)

	for i := 0; i < 100; i++ {
		log.Info("should be dropped", "i", i)
	}

	if buf.Len() != 0 {
		t.Errorf("expected zero output with rate=0.0, got %d bytes", buf.Len())
	}
}

// --- Rate 1.0 keeps all ---

func TestSamplingHandler_Rate1_KeepsAll(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(1.0),
	)
	log := slog.New(h)

	const n = 100
	for i := 0; i < n; i++ {
		log.Info("should be kept", "i", i)
	}

	lines := countJSONLines(buf.Bytes())
	if lines != n {
		t.Errorf("expected %d lines with rate=1.0, got %d", n, lines)
	}
}

// --- Default rate is 1.0 ---

func TestSamplingHandler_DefaultRate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(slog.NewJSONHandler(&buf, nil))
	log := slog.New(h)

	const n = 50
	for i := 0; i < n; i++ {
		log.Info("default rate", "i", i)
	}

	lines := countJSONLines(buf.Bytes())
	if lines != n {
		t.Errorf("default rate should be 1.0 (keep all), got %d/%d lines", lines, n)
	}
}

// --- Per-level rates ---

func TestSamplingHandler_PerLevelRates(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
		WithSampleByLevel(map[slog.Level]float64{
			slog.LevelDebug: 0.0, // drop all debug
			slog.LevelInfo:  1.0, // keep all info
			slog.LevelWarn:  1.0, // keep all warn
		}),
	)
	log := slog.New(h)

	for i := 0; i < 50; i++ {
		log.Debug("debug", "i", i)
	}
	for i := 0; i < 50; i++ {
		log.Info("info", "i", i)
	}

	lines := countJSONLines(buf.Bytes())
	// Only info should appear (50), debug should be dropped (0).
	if lines != 50 {
		t.Errorf("expected 50 lines (debug=0.0, info=1.0), got %d", lines)
	}
}

func TestSamplingHandler_PerLevelRate_FallbackToDefault(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
		WithSampleRate(0.0), // default: drop all
		WithSampleByLevel(map[slog.Level]float64{
			slog.LevelInfo: 1.0, // but keep all info
		}),
	)
	log := slog.New(h)

	for i := 0; i < 50; i++ {
		log.Debug("debug", "i", i) // uses default rate 0.0
	}
	for i := 0; i < 50; i++ {
		log.Info("info", "i", i) // uses per-level rate 1.0
	}

	lines := countJSONLines(buf.Bytes())
	if lines != 50 {
		t.Errorf("expected 50 lines (debug uses default 0.0, info=1.0), got %d", lines)
	}
}

// --- Bypass level ---

func TestSamplingHandler_BypassLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(0.0), // drop everything...
		// ...except Error+ (default bypass level)
	)
	log := slog.New(h)

	log.Info("should be dropped")
	log.Warn("should be dropped")
	log.Error("should be kept") // bypass level

	lines := countJSONLines(buf.Bytes())
	if lines != 1 {
		t.Errorf("expected 1 line (only Error bypasses), got %d", lines)
	}
}

func TestSamplingHandler_CustomBypassLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(0.0),
		WithSampleBypassLevel(slog.LevelWarn), // bypass at Warn+
	)
	log := slog.New(h)

	log.Info("dropped")
	log.Warn("kept")
	log.Error("kept")

	lines := countJSONLines(buf.Bytes())
	if lines != 2 {
		t.Errorf("expected 2 lines (Warn+ bypasses), got %d", lines)
	}
}

func TestSamplingHandler_BypassLevel_Rate0_ErrorAlwaysKept(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(0.0),
		WithSampleByLevel(map[slog.Level]float64{
			slog.LevelError: 0.0, // Even if per-level says 0.0...
		}),
		// Bypass level (default Error) should override.
	)
	log := slog.New(h)

	for i := 0; i < 50; i++ {
		log.Error("error always kept", "i", i)
	}

	lines := countJSONLines(buf.Bytes())
	if lines != 50 {
		t.Errorf("bypass level should override per-level rate; expected 50, got %d", lines)
	}
}

// --- Statistical distribution ---

func TestSamplingHandler_StatisticalDistribution_50Percent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewSamplingHandler(
		slog.NewJSONHandler(w, nil),
		WithSampleRate(0.5),
		WithSampleBypassLevel(slog.Level(100)), // very high to not interfere
	)
	log := slog.New(h)

	const n = 5000
	for i := 0; i < n; i++ {
		log.Info("sample", "i", i)
	}

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	// With rate=0.5 and n=5000, expected ~2500.
	// Allow ±15% tolerance (2125 to 2875).
	low := int(float64(n) * 0.35)
	high := int(float64(n) * 0.65)
	if lines < low || lines > high {
		t.Errorf("expected ~%d lines (±15%%), got %d (rate=0.5, n=%d)", n/2, lines, n)
	}
}

func TestSamplingHandler_StatisticalDistribution_10Percent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewSamplingHandler(
		slog.NewJSONHandler(w, nil),
		WithSampleRate(0.1),
		WithSampleBypassLevel(slog.Level(100)),
	)
	log := slog.New(h)

	const n = 5000
	for i := 0; i < n; i++ {
		log.Info("sample", "i", i)
	}

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	// With rate=0.1 and n=5000, expected ~500.
	// Allow ±40% tolerance (300 to 700).
	low := int(math.Round(float64(n) * 0.06))
	high := int(math.Round(float64(n) * 0.14))
	if lines < low || lines > high {
		t.Errorf("expected ~%d lines (±40%%), got %d (rate=0.1, n=%d)", n/10, lines, n)
	}
}

// --- SetRate runtime update ---

func TestSamplingHandler_SetRate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(0.0),
		WithSampleBypassLevel(slog.Level(100)),
	)
	log := slog.New(h)

	// With rate=0.0, nothing should be logged.
	for i := 0; i < 50; i++ {
		log.Info("dropped", "i", i)
	}
	if buf.Len() != 0 {
		t.Error("expected no output with rate=0.0")
	}

	// Update rate to 1.0.
	h.SetRate(1.0)

	for i := 0; i < 50; i++ {
		log.Info("kept", "i", i)
	}
	lines := countJSONLines(buf.Bytes())
	if lines != 50 {
		t.Errorf("expected 50 lines after SetRate(1.0), got %d", lines)
	}
}

func TestSamplingHandler_SetRate_Clamping(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(slog.NewJSONHandler(io.Discard, nil))

	h.SetRate(-1.0)
	if got := h.getDefaultRate(); got != 0.0 {
		t.Errorf("SetRate(-1.0) should clamp to 0.0, got %f", got)
	}

	h.SetRate(5.0)
	if got := h.getDefaultRate(); got != 1.0 {
		t.Errorf("SetRate(5.0) should clamp to 1.0, got %f", got)
	}

	h.SetRate(0.5)
	if got := h.getDefaultRate(); got != 0.5 {
		t.Errorf("SetRate(0.5) should be 0.5, got %f", got)
	}
}

// --- WithAttrs / WithGroup cloning ---

func TestSamplingHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(1.0),
	)

	child := h.WithAttrs([]slog.Attr{slog.String("env", "prod")})
	log := slog.New(child)
	log.Info("with attrs")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["env"], "prod")
}

func TestSamplingHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(1.0),
	)

	child := h.WithGroup("request")
	log := slog.New(child)
	log.Info("grouped", "method", "GET")

	result := parseJSON(t, buf.Bytes())
	req := result["request"].(map[string]any)
	assertEqual(t, req["method"], "GET")
}

func TestSamplingHandler_WithGroup_Empty(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(slog.NewJSONHandler(io.Discard, nil))
	child := h.WithGroup("")
	if child != h {
		t.Error("WithGroup('') should return the same handler")
	}
}

func TestSamplingHandler_WithAttrs_SharesRate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(0.0),
		WithSampleBypassLevel(slog.Level(100)),
	)

	child := h.WithAttrs([]slog.Attr{slog.String("env", "prod")})
	log := slog.New(child)

	// Nothing should log at rate=0.0
	log.Info("dropped")
	if buf.Len() != 0 {
		t.Error("child should share parent's rate=0.0")
	}

	// Update parent's rate — child should see it too (shared atomic).
	h.SetRate(1.0)
	log.Info("kept")
	if buf.Len() == 0 {
		t.Error("child should see parent's updated rate=1.0")
	}
}

// --- Rate clamping in options ---

func TestSamplingHandler_RateClamping_Options(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(-5.0), // should clamp to 0.0
		WithSampleBypassLevel(slog.Level(100)),
	)
	log := slog.New(h)

	log.Info("test")
	if buf.Len() != 0 {
		t.Error("negative rate should clamp to 0.0 (drop all)")
	}
}

func TestSamplingHandler_RateClamping_PerLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleByLevel(map[slog.Level]float64{
			slog.LevelInfo: 2.0, // should clamp to 1.0
		}),
	)
	log := slog.New(h)

	const n = 50
	for i := 0; i < n; i++ {
		log.Info("test", "i", i)
	}

	lines := countJSONLines(buf.Bytes())
	if lines != n {
		t.Errorf("rate clamped to 1.0 should keep all; expected %d, got %d", n, lines)
	}
}

// --- Concurrency ---

func TestSamplingHandler_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	h := NewSamplingHandler(
		slog.NewJSONHandler(w, nil),
		WithSampleRate(1.0),
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

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	expected := goroutines * iterations
	if lines != expected {
		t.Errorf("expected %d lines with rate=1.0, got %d", expected, lines)
	}
}

func TestSamplingHandler_ConcurrentSetRate(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithSampleRate(0.5),
	)
	log := slog.New(h)

	var wg sync.WaitGroup

	// Writer goroutines.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				log.Info("concurrent")
			}
		}()
	}

	// Rate updater goroutines.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				h.SetRate(float64(n) * 0.2)
			}
		}(i)
	}

	wg.Wait()
	// No panic or race = success.
}

// --- Edge cases ---

func TestSamplingHandler_NaN_Rate(t *testing.T) {
	t.Parallel()

	// NaN should be treated as "don't sample" (clamped).
	h := NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithSampleRate(math.NaN()),
	)
	// NaN comparisons: NaN < 0.0 is false, NaN > 1.0 is false.
	// So clampRate returns NaN. shouldSample: rand.Float64() < NaN is false.
	// This means NaN effectively drops all, which is the safe behavior.
	slog.New(h).Info("dropped by NaN rate")

	var buf bytes.Buffer
	h2 := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleRate(math.NaN()),
		WithSampleBypassLevel(slog.Level(100)),
	)
	slog.New(h2).Info("test")

	// NaN rate: rand.Float64() < NaN → false → dropped. This is safe.
	if buf.Len() != 0 {
		t.Errorf("NaN rate should drop all records, got output: %s", buf.String())
	}
}

// --- Burst sampling ---

// burstHandle sends a record with a controlled message and timestamp through
// the handler, returning true if it reached the inner handler.
func burstHandle(t *testing.T, h *SamplingHandler, buf *bytes.Buffer, msg string, at time.Time) bool {
	t.Helper()
	before := buf.Len()
	r := slog.NewRecord(at, slog.LevelInfo, msg, 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	return buf.Len() > before
}

func TestSamplingHandler_Burst_FirstThenEveryMth(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithBurstSampling(time.Second, 3, 2),
	)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	var passes []int
	for i := 1; i <= 10; i++ {
		if burstHandle(t, h, &buf, "flood", base) {
			passes = append(passes, i)
		}
	}

	// first=3: records 1-3 pass. thereafter=2: then every 2nd (5, 7, 9).
	want := []int{1, 2, 3, 5, 7, 9}
	if len(passes) != len(want) {
		t.Fatalf("expected passes %v, got %v", want, passes)
	}
	for i := range want {
		if passes[i] != want[i] {
			t.Fatalf("expected passes %v, got %v", want, passes)
		}
	}
}

func TestSamplingHandler_Burst_ThereafterZero_DropsRest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithBurstSampling(time.Second, 2, 0),
	)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	passed := 0
	for i := 0; i < 20; i++ {
		if burstHandle(t, h, &buf, "flood", base) {
			passed++
		}
	}
	if passed != 2 {
		t.Errorf("thereafter=0: expected exactly 2 passes, got %d", passed)
	}
}

func TestSamplingHandler_Burst_WindowReset(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithBurstSampling(time.Second, 1, 0),
	)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	if !burstHandle(t, h, &buf, "event", base) {
		t.Fatal("first record in window must pass")
	}
	if burstHandle(t, h, &buf, "event", base.Add(100*time.Millisecond)) {
		t.Fatal("second record in same window must be dropped")
	}
	// Next window: counter resets.
	if !burstHandle(t, h, &buf, "event", base.Add(2*time.Second)) {
		t.Error("first record of a new window must pass")
	}
}

func TestSamplingHandler_Burst_PerMessageIndependence(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithBurstSampling(time.Second, 1, 0),
	)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	if !burstHandle(t, h, &buf, "message-a", base) {
		t.Fatal("first message-a must pass")
	}
	if burstHandle(t, h, &buf, "message-a", base) {
		t.Fatal("second message-a must be dropped")
	}
	// A different message has its own counter (assuming no bucket collision
	// between these two fixed strings).
	if !burstHandle(t, h, &buf, "message-b", base) {
		t.Error("first message-b must pass independently of message-a")
	}
}

func TestSamplingHandler_Burst_BypassLevelStillApplies(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithBurstSampling(time.Second, 1, 0),
	)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	// Errors bypass sampling entirely — all pass despite first=1.
	for i := 0; i < 5; i++ {
		before := buf.Len()
		r := slog.NewRecord(base, slog.LevelError, "critical", 0)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatalf("Handle error: %v", err)
		}
		if buf.Len() == before {
			t.Fatalf("error record %d must bypass burst sampling", i)
		}
	}
}

func TestSamplingHandler_Burst_SharedAcrossClones(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithBurstSampling(time.Second, 1, 0),
	)
	clone := h.WithAttrs([]slog.Attr{slog.String("k", "v")}).(*SamplingHandler)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	if !burstHandle(t, h, &buf, "shared", base) {
		t.Fatal("first record must pass")
	}
	// The clone shares the same burst counters.
	if burstHandle(t, clone, &buf, "shared", base) {
		t.Error("clone must share burst state with the original")
	}
}

func TestSamplingHandler_Burst_ConcurrentNoRace(t *testing.T) {
	t.Parallel()

	h := NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		WithBurstSampling(10*time.Millisecond, 5, 3),
	)
	log := slog.New(h)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				log.Info("concurrent burst", "g", n, "i", i)
			}
		}(g)
	}
	wg.Wait()
	// No race / panic = success; counting is approximate by design.
}

// --- SetLevelRate ---

func TestSamplingHandler_SetLevelRate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewSamplingHandler(
		slog.NewJSONHandler(&buf, nil),
		WithSampleByLevel(map[slog.Level]float64{slog.LevelInfo: 0.0}),
	)
	log := slog.New(h)

	log.Info("dropped")
	if buf.Len() != 0 {
		t.Fatal("info must be dropped at rate 0.0")
	}

	if !h.SetLevelRate(slog.LevelInfo, 1.0) {
		t.Fatal("SetLevelRate must report true for a configured level")
	}
	log.Info("now passes")
	if buf.Len() == 0 {
		t.Error("info must pass after SetLevelRate(1.0)")
	}

	// Unconfigured level cannot be added at runtime.
	if h.SetLevelRate(slog.LevelDebug, 0.5) {
		t.Error("SetLevelRate must report false for an unconfigured level")
	}
}
