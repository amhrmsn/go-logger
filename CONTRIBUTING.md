# Contributing to go-logger

Thanks for your interest in contributing!

## Ground Rules

- **Zero dependencies.** The core module must import only the Go standard
  library. PRs adding third-party imports will be declined; adapters belong in
  separate modules.
- **slog-native.** The public API produces and consumes standard
  `*slog.Logger` / `slog.Handler` values — no wrapper logger types.
- **Immutable handlers.** `WithAttrs` and `WithGroup` must return new
  instances; receivers are never mutated.
- **Concurrency changes need proof.** Anything touching the async worker,
  lifecycle, or shared state must pass `go test -race ./...` and, where
  practical, come with a stress test.

## Development

```bash
go vet ./...
go test -race -count=1 ./...   # requires cgo; CI runs this on Linux
```

On Windows without gcc, plain `go test ./...` works locally — CI covers the
race detector.

## Pull Requests

1. Keep changes focused; one concern per PR.
2. Add or update tests for any behavior change (regression tests for bug
   fixes, `Example*` functions for new public API).
3. Update `CHANGELOG.md` under an `Unreleased` heading.
4. Make sure `go vet` and the full test suite pass.
