package handler

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync/atomic"
)

// SamplingHandler applies probabilistic, per-level, or burst sampling to log
// records, allowing high-volume logging in production without overwhelming
// storage or processing systems.
//
// Two sampling modes are available:
//
//   - Probabilistic (default): each record passes with the configured
//     probability ([WithSampleRate], [WithSampleByLevel]).
//   - Burst ([WithBurstSampling]): within each time window, the first N
//     records per unique message always pass, then every M-th passes. This
//     guarantees the first occurrences of a rare event are never lost, which
//     probabilistic sampling cannot promise. When burst sampling is
//     configured it replaces the probabilistic decision entirely.
//
// Records at or above the bypass level (default: [slog.LevelError]) are never
// sampled — they always pass through. This ensures critical logs are never lost.
//
// SamplingHandler implements [slog.Handler] and follows the immutable clone
// pattern for [slog.Handler.WithAttrs] and [slog.Handler.WithGroup].
//
// The probabilistic decision uses [math/rand/v2], which is safe for concurrent
// use without additional synchronization.
type SamplingHandler struct {
	inner       slog.Handler
	defaultRate *atomic.Uint64                 // float64 bits stored atomically; shared across clones
	levelRates  map[slog.Level]*atomic.Uint64 // keys fixed at construction; rates updatable via SetLevelRate
	burst       *burstSampler                  // non-nil when burst sampling is configured; shared across clones
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

	var levelRates map[slog.Level]*atomic.Uint64
	if len(o.levelRates) > 0 {
		levelRates = make(map[slog.Level]*atomic.Uint64, len(o.levelRates))
		for lv, r := range o.levelRates {
			p := &atomic.Uint64{}
			p.Store(math.Float64bits(r))
			levelRates[lv] = p
		}
	}

	var burst *burstSampler
	if o.burst != nil {
		burst = newBurstSampler(o.burst.interval, o.burst.first, o.burst.thereafter)
	}

	h := &SamplingHandler{
		inner:       inner,
		defaultRate: rate,
		levelRates:  levelRates,
		burst:       burst,
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
		burst:       h.burst,
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
		burst:       h.burst,
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

// SetLevelRate updates the sampling rate for a level that was configured via
// [WithSampleByLevel]. It reports whether the level was found.
//
// Levels not present at construction cannot be added at runtime (the level
// map is fixed to keep the hot path lock-free); use [SetRate] for the
// default rate instead. Rate is clamped to [0.0, 1.0].
//
// This is safe for concurrent use.
func (h *SamplingHandler) SetLevelRate(level slog.Level, rate float64) bool {
	p, ok := h.levelRates[level]
	if !ok {
		return false
	}
	p.Store(math.Float64bits(clampRate(rate)))
	return true
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

// shouldSample makes the sampling decision for the given record.
//
// When burst sampling is configured it replaces the probabilistic decision;
// otherwise the record is sampled by its level-specific or default rate.
func (h *SamplingHandler) shouldSample(r slog.Record) bool {
	if h.burst != nil {
		return h.burst.allow(r.Message, r.Time)
	}

	// Look up the rate for this level.
	rate := h.getDefaultRate()
	if p, ok := h.levelRates[r.Level]; ok {
		rate = math.Float64frombits(p.Load())
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
