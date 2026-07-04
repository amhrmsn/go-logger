<h1 align="center">
  go-logger
</h1>

<p align="center">
  A <strong>zero-dependency</strong>, <strong>production-ready</strong> structured logging library for Go, built entirely on top of <a href="https://pkg.go.dev/log/slog"><code>log/slog</code></a>.
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/amhrmsn/go-logger"><img src="https://pkg.go.dev/badge/github.com/amhrmsn/go-logger.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/amhrmsn/go-logger"><img src="https://goreportcard.com/badge/github.com/amhrmsn/go-logger" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-%3E%3D%201.22-00ADD8?logo=go" alt="Go Version">
</p>

---

`go-logger` extends the Go standard library's `slog` with production-grade middleware handlers—async buffering, sensitive data redaction, probabilistic sampling, and per-component filtering—**without introducing a custom logger type**. You keep using `*slog.Logger` everywhere in your codebase.

## 📑 Table of Contents
- [✨ Features](#-features)
- [📦 Installation](#-installation)
- [🚀 Quick Start](#-quick-start)
- [🛠️ Middleware Handlers](#️-middleware-handlers)
  - [AsyncHandler](#asynchandler)
  - [RedactionHandler](#redactionhandler)
  - [SamplingHandler](#samplinghandler)
  - [ModuleHandler](#modulehandler)
  - [MultiHandler](#multihandler)
- [🏗️ Builder Pattern](#️-builder-pattern)
- [🛑 Graceful Shutdown](#-graceful-shutdown)
- [📊 Benchmarks](#-benchmarks)
- [📂 Project Structure](#-project-structure)
- [📝 License](#-license)

---

## ✨ Features

| Feature | Description |
|:---|:---|
| 🛡️ **Zero Dependencies** | Core module uses only the Go standard library. No third-party imports, no supply chain risks. |
| 🤝 **`slog`-Native** | Works directly with `*slog.Logger`. No wrapper types, 100% compatible with the ecosystem. |
| ⚡ **AsyncHandler** | Non-blocking I/O via a background goroutine with configurable buffer and drop policies. |
| 🕵️ **RedactionHandler** | Protects sensitive data (PII) via key-based, pattern-based (Regex), and type-based redaction. |
| 🎲 **SamplingHandler** | Reduces log volume with probabilistic filtering, per-level rates, and burst sampling (first-N-per-message guaranteed). |
| 🎛️ **ModuleHandler** | Per-component log level filtering with runtime hot-reload capabilities. |
| 🔀 **MultiHandler** | Fan-out capability to write logs to multiple outputs simultaneously (e.g., stdout + file). |
| 🧱 **Builder API** | Fluent, safe API for composing middleware chains in the correct topological order. |
| ♻️ **Lifecycle Propagation** | Graceful shutdown with `Flush()` and `Close()` traversing safely through all middleware. |
| 🚪 **Async-Safe Fatal** | `Fatal()`/`Exit()` flush and close the chain before terminating, so buffered records are never lost. |

---

## 📦 Installation

```bash
go get github.com/amhrmsn/go-logger
```

---

## 🚀 Quick Start

```go
package main

import (
	"log/slog"
	"os"

	logger "github.com/amhrmsn/go-logger"
)

func main() {
	// Create a JSON logger with source location
	log := logger.NewJSON(os.Stdout,
		logger.WithLevel(slog.LevelDebug),
		logger.WithSource(true),
	)

	log.Info("application started",
		slog.String("version", "1.0.0"),
		logger.Component("api"),
	)

	log.Error("request failed",
		logger.Err(os.ErrNotExist),
		logger.TraceID("abc-12345"),
		slog.Int("status", 404),
	)
}
```

---

## 🛠️ Middleware Handlers

`go-logger` provides powerful `slog.Handler` wrappers that you can compose together.

### AsyncHandler
Decouples log production from I/O by buffering records in a background goroutine. Crucial for high-throughput applications.

```go
import "github.com/amhrmsn/go-logger/handler"

h := handler.NewAsyncHandler(
	slog.NewJSONHandler(os.Stdout, nil),
	handler.WithBufferSize(4096),
	handler.WithDropPolicy(handler.Block),         // Options: Block | DropNewest | SyncFallback
	handler.WithAsyncBypassLevel(slog.LevelError), // Errors are written synchronously to ensure they are never lost
)
defer h.Close()

log := slog.New(h)
log.Info("this writes instantly to memory channel")
log.Error("this bypasses the channel and writes synchronously")
```

### RedactionHandler
Protects sensitive data using three complementary strategies: key matching, regex pattern matching, and strong typing.

```go
h := handler.NewRedactionHandler(
	slog.NewJSONHandler(os.Stdout, nil),
	handler.WithRedactKeys("password", "ssn", "auth.token"), // Exact keys or dotted paths
	handler.WithRedactPatterns(`(?i)secret`, `(?i)token`),   // Regex patterns
)
log := slog.New(h)

log.Info("user login",
	slog.String("username", "alice"),
	slog.String("password", "s3cret"),                  // → [REDACTED]
	slog.String("api_token", "eyJhb..."),               // → [REDACTED] (pattern match)
	slog.Any("key", logger.Redacted("sk-1234")),        // → [REDACTED] (type-based)
	slog.Any("cert", logger.SensitiveBytes(certBytes)), // → [REDACTED:256 bytes]
)
```

### SamplingHandler
Reduces log volume by probabilistically dropping log records. Extremely useful for high-traffic environments where storing 100% of logs is cost-prohibitive.

```go
h := handler.NewSamplingHandler(
	slog.NewJSONHandler(os.Stdout, nil),
	handler.WithSampleRate(0.1),                    // Keep only 10% of records
	handler.WithSampleBypassLevel(slog.LevelError), // Never sample errors (keep 100%)
	handler.WithSampleByLevel(map[slog.Level]float64{
		slog.LevelDebug: 0.01, // Keep 1% of debug logs
		slog.LevelInfo:  0.1,  // Keep 10% of info logs
	}),
)
```
*Note: Rates can be adjusted at runtime lock-free using `h.SetRate(0.5)` and `h.SetLevelRate(slog.LevelInfo, 0.2)`.*

For repetitive floods, **burst sampling** guarantees the first occurrences of every message are kept — something probabilistic sampling cannot promise:

```go
h := handler.NewSamplingHandler(
	slog.NewJSONHandler(os.Stdout, nil),
	// Per unique message: first 5 records each second always pass,
	// then every 100th. Errors still bypass sampling entirely.
	handler.WithBurstSampling(time.Second, 5, 100),
)
```

### ModuleHandler
Provides fine-grained, per-component log level filtering that can be updated at runtime without restarting the application.

```go
config := handler.NewModuleConfig(slog.LevelInfo) // Default fallback level
config.SetLevel("database", slog.LevelDebug)      // Verbose DB logging
config.SetLevel("auth", slog.LevelWarn)           // Quiet auth logging

h := handler.NewModuleHandler(
	slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
	config,
)

dbLog := slog.New(h).With(logger.Component("database"))
dbLog.Debug("query executed")  // ✅ Appears (database is set to Debug)

authLog := slog.New(h).With(logger.Component("auth"))
authLog.Info("user logged in") // ❌ Filtered out (auth is set to Warn)

// Runtime hot-reload:
config.SetLevel("auth", slog.LevelDebug)
authLog.Debug("now visible")   // ✅ Appears immediately

// Or drive levels from an env var / config file / admin endpoint:
_ = config.SetLevels("database=debug,auth=warn,*=info")
```

### MultiHandler
Fans out log records to multiple destinations (e.g., console and file) simultaneously.

```go
h := handler.NewMultiHandler(
	slog.NewJSONHandler(os.Stdout, nil),           // Structured JSON to stdout
	slog.NewTextHandler(logFile, nil),             // Human-readable Text to file
)
log := slog.New(h)
```

---

## 🏗️ Builder Pattern

Composing multiple middlewares manually can be error-prone (e.g., placing `Async` before `Redaction` might leak PII into memory channels). The `Builder` API enforces the optimal topological order:

```go
import (
	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

config := handler.NewModuleConfig(slog.LevelInfo)
config.SetLevel("database", slog.LevelDebug)

// Base handler
base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})

// Build the chain safely
h := logger.NewBuilder(base).
	WithAsync(handler.WithBufferSize(4096), handler.WithDropPolicy(handler.Block)).
	WithRedaction(handler.WithRedactKeys("password", "token")).
	WithSampling(handler.WithSampleRate(1.0)).
	WithModuleFilter(config).
	Build()

log := slog.New(h)
defer logger.Close(h) // Safely cascades through all layers to close the AsyncHandler
```
**Execution Order:** `ModuleHandler` → `SamplingHandler` → `RedactionHandler` → `AsyncHandler` → `JSONHandler`

Need a specific handler back out of the chain (e.g. for runtime stats)? Use the generic `Find`:

```go
if async, ok := logger.Find[*handler.AsyncHandler](log.Handler()); ok {
	fmt.Println("dropped:", async.Stats().Dropped)
}
```

---

## 🧩 Request-Scoped Loggers

Attach a logger to a `context.Context` once, retrieve it anywhere below — the standard pattern for per-request logging with correlation IDs:

```go
// In middleware:
log := logger.FromContext(ctx).With("request_id", reqID)
ctx = logger.NewContext(ctx, log)

// Deep inside a handler or service:
logger.FromContext(ctx).Info("query executed", "rows", n)
// Falls back to slog.Default() when the context carries no logger.
```

---

## 🛑 Graceful Shutdown

When using asynchronous buffering, it is critical to flush remaining logs to disk before the application exits.

```go
import (
	"os"
	"os/signal"
	"syscall"
	logger "github.com/amhrmsn/go-logger"
)

// ... setup logger ...

sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
<-sigCh // Wait for termination signal

// 1. Flush ensures all buffered records are written (blocks until queue is empty)
logger.Flush(h)

// 2. Close stops the background worker to prevent goroutine leaks
logger.Close(h)
```
`logger.Flush()` and `logger.Close()` automatically traverse the entire middleware chain to find the `AsyncHandler`.

### Fatal Errors

Calling `os.Exit` directly after logging **loses any records still queued in the async buffer**. Use the async-safe helpers instead:

```go
// Logs at Error level, flushes & closes the whole chain, then exits with code 1:
logger.Fatal(log, "cannot bind listener", "addr", addr, logger.Err(err))

// Non-logging variant with a custom exit code:
logger.Exit(log.Handler(), 2)
```

Both are best-effort with a 5-second timeout, so a stuck sink cannot hang a dying process.

---

## 📊 Benchmarks

*Measured on AMD Ryzen AI 9 HX 370 (24 cores), Windows, amd64.*

| Benchmark | ns/op | B/op | allocs/op |
|:---|---:|---:|---:|
| **Disabled (Filtered out)** | `1.5` | `0` | `0` |
| **JSON 10 fields (Baseline)** | `451.6` | `218` | `2` |
| **Full Chain (All Middleware)** | `2439` | `1435` | `9` |
| **AsyncHandler 10 fields** | `2055` | `825` | `5` |
| **RedactionHandler 10 fields** | `913.7` | `833` | `6` |
| **SamplingHandler (Sampled out)**| `130.4` | `208` | `1` |
| **MultiHandler 2 outputs** | `1210` | `842` | `7` |
| **ModuleHandler 10 fields** | `509.5` | `218` | `2` |

> *Note: The full chain overhead comes primarily from deep cloning records (vital for async stack safety), regex pattern matching in redaction, and `sync.RWMutex` locks ensuring 100% thread safety.*

---

## 📂 Project Structure

```text
go-logger/
├── logger.go           # Core constructors: New(), NewJSON(), NewText(), SetDefault()
├── options.go          # Config options: WithLevel(), WithSource()
├── attributes.go       # Attribute helpers: Err(), Component(), TraceID()
├── redact.go           # Strong types: Redacted, SensitiveBytes
├── lifecycle.go        # Closer/Flusher/Unwrapper interfaces and chain traversal
├── exit.go             # Exit()/Fatal(): flush-then-terminate helpers
├── context.go          # NewContext()/FromContext(): request-scoped loggers
├── find.go             # Find[T](): locate a handler inside the middleware chain
├── builder.go          # Fluent Builder API
├── handler/            # Core Middlewares
│   ├── multi.go        # Fan-out
│   ├── redaction.go    # PII masking
│   ├── sampling.go     # Probabilistic dropping
│   ├── module.go       # Component filtering
│   └── async.go        # Non-blocking background worker
└── examples/           # Ready-to-run demonstration code
```

---

## 📝 License

`go-logger` is distributed under the **MIT License**. See [`LICENSE`](LICENSE) for more details.
