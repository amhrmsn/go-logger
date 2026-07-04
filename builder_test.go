package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/amhrmsn/go-logger/handler"
)

// --- Builder: basic construction ---

func TestBuilder_NewBuilder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	b := NewBuilder(slog.NewJSONHandler(&buf, nil))
	h := b.Build()

	log := slog.New(h)
	log.Info("test", "key", "value")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result["msg"] != "test" {
		t.Errorf("expected msg 'test', got %v", result["msg"])
	}
	if result["key"] != "value" {
		t.Errorf("expected key 'value', got %v", result["key"])
	}
}

func TestBuilder_BuildLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewBuilder(slog.NewJSONHandler(&buf, nil)).BuildLogger()

	log.Info("from BuildLogger")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result["msg"] != "from BuildLogger" {
		t.Errorf("expected msg 'from BuildLogger', got %v", result["msg"])
	}
}

// --- Builder: individual middleware ---

func TestBuilder_WithRedaction(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithRedaction(handler.WithRedactKeys("password")).
		BuildLogger()

	log.Info("login", "user", "alice", "password", "secret123")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result["password"] != "[REDACTED]" {
		t.Errorf("expected password '[REDACTED]', got %v", result["password"])
	}
	if result["user"] != "alice" {
		t.Errorf("expected user 'alice', got %v", result["user"])
	}
}

func TestBuilder_WithSampling(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithSampling(
			handler.WithSampleRate(0.0),
			handler.WithSampleBypassLevel(slog.Level(100)),
		).
		BuildLogger()

	log.Info("should be dropped")

	if buf.Len() != 0 {
		t.Error("expected no output with rate=0.0")
	}
}

func TestBuilder_WithModuleFilter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := handler.NewModuleConfig(slog.LevelError)
	config.SetLevel("api", slog.LevelDebug)

	log := NewBuilder(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		WithModuleFilter(config).
		BuildLogger()

	// Without component — filtered (default=Error).
	log.Info("filtered")
	if buf.Len() != 0 {
		t.Error("expected no output without component (default=Error)")
	}

	// With api component — accepted.
	log.Info("accepted", "component", "api")
	if buf.Len() == 0 {
		t.Error("expected output with api component (level=Debug)")
	}
}

func TestBuilder_WithAsync(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithAsync(
			handler.WithBufferSize(64),
			handler.WithDropPolicy(handler.Block),
			handler.WithAsyncBypassLevel(slog.Level(100)),
		).
		Build()

	log := slog.New(h)
	log.Info("async test")

	_ = Flush(h)
	_ = Close(h)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result["msg"] != "async test" {
		t.Errorf("expected msg 'async test', got %v", result["msg"])
	}
}

func TestBuilder_WithMiddleware(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithMiddleware(func(inner slog.Handler) slog.Handler {
			// Custom middleware that adds a "custom" attribute.
			return handler.NewRedactionHandler(inner) // no-op redaction as a pass-through
		}).
		BuildLogger()

	log.Info("custom middleware")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result["msg"] != "custom middleware" {
		t.Errorf("expected msg 'custom middleware', got %v", result["msg"])
	}
}

// --- Builder: full chain integration ---

func TestBuilder_FullChain(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := handler.NewModuleConfig(slog.LevelDebug)

	h := NewBuilder(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		WithAsync(
			handler.WithBufferSize(256),
			handler.WithDropPolicy(handler.Block),
			handler.WithAsyncBypassLevel(slog.Level(100)),
		).
		WithRedaction(handler.WithRedactKeys("password", "token")).
		WithSampling(handler.WithSampleRate(1.0)).
		WithModuleFilter(config).
		Build()

	log := slog.New(h)

	log.Info("full chain",
		"user", "alice",
		"password", "secret123",
		"token", "bearer-xyz",
		"visible", "yes",
		"component", "api",
	)

	_ = Flush(h)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	// Verify redaction.
	if result["password"] != "[REDACTED]" {
		t.Errorf("password should be '[REDACTED]', got %v", result["password"])
	}
	if result["token"] != "[REDACTED]" {
		t.Errorf("token should be '[REDACTED]', got %v", result["token"])
	}

	// Verify non-redacted fields pass through.
	if result["user"] != "alice" {
		t.Errorf("user should be 'alice', got %v", result["user"])
	}
	if result["visible"] != "yes" {
		t.Errorf("visible should be 'yes', got %v", result["visible"])
	}

	_ = Close(h)
}

func TestBuilder_FullChain_WithTypeRedaction(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := handler.NewModuleConfig(slog.LevelDebug)

	h := NewBuilder(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		WithAsync(
			handler.WithBufferSize(64),
			handler.WithDropPolicy(handler.Block),
			handler.WithAsyncBypassLevel(slog.Level(100)),
		).
		WithRedaction(handler.WithRedactKeys("extra_secret")).
		WithModuleFilter(config).
		Build()

	log := slog.New(h)

	// Test type-level redaction (Redacted type) + key-level redaction.
	log.Info("mixed redaction",
		"api_key", Redacted("sk-1234567890"),
		"extra_secret", "should-be-redacted",
		"normal", "visible",
		"component", "auth",
	)

	_ = Flush(h)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	if result["api_key"] != "[REDACTED]" {
		t.Errorf("api_key (Redacted type) should be '[REDACTED]', got %v", result["api_key"])
	}
	if result["extra_secret"] != "[REDACTED]" {
		t.Errorf("extra_secret (key redaction) should be '[REDACTED]', got %v", result["extra_secret"])
	}
	if result["normal"] != "visible" {
		t.Errorf("normal should be 'visible', got %v", result["normal"])
	}

	_ = Close(h)
}

// --- Builder: composition order ---

func TestBuilder_CompositionOrder_ModuleFiltersBeforeSampling(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := handler.NewModuleConfig(slog.LevelError) // reject Info

	log := NewBuilder(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		WithSampling(handler.WithSampleRate(1.0)). // keep all if it reaches sampling
		WithModuleFilter(config).                  // outermost — should filter first
		BuildLogger()

	log.Info("should be filtered by module", "component", "unknown")

	if buf.Len() != 0 {
		t.Error("ModuleHandler (outermost) should filter before SamplingHandler sees the record")
	}
}

// --- Builder: lifecycle integration ---

func TestBuilder_Close_CascadesToAsync(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithAsync(
			handler.WithBufferSize(64),
			handler.WithDropPolicy(handler.Block),
			handler.WithAsyncBypassLevel(slog.Level(100)),
		).
		Build()

	log := slog.New(h)
	log.Info("before close")

	// Close should cascade to AsyncHandler.
	err := Close(h)
	if err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestBuilder_Flush_CascadesToAsync(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithAsync(
			handler.WithBufferSize(64),
			handler.WithDropPolicy(handler.Block),
			handler.WithAsyncBypassLevel(slog.Level(100)),
		).
		Build()

	log := slog.New(h)
	log.Info("before flush")

	err := Flush(h)
	if err != nil {
		t.Errorf("Flush() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("expected output after Flush")
	}

	_ = Close(h)
}

// --- Builder: no middleware ---

func TestBuilder_NoMiddleware(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := NewBuilder(slog.NewJSONHandler(&buf, nil)).BuildLogger()

	log.Info("bare handler", "key", "value")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result["msg"] != "bare handler" {
		t.Errorf("expected msg 'bare handler', got %v", result["msg"])
	}
}

// --- Builder: chaining ---

func TestBuilder_Chaining(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// Verify fluent chaining compiles and works.
	h := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithRedaction(handler.WithRedactKeys("secret")).
		WithSampling(handler.WithSampleRate(1.0)).
		Build()

	log := slog.New(h)
	log.Info("chained", "secret", "hidden", "open", "visible")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result["secret"] != "[REDACTED]" {
		t.Errorf("expected secret '[REDACTED]', got %v", result["secret"])
	}
	if result["open"] != "visible" {
		t.Errorf("expected open 'visible', got %v", result["open"])
	}
}

// --- Builder: multiple custom middleware ---

func TestBuilder_MultipleMiddlewareOrder(t *testing.T) {
	t.Parallel()

	var callOrder []string

	var buf bytes.Buffer
	h := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithMiddleware(func(inner slog.Handler) slog.Handler {
			return &middlewareMock{
				inner: inner,
				name:  "mw1",
				order: &callOrder,
			}
		}).
		WithMiddleware(func(inner slog.Handler) slog.Handler {
			return &middlewareMock{
				inner: inner,
				name:  "mw2",
				order: &callOrder,
			}
		}).
		Build()

	log := slog.New(h)
	log.Info("test order")

	// Middleware should be applied in registration order (mw1 wraps core, mw2 wraps mw1).
	// Therefore, at execution time (Handle), mw2 is the outermost and runs FIRST.
	if len(callOrder) != 2 {
		t.Fatalf("expected 2 middleware calls, got %d", len(callOrder))
	}
	if callOrder[0] != "mw2" || callOrder[1] != "mw1" {
		t.Errorf("expected execution order [mw2, mw1], got %v", callOrder)
	}
}

type middlewareMock struct {
	inner slog.Handler
	name  string
	order *[]string
}

func (m *middlewareMock) Enabled(ctx context.Context, level slog.Level) bool { return true }
func (m *middlewareMock) Handle(ctx context.Context, r slog.Record) error {
	*m.order = append(*m.order, m.name)
	return m.inner.Handle(ctx, r)
}
func (m *middlewareMock) WithAttrs(attrs []slog.Attr) slog.Handler { return m }
func (m *middlewareMock) WithGroup(name string) slog.Handler       { return m }

// --- Builder: lifecycle on non-async handler ---

func TestBuilder_Close_NoAsync(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewBuilder(slog.NewJSONHandler(&buf, nil)).
		WithRedaction(handler.WithRedactKeys("pw")).
		Build()

	// Close on a non-Closer handler should return nil.
	err := Close(h)
	if err != nil {
		t.Errorf("Close() on non-async handler should return nil, got %v", err)
	}
}

// --- Builder: full chain lifecycle cascade ---

func TestBuilder_FullChain_Lifecycle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := handler.NewModuleConfig(slog.LevelDebug)

	h := NewBuilder(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		WithAsync(
			handler.WithBufferSize(128),
			handler.WithDropPolicy(handler.Block),
			handler.WithAsyncBypassLevel(slog.Level(100)),
		).
		WithRedaction(handler.WithRedactKeys("password")).
		WithSampling(handler.WithSampleRate(1.0)).
		WithModuleFilter(config).
		Build()

	log := slog.New(h)

	// Log several records.
	for i := 0; i < 10; i++ {
		log.Info("lifecycle test", "i", i, "password", "secret", "component", "api")
	}

	// Flush should ensure all records are written.
	_ = Flush(h)

	lines := countLines(buf.Bytes())
	if lines != 10 {
		t.Errorf("expected 10 lines after Flush, got %d", lines)
	}

	// Verify redaction worked.
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var result map[string]any
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("parse JSON: %v", err)
		}
		if result["password"] != "[REDACTED]" {
			t.Errorf("password should be '[REDACTED]', got %v", result["password"])
		}
	}

	// Close should cascade.
	_ = Close(h)
}

// --- helpers ---

func countLines(data []byte) int {
	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) > 0 {
			count++
		}
	}
	return count
}
