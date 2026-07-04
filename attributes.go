package logger

import "log/slog"

// Err creates a [slog.Attr] for an error value with the key "error".
//
// This is a convenience helper that ensures consistent key naming for error
// attributes across an application.
//
//	if err != nil {
//	    log.Error("operation failed", logger.Err(err))
//	}
func Err(err error) slog.Attr {
	return slog.Any("error", err)
}

// Component creates a [slog.Attr] with the key "component" for module or
// subsystem identification.
//
// This attribute is used by [ModuleHandler] to apply per-component log level
// filtering. The component name should identify the subsystem, not the
// application domain.
//
//	log := slog.New(h).With(logger.Component("networking"))
//	log.Info("listening", "port", 9000)
func Component(name string) slog.Attr {
	return slog.String("component", name)
}

// TraceID creates a [slog.Attr] with the key "trace_id" for distributed
// trace correlation.
//
// In applications using OpenTelemetry or similar tracing systems, include
// the trace ID in log records to enable log-trace correlation in your
// observability backend.
//
//	slog.InfoContext(ctx, "processing",
//	    logger.TraceID(extractTraceID(ctx)),
//	)
func TraceID(id string) slog.Attr {
	return slog.String("trace_id", id)
}

// SpanID creates a [slog.Attr] with the key "span_id" for distributed
// trace span correlation.
//
//	slog.InfoContext(ctx, "handling request",
//	    logger.TraceID(tid),
//	    logger.SpanID(sid),
//	)
func SpanID(id string) slog.Attr {
	return slog.String("span_id", id)
}
