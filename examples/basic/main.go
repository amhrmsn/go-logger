// Package main demonstrates the basic usage of go-logger.
//
// This example creates a JSON logger using the convenience function
// and logs several messages at different levels with structured attributes.
package main

import (
	"log/slog"
	"os"
	"time"

	logger "github.com/amhrmsn/go-logger"
)

func main() {
	// Create a JSON logger writing to stdout with Debug level enabled.
	log := logger.NewJSON(os.Stdout, logger.WithLevel(slog.LevelDebug), logger.WithSource(true))

	// --- Basic structured logging ---

	log.Info("application started",
		slog.String("version", "1.0.0"),
		slog.String("env", "production"),
	)

	log.Debug("loading configuration",
		slog.String("path", "/etc/app/config.yaml"),
		slog.Duration("parse_time", 12*time.Millisecond),
	)

	// --- Using attribute helpers ---

	log.Warn("connection pool running low",
		logger.Component("database"),
		slog.Int("available", 3),
		slog.Int("max", 50),
	)

	log.Error("failed to process request",
		logger.Err(os.ErrNotExist),
		logger.Component("api"),
		logger.TraceID("abc123def456"),
		slog.String("method", "GET"),
		slog.String("path", "/users/42"),
		slog.Int("status", 404),
	)

	// --- Using With() for logger context ---

	reqLog := log.With(
		logger.Component("http"),
		slog.String("request_id", "req-9f8e7d"),
	)

	reqLog.Info("handling request",
		slog.String("method", "POST"),
		slog.String("path", "/api/v1/orders"),
	)

	reqLog.Info("request completed",
		slog.Int("status", 201),
		slog.Duration("latency", 47*time.Millisecond),
	)
}
