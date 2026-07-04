// Package logger provides a system-agnostic, reusable, modular logging library
// built on top of Go's standard library [log/slog].
//
// go-logger does not wrap or replace slog — it extends slog through composable
// [slog.Handler] middleware. The public API produces and consumes standard
// [*slog.Logger] instances, ensuring full ecosystem compatibility.
//
// # Core Features
//
//   - Async logging with configurable backpressure (drop, block, sync-fallback)
//   - Sensitive data redaction (type-level, key-based, pattern-based, nested groups)
//   - Probabilistic and per-level log sampling
//   - Per-module/component log level filtering with runtime hot-reload
//   - Multi-output fan-out to multiple handlers simultaneously
//   - Builder pattern for composing handler middleware chains
//   - Graceful shutdown lifecycle (Flush/Close) with cascade support
//
// # Design Principles
//
//   - Standard library first: zero third-party dependencies in the core module
//   - System-agnostic: no domain-specific types (blockchain, HTTP, IoT, etc.)
//   - All domain metadata is represented as generic [slog.Attr] key-value pairs
//   - Composable middleware: all features are [slog.Handler] implementations
//   - Immutable handlers: [slog.Handler.WithAttrs] and [slog.Handler.WithGroup]
//     always return new instances; receivers are never mutated
//
// # Quick Start
//
//	log := logger.NewJSON(os.Stdout, logger.WithLevel(slog.LevelInfo))
//	log.Info("server started", "port", 8080)
//
// # Builder Pattern
//
//	log := logger.NewBuilder(slog.NewJSONHandler(os.Stdout, nil)).
//	    WithRedaction(handler.WithRedactKeys("password", "token")).
//	    WithAsync(handler.WithBufferSize(4096)).
//	    BuildLogger()
//	defer logger.Close(log.Handler())
package logger
