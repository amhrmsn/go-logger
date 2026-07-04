// Package main demonstrates the AsyncHandler with graceful shutdown.
//
// This example creates an async handler that buffers log records in a
// background goroutine, then performs a graceful shutdown using os/signal
// to ensure all buffered records are flushed before exit.
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
	// Create an async handler wrapping a JSON handler.
	// - Buffer size of 256 records
	// - Block policy: callers wait if buffer is full (no data loss)
	// - Bypass level: Error and above write synchronously (never lost)
	asyncH := handler.NewAsyncHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		handler.WithBufferSize(256),
		handler.WithDropPolicy(handler.Block),
		// Default bypass: slog.LevelError (errors write synchronously)
	)

	log := slog.New(asyncH)

	// --- Simulate application work ---

	log.Info("async logger initialized",
		slog.Int("buffer_size", 256),
		slog.String("drop_policy", "block"),
	)

	// Simulate a burst of log messages.
	for i := 0; i < 10; i++ {
		log.Info("processing task",
			logger.Component("worker"),
			slog.Int("task_id", i),
			slog.Duration("elapsed", time.Duration(i*50)*time.Millisecond),
		)
	}

	// Error bypasses the async buffer and writes immediately.
	log.Error("critical failure detected",
		logger.Component("worker"),
		logger.Err(fmt.Errorf("disk full")),
	)

	// --- Graceful shutdown ---

	// Set up signal handling for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// In a real application, you'd wait for the signal here:
	//   <-sigCh

	// For this example, we proceed directly to shutdown.
	fmt.Fprintln(os.Stderr, "\n--- Shutting down ---")

	// Flush ensures all buffered records are written.
	if err := asyncH.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "flush error: %v\n", err)
	}
	fmt.Fprintln(os.Stderr, "flush complete: all buffered records written")

	// Close stops the background worker goroutine.
	if err := asyncH.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close error: %v\n", err)
	}
	fmt.Fprintln(os.Stderr, "async handler closed")

	// Report dropped count (should be 0 with Block policy).
	fmt.Fprintf(os.Stderr, "dropped records: %d\n", asyncH.DroppedCount())
}
