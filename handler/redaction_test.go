package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

// --- slogtest compliance ---

func TestRedactionHandler_SlogtestCompliance(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	// Use a handler with no redaction keys to ensure compliance passes.
	h := NewRedactionHandler(inner)

	err := slogtest.TestHandler(h, func() []map[string]any {
		var results []map[string]any
		for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(line, &m); err == nil {
				results = append(results, m)
			}
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRedactionHandler_SlogtestCompliance_WithRedaction(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	// Redacting keys that slogtest doesn't use — must still pass compliance.
	h := NewRedactionHandler(inner, WithRedactKeys("password", "secret"))

	err := slogtest.TestHandler(h, func() []map[string]any {
		var results []map[string]any
		for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(line, &m); err == nil {
				results = append(results, m)
			}
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}

// --- Exact key redaction ---

func TestRedactionHandler_ExactKey(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("password", "token"),
	)
	log := slog.New(h)

	log.Info("login",
		"user", "alice",
		"password", "secret123",
		"token", "bearer-xyz",
		"visible", "yes",
	)

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["user"], "alice")
	assertEqual(t, result["password"], "[REDACTED]")
	assertEqual(t, result["token"], "[REDACTED]")
	assertEqual(t, result["visible"], "yes")
}

func TestRedactionHandler_NoMatchingKeys(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("password"),
	)
	log := slog.New(h)

	log.Info("test", "user", "bob", "role", "admin")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["user"], "bob")
	assertEqual(t, result["role"], "admin")
}

func TestRedactionHandler_CaseSensitive(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("Password"), // Capital P
	)
	log := slog.New(h)

	log.Info("test", "password", "visible", "Password", "hidden")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["password"], "visible")    // lowercase not matched
	assertEqual(t, result["Password"], "[REDACTED]") // exact match
}

// --- Regex pattern redaction ---

func TestRedactionHandler_RegexPattern(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactPatterns(`(?i)(password|secret|token|key|credential)`),
	)
	log := slog.New(h)

	log.Info("config",
		"api_key", "sk-123",
		"db_password", "pass",
		"access_token", "tok-abc",
		"hostname", "example.com",
	)

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["api_key"], "[REDACTED]")
	assertEqual(t, result["db_password"], "[REDACTED]")
	assertEqual(t, result["access_token"], "[REDACTED]")
	assertEqual(t, result["hostname"], "example.com")
}

func TestRedactionHandler_MultiplePatterns(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactPatterns(`^ssn$`, `^credit_card`),
	)
	log := slog.New(h)

	log.Info("pii", "ssn", "123-45-6789", "credit_card_number", "4111111111111111", "name", "Bob")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["ssn"], "[REDACTED]")
	assertEqual(t, result["credit_card_number"], "[REDACTED]")
	assertEqual(t, result["name"], "Bob")
}

func TestRedactionHandler_InvalidPattern_Panics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid regex pattern")
		}
	}()

	_ = NewRedactionHandler(
		slog.NewJSONHandler(nil, nil),
		WithRedactPatterns(`[invalid`),
	)
}

// --- Custom redact function ---

func TestRedactionHandler_CustomFunc(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactFunc(func(groups []string, key string, value slog.Value) slog.Value {
			// Redact any value that looks like a JWT.
			if strings.HasPrefix(value.String(), "eyJ") {
				return slog.StringValue("[JWT_REDACTED]")
			}
			return value
		}),
	)
	log := slog.New(h)

	log.Info("auth",
		"token", "eyJhbGciOiJIUzI1NiJ9.payload.sig",
		"session", "normal-session-id",
	)

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["token"], "[JWT_REDACTED]")
	assertEqual(t, result["session"], "normal-session-id")
}

func TestRedactionHandler_CustomFunc_WithGroups(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactFunc(func(groups []string, key string, value slog.Value) slog.Value {
			if len(groups) > 0 && groups[0] == "auth" && key == "secret" {
				return slog.StringValue("[CUSTOM_REDACTED]")
			}
			return value
		}),
	)
	log := slog.New(h)

	log.Info("test",
		slog.Group("auth", "secret", "s3cr3t", "user", "alice"),
		"secret", "top-level-visible",
	)

	result := parseJSON(t, buf.Bytes())
	auth := result["auth"].(map[string]any)
	assertEqual(t, auth["secret"], "[CUSTOM_REDACTED]")
	assertEqual(t, auth["user"], "alice")
	assertEqual(t, result["secret"], "top-level-visible") // top-level not in auth group
}

// --- Nested group redaction ---

func TestRedactionHandler_NestedGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("token", "password"),
	)
	log := slog.New(h)

	log.Info("auth",
		slog.Group("auth",
			"token", "my-secret-token",
			"user", "alice",
		),
	)

	result := parseJSON(t, buf.Bytes())
	auth := result["auth"].(map[string]any)
	assertEqual(t, auth["token"], "[REDACTED]")
	assertEqual(t, auth["user"], "alice")
}

func TestRedactionHandler_DeeplyNestedGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("password"),
	)
	log := slog.New(h)

	log.Info("deep",
		slog.Group("level1",
			slog.Group("level2",
				slog.Group("level3",
					"password", "deep-secret",
					"visible", "ok",
				),
			),
		),
	)

	result := parseJSON(t, buf.Bytes())
	l1 := result["level1"].(map[string]any)
	l2 := l1["level2"].(map[string]any)
	l3 := l2["level3"].(map[string]any)
	assertEqual(t, l3["password"], "[REDACTED]")
	assertEqual(t, l3["visible"], "ok")
}

// --- Dotted key path matching ---

func TestRedactionHandler_DottedKeyPath(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("auth.token", "db.password"),
	)
	log := slog.New(h)

	log.Info("config",
		slog.Group("auth",
			"token", "secret-token",
			"user", "alice",
		),
		slog.Group("db",
			"password", "db-pass",
			"host", "localhost",
		),
		"token", "top-level-visible", // not matched: "token" alone is not in the key set
	)

	result := parseJSON(t, buf.Bytes())

	auth := result["auth"].(map[string]any)
	assertEqual(t, auth["token"], "[REDACTED]")
	assertEqual(t, auth["user"], "alice")

	db := result["db"].(map[string]any)
	assertEqual(t, db["password"], "[REDACTED]")
	assertEqual(t, db["host"], "localhost")

	assertEqual(t, result["token"], "top-level-visible")
}

func TestRedactionHandler_DottedKeyPath_ThreeLevels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("request.headers.authorization"),
	)
	log := slog.New(h)

	log.Info("http",
		slog.Group("request",
			slog.Group("headers",
				"authorization", "Bearer secret-jwt",
				"content-type", "application/json",
			),
		),
	)

	result := parseJSON(t, buf.Bytes())
	req := result["request"].(map[string]any)
	headers := req["headers"].(map[string]any)
	assertEqual(t, headers["authorization"], "[REDACTED]")
	assertEqual(t, headers["content-type"], "application/json")
}

// --- LogValuer interaction ---

// redactedType implements slog.LogValuer for testing.
type redactedType string

func (r redactedType) LogValue() slog.Value {
	return slog.StringValue("[TYPE_REDACTED]")
}

func TestRedactionHandler_LogValuerStillWorks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		// No redaction keys — LogValuer should still work.
	)
	log := slog.New(h)

	log.Info("test", "api_key", redactedType("sk-secret"))

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["api_key"], "[TYPE_REDACTED]")
}

func TestRedactionHandler_LogValuerWithRedactKeys(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("api_key"),
	)
	log := slog.New(h)

	// LogValuer is resolved first, then key-based redaction checks the key.
	// Since "api_key" is in the redact list, it should be "[REDACTED]" regardless.
	log.Info("test", "api_key", redactedType("sk-secret"))

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["api_key"], "[REDACTED]")
}

// --- WithAttrs ---

func TestRedactionHandler_WithAttrs_RedactsPreAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("token"),
	)

	child := h.WithAttrs([]slog.Attr{
		slog.String("token", "pre-applied-secret"),
		slog.String("service", "api"),
	})
	log := slog.New(child)
	log.Info("test")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["token"], "[REDACTED]")
	assertEqual(t, result["service"], "api")
}

func TestRedactionHandler_WithAttrs_OriginalUnchanged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("password"),
	)

	_ = h.WithAttrs([]slog.Attr{slog.String("env", "prod")})

	// Original handler should not have the attrs.
	log := slog.New(h)
	log.Info("original", "password", "secret")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["password"], "[REDACTED]")
	if _, ok := result["env"]; ok {
		t.Error("original handler should not have 'env' attr from WithAttrs child")
	}
}

// --- WithGroup ---

func TestRedactionHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("req.password"),
	)

	grouped := h.WithGroup("req")
	log := slog.New(grouped)
	log.Info("test", "password", "secret", "user", "alice")

	result := parseJSON(t, buf.Bytes())
	req := result["req"].(map[string]any)
	assertEqual(t, req["password"], "[REDACTED]")
	assertEqual(t, req["user"], "alice")
}

func TestRedactionHandler_WithGroup_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(slog.NewJSONHandler(&buf, nil))

	child := h.WithGroup("")
	if child != h {
		t.Error("WithGroup('') should return the same handler")
	}
}

// --- Combined keys + patterns + func ---

func TestRedactionHandler_CombinedStrategies(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("password"),
		WithRedactPatterns(`(?i)secret`),
		WithRedactFunc(func(groups []string, key string, value slog.Value) slog.Value {
			if strings.HasPrefix(value.String(), "eyJ") {
				return slog.StringValue("[JWT]")
			}
			return value
		}),
	)
	log := slog.New(h)

	log.Info("combo",
		"password", "pass123", // key match
		"my_secret_key", "s3cr3t", // pattern match
		"jwt_token", "eyJhbGci.payload", // custom func match
		"normal", "visible", // no match
	)

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["password"], "[REDACTED]")
	assertEqual(t, result["my_secret_key"], "[REDACTED]")
	assertEqual(t, result["jwt_token"], "[JWT]")
	assertEqual(t, result["normal"], "visible")
}

// --- Concurrency ---

func TestRedactionHandler_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &lockedWriter{buf: &buf, mu: &mu}

	h := NewRedactionHandler(
		slog.NewJSONHandler(w, nil),
		WithRedactKeys("password"),
	)
	log := slog.New(h)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				log.Info("concurrent",
					"goroutine", n,
					"password", "secret",
					"iteration", j,
				)
			}
		}(i)
	}
	wg.Wait()

	mu.Lock()
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	mu.Unlock()

	count := 0
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		count++
		var result map[string]any
		if err := json.Unmarshal(line, &result); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if result["password"] != "[REDACTED]" {
			t.Errorf("password should be redacted, got %v", result["password"])
		}
	}

	expected := goroutines * iterations
	if count != expected {
		t.Errorf("expected %d log lines, got %d", expected, count)
	}
}

// --- Edge cases ---

func TestRedactionHandler_NoOptions(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(slog.NewJSONHandler(&buf, nil))
	log := slog.New(h)

	log.Info("test", "password", "visible", "key", "value")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["password"], "visible") // No redaction configured.
	assertEqual(t, result["key"], "value")
}

func TestRedactionHandler_EmptyRecord(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactKeys("password"),
	)
	log := slog.New(h)

	log.Info("no attrs")

	result := parseJSON(t, buf.Bytes())
	assertEqual(t, result["msg"], "no attrs")
}

func TestRedactionHandler_GroupWithPatternMatch(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRedactionHandler(
		slog.NewJSONHandler(&buf, nil),
		WithRedactPatterns(`auth\..*token`),
	)
	log := slog.New(h)

	log.Info("test",
		slog.Group("auth",
			"access_token", "secret",
			"user", "alice",
		),
	)

	result := parseJSON(t, buf.Bytes())
	auth := result["auth"].(map[string]any)
	assertEqual(t, auth["access_token"], "[REDACTED]")
	assertEqual(t, auth["user"], "alice")
}

// --- helpers ---

// lockedWriter wraps a bytes.Buffer with a mutex for concurrent writes.
type lockedWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func parseJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	// Find the last complete JSON line.
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) > 0 {
			var result map[string]any
			if err := json.Unmarshal(lines[i], &result); err != nil {
				t.Fatalf("failed to parse JSON: %v\ndata: %s", err, lines[i])
			}
			return result
		}
	}
	t.Fatal("no JSON output found")
	return nil
}

func assertEqual(t *testing.T, got, expected any) {
	t.Helper()
	if got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

// Ensure time is used for slog.NewRecord in error tests.
var _ = time.Now
