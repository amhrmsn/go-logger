package handler

import (
	"log/slog"
	"time"
)

// DedupOption configures a [DedupHandler].
type DedupOption func(*dedupOptions)

// dedupOptions holds the collected configuration for [DedupHandler].
type dedupOptions struct {
	window      time.Duration
	bypassLevel *slog.Level
}

func applyDedupOptions(opts []DedupOption) *dedupOptions {
	o := &dedupOptions{
		window: time.Second, // default
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithDedupWindow sets the deduplication time window.
//
// Within each window, the first record per unique message passes and later
// copies are suppressed. A window <= 0 falls back to the 1-second default.
func WithDedupWindow(window time.Duration) DedupOption {
	return func(o *dedupOptions) {
		if window > 0 {
			o.window = window
		}
	}
}

// WithDedupBypassLevel sets a level at or above which records are never
// deduplicated.
//
// By default there is no bypass: repeated errors are exactly the flood that
// deduplication exists to trim, and the suppressed count keeps the flood
// visible. Set a bypass only when every individual record at some level must
// be preserved.
func WithDedupBypassLevel(level slog.Level) DedupOption {
	return func(o *dedupOptions) {
		o.bypassLevel = &level
	}
}

// DedupStats holds runtime statistics for a [DedupHandler].
//
// All counters are accumulated since handler creation and read atomically.
type DedupStats struct {
	// Passed is the total number of records forwarded to the inner handler.
	Passed uint64

	// Suppressed is the total number of duplicate records dropped.
	Suppressed uint64
}
