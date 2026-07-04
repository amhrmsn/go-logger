package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// --- logger.go tests ---

func TestNew(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, nil)
	log := New(h)

	log.Info("test message", "key", "value")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if result["msg"] != "test message" {
		t.Errorf("expected msg 'test message', got %v", result["msg"])
	}
	if result["key"] != "value" {
		t.Errorf("expected key 'value', got %v", result["key"])
	}
}

func TestNewJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	log.Info("json test", "count", 42)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if result["msg"] != "json test" {
		t.Errorf("expected msg 'json test', got %v", result["msg"])
	}
	// JSON numbers decode as float64
	if result["count"] != float64(42) {
		t.Errorf("expected count 42, got %v", result["count"])
	}
}

func TestNewJSON_WithOptions(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf,
		WithLevel(slog.LevelWarn),
		WithSource(true),
	)

	// Info should be filtered out
	log.Info("should not appear")
	if buf.Len() != 0 {
		t.Error("info message should not appear when level is Warn")
	}

	// Warn should appear with source
	log.Warn("warning message")
	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if result["msg"] != "warning message" {
		t.Errorf("expected msg 'warning message', got %v", result["msg"])
	}
	if result["source"] == nil {
		t.Error("expected source to be present when WithSource(true)")
	}
}

func TestNewText(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewText(&buf)

	log.Info("text test", "name", "alice")

	output := buf.String()
	if !strings.Contains(output, "text test") {
		t.Errorf("expected output to contain 'text test', got %q", output)
	}
	if !strings.Contains(output, "name=alice") {
		t.Errorf("expected output to contain 'name=alice', got %q", output)
	}
}

func TestNewText_WithOptions(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewText(&buf, WithLevel(slog.LevelError))

	log.Info("should not appear")
	log.Warn("should not appear either")
	if buf.Len() != 0 {
		t.Error("messages below Error should not appear")
	}

	log.Error("error message")
	output := buf.String()
	if !strings.Contains(output, "error message") {
		t.Errorf("expected output to contain 'error message', got %q", output)
	}
}

func TestSetDefaultAndDefault(t *testing.T) {
	// Not parallel: modifies global state
	var buf bytes.Buffer
	log := NewJSON(&buf)
	SetDefault(log)

	got := Default()
	// Verify they share the same handler
	got.Info("default test")
	if buf.Len() == 0 {
		t.Error("expected output from Default() logger after SetDefault()")
	}
}

// --- options.go tests ---

func TestWithLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf, WithLevel(slog.LevelDebug))

	log.Debug("debug message")
	if buf.Len() == 0 {
		t.Error("debug message should appear when level is Debug")
	}
}

func TestWithLevelVar(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	lv := &slog.LevelVar{}
	lv.Set(slog.LevelWarn)
	log := NewJSON(&buf, WithLevelVar(lv))

	// Info should be filtered
	log.Info("should not appear")
	if buf.Len() != 0 {
		t.Error("info should be filtered when LevelVar is Warn")
	}

	// Change level dynamically
	lv.Set(slog.LevelInfo)
	log.Info("should now appear")
	if buf.Len() == 0 {
		t.Error("info should appear after LevelVar changed to Info")
	}
}

func TestWithReplaceAttr(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf, WithReplaceAttr(func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "secret" {
			return slog.String(a.Key, "***")
		}
		return a
	}))

	log.Info("test", "secret", "my-password", "normal", "visible")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["secret"] != "***" {
		t.Errorf("expected secret '***', got %v", result["secret"])
	}
	if result["normal"] != "visible" {
		t.Errorf("expected normal 'visible', got %v", result["normal"])
	}
}

func TestWithSource_Disabled(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf, WithSource(false))

	log.Info("no source")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["source"] != nil {
		t.Error("source should not be present when disabled")
	}
}

func TestApplyOptions_NoOptions(t *testing.T) {
	t.Parallel()
	o := applyOptions(nil)
	ho := o.handlerOptions()

	if ho.Level != nil {
		t.Error("default level should be nil")
	}
	if ho.AddSource {
		t.Error("default AddSource should be false")
	}
	if ho.ReplaceAttr != nil {
		t.Error("default ReplaceAttr should be nil")
	}
}

// --- attrs.go tests ---

func TestErr(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	testErr := errors.New("something failed")
	log.Info("operation", Err(testErr))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["error"] != "something failed" {
		t.Errorf("expected error 'something failed', got %v", result["error"])
	}
}

func TestErr_Nil(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	log.Info("operation", Err(nil))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	// nil error should be logged as null
	if result["error"] != nil {
		t.Errorf("expected error nil, got %v", result["error"])
	}
}

func TestComponent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	log.Info("test", Component("networking"))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["component"] != "networking" {
		t.Errorf("expected component 'networking', got %v", result["component"])
	}
}

func TestTraceID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	log.Info("test", TraceID("abc123def456"))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["trace_id"] != "abc123def456" {
		t.Errorf("expected trace_id 'abc123def456', got %v", result["trace_id"])
	}
}

func TestSpanID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	log.Info("test", SpanID("span789"))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["span_id"] != "span789" {
		t.Errorf("expected span_id 'span789', got %v", result["span_id"])
	}
}

func TestAttrs_CombinedUsage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	testErr := errors.New("timeout")
	log.Error("request failed",
		Component("api"),
		TraceID("trace-1"),
		SpanID("span-1"),
		Err(testErr),
	)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["component"] != "api" {
		t.Errorf("expected component 'api', got %v", result["component"])
	}
	if result["trace_id"] != "trace-1" {
		t.Errorf("expected trace_id 'trace-1', got %v", result["trace_id"])
	}
	if result["span_id"] != "span-1" {
		t.Errorf("expected span_id 'span-1', got %v", result["span_id"])
	}
	if result["error"] != "timeout" {
		t.Errorf("expected error 'timeout', got %v", result["error"])
	}
}

// --- redact.go tests ---

func TestRedacted_LogValue(t *testing.T) {
	t.Parallel()
	r := Redacted("super-secret-password")
	val := r.LogValue()

	if val.String() != "[REDACTED]" {
		t.Errorf("expected '[REDACTED]', got %q", val.String())
	}
}

func TestRedacted_InLog(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	log.Info("config", "api_key", Redacted("sk-1234567890"))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["api_key"] != "[REDACTED]" {
		t.Errorf("expected api_key '[REDACTED]', got %v", result["api_key"])
	}
}

func TestRedacted_EmptyString(t *testing.T) {
	t.Parallel()
	r := Redacted("")
	val := r.LogValue()
	if val.String() != "[REDACTED]" {
		t.Errorf("expected '[REDACTED]' even for empty string, got %q", val.String())
	}
}

func TestSensitiveBytes_LogValue(t *testing.T) {
	t.Parallel()
	s := SensitiveBytes([]byte{0x01, 0x02, 0x03, 0x04})
	val := s.LogValue()

	if val.String() != "[REDACTED:4 bytes]" {
		t.Errorf("expected '[REDACTED:4 bytes]', got %q", val.String())
	}
}

func TestSensitiveBytes_InLog(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	key := make([]byte, 32)
	log.Info("loaded key", "private_key", SensitiveBytes(key))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["private_key"] != "[REDACTED:32 bytes]" {
		t.Errorf("expected '[REDACTED:32 bytes]', got %v", result["private_key"])
	}
}

func TestSensitiveBytes_Empty(t *testing.T) {
	t.Parallel()
	s := SensitiveBytes(nil)
	val := s.LogValue()
	if val.String() != "[REDACTED:0 bytes]" {
		t.Errorf("expected '[REDACTED:0 bytes]', got %q", val.String())
	}
}

func TestSensitiveBytes_LargePayload(t *testing.T) {
	t.Parallel()
	s := SensitiveBytes(make([]byte, 1048576)) // 1 MB
	val := s.LogValue()
	if val.String() != "[REDACTED:1048576 bytes]" {
		t.Errorf("expected '[REDACTED:1048576 bytes]', got %q", val.String())
	}
}

// --- Integration: combined usage test ---

func TestIntegration_NewJSON_WithAllOptions(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	lv := &slog.LevelVar{}
	lv.Set(slog.LevelDebug)

	log := NewJSON(&buf,
		WithLevelVar(lv),
		WithSource(false),
		WithReplaceAttr(func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "secret" {
				return slog.String(a.Key, "[HIDDEN]")
			}
			return a
		}),
	)

	log.Debug("debug msg", "secret", "password123", "visible", "yes")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["level"] != "DEBUG" {
		t.Errorf("expected level DEBUG, got %v", result["level"])
	}
	if result["secret"] != "[HIDDEN]" {
		t.Errorf("expected secret '[HIDDEN]', got %v", result["secret"])
	}
	if result["visible"] != "yes" {
		t.Errorf("expected visible 'yes', got %v", result["visible"])
	}
}

func TestIntegration_WithAttrs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	// Test slog.With which calls handler.WithAttrs
	childLog := log.With(Component("storage"), TraceID("trace-abc"))
	childLog.Info("writing data", "key", "value")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["component"] != "storage" {
		t.Errorf("expected component 'storage', got %v", result["component"])
	}
	if result["trace_id"] != "trace-abc" {
		t.Errorf("expected trace_id 'trace-abc', got %v", result["trace_id"])
	}
	if result["key"] != "value" {
		t.Errorf("expected key 'value', got %v", result["key"])
	}
}

func TestIntegration_WithGroup(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf)

	groupedLog := log.WithGroup("request")
	groupedLog.Info("incoming", "method", "GET", "path", "/api")

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	request, ok := result["request"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'request' to be a map, got %T", result["request"])
	}
	if request["method"] != "GET" {
		t.Errorf("expected method 'GET', got %v", request["method"])
	}
	if request["path"] != "/api" {
		t.Errorf("expected path '/api', got %v", request["path"])
	}
}

func TestIntegration_Redacted_WithReplaceAttr(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := NewJSON(&buf, WithReplaceAttr(func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "token" {
			return slog.String(a.Key, "[REPLACED]")
		}
		return a
	}))

	// Redacted type should take precedence over ReplaceAttr for api_key
	// because LogValuer is resolved before ReplaceAttr is called
	log.Info("auth",
		"api_key", Redacted("sk-secret"),
		"token", "bearer-xyz",
	)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["api_key"] != "[REDACTED]" {
		t.Errorf("expected api_key '[REDACTED]', got %v", result["api_key"])
	}
	if result["token"] != "[REPLACED]" {
		t.Errorf("expected token '[REPLACED]', got %v", result["token"])
	}
}
