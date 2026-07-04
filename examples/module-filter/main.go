// Package main demonstrates the ModuleHandler for per-component log level filtering.
//
// This example shows how different parts of an application can have different
// log levels, and how those levels can be changed at runtime without restarting.
package main

import (
	"fmt"
	"log/slog"
	"os"

	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

func main() {
	// Create a ModuleConfig with different levels per component.
	// Default level is Info — components without explicit config use this.
	config := handler.NewModuleConfig(slog.LevelInfo)
	config.SetLevel("database", slog.LevelDebug) // Verbose: show all DB queries
	config.SetLevel("auth", slog.LevelWarn)      // Quiet: only warnings and errors
	config.SetLevel("api", slog.LevelInfo)       // Normal: info and above

	// Create a ModuleHandler wrapping a JSON handler.
	moduleH := handler.NewModuleHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		config,
	)

	// --- Create per-component loggers using With() ---

	dbLog := slog.New(moduleH).With(logger.Component("database"))
	authLog := slog.New(moduleH).With(logger.Component("auth"))
	apiLog := slog.New(moduleH).With(logger.Component("api"))
	unknownLog := slog.New(moduleH) // No component — uses default level (Info)

	fmt.Fprintln(os.Stderr, "\n=== Initial levels: database=Debug, auth=Warn, api=Info, default=Info ===")

	// Database: Debug level — all messages appear.
	dbLog.Debug("executing query", slog.String("sql", "SELECT * FROM users WHERE id = ?"))
	dbLog.Info("query completed", slog.Duration("elapsed", 3_000_000)) // 3ms

	// Auth: Warn level — Debug and Info are filtered out.
	authLog.Debug("checking credentials") // FILTERED (below Warn)
	authLog.Info("user authenticated")    // FILTERED (below Warn)
	authLog.Warn("multiple failed attempts", slog.String("user", "bob"), slog.Int("attempts", 5))

	// API: Info level — Debug is filtered, Info and above appear.
	apiLog.Debug("parsing request body") // FILTERED (below Info)
	apiLog.Info("handling request", slog.String("method", "GET"), slog.String("path", "/users"))

	// Unknown component: uses default level (Info).
	unknownLog.Debug("this is filtered") // FILTERED (below Info)
	unknownLog.Info("this appears", slog.String("note", "no component, using default"))

	// --- Runtime level change ---

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "=== Changing auth level to Debug at runtime ===")

	// Change auth level to Debug at runtime — takes effect immediately
	// for ALL loggers using this config, without restart.
	config.SetLevel("auth", slog.LevelDebug)

	// Now auth debug messages appear!
	authLog.Debug("checking credentials", slog.String("method", "OAuth2"))
	authLog.Info("user authenticated", slog.String("user", "alice"))

	// --- Change default level ---

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "=== Changing default level to Error ===")

	config.SetDefaultLevel(slog.LevelError)

	// Unknown component now uses Error level — Info is filtered.
	unknownLog.Info("this is now filtered") // FILTERED (below Error)
	unknownLog.Error("only errors pass through")
}
