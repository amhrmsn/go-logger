package handler

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/amhrmsn/go-logger/internal/record"
)

// DedupSuppressedKey is the attribute key added to a record that passes
// after earlier duplicates were suppressed. Its value is the number of
// suppressed copies.
const DedupSuppressedKey = "dedup_suppressed"

// dedupBuckets is the fixed number of message counters, mirroring the burst
// sampler design: bounded memory, no locks, approximate counting under
// bucket collisions.
const dedupBuckets = 1024

// dedupCounter tracks occurrences of one message bucket within the current
// time window.
type dedupCounter struct {
	resetAt atomic.Int64  // window end, unix nanoseconds
	n       atomic.Uint64 // records seen in the current window
}

// dedupCore holds the deduplication state shared across WithAttrs/WithGroup
// clones.
type dedupCore struct {
	window     time.Duration
	counters   [dedupBuckets]dedupCounter
	passed     atomic.Uint64
	suppressed atomic.Uint64
}

// DedupHandler suppresses repeated identical messages: within each time
// window, the first record per unique message passes and subsequent copies
// are dropped. When a message passes again in a later window, the number of
// copies suppressed since it last passed is attached as the
// [DedupSuppressedKey] attribute, so the volume of the flood remains visible.
//
// This targets the classic failure flood — a broken dependency emitting the
// same error thousands of times per minute — without hiding that the flood
// happened.
//
// Deduplication keys on the record message only; attribute values are not
// compared. Two records with the same message but different attributes are
// considered duplicates. Counting is approximate: messages are hashed into a
// fixed array of 1024 counters, so distinct messages can occasionally share
// a counter. Memory is bounded regardless of message cardinality.
//
// Unlike SamplingHandler there is no bypass level by default: repeated
// errors are exactly the flood dedup exists for. Configure
// [WithDedupBypassLevel] if some level must never be suppressed.
//
// DedupHandler implements [slog.Handler] and follows the immutable clone
// pattern for [slog.Handler.WithAttrs] and [slog.Handler.WithGroup]; clones
// share the deduplication state.
type DedupHandler struct {
	inner       slog.Handler
	core        *dedupCore
	bypassLevel *slog.Level // nil: no bypass
}

// NewDedupHandler creates a [DedupHandler] wrapping the given inner handler.
//
// Defaults: window=1s, no bypass level.
func NewDedupHandler(inner slog.Handler, opts ...DedupOption) *DedupHandler {
	o := applyDedupOptions(opts)
	return &DedupHandler{
		inner:       inner,
		core:        &dedupCore{window: o.window},
		bypassLevel: o.bypassLevel,
	}
}

// Enabled reports whether the inner handler is enabled for the given level.
func (h *DedupHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Unwrap returns the inner handler, enabling lifecycle traversal.
func (h *DedupHandler) Unwrap() slog.Handler { return h.inner }

// Handle applies the deduplication decision. The first record per message in
// each window passes; duplicates are dropped silently. A record that passes
// after suppression carries the [DedupSuppressedKey] attribute.
func (h *DedupHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.bypassLevel != nil && r.Level >= *h.bypassLevel {
		_ = h.core.passed.Add(1)
		return h.inner.Handle(ctx, r)
	}

	pass, suppressed := h.core.check(r.Message, r.Time)
	if !pass {
		_ = h.core.suppressed.Add(1)
		return nil
	}
	_ = h.core.passed.Add(1)

	if suppressed > 0 {
		// Clone before mutating: we do not own r's backing storage.
		rr := record.CloneRecord(r)
		rr.AddAttrs(slog.Uint64(DedupSuppressedKey, suppressed))
		return h.inner.Handle(ctx, rr)
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new [DedupHandler] sharing the same deduplication
// state, wrapping a child inner handler with the given attributes.
func (h *DedupHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DedupHandler{
		inner:       h.inner.WithAttrs(attrs),
		core:        h.core,
		bypassLevel: h.bypassLevel,
	}
}

// WithGroup returns a new [DedupHandler] sharing the same deduplication
// state, wrapping a child inner handler with the given group.
func (h *DedupHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &DedupHandler{
		inner:       h.inner.WithGroup(name),
		core:        h.core,
		bypassLevel: h.bypassLevel,
	}
}

// Stats returns a snapshot of the handler's runtime statistics.
func (h *DedupHandler) Stats() DedupStats {
	return DedupStats{
		Passed:     h.core.passed.Load(),
		Suppressed: h.core.suppressed.Load(),
	}
}

// check decides whether a record with the given message and timestamp passes,
// and how many copies were suppressed since the message last passed.
// A zero timestamp falls back to time.Now().
func (c *dedupCore) check(msg string, t time.Time) (pass bool, suppressed uint64) {
	if t.IsZero() {
		t = time.Now()
	}
	ctr := &c.counters[fnv32a(msg)%dedupBuckets]

	now := t.UnixNano()
	resetAt := ctr.resetAt.Load()
	if now >= resetAt {
		// New window: the CAS winner passes and reports the previous
		// window's suppressed count. Losers count into the fresh window.
		if ctr.resetAt.CompareAndSwap(resetAt, now+int64(c.window)) {
			prev := ctr.n.Swap(1)
			if prev > 1 {
				return true, prev - 1 // copies beyond the one that passed
			}
			return true, 0
		}
	}

	n := ctr.n.Add(1)
	return n == 1, 0
}
