# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-04

Initial release.

### Added

- **AsyncHandler** — non-blocking logging via a background worker with
  configurable buffer size, three drop policies (`DropNewest`, `Block`,
  `SyncFallback`), synchronous bypass for critical levels, deterministic
  `Flush` barrier, graceful `Close` with full drain, and runtime `Stats()`.
- **RedactionHandler** — sensitive data masking by exact key, dotted group
  path, regex pattern, or custom function; recursive group inspection.
- **SamplingHandler** — probabilistic and per-level sampling with a
  never-sampled bypass level and lock-free runtime rate updates.
- **ModuleHandler / ModuleConfig** — per-component log level filtering with
  runtime hot-reload.
- **MultiHandler** — fan-out to multiple handlers with error aggregation and
  lifecycle propagation through wrapped children.
- **Builder** — fluent composition of the middleware chain in a safe,
  fixed topological order.
- **Lifecycle** — `Close`/`Flush` (and context variants) that traverse the
  entire middleware chain via `Unwrap()`.
- **Exit / Fatal** — process-termination helpers that flush and close the
  handler chain before exiting, so buffered async records are never lost.
- **Context helpers** — `NewContext` / `FromContext` for the request-scoped
  logger pattern; `FromContext` falls back to `slog.Default()` and tolerates
  a nil context.
- **`Unwrapper` interface** — formalizes the `Unwrap() slog.Handler` contract
  so third-party middleware can participate in lifecycle traversal.
- **`ModuleConfig.SetLevels`** — parse and apply a level spec string such as
  `"database=debug,auth=warn,*=info"`; validated atomically, easy to wire to
  env vars, config files, or admin endpoints.
- **Self-redacting types** — `Redacted` and `SensitiveBytes`.
- **Attribute helpers** — `Err`, `Component`, `TraceID`, `SpanID`.
- Core constructors `New`, `NewJSON`, `NewText` with functional options.

[0.1.0]: https://github.com/amhrmsn/go-logger/releases/tag/v0.1.0
