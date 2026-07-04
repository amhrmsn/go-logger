// Package record provides internal utilities for safe manipulation of
// [slog.Record] values.
//
// This package is internal and not intended for use by external consumers.
package record

import "log/slog"

// CloneRecord creates an attribute-materializing clone of a [slog.Record]
// that is safe for use across goroutine boundaries.
//
// The standard [slog.Record] stores attributes via an iterator function
// that may reference stack-allocated memory. When a record needs to be
// processed asynchronously (e.g., sent through a channel to a background
// worker) or fanned out to multiple handlers, all attributes must be
// "materialized" — copied to heap-allocated storage — to prevent
// use-after-return bugs.
//
// The cloned record preserves:
//   - Timestamp (Time)
//   - Level
//   - Message
//   - Source location (PC)
//   - All attributes (including nested groups)
//
// Note: this is NOT a reflection-based deep copy. Attribute values of type
// [slog.AnyValue] that contain pointers, maps, or slices are copied by
// reference. If callers store mutable values in attributes, they must ensure
// those values are not mutated after the log call.
func CloneRecord(r slog.Record) slog.Record {
	clone := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clone.AddAttrs(a)
		return true
	})
	return clone
}
