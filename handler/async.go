package handler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/amhrmsn/go-logger/internal/record"
)

// ErrHandlerClosed is returned by [AsyncHandler.Handle] after the handler
// has been closed via [AsyncHandler.Close].
var ErrHandlerClosed = errors.New("go-logger: handler is closed")

// asyncCore holds the shared async infrastructure: channel, worker, lifecycle.
// Multiple AsyncHandler instances (created by WithAttrs/WithGroup) share the
// same core.
type asyncCore struct {
	ch        chan asyncItem
	done      chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool

	// acceptMu gates the enqueue path against Close. Handle takes RLock,
	// CloseContext takes Lock. This prevents records from being enqueued
	// after the worker has started draining / exiting.
	acceptMu sync.RWMutex

	dropped atomic.Uint64
	written atomic.Uint64
	errors  atomic.Uint64
}

// asyncItem pairs a record with the handler that should process it.
// This allows WithAttrs/WithGroup clones to send records through the shared
// channel while using their own inner handler (which has the correct
// attrs/groups applied).
//
// An item with a non-nil barrier is a flush sentinel: it carries no record
// and the worker acknowledges it by sending on the barrier channel.
type asyncItem struct {
	ctx     context.Context // original context from Handle; preserved for inner handler
	record  slog.Record
	handler slog.Handler
	barrier chan struct{} // non-nil for flush barriers from FlushContext
}

// AsyncHandler buffers log records in a channel and processes them in a
// background worker goroutine, decoupling log production from log I/O.
//
// Features:
//   - Configurable buffer size for burst absorption
//   - Three drop policies: [DropNewest], [Block], [SyncFallback]
//   - Bypass level for synchronous writes of critical records
//   - Deterministic [Flush] via barrier channel
//   - Idempotent [Close] via [sync.Once]
//   - Dropped record counting via atomic counter
//
// Records are deep-copied via [record.CloneRecord] before being sent to the
// channel to prevent use-after-return bugs with stack-allocated attributes.
//
// AsyncHandler implements [slog.Handler], [Closer], and [Flusher].
type AsyncHandler struct {
	inner       slog.Handler
	core        *asyncCore
	dropPolicy  DropPolicy
	bypassLevel slog.Level
}

// NewAsyncHandler creates an [AsyncHandler] that wraps the given inner handler
// with asynchronous record processing.
//
// A background worker goroutine is started immediately. The caller must call
// [AsyncHandler.Close] to stop the worker and release resources.
//
// The inner handler must be safe for concurrent use by multiple goroutines
// (the standard [slog.Handler] contract, satisfied by all standard library
// handlers): bypass-level and SyncFallback writes happen on caller goroutines
// concurrently with the background worker. AsyncHandler does not add its own
// serialization around the inner handler.
//
// Defaults: bufferSize=1024, dropPolicy=[DropNewest], bypassLevel=[slog.LevelError].
func NewAsyncHandler(inner slog.Handler, opts ...AsyncOption) *AsyncHandler {
	o := applyAsyncOptions(opts)

	bypassLevel := slog.LevelError
	if o.bypassLevel != nil {
		bypassLevel = *o.bypassLevel
	}

	core := &asyncCore{
		ch:      make(chan asyncItem, o.bufferSize),
		done:    make(chan struct{}),
		closeCh: make(chan struct{}),
	}

	h := &AsyncHandler{
		inner:       inner,
		core:        core,
		dropPolicy:  o.dropPolicy,
		bypassLevel: bypassLevel,
	}

	go h.worker()

	return h
}

// Enabled reports whether the inner handler is enabled for the given level.
func (h *AsyncHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Unwrap returns the inner handler, enabling lifecycle traversal.
func (h *AsyncHandler) Unwrap() slog.Handler { return h.inner }

// Handle sends the record to the background worker for processing.
//
// Records at or above the bypass level are written synchronously
// to ensure critical logs are never lost or delayed. Note that this can
// reorder output: a bypass-level record may appear in the output before
// lower-level records that were logged earlier but are still queued in the
// async buffer. Bypass and SyncFallback writes are counted in
// [AsyncStats.Written] (or [AsyncStats.Errors] on failure).
//
// The record is deep-copied before being buffered to prevent use-after-return
// bugs with stack-allocated attributes.
//
// The original context is preserved and forwarded to the inner handler in the
// background worker, maintaining context values (e.g., trace spans).
//
// Returns [ErrHandlerClosed] if the handler has been closed.
func (h *AsyncHandler) Handle(ctx context.Context, r slog.Record) error {
	// Gate against Close: take RLock so CloseContext cannot complete its
	// exclusive Lock until all in-flight Handle calls finish.
	h.core.acceptMu.RLock()
	defer h.core.acceptMu.RUnlock()

	if h.core.closed.Load() {
		return ErrHandlerClosed
	}

	// Bypass level: write synchronously.
	if r.Level >= h.bypassLevel {
		err := h.inner.Handle(ctx, r)
		if err != nil {
			_ = h.core.errors.Add(1)
		} else {
			_ = h.core.written.Add(1)
		}
		return err
	}

	// Clone the record for safe async use.
	cloned := record.CloneRecord(r)

	item := asyncItem{
		ctx:     ctx,
		record:  cloned,
		handler: h.inner,
	}

	switch h.dropPolicy {
	case Block:
		select {
		case h.core.ch <- item:
		case <-h.core.closeCh:
			return ErrHandlerClosed
		}
	case SyncFallback:
		select {
		case h.core.ch <- item:
		default:
			// Buffer full: fall back to sync write.
			err := h.inner.Handle(ctx, cloned)
			if err != nil {
				_ = h.core.errors.Add(1)
			} else {
				_ = h.core.written.Add(1)
			}
			return err
		}
	default: // DropNewest
		select {
		case h.core.ch <- item:
		default:
			_ = h.core.dropped.Add(1)
		}
	}

	return nil
}

// WithAttrs returns a new [AsyncHandler] that shares the same async
// infrastructure (channel, worker) but wraps a child inner handler with
// the given attributes.
func (h *AsyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &AsyncHandler{
		inner:       h.inner.WithAttrs(attrs),
		core:        h.core,
		dropPolicy:  h.dropPolicy,
		bypassLevel: h.bypassLevel,
	}
}

// WithGroup returns a new [AsyncHandler] that shares the same async
// infrastructure but wraps a child inner handler with the given group.
func (h *AsyncHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	return &AsyncHandler{
		inner:       h.inner.WithGroup(name),
		core:        h.core,
		dropPolicy:  h.dropPolicy,
		bypassLevel: h.bypassLevel,
	}
}

// Flush ensures all records submitted before this call have been written
// to the inner handler.
//
// Flush delegates to [FlushContext] with a background context.
func (h *AsyncHandler) Flush() error {
	return h.FlushContext(context.Background())
}

// FlushContext ensures all records submitted before this call have been
// written to the inner handler.
//
// FlushContext uses a deterministic barrier: it sends a sentinel item through
// the channel and waits for the worker to acknowledge processing it. This
// guarantees that all previously enqueued records have been written.
//
// The barrier channel is buffered (cap 1) and the worker uses a non-blocking
// send to acknowledge. This prevents the worker from deadlocking if the
// caller's context times out after the barrier is enqueued but before the
// worker sends the ack.
//
// The context can be used to set a deadline or cancel the flush operation.
// Returns an error if the handler is closed or the context is cancelled.
// A flush that races with [CloseContext] may return [ErrHandlerClosed]; the
// close path itself drains all buffered records, so nothing is lost.
func (h *AsyncHandler) FlushContext(ctx context.Context) error {
	if h.core.closed.Load() {
		return nil
	}

	// Buffered channel (cap 1) prevents worker deadlock on timeout.
	barrier := make(chan struct{}, 1)
	item := asyncItem{barrier: barrier}

	select {
	case h.core.ch <- item:
		select {
		case <-barrier: // Wait for the worker to process everything up to here.
			return nil
		case <-h.core.done:
			// The worker exited (Close raced with this flush). It may have
			// processed the barrier during its final drain, so prefer the ack
			// if it is already there.
			select {
			case <-barrier:
				return nil
			default:
			}
			return ErrHandlerClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-h.core.closeCh:
		return ErrHandlerClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops the background worker and waits for it to finish processing
// all remaining buffered records.
//
// Close delegates to [CloseContext] with a background context.
func (h *AsyncHandler) Close() error {
	return h.CloseContext(context.Background())
}

// CloseContext stops the background worker and waits for it to finish
// processing all remaining buffered records.
//
// CloseContext is idempotent: calling it multiple times is safe and subsequent
// calls return nil.
//
// CloseContext takes an exclusive lock on acceptMu to ensure no new records
// can be enqueued after closed is set. This eliminates the race window between
// Handle checking closed and CloseContext setting it.
//
// The context can be used to set a deadline for the drain operation.
// After CloseContext returns, any further calls to Handle return [ErrHandlerClosed].
func (h *AsyncHandler) CloseContext(ctx context.Context) error {
	h.core.closeOnce.Do(func() {
		// Exclusive lock: blocks all in-flight Handle RLocks.
		h.core.acceptMu.Lock()
		h.core.closed.Store(true)
		close(h.core.closeCh) // Signal worker to drain and stop.
		h.core.acceptMu.Unlock()
	})
	select {
	case <-h.core.done: // Wait for worker to finish.
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DroppedCount returns the total number of records dropped due to a full
// buffer when using the [DropNewest] policy.
func (h *AsyncHandler) DroppedCount() uint64 {
	return h.core.dropped.Load()
}

// Stats returns a snapshot of the handler's runtime statistics.
//
// Written includes all successful writes: background worker, bypass-level
// synchronous writes, and SyncFallback writes.
// Errors includes all write failures across all paths.
func (h *AsyncHandler) Stats() AsyncStats {
	return AsyncStats{
		Written:  h.core.written.Load(),
		Dropped:  h.core.dropped.Load(),
		Errors:   h.core.errors.Load(),
		QueueLen: len(h.core.ch),
	}
}

// worker is the background goroutine that reads items from the channel
// and writes them to their associated inner handler.
func (h *AsyncHandler) worker() {
	defer close(h.core.done)

	for {
		select {
		case item := <-h.core.ch:
			h.processItem(item)
		case <-h.core.closeCh:
			h.drain()
			return
		}
	}
}

// drain processes all remaining items in the channel before shutdown.
func (h *AsyncHandler) drain() {
	for {
		select {
		case item := <-h.core.ch:
			h.processItem(item)
		default:
			return
		}
	}
}

// processItem handles a single item from the channel.
func (h *AsyncHandler) processItem(item asyncItem) {
	if item.barrier != nil {
		// Non-blocking send: if the caller's context timed out and nobody
		// is waiting on the barrier channel, we must not block the worker.
		select {
		case item.barrier <- struct{}{}:
		default:
		}
		return
	}
	if item.handler != nil {
		ctx := item.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		err := item.handler.Handle(ctx, item.record)
		if err != nil {
			_ = h.core.errors.Add(1)
		} else {
			_ = h.core.written.Add(1)
		}
	}
}
