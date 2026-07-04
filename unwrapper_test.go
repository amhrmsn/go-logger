package logger_test

import (
	"log/slog"

	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

// Compile-time assertions: every built-in middleware must implement
// [logger.Unwrapper] so lifecycle traversal can pass through it.
var (
	_ logger.Unwrapper = (*handler.AsyncHandler)(nil)
	_ logger.Unwrapper = (*handler.RedactionHandler)(nil)
	_ logger.Unwrapper = (*handler.SamplingHandler)(nil)
	_ logger.Unwrapper = (*handler.ModuleHandler)(nil)
)

// Compile-time assertions for the lifecycle interfaces themselves.
var (
	_ logger.Closer         = (*handler.AsyncHandler)(nil)
	_ logger.ContextCloser  = (*handler.AsyncHandler)(nil)
	_ logger.Flusher        = (*handler.AsyncHandler)(nil)
	_ logger.ContextFlusher = (*handler.AsyncHandler)(nil)
	_ logger.ContextCloser  = (*handler.MultiHandler)(nil)
	_ logger.ContextFlusher = (*handler.MultiHandler)(nil)
)

// Silence unused-import lint if assertions are ever trimmed.
var _ slog.Handler = (*handler.MultiHandler)(nil)
