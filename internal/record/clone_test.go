package record

import (
	"log/slog"
	"testing"
	"time"
)

func TestCloneRecord_Basic(t *testing.T) {
	t.Parallel()

	now := time.Now()
	original := slog.NewRecord(now, slog.LevelInfo, "test message", 0)
	original.AddAttrs(
		slog.String("key1", "value1"),
		slog.Int("key2", 42),
		slog.Bool("key3", true),
	)

	clone := CloneRecord(original)

	// Verify metadata is preserved
	if clone.Time != now {
		t.Errorf("Time: expected %v, got %v", now, clone.Time)
	}
	if clone.Level != slog.LevelInfo {
		t.Errorf("Level: expected %v, got %v", slog.LevelInfo, clone.Level)
	}
	if clone.Message != "test message" {
		t.Errorf("Message: expected 'test message', got %q", clone.Message)
	}

	// Verify attrs are preserved
	cloneAttrs := collectAttrs(clone)
	if len(cloneAttrs) != 3 {
		t.Fatalf("expected 3 attrs, got %d", len(cloneAttrs))
	}

	assertAttr(t, cloneAttrs, "key1", "value1")
	assertAttrInt(t, cloneAttrs, "key2", 42)
	assertAttrBool(t, cloneAttrs, "key3", true)
}

func TestCloneRecord_PreservesPC(t *testing.T) {
	t.Parallel()

	// Use a non-zero PC to verify it's cloned
	var pc uintptr = 12345
	original := slog.NewRecord(time.Now(), slog.LevelWarn, "pc test", pc)

	clone := CloneRecord(original)

	if clone.PC != pc {
		t.Errorf("PC: expected %d, got %d", pc, clone.PC)
	}
}

func TestCloneRecord_EmptyAttrs(t *testing.T) {
	t.Parallel()

	original := slog.NewRecord(time.Now(), slog.LevelDebug, "no attrs", 0)

	clone := CloneRecord(original)

	if clone.Message != "no attrs" {
		t.Errorf("Message: expected 'no attrs', got %q", clone.Message)
	}

	attrs := collectAttrs(clone)
	if len(attrs) != 0 {
		t.Errorf("expected 0 attrs, got %d", len(attrs))
	}
}

func TestCloneRecord_ManyAttrs(t *testing.T) {
	t.Parallel()

	original := slog.NewRecord(time.Now(), slog.LevelInfo, "many attrs", 0)
	for i := 0; i < 20; i++ {
		original.AddAttrs(slog.Int("field", i))
	}

	clone := CloneRecord(original)

	cloneAttrs := collectAttrs(clone)
	origAttrs := collectAttrs(original)

	if len(cloneAttrs) != len(origAttrs) {
		t.Fatalf("attr count mismatch: original=%d, clone=%d", len(origAttrs), len(cloneAttrs))
	}

	// Verify all values match
	for i, ca := range cloneAttrs {
		oa := origAttrs[i]
		if ca.Key != oa.Key {
			t.Errorf("attr[%d] key: expected %q, got %q", i, oa.Key, ca.Key)
		}
		if ca.Value.Int64() != oa.Value.Int64() {
			t.Errorf("attr[%d] value: expected %d, got %d", i, oa.Value.Int64(), ca.Value.Int64())
		}
	}
}

func TestCloneRecord_WithGroup(t *testing.T) {
	t.Parallel()

	original := slog.NewRecord(time.Now(), slog.LevelInfo, "group test", 0)
	original.AddAttrs(
		slog.Group("request",
			slog.String("method", "GET"),
			slog.Int("status", 200),
		),
		slog.String("outside", "value"),
	)

	clone := CloneRecord(original)

	cloneAttrs := collectAttrs(clone)
	if len(cloneAttrs) != 2 {
		t.Fatalf("expected 2 top-level attrs, got %d", len(cloneAttrs))
	}

	// Verify group attr
	groupAttr := cloneAttrs[0]
	if groupAttr.Key != "request" {
		t.Errorf("expected group key 'request', got %q", groupAttr.Key)
	}
	if groupAttr.Value.Kind() != slog.KindGroup {
		t.Fatalf("expected KindGroup, got %v", groupAttr.Value.Kind())
	}

	groupItems := groupAttr.Value.Group()
	if len(groupItems) != 2 {
		t.Fatalf("expected 2 items in group, got %d", len(groupItems))
	}
	if groupItems[0].Key != "method" || groupItems[0].Value.String() != "GET" {
		t.Errorf("group[0]: expected method=GET, got %s=%s", groupItems[0].Key, groupItems[0].Value.String())
	}
	if groupItems[1].Key != "status" || groupItems[1].Value.Int64() != 200 {
		t.Errorf("group[1]: expected status=200, got %s=%d", groupItems[1].Key, groupItems[1].Value.Int64())
	}

	// Verify non-group attr
	assertAttr(t, cloneAttrs, "outside", "value")
}

func TestCloneRecord_NestedGroups(t *testing.T) {
	t.Parallel()

	original := slog.NewRecord(time.Now(), slog.LevelInfo, "nested", 0)
	original.AddAttrs(
		slog.Group("outer",
			slog.Group("inner",
				slog.String("deep", "value"),
			),
		),
	)

	clone := CloneRecord(original)

	cloneAttrs := collectAttrs(clone)
	if len(cloneAttrs) != 1 {
		t.Fatalf("expected 1 attr, got %d", len(cloneAttrs))
	}

	outer := cloneAttrs[0]
	if outer.Key != "outer" {
		t.Fatalf("expected 'outer', got %q", outer.Key)
	}
	outerGroup := outer.Value.Group()
	if len(outerGroup) != 1 {
		t.Fatalf("expected 1 item in outer group, got %d", len(outerGroup))
	}

	inner := outerGroup[0]
	if inner.Key != "inner" {
		t.Fatalf("expected 'inner', got %q", inner.Key)
	}
	innerGroup := inner.Value.Group()
	if len(innerGroup) != 1 {
		t.Fatalf("expected 1 item in inner group, got %d", len(innerGroup))
	}

	if innerGroup[0].Key != "deep" || innerGroup[0].Value.String() != "value" {
		t.Errorf("expected deep=value, got %s=%s", innerGroup[0].Key, innerGroup[0].Value.String())
	}
}

func TestCloneRecord_AllValueTypes(t *testing.T) {
	t.Parallel()

	dur := 5 * time.Second
	now := time.Now()

	original := slog.NewRecord(now, slog.LevelInfo, "types", 0)
	original.AddAttrs(
		slog.String("s", "hello"),
		slog.Int64("i64", -99),
		slog.Uint64("u64", 999),
		slog.Float64("f64", 3.14),
		slog.Bool("b", false),
		slog.Time("t", now),
		slog.Duration("d", dur),
		slog.Any("any", []int{1, 2, 3}),
	)

	clone := CloneRecord(original)

	cloneAttrs := collectAttrs(clone)
	if len(cloneAttrs) != 8 {
		t.Fatalf("expected 8 attrs, got %d", len(cloneAttrs))
	}

	assertAttr(t, cloneAttrs, "s", "hello")
	assertAttrInt(t, cloneAttrs, "i64", -99)
	assertAttrBool(t, cloneAttrs, "b", false)
}

func TestCloneRecord_IndependentFromOriginal(t *testing.T) {
	t.Parallel()

	original := slog.NewRecord(time.Now(), slog.LevelInfo, "independent", 0)
	original.AddAttrs(slog.String("key", "original_value"))

	clone := CloneRecord(original)

	// Modify original by adding more attrs
	original.AddAttrs(slog.String("extra", "added_after_clone"))

	origAttrs := collectAttrs(original)
	cloneAttrs := collectAttrs(clone)

	if len(origAttrs) == len(cloneAttrs) {
		t.Error("clone should be independent: adding attrs to original should not affect clone")
	}

	// Clone should have exactly 1 attr
	if len(cloneAttrs) != 1 {
		t.Errorf("expected 1 attr in clone, got %d", len(cloneAttrs))
	}
}

func TestCloneRecord_AllLevels(t *testing.T) {
	t.Parallel()

	levels := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
		slog.Level(12), // custom level
		slog.Level(-8), // custom level below debug
	}

	for _, level := range levels {
		original := slog.NewRecord(time.Now(), level, "level test", 0)
		clone := CloneRecord(original)
		if clone.Level != level {
			t.Errorf("Level %v: expected %v, got %v", level, level, clone.Level)
		}
	}
}

// --- helpers ---

func collectAttrs(r slog.Record) []slog.Attr {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}

func assertAttr(t *testing.T, attrs []slog.Attr, key, expected string) {
	t.Helper()
	for _, a := range attrs {
		if a.Key == key {
			if a.Value.String() != expected {
				t.Errorf("attr %q: expected %q, got %q", key, expected, a.Value.String())
			}
			return
		}
	}
	t.Errorf("attr %q not found", key)
}

func assertAttrInt(t *testing.T, attrs []slog.Attr, key string, expected int64) {
	t.Helper()
	for _, a := range attrs {
		if a.Key == key {
			if a.Value.Int64() != expected {
				t.Errorf("attr %q: expected %d, got %d", key, expected, a.Value.Int64())
			}
			return
		}
	}
	t.Errorf("attr %q not found", key)
}

func assertAttrBool(t *testing.T, attrs []slog.Attr, key string, expected bool) {
	t.Helper()
	for _, a := range attrs {
		if a.Key == key {
			if a.Value.Bool() != expected {
				t.Errorf("attr %q: expected %v, got %v", key, expected, a.Value.Bool())
			}
			return
		}
	}
	t.Errorf("attr %q not found", key)
}
