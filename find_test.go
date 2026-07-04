package logger_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

func TestFind_ConcreteTypeInBuilderChain(t *testing.T) {
	t.Parallel()

	h := logger.NewBuilder(slog.NewJSONHandler(io.Discard, nil)).
		WithAsync().
		WithSampling(handler.WithSampleRate(1.0)).
		WithRedaction(handler.WithRedactKeys("password")).
		Build()
	defer func() { _ = logger.Close(h) }()

	async, ok := logger.Find[*handler.AsyncHandler](h)
	if !ok || async == nil {
		t.Fatal("Find must locate the AsyncHandler inside the builder chain")
	}
	// Prove it is the live handler: stats must be readable.
	_ = async.Stats()

	sampling, ok := logger.Find[*handler.SamplingHandler](h)
	if !ok || sampling == nil {
		t.Error("Find must locate the SamplingHandler inside the builder chain")
	}
}

func TestFind_InterfaceType(t *testing.T) {
	t.Parallel()

	h := logger.NewBuilder(slog.NewJSONHandler(io.Discard, nil)).
		WithAsync().
		Build()
	defer func() { _ = logger.Close(h) }()

	f, ok := logger.Find[logger.Flusher](h)
	if !ok || f == nil {
		t.Fatal("Find must locate a Flusher via interface type")
	}
	if err := f.Flush(); err != nil {
		t.Errorf("found Flusher must be usable: %v", err)
	}
}

func TestFind_NotFound(t *testing.T) {
	t.Parallel()

	h := slog.NewJSONHandler(io.Discard, nil)
	if _, ok := logger.Find[*handler.AsyncHandler](h); ok {
		t.Error("Find must report false when the type is absent")
	}
}

// cyclicHandler unwraps to itself — a degenerate chain that must not hang
// Find or the lifecycle traversal.
type cyclicHandler struct{ slog.Handler }

func (c *cyclicHandler) Unwrap() slog.Handler { return c }

func TestFind_CyclicChain_Terminates(t *testing.T) {
	t.Parallel()

	c := &cyclicHandler{Handler: slog.NewJSONHandler(io.Discard, nil)}
	if _, ok := logger.Find[*handler.AsyncHandler](c); ok {
		t.Error("cyclic chain must terminate with not-found")
	}
}

func TestLifecycle_CyclicChain_Terminates(t *testing.T) {
	t.Parallel()

	c := &cyclicHandler{Handler: slog.NewJSONHandler(io.Discard, nil)}
	// Must return (not hang) despite the cycle.
	if err := logger.CloseContext(context.Background(), c); err != nil {
		t.Errorf("CloseContext on cyclic chain: %v", err)
	}
	if err := logger.FlushContext(context.Background(), c); err != nil {
		t.Errorf("FlushContext on cyclic chain: %v", err)
	}
}
