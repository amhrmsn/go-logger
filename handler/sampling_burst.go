package handler

import (
	"sync/atomic"
	"time"
)

// burstBuckets is the fixed number of message counters. Messages are hashed
// into this array, so distinct messages may share a counter (collisions make
// counting approximate, which is acceptable for sampling).
const burstBuckets = 1024

// burstCounter tracks how many records for one message bucket have been seen
// in the current time window.
type burstCounter struct {
	resetAt atomic.Int64  // window end, unix nanoseconds
	n       atomic.Uint64 // records seen in the current window
}

// burstSampler implements deterministic burst sampling: within each interval
// window, the first `first` records per message pass, then every
// `thereafter`-th record passes.
//
// The design follows the classic fixed-bucket approach: no locks, no
// allocations, bounded memory regardless of message cardinality. Window
// resets race benignly — a handful of records at a window boundary may be
// counted into either window.
type burstSampler struct {
	interval   time.Duration
	first      uint64
	thereafter uint64
	counters   [burstBuckets]burstCounter
}

func newBurstSampler(interval time.Duration, first, thereafter uint64) *burstSampler {
	if interval <= 0 {
		interval = time.Second
	}
	return &burstSampler{
		interval:   interval,
		first:      first,
		thereafter: thereafter,
	}
}

// allow reports whether a record with the given message and timestamp passes
// the burst check. A zero timestamp falls back to time.Now().
func (s *burstSampler) allow(msg string, t time.Time) bool {
	if t.IsZero() {
		t = time.Now()
	}
	c := &s.counters[fnv32a(msg)%burstBuckets]

	now := t.UnixNano()
	resetAt := c.resetAt.Load()
	if now >= resetAt {
		// New window: one caller claims the reset; concurrent losers simply
		// count into the fresh window.
		if c.resetAt.CompareAndSwap(resetAt, now+int64(s.interval)) {
			c.n.Store(0)
		}
	}

	n := c.n.Add(1)
	if n <= s.first {
		return true
	}
	if s.thereafter == 0 {
		return false
	}
	return (n-s.first)%s.thereafter == 0
}

// fnv32a is an allocation-free FNV-1a hash over the message string.
func fnv32a(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}
