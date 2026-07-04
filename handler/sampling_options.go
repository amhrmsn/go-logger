package handler

import (
	"log/slog"
	"time"
)

// SampleOption configures a [SamplingHandler].
type SampleOption func(*sampleOptions)

// burstConfig holds the configuration collected by [WithBurstSampling].
type burstConfig struct {
	interval   time.Duration
	first      uint64
	thereafter uint64
}

// sampleOptions holds the collected configuration for [SamplingHandler].
type sampleOptions struct {
	defaultRate float64
	levelRates  map[slog.Level]float64
	burst       *burstConfig
	bypassLevel *slog.Level
}

func applySampleOptions(opts []SampleOption) *sampleOptions {
	o := &sampleOptions{
		defaultRate: 1.0, // keep all by default
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithSampleRate sets the global sampling rate applied to all levels
// (unless overridden by [WithSampleByLevel]).
//
// Rate must be between 0.0 (drop all) and 1.0 (keep all).
// Values outside this range are clamped.
func WithSampleRate(rate float64) SampleOption {
	return func(o *sampleOptions) {
		o.defaultRate = clampRate(rate)
	}
}

// WithSampleByLevel sets per-level sampling rates.
//
// Levels not present in the map use the default rate (set via [WithSampleRate],
// default 1.0). The bypass level (default Error) always keeps all records
// regardless of the rate specified here.
func WithSampleByLevel(rates map[slog.Level]float64) SampleOption {
	return func(o *sampleOptions) {
		o.levelRates = make(map[slog.Level]float64, len(rates))
		for k, v := range rates {
			o.levelRates[k] = clampRate(v)
		}
	}
}

// WithBurstSampling enables deterministic burst sampling: within each
// interval window, the first `first` records per unique message pass, then
// every `thereafter`-th record passes (thereafter=0 drops everything after
// the first `first`).
//
// Unlike probabilistic sampling, this guarantees the first occurrences of a
// rare event are always logged while repetitive floods are trimmed. When
// configured, burst sampling replaces the probabilistic decision entirely
// ([WithSampleRate] and [WithSampleByLevel] are ignored); the bypass level
// still applies.
//
// Counting is approximate: messages are hashed into a fixed array of 1024
// counters, so distinct messages can occasionally share a counter. Memory is
// bounded regardless of message cardinality.
//
// An interval <= 0 defaults to one second.
func WithBurstSampling(interval time.Duration, first, thereafter uint64) SampleOption {
	return func(o *sampleOptions) {
		o.burst = &burstConfig{
			interval:   interval,
			first:      first,
			thereafter: thereafter,
		}
	}
}

// WithSampleBypassLevel sets the level at or above which sampling is bypassed
// and all records are kept.
//
// Default: [slog.LevelError]. Set to a very high value to disable bypass.
func WithSampleBypassLevel(level slog.Level) SampleOption {
	return func(o *sampleOptions) {
		o.bypassLevel = &level
	}
}

// clampRate clamps a rate to the valid range [0.0, 1.0].
func clampRate(rate float64) float64 {
	if rate < 0.0 {
		return 0.0
	}
	if rate > 1.0 {
		return 1.0
	}
	return rate
}
