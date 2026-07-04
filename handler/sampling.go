package handler

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync/atomic"
)

// SamplingHandler applies probabilistic or per-level sampling to log records,
// allowing high-volume logging in production without overwhelming storage or
// processing systems.
//
// Records at or above the bypass level (default: [slog.LevelError]) are never
// sampled — they always pass through. This ensures critical logs are never lost.
//
// SamplingHandler implements [slog.Handler] and follows the immutable clone
// pattern for [slog.Handler.WithAttrs] and [slog.Handler.WithGroup].
//
// The sampling decision uses [math/rand/v2], which is safe for concurrent use
// without additional synchronization.
type SamplingHandler struct {
	inner       slog.Handler
	defaultRate *atomic.Uint64         // float64 bits stored atomically; shared across clones
	levelRates  map[slog.Level]float64 // read-only after construction
	bypassLevel slog.Level
	passed      *atomic.Uint64 // shared across clones
	dropped     *atomic.Uint64 // shared across clones
}

// NewSamplingHandler creates a [SamplingHandler] that wraps the given inner
// handler with the specified sampling configuration.
//
// Defaults: rate=1.0 (keep all), bypassLevel=slog.LevelError.
func NewSamplingHandler(inner slog.Handler, opts ...SampleOption) *SamplingHandler {
	o := applySampleOptions(opts)

	bypassLevel := slog.LevelError
	if o.bypassLevel != nil {
		bypassLevel = *o.bypassLevel
	}

	rate := &atomic.Uint64{}
	rate.Store(math.Float64bits(o.defaultRate))

	h := &SamplingHandler{
		inner:       inner,
		defaultRate: rate,
		levelRates:  o.levelRates,
		bypassLevel: bypassLevel,
		passed:      &atomic.Uint64{},
		dropped:     &atomic.Uint64{},
	}

	return h
}

// Enabled reports whether the inner handler is enabled for the given level.
//
// SamplingHandler does not filter in Enabled — the sampling decision happens
// in Handle to avoid losing records before they can be evaluated. If the inner
// handler would not log at this level, Enabled returns false immediately.
func (h *SamplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Unwrap returns the inner handler, enabling lifecycle traversal.
func (h *SamplingHandler) Unwrap() slog.Handler { return h.inner }

// Handle applies the sampling decision and, if the record passes, delegates
// to the inner handler.
//
// Records at or above the bypass level always pass through. Other records
// are sampled based on their level-specific rate (if configured) or the
// default rate.
func (h *SamplingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Bypass level: always pass through.
	if r.Level >= h.bypassLevel {
		_ = h.passed.Add(1)
		return h.inner.Handle(ctx, r)
	}

	// Check sampling.
	if !h.shouldSample(r) {
		_ = h.dropped.Add(1)
		return nil // Sampled out; drop silently.
	}

	_ = h.passed.Add(1)
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new [SamplingHandler] where the inner handler has been
// cloned with the given attributes.
//
// The sampling configuration is shared (read-only) across all clones.
func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SamplingHandler{
		inner:       h.inner.WithAttrs(attrs),
		defaultRate: h.defaultRate,
		levelRates:  h.levelRates,
		bypassLevel: h.bypassLevel,
		passed:      h.passed,
		dropped:     h.dropped,
	}
}

// WithGroup returns a new [SamplingHandler] where the inner handler has been
// cloned with the given group name.
func (h *SamplingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &SamplingHandler{
		inner:       h.inner.WithGroup(name),
		defaultRate: h.defaultRate,
		levelRates:  h.levelRates,
		bypassLevel: h.bypassLevel,
		passed:      h.passed,
		dropped:     h.dropped,
	}
}

// SetRate updates the default sampling rate at runtime.
//
// This is safe for concurrent use. The new rate takes effect on the next
// sampling decision. Rate is clamped to [0.0, 1.0].
func (h *SamplingHandler) SetRate(rate float64) {
	h.defaultRate.Store(math.Float64bits(clampRate(rate)))
}

// getDefaultRate reads the current default rate atomically.
func (h *SamplingHandler) getDefaultRate() float64 {
	return math.Float64frombits(h.defaultRate.Load())
}

// Stats returns a snapshot of the handler's runtime statistics.
func (h *SamplingHandler) Stats() SampleStats {
	return SampleStats{
		Passed:  h.passed.Load(),
		Dropped: h.dropped.Load(),
	}
}

// shouldSample makes the probabilistic sampling decision for the given record.
func (h *SamplingHandler) shouldSample(r slog.Record) bool {
	// Look up the rate for this level.
	rate, ok := h.levelRates[r.Level]
	if !ok {
		rate = h.getDefaultRate()
	}

	// Fast paths.
	if rate >= 1.0 {
		return true
	}
	if rate <= 0.0 {
		return false
	}

	// Probabilistic decision. math/rand/v2 is concurrency-safe.
	return rand.Float64() < rate
}
