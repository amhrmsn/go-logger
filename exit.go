package logger

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// exitFlushTimeout bounds the flush/close performed by [Exit] so that a dying
// process cannot hang indefinitely on a stuck log sink.
const exitFlushTimeout = 5 * time.Second

// osExit is a variable so tests can intercept process termination.
var osExit = os.Exit

// Exit flushes and closes the handler chain, then terminates the process
// with the given status code.
//
// Calling [os.Exit] directly after logging loses any records still queued in
// an [handler.AsyncHandler] buffer: the process dies before the background
// worker drains them. Exit closes that gap by running [FlushContext] and
// [CloseContext] over the entire middleware chain first.
//
// The flush and close are best-effort: they share a 5-second timeout and
// their errors are discarded, because the process is terminating either way.
//
//	log.Error("unrecoverable", logger.Err(err))
//	logger.Exit(log.Handler(), 1)
func Exit(h slog.Handler, code int) {
	ctx, cancel := context.WithTimeout(context.Background(), exitFlushTimeout)
	defer cancel()
	_ = FlushContext(ctx, h)
	_ = CloseContext(ctx, h)
	osExit(code)
}

// Fatal logs the message at [slog.LevelError] with the given arguments, then
// calls [Exit] with status code 1.
//
// This is the async-safe replacement for the common pattern of logging an
// error followed by [os.Exit]: buffered records — including the fatal message
// itself — are flushed before the process terminates.
//
//	logger.Fatal(log, "cannot bind listener", "addr", addr, logger.Err(err))
func Fatal(log *slog.Logger, msg string, args ...any) {
	log.Error(msg, args...)
	Exit(log.Handler(), 1)
}
