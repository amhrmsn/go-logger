package handler

// AsyncStats holds runtime statistics for an [AsyncHandler].
//
// All counters are accumulated since handler creation and read atomically.
type AsyncStats struct {
	// Written is the total number of records successfully written by the
	// background worker.
	Written uint64

	// Dropped is the total number of records dropped due to a full buffer
	// when using the [DropNewest] policy.
	Dropped uint64

	// Errors is the total number of errors returned by the inner handler
	// during background processing.
	Errors uint64

	// QueueLen is the current number of items waiting in the async buffer.
	QueueLen int
}

// SampleStats holds runtime statistics for a [SamplingHandler].
//
// All counters are accumulated since handler creation and read atomically.
type SampleStats struct {
	// Passed is the total number of records that passed the sampling check
	// and were forwarded to the inner handler.
	Passed uint64

	// Dropped is the total number of records that were sampled out and
	// silently discarded.
	Dropped uint64
}
