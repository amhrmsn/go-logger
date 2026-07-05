# Roadmap

Items below are **deliberately deferred until real-world usage proves they
are needed**. Designing them speculatively risks building the wrong shape;
each entry records the motivation and the design constraints discovered so
far, so future work can start from context instead of from scratch.

If one of these blocks your use of go-logger, please open an issue describing
the concrete scenario — that is exactly the signal these items are waiting
for.

## Deferred — waiting for a concrete use case

### Redaction presets & value scanning

**What:** Ready-made key/pattern sets for common secrets (email, credit card
PAN, JWT, hex-encoded private keys), plus *value-based* scanning: inspecting
attribute **values** for secret-shaped content regardless of the key.

**Why deferred:** Preset quality depends on a corpus of real log data —
guessing patterns produces both false positives (redacting order IDs that
look like card numbers) and false negatives (missing a provider-specific
token format). Value scanning also has a real CPU cost per record that
should be measured against an actual workload before choosing a design
(per-value regex vs. Aho-Corasick vs. entropy heuristics).

**Constraints discovered so far:** must stay allocation-conscious on the hot
path; must compose with the existing key/pattern/func layers; presets must be
opt-in per-set rather than one bundle.

### Prometheus adapter

**What:** Native Prometheus collectors for `AsyncStats`, `SampleStats`,
`DedupStats`.

**Status:** The stdlib-only expvar adapter exists (`handler.PublishAsyncStats`
et al.) and covers the `/debug/vars` route, which common expvar→Prometheus
exporters can scrape. A native collector requires the
`prometheus/client_golang` dependency, so it must live in a **separate
module** (like the removed `otel/` adapter did) to keep the core
zero-dependency.

**Why deferred:** which metric system the first real consumers use should
decide whether this module is worth maintaining.

### Batching / network sinks

**What:** A sink that batches records and ships them to a remote backend
(Loki, Elasticsearch, OTLP) with retry, backoff, and bounded buffering.

**Why deferred:** This is a different engineering domain (network failure
modes, wire formats, delivery guarantees) and each backend already maintains
its own slog-compatible client. If built, it would be a separate module —
the `AsyncHandler` worker is intentionally a single-goroutine, one-record
writer and will stay that way.

### Benchmark regression gate

**What:** CI currently runs benchmarks as a smoke test (they must compile and
run; numbers are printed for eyeballing). A true regression gate
(benchstat-style comparison against a baseline with failure thresholds)
is deferred.

**Why deferred:** shared CI runners have double-digit-percent noise; a
reliable gate needs either dedicated runners or a long-window statistical
baseline. Revisit when performance regressions actually bite.

## Permanent non-goals

These are decisions, not gaps:

- **Zero-allocation logging (zap-style).** go-logger's identity is
  slog-native simplicity; the ~2µs async enqueue path is the accepted cost.
  Use aggressive sampling for hotter paths, or a different library.
- **Simplifying `ModuleHandler`'s `Enabled` heuristic.** Its complexity buys
  the dual component-resolution modes (via `.With()` and via call-site attr)
  and is locked in by tests.
- **In-core adapters with third-party dependencies.** Anything importing
  outside the standard library goes in a separate module.
