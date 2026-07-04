package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/slogtest"
)

// --- slogtest compliance ---

func TestModuleHandler_SlogtestCompliance(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelDebug) // accept all for slogtest
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

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

// --- Per-module filtering ---

func TestModuleHandler_PerModuleFiltering(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelInfo)
	config.SetLevel("networking", slog.LevelDebug)
	config.SetLevel("storage", slog.LevelError)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)
	log := slog.New(h)

	// Networking: Debug is accepted (level=Debug)
	log.Debug("net debug", "component", "networking")
	if buf.Len() == 0 {
		t.Error("networking debug should be logged (module level=Debug)")
	}
	buf.Reset()

	// Storage: Info is rejected (level=Error)
	log.Info("storage info", "component", "storage")
	if buf.Len() != 0 {
		t.Error("storage info should be filtered (module level=Error)")
	}

	// Storage: Error is accepted
	log.Error("storage error", "component", "storage")
	if buf.Len() == 0 {
		t.Error("storage error should be logged (module level=Error)")
	}
}

// --- Default level ---

func TestModuleHandler_DefaultLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelWarn)
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)
	log := slog.New(h)

	// No component attr → uses default level (Warn)
	log.Info("info without component")
	if buf.Len() != 0 {
		t.Error("info should be filtered when default level is Warn")
	}

	log.Warn("warn without component")
	if buf.Len() == 0 {
		t.Error("warn should be logged when default level is Warn")
	}
}

func TestModuleHandler_DefaultLevel_UnknownComponent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError)
	config.SetLevel("known", slog.LevelDebug)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)
	log := slog.New(h)

	// Unknown component → uses default level (Error)
	log.Info("info", "component", "unknown-module")
	if buf.Len() != 0 {
		t.Error("unknown component should use default level (Error)")
	}

	log.Error("error", "component", "unknown-module")
	if buf.Len() == 0 {
		t.Error("error should be logged for unknown component")
	}
}

// --- SetLevels spec parsing ---

func TestModuleConfig_SetLevels_Valid(t *testing.T) {
	t.Parallel()

	c := NewModuleConfig(slog.LevelInfo)
	if err := c.SetLevels("database=debug, auth=warn ,*=error,"); err != nil {
		t.Fatalf("SetLevels error: %v", err)
	}

	if got := c.levelFor("database").Level(); got != slog.LevelDebug {
		t.Errorf("database: expected Debug, got %v", got)
	}
	if got := c.levelFor("auth").Level(); got != slog.LevelWarn {
		t.Errorf("auth: expected Warn, got %v", got)
	}
	if got := c.levelFor("unknown").Level(); got != slog.LevelError {
		t.Errorf("default via *: expected Error, got %v", got)
	}
	if got := c.minLevel(); got != slog.LevelDebug {
		t.Errorf("minLevel cache: expected Debug, got %v", got)
	}
}

func TestModuleConfig_SetLevels_CaseInsensitiveAndOffsets(t *testing.T) {
	t.Parallel()

	c := NewModuleConfig(slog.LevelInfo)
	if err := c.SetLevels("api=INFO,worker=warn+2"); err != nil {
		t.Fatalf("SetLevels error: %v", err)
	}
	if got := c.levelFor("api").Level(); got != slog.LevelInfo {
		t.Errorf("api: expected Info, got %v", got)
	}
	if got := c.levelFor("worker").Level(); got != slog.LevelWarn+2 {
		t.Errorf("worker: expected Warn+2, got %v", got)
	}
}

func TestModuleConfig_SetLevels_InvalidSegment_NothingApplied(t *testing.T) {
	t.Parallel()

	c := NewModuleConfig(slog.LevelInfo)

	// First pair is valid, second is malformed: the whole spec must be
	// rejected atomically.
	if err := c.SetLevels("database=debug,brokenpair"); err == nil {
		t.Fatal("expected error for malformed segment")
	}
	if got := c.levelFor("database").Level(); got != slog.LevelInfo {
		t.Errorf("valid pair before invalid one must not be applied, got %v", got)
	}

	if err := c.SetLevels("auth=loud"); err == nil {
		t.Fatal("expected error for unknown level name")
	}
	if err := c.SetLevels("=debug"); err == nil {
		t.Fatal("expected error for empty component")
	}
	if err := c.SetLevels("auth="); err == nil {
		t.Fatal("expected error for empty level")
	}
}

func TestModuleConfig_SetLevels_EmptySpec_NoOp(t *testing.T) {
	t.Parallel()

	c := NewModuleConfig(slog.LevelWarn)
	if err := c.SetLevels(""); err != nil {
		t.Fatalf("empty spec should be a no-op, got error: %v", err)
	}
	if got := c.levelFor("anything").Level(); got != slog.LevelWarn {
		t.Errorf("default level changed by empty spec: %v", got)
	}
}

func TestModuleConfig_SetLevels_AffectsLiveHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	c := NewModuleConfig(slog.LevelInfo)
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	log := slog.New(NewModuleHandler(inner, c)).With(slog.String("component", "database"))

	log.Debug("before") // filtered: database uses Info default
	if buf.Len() != 0 {
		t.Fatal("debug should be filtered before SetLevels")
	}

	if err := c.SetLevels("database=debug"); err != nil {
		t.Fatalf("SetLevels error: %v", err)
	}
	log.Debug("after") // now passes
	if buf.Len() == 0 {
		t.Error("debug should be logged after SetLevels hot-reload")
	}
}

// Regression: NewModuleHandler with a nil config must not panic; it falls
// back to a default config with LevelInfo.
func TestModuleHandler_NilConfig_DefaultsToInfo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, nil)
	log := slog.New(h)

	log.Debug("debug without config")
	if buf.Len() != 0 {
		t.Error("debug should be filtered by the default Info level")
	}

	log.Info("info without config")
	if buf.Len() == 0 {
		t.Error("info should be logged with the default Info level")
	}
}

// --- WithAttrs component resolution ---

func TestModuleHandler_WithAttrs_ComponentResolution(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError) // default: only Error
	config.SetLevel("api", slog.LevelDebug)    // api: all levels

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	// Create a child logger with component via WithAttrs.
	apiLog := slog.New(h.WithAttrs([]slog.Attr{slog.String("component", "api")}))

	apiLog.Debug("api debug")
	if buf.Len() == 0 {
		t.Error("api debug should be logged (component resolved from WithAttrs)")
	}
}

func TestModuleHandler_WithAttrs_ComponentFromWith(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError)
	config.SetLevel("networking", slog.LevelDebug)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	// Using slog.Logger.With (which calls WithAttrs internally).
	netLog := slog.New(h).With("component", "networking")

	netLog.Debug("net debug from With")
	if buf.Len() == 0 {
		t.Error("networking debug should be logged (component resolved from .With())")
	}
}

// --- Handle fallback resolution ---

func TestModuleHandler_HandleRecordAttrsOverridePreAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError)
	config.SetLevel("storage", slog.LevelError) // storage: only Error
	config.SetLevel("api", slog.LevelDebug)     // api: all levels

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	// Pre-set component to "storage" via WithAttrs.
	storageLog := slog.New(h.WithAttrs([]slog.Attr{slog.String("component", "storage")}))

	// But override at call site with "api" component.
	storageLog.Debug("overridden", "component", "api")
	if buf.Len() == 0 {
		t.Error("record attr 'api' should override pre-attr 'storage' and accept Debug")
	}
}

func TestModuleHandler_Handle_NoComponent_FallbackToPreAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError)
	config.SetLevel("networking", slog.LevelDebug)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	netLog := slog.New(h.WithAttrs([]slog.Attr{slog.String("component", "networking")}))

	// No component in the record attrs → falls back to pre-attrs.
	netLog.Debug("fallback resolution")
	if buf.Len() == 0 {
		t.Error("should resolve component from pre-attrs when not in record")
	}
}

// --- Runtime level update ---

func TestModuleHandler_RuntimeLevelUpdate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError)
	config.SetLevel("api", slog.LevelError) // initially: only Error

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)
	log := slog.New(h)

	// Info should be filtered.
	log.Info("filtered", "component", "api")
	if buf.Len() != 0 {
		t.Error("info should be filtered when api level is Error")
	}

	// Update level at runtime.
	config.SetLevel("api", slog.LevelInfo)

	// Now Info should be logged.
	log.Info("allowed", "component", "api")
	if buf.Len() == 0 {
		t.Error("info should be logged after api level changed to Info")
	}
}

func TestModuleHandler_RuntimeDefaultLevelUpdate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)
	log := slog.New(h)

	// Info without component should be filtered (default=Error).
	log.Info("filtered")
	if buf.Len() != 0 {
		t.Error("info should be filtered when default level is Error")
	}

	// Update default level at runtime.
	config.SetDefaultLevel(slog.LevelDebug)

	log.Info("allowed")
	if buf.Len() == 0 {
		t.Error("info should be logged after default level changed to Debug")
	}
}

// --- WithGroup cloning ---

func TestModuleHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelDebug)
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	grouped := h.WithGroup("request")
	log := slog.New(grouped)
	log.Info("grouped log", "method", "GET")

	result := parseJSON(t, buf.Bytes())
	req := result["request"].(map[string]any)
	assertEqual(t, req["method"], "GET")
}

func TestModuleHandler_WithGroup_Empty(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelInfo)
	h := NewModuleHandler(slog.NewJSONHandler(io.Discard, nil), config)

	child := h.WithGroup("")
	if child != h {
		t.Error("WithGroup('') should return the same handler")
	}
}

func TestModuleHandler_WithGroup_ComponentNotInGroupedAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError) // default: Error only
	config.SetLevel("api", slog.LevelDebug)    // api: all levels

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	// Set component, then add a group.
	apiLog := slog.New(
		h.WithAttrs([]slog.Attr{slog.String("component", "api")}).
			WithGroup("request"),
	)

	// Component was set before the group — should still be resolvable.
	apiLog.Debug("debug in group", "method", "POST")
	if buf.Len() == 0 {
		t.Error("component from pre-group WithAttrs should still be resolved")
	}
}

// --- Multiple components ---

func TestModuleHandler_MultipleComponents(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelInfo)
	config.SetLevel("consensus", slog.LevelWarn)
	config.SetLevel("networking", slog.LevelDebug)
	config.SetLevel("storage", slog.LevelError)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	tests := []struct {
		component string
		level     slog.Level
		expected  bool
	}{
		{"consensus", slog.LevelInfo, false},  // Warn threshold
		{"consensus", slog.LevelWarn, true},   // At threshold
		{"networking", slog.LevelDebug, true}, // Debug threshold
		{"storage", slog.LevelWarn, false},    // Error threshold
		{"storage", slog.LevelError, true},    // At threshold
		{"", slog.LevelInfo, true},            // Default threshold (Info)
		{"", slog.LevelDebug, false},          // Below default
	}

	for _, tt := range tests {
		buf.Reset()
		log := slog.New(h)
		if tt.component != "" {
			log.Log(context.TODO(), tt.level, "test", "component", tt.component)
		} else {
			log.Log(context.TODO(), tt.level, "test")
		}
		got := buf.Len() > 0
		if got != tt.expected {
			t.Errorf("component=%q level=%v: expected logged=%v, got logged=%v",
				tt.component, tt.level, tt.expected, got)
		}
	}
}

// --- Concurrency ---

func TestModuleHandler_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{buf: &buf, mu: &mu}

	config := NewModuleConfig(slog.LevelDebug)
	inner := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)
	log := slog.New(h)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				log.Info("concurrent", "component", "test", "goroutine", n)
			}
		}(i)
	}
	wg.Wait()

	mu.Lock()
	lines := countJSONLines(buf.Bytes())
	mu.Unlock()

	expected := goroutines * iterations
	if lines != expected {
		t.Errorf("expected %d lines, got %d", expected, lines)
	}
}

func TestModuleHandler_ConcurrentSetLevel(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelInfo)
	inner := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)
	log := slog.New(h)

	var wg sync.WaitGroup

	// Writer goroutines.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				log.Info("test", "component", "api")
			}
		}()
	}

	// Config updater goroutines.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			levels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
			for j := 0; j < 50; j++ {
				config.SetLevel("api", levels[j%len(levels)])
			}
		}(i)
	}

	wg.Wait()
	// No panic or race = success.
}

// --- ModuleConfig unit tests ---

func TestModuleConfig_NewModuleConfig(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelWarn)
	lv := config.levelFor("")
	if lv.Level() != slog.LevelWarn {
		t.Errorf("default level should be Warn, got %v", lv.Level())
	}
}

func TestModuleConfig_SetLevel_New(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelInfo)
	config.SetLevel("api", slog.LevelDebug)

	lv := config.levelFor("api")
	if lv.Level() != slog.LevelDebug {
		t.Errorf("api level should be Debug, got %v", lv.Level())
	}
}

func TestModuleConfig_SetLevel_Update(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelInfo)
	config.SetLevel("api", slog.LevelDebug)
	config.SetLevel("api", slog.LevelError) // update

	lv := config.levelFor("api")
	if lv.Level() != slog.LevelError {
		t.Errorf("api level should be updated to Error, got %v", lv.Level())
	}
}

func TestModuleConfig_LevelFor_Unknown(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelWarn)
	lv := config.levelFor("unknown")
	if lv.Level() != slog.LevelWarn {
		t.Errorf("unknown component should use default Warn, got %v", lv.Level())
	}
}

func TestModuleConfig_SetDefaultLevel(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelInfo)
	if config.defaultLevel.Level() != slog.LevelInfo {
		t.Errorf("initial default should be Info, got %v", config.defaultLevel.Level())
	}

	config.SetDefaultLevel(slog.LevelDebug)
	if config.defaultLevel.Level() != slog.LevelDebug {
		t.Errorf("default should be updated to Debug, got %v", config.defaultLevel.Level())
	}
}

// --- Fix 1 regression: cached component + minLevel ---

func TestModuleHandler_CachedComponent_InnerDefaultInfo(t *testing.T) {
	t.Parallel()

	// Inner handler default level is Info. Module "database" is set to Debug.
	// Logger created with .With(Component("database")).
	// .Debug(...) MUST be written — the cached component should allow Enabled() to pass.
	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelInfo) // default Info
	config.SetLevel("database", slog.LevelDebug)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := NewModuleHandler(inner, config)

	dbLog := slog.New(h).With("component", "database")
	dbLog.Debug("query executed")

	if buf.Len() == 0 {
		t.Error("database Debug log should be written when module level=Debug, even if inner default=Info")
	}
}

func TestModuleHandler_CachedComponent_AuthWarnFiltering(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelInfo)
	config.SetLevel("auth", slog.LevelWarn)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	authLog := slog.New(h).With("component", "auth")

	// Info must be dropped (auth = Warn).
	authLog.Info("user logged in")
	if buf.Len() != 0 {
		t.Error("auth Info log should be filtered when module level=Warn")
	}

	// Runtime update: change auth to Info.
	config.SetLevel("auth", slog.LevelInfo)
	authLog.Info("user logged in after update")
	if buf.Len() == 0 {
		t.Error("auth Info log should be written after runtime level update to Info")
	}
}

func TestModuleHandler_LogCallSiteComponent_MinLevelFallback(t *testing.T) {
	t.Parallel()

	// Component provided only as a log-call attr (not via .With).
	// Module "database" is Debug but inner default is Info.
	// The minLevel() fallback should allow Debug to reach Handle().
	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelInfo)
	config.SetLevel("database", slog.LevelDebug)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	log := slog.New(h)
	log.Debug("query executed", "component", "database")

	if buf.Len() == 0 {
		t.Error("log-call-site component 'database' Debug should reach Handle() via minLevel fallback")
	}
}

func TestModuleHandler_CachedComponent_PreservedAcrossWithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError) // default Error
	config.SetLevel("api", slog.LevelDebug)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	// Set component, then add a group. cachedComponent must be preserved.
	apiLog := slog.New(h).With("component", "api")
	groupedLog := apiLog.WithGroup("request")

	groupedLog.Debug("debug in group", "method", "POST")
	if buf.Len() == 0 {
		t.Error("cachedComponent should be preserved after WithGroup, allowing Debug for api")
	}
}

func TestModuleConfig_MinLevel(t *testing.T) {
	t.Parallel()

	config := NewModuleConfig(slog.LevelInfo)
	config.SetLevel("database", slog.LevelDebug)
	config.SetLevel("auth", slog.LevelWarn)

	min := config.minLevel()
	if min != slog.LevelDebug {
		t.Errorf("minLevel should be Debug (lowest), got %v", min)
	}
}

func TestModuleHandler_LastPreAttrWins(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	config := NewModuleConfig(slog.LevelError)
	config.SetLevel("storage", slog.LevelError)
	config.SetLevel("api", slog.LevelDebug)

	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewModuleHandler(inner, config)

	// Multiple With() calls.
	log := slog.New(h).
		With("component", "storage").
		With("component", "api")

	log.Debug("overridden")
	if buf.Len() == 0 {
		t.Error("later With('component', 'api') should override earlier 'storage'")
	}
}
