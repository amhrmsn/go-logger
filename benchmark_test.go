package logger

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/amhrmsn/go-logger/handler"
)

// --- 10 standard fields used across benchmarks ---

func logTenFields(log *slog.Logger, ctx context.Context) {
	log.LogAttrs(ctx, slog.LevelInfo, "benchmark",
		slog.String("key1", "value1"),
		slog.Int("key2", 42),
		slog.Bool("key3", true),
		slog.Float64("key4", 3.14),
		slog.String("key5", "value5"),
		slog.Int("key6", 100),
		slog.String("key7", "value7"),
		slog.Duration("key8", time.Second),
		slog.String("key9", "value9"),
		slog.Int("key10", 999),
	)
}

// BenchmarkDisabled measures the cost of logging below the level threshold.
// This should be near-zero: Enabled() returns false, no record is created.
func BenchmarkDisabled(b *testing.B) {
	h := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})
	log := slog.New(h)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			log.LogAttrs(ctx, slog.LevelDebug, "should be disabled",
				slog.String("key1", "value1"),
				slog.Int("key2", 42),
			)
		}
	})
}

// BenchmarkJSON_10Fields measures the baseline cost of slog.JSONHandler
// with 10 structured fields. No middleware is active.
func BenchmarkJSON_10Fields(b *testing.B) {
	h := slog.NewJSONHandler(io.Discard, nil)
	log := slog.New(h)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logTenFields(log, ctx)
		}
	})
}

// BenchmarkJSON_FullChain_10Fields measures the cost of the full middleware
// chain (Async + Redaction + Sampling + Module) with 10 fields.
func BenchmarkJSON_FullChain_10Fields(b *testing.B) {
	config := handler.NewModuleConfig(slog.LevelDebug)

	h := NewBuilder(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})).
		WithAsync(
			handler.WithBufferSize(8192),
			handler.WithDropPolicy(handler.Block),
			handler.WithAsyncBypassLevel(slog.Level(100)), // disable bypass for benchmark
		).
		WithRedaction(handler.WithRedactKeys("key1", "key5")).
		WithSampling(handler.WithSampleRate(1.0)).
		WithModuleFilter(config).
		Build()

	log := slog.New(h).With(Component("benchmark"))
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logTenFields(log, ctx)
		}
	})

	b.StopTimer()
	_ = Flush(h)
	_ = Close(h)
}

// BenchmarkJSON_AsyncHandler_10Fields measures the cost of the async path
// (record cloning + channel send) with 10 fields.
func BenchmarkJSON_AsyncHandler_10Fields(b *testing.B) {
	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithBufferSize(8192),
		handler.WithDropPolicy(handler.Block),
		handler.WithAsyncBypassLevel(slog.Level(100)),
	)

	log := slog.New(h)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logTenFields(log, ctx)
		}
	})

	b.StopTimer()
	_ = h.Flush()
	_ = h.Close()
}

// BenchmarkJSON_RedactionHandler_10Fields measures the cost of the redaction
// handler with 2 keys marked for redaction out of 10 fields.
func BenchmarkJSON_RedactionHandler_10Fields(b *testing.B) {
	h := handler.NewRedactionHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithRedactKeys("key1", "key5"),
	)

	log := slog.New(h)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logTenFields(log, ctx)
		}
	})
}

// BenchmarkJSON_SamplingHandler_SampledOut measures the cost of a record
// that gets sampled out (rate=0.0). This should be very cheap since no
// record reaches the inner handler.
func BenchmarkJSON_SamplingHandler_SampledOut(b *testing.B) {
	h := handler.NewSamplingHandler(
		slog.NewJSONHandler(io.Discard, nil),
		handler.WithSampleRate(0.0),
		handler.WithSampleBypassLevel(slog.Level(100)), // disable bypass
	)

	log := slog.New(h)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logTenFields(log, ctx)
		}
	})
}

// BenchmarkJSON_MultiHandler_2Outputs measures the cost of fan-out to
// 2 JSONHandler outputs with 10 fields.
func BenchmarkJSON_MultiHandler_2Outputs(b *testing.B) {
	h := handler.NewMultiHandler(
		slog.NewJSONHandler(io.Discard, nil),
		slog.NewJSONHandler(io.Discard, nil),
	)

	log := slog.New(h)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logTenFields(log, ctx)
		}
	})
}

// BenchmarkJSON_ModuleHandler_10Fields measures the cost of the module
// handler with per-component filtering and 10 fields.
func BenchmarkJSON_ModuleHandler_10Fields(b *testing.B) {
	config := handler.NewModuleConfig(slog.LevelDebug)
	config.SetLevel("benchmark", slog.LevelDebug)

	h := handler.NewModuleHandler(
		slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}),
		config,
	)

	log := slog.New(h).With(Component("benchmark"))
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logTenFields(log, ctx)
		}
	})
}
