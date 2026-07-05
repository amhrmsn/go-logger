package handler

import "expvar"

// This file wires handler statistics into the standard library's [expvar]
// package, so they appear on the /debug/vars endpoint alongside the runtime's
// own metrics — no third-party dependency required. Most metric systems
// (including Prometheus via common expvar exporters) can scrape that endpoint.
//
// Each Publish* function registers an [expvar.Func] that snapshots the
// handler's stats on every read. Like [expvar.Publish], registering the same
// name twice panics, so call these once per handler at startup:
//
//	async := handler.NewAsyncHandler(base)
//	handler.PublishAsyncStats("logger.async", async)
//	// GET /debug/vars → "logger.async": {"Written":123,"Dropped":0,...}

// PublishAsyncStats publishes the [AsyncHandler]'s statistics under the given
// expvar name.
func PublishAsyncStats(name string, h *AsyncHandler) {
	expvar.Publish(name, expvar.Func(func() any { return h.Stats() }))
}

// PublishSampleStats publishes the [SamplingHandler]'s statistics under the
// given expvar name.
func PublishSampleStats(name string, h *SamplingHandler) {
	expvar.Publish(name, expvar.Func(func() any { return h.Stats() }))
}

// PublishDedupStats publishes the [DedupHandler]'s statistics under the given
// expvar name.
func PublishDedupStats(name string, h *DedupHandler) {
	expvar.Publish(name, expvar.Func(func() any { return h.Stats() }))
}
