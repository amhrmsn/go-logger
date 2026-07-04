// Package main demonstrates the MultiHandler for fan-out to multiple outputs.
//
// This example sends every log record to both stdout (JSON format) and a
// temporary file (Text format) simultaneously. This pattern is useful for
// sending structured logs to a collector while keeping human-readable logs
// on disk for debugging.
package main

import (
	"fmt"
	"log/slog"
	"os"

	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

func main() {
	// Create a temporary file for text logs.
	logFile, err := os.CreateTemp("", "go-logger-demo-*.log")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(logFile.Name())
	defer logFile.Close()

	fmt.Fprintf(os.Stderr, "text logs writing to: %s\n\n", logFile.Name())

	// Create a MultiHandler that fans out to two handlers:
	// 1. JSON to stdout (for log aggregation / machine consumption)
	// 2. Text to file (for human-readable debugging)
	multi := handler.NewMultiHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)

	log := slog.New(multi)

	// --- Log some messages ---

	log.Info("multi-output logging initialized",
		slog.String("json_output", "stdout"),
		slog.String("text_output", logFile.Name()),
	)

	log.Debug("loading user profile",
		logger.Component("database"),
		slog.Int("user_id", 42),
	)

	log.Warn("rate limit approaching",
		logger.Component("api"),
		slog.Int("requests_per_min", 95),
		slog.Int("limit", 100),
	)

	log.Error("authentication failed",
		logger.Component("auth"),
		logger.Err(fmt.Errorf("invalid token")),
		slog.String("ip", "192.168.1.100"),
	)

	// --- Read back the text log file to show both outputs ---
	logFile.Sync()

	fmt.Fprintln(os.Stderr, "\n--- Text log file contents ---")
	content, _ := os.ReadFile(logFile.Name())
	fmt.Fprintln(os.Stderr, string(content))
}
