// Package main demonstrates the full go-logger middleware chain using the Builder.
//
// This example composes ALL middleware handlers together:
// - ModuleHandler (per-component filtering)
// - SamplingHandler (probabilistic filtering)
// - RedactionHandler (sensitive data protection)
// - AsyncHandler (non-blocking I/O)
//
// It also demonstrates graceful shutdown with os/signal and runtime level changes.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

func main() {
	// --- Configure per-component levels ---
	config := handler.NewModuleConfig(slog.LevelInfo)
	config.SetLevel("database", slog.LevelDebug)
	config.SetLevel("auth", slog.LevelWarn)
	config.SetLevel("api", slog.LevelInfo)

	// --- Build the full handler chain ---
	//
	// Composition order (innermost → outermost):
	//   JSONHandler → AsyncHandler → RedactionHandler → SamplingHandler → ModuleHandler
	//
	// At call time, execution order is:
	//   ModuleHandler → SamplingHandler → RedactionHandler → AsyncHandler → JSONHandler

	h := logger.NewBuilder(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})).
		WithAsync(
			handler.WithBufferSize(1024),
			handler.WithDropPolicy(handler.Block),
			// Default bypass: Error+ writes synchronously
		).
		WithRedaction(
			handler.WithRedactKeys("password", "ssn", "credit_card"),
			handler.WithRedactPatterns(`(?i)token`, `(?i)secret`),
		).
		WithSampling(
			handler.WithSampleRate(1.0), // Keep all records (set < 1.0 in production)
		).
		WithModuleFilter(config).
		Build()

	// --- Create loggers for different components ---

	dbLog := slog.New(h).With(logger.Component("database"))
	authLog := slog.New(h).With(logger.Component("auth"))
	apiLog := slog.New(h).With(logger.Component("api"))

	fmt.Fprintln(os.Stderr, "\n=== Full chain initialized (Module → Sampling → Redaction → Async → JSON) ===")

	// --- Simulate application activity ---

	// Database: Debug level enabled — verbose logging.
	dbLog.Debug("opening connection pool",
		slog.Int("max_conns", 25),
		slog.String("host", "db.internal:5432"),
	)

	dbLog.Info("query executed",
		slog.String("sql", "SELECT id, name FROM users WHERE active = true"),
		slog.Duration("elapsed", 4*time.Millisecond),
		slog.Int("rows", 142),
	)

	// Auth: Warn level — only warnings and errors pass.
	authLog.Info("this is filtered by ModuleHandler") // FILTERED
	authLog.Warn("suspicious login attempt",
		slog.String("user", "bob"),
		slog.String("ip", "203.0.113.42"),
		slog.String("auth_token", "eyJhbGciOiJIUzI1NiJ9"), // REDACTED by pattern
		slog.Int("failed_attempts", 7),
	)

	// API: Info level — normal logging with redaction.
	apiLog.Info("processing payment",
		slog.String("order_id", "ORD-98765"),
		slog.String("credit_card", "4111-1111-1111-1111"), // REDACTED by key
		slog.String("password", "user-p@ssw0rd"),          // REDACTED by key
		slog.Float64("amount", 249.99),
		slog.String("currency", "USD"),
	)

	// Error bypasses the async buffer — written synchronously.
	apiLog.Error("payment gateway timeout",
		logger.Err(fmt.Errorf("connection refused")),
		slog.String("gateway", "stripe"),
		slog.Duration("timeout", 30*time.Second),
	)

	// --- Runtime level change ---
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "=== Changing auth level to Debug at runtime ===")

	config.SetLevel("auth", slog.LevelDebug)

	authLog.Debug("token validation started",
		slog.String("method", "JWT"),
		slog.String("api_secret", "sk-live-1234"), // REDACTED by pattern
	)

	// --- Type-based redaction ---
	apiLog.Info("webhook configured",
		slog.String("url", "https://hooks.example.com/notify"),
		slog.Any("signing_secret", logger.Redacted("whsec_abc123")),
		slog.Any("private_key", logger.SensitiveBytes([]byte{0x01, 0x02, 0x03})),
	)

	// --- Graceful shutdown ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// In a real application: <-sigCh
	fmt.Fprintln(os.Stderr, "\n--- Graceful shutdown ---")

	// Flush all buffered records.
	if err := logger.Flush(h); err != nil {
		fmt.Fprintf(os.Stderr, "flush error: %v\n", err)
	}
	fmt.Fprintln(os.Stderr, "all records flushed")

	// Close the async handler and release resources.
	if err := logger.Close(h); err != nil {
		fmt.Fprintf(os.Stderr, "close error: %v\n", err)
	}
	fmt.Fprintln(os.Stderr, "handler closed — shutdown complete")
}
