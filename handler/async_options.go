package handler

import "log/slog"

// DropPolicy defines the behavior when the async buffer is full.
type DropPolicy int

const (
	// DropNewest drops the new record when the buffer is full.
	// The record is silently discarded and [AsyncHandler.DroppedCount] is incremented.
	DropNewest DropPolicy = iota

	// Block blocks the calling goroutine until space is available in the buffer.
	// This provides backpressure to the caller but may impact latency.
	Block

	// SyncFallback writes the record synchronously to the inner handler when
	// the buffer is full. This ensures no records are lost but bypasses the
	// async path, so the caller blocks for the duration of the write.
	SyncFallback
)

// AsyncOption configures an [AsyncHandler].
type AsyncOption func(*asyncOptions)

// asyncOptions holds the collected configuration for [AsyncHandler].
type asyncOptions struct {
	bufferSize  int
	dropPolicy  DropPolicy
	bypassLevel *slog.Level
}

func applyAsyncOptions(opts []AsyncOption) *asyncOptions {
	o := &asyncOptions{
		bufferSize: 1024, // default
		dropPolicy: DropNewest,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithBufferSize sets the size of the internal record channel buffer.
//
// Larger buffers absorb more bursts but consume more memory. Default: 1024.
func WithBufferSize(size int) AsyncOption {
	return func(o *asyncOptions) {
		if size > 0 {
			o.bufferSize = size
		}
	}
}

// WithDropPolicy sets the behavior when the buffer is full.
//
// Default: [DropNewest].
func WithDropPolicy(policy DropPolicy) AsyncOption {
	return func(o *asyncOptions) {
		o.dropPolicy = policy
	}
}

// WithAsyncBypassLevel sets the level at or above which records are written
// synchronously, bypassing the async buffer entirely.
//
// This ensures critical records (e.g., Error, Fatal) are written immediately
// even if the buffer is full or the worker is behind. The trade-off is
// ordering: a bypassed record can appear in the output before lower-level
// records that were logged earlier but are still queued in the buffer. If
// strict output ordering matters more than immediacy, set the bypass level
// above your highest level so every record goes through the queue.
//
// Default: [slog.LevelError].
func WithAsyncBypassLevel(level slog.Level) AsyncOption {
	return func(o *asyncOptions) {
		o.bypassLevel = &level
	}
}
