package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func dedupHandle(t *testing.T, h *DedupHandler, buf *bytes.Buffer, msg string, at time.Time) bool {
	t.Helper()
	before := buf.Len()
	r := slog.NewRecord(at, slog.LevelInfo, msg, 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	return buf.Len() > before
}

// Note: DedupHandler deliberately has no slogtest compliance test. slogtest
// emits every case with the same record message, which is exactly what the
// handler suppresses — dropping records by design violates slogtest's
// "all records appear" assumption, the same way it would for SamplingHandler
// with a rate below 1.0.

func TestDedupHandler_SuppressesRepeatsWithinWindow(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewDedupHandler(slog.NewJSONHandler(&buf, nil), WithDedupWindow(time.Second))

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	if !dedupHandle(t, h, &buf, "db connection refused", base) {
		t.Fatal("first occurrence must pass")
	}
	for i := 1; i <= 10; i++ {
		if dedupHandle(t, h, &buf, "db connection refused", base.Add(time.Duration(i)*time.Millisecond)) {
			t.Fatalf("repeat %d within window must be suppressed", i)
		}
	}

	stats := h.Stats()
	if stats.Passed != 1 || stats.Suppressed != 10 {
		t.Errorf("expected 1 passed / 10 suppressed, got %+v", stats)
	}
}

func TestDedupHandler_NewWindowCarriesSuppressedCount(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewDedupHandler(slog.NewJSONHandler(&buf, nil), WithDedupWindow(time.Second))

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	dedupHandle(t, h, &buf, "flood", base) // passes
	for i := 0; i < 7; i++ {
		dedupHandle(t, h, &buf, "flood", base.Add(10*time.Millisecond)) // suppressed
	}

	buf.Reset()
	if !dedupHandle(t, h, &buf, "flood", base.Add(2*time.Second)) {
		t.Fatal("first record of a new window must pass")
	}

	var rec map[string]any
	line := strings.TrimSpace(buf.String())
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got, ok := rec[DedupSuppressedKey].(float64); !ok || got != 7 {
		t.Errorf("expected %s=7 on the new-window record, got %v", DedupSuppressedKey, rec[DedupSuppressedKey])
	}
}

func TestDedupHandler_NoSuppressionNoAttr(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewDedupHandler(slog.NewJSONHandler(&buf, nil))

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	dedupHandle(t, h, &buf, "once", base)

	if strings.Contains(buf.String(), DedupSuppressedKey) {
		t.Errorf("record without prior suppression must not carry %s", DedupSuppressedKey)
	}
}

func TestDedupHandler_DifferentMessagesIndependent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewDedupHandler(slog.NewJSONHandler(&buf, nil))

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	if !dedupHandle(t, h, &buf, "alpha failure", base) {
		t.Fatal("first alpha must pass")
	}
	// Assuming no bucket collision between these fixed strings.
	if !dedupHandle(t, h, &buf, "beta failure", base) {
		t.Error("first beta must pass independently of alpha")
	}
}

func TestDedupHandler_BypassLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewDedupHandler(
		slog.NewJSONHandler(&buf, nil),
		WithDedupBypassLevel(slog.LevelError),
	)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		before := buf.Len()
		r := slog.NewRecord(base, slog.LevelError, "critical repeat", 0)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatalf("Handle error: %v", err)
		}
		if buf.Len() == before {
			t.Fatalf("error record %d must bypass dedup", i)
		}
	}
}

func TestDedupHandler_SharedAcrossClones(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewDedupHandler(slog.NewJSONHandler(&buf, nil))
	clone := h.WithAttrs([]slog.Attr{slog.String("k", "v")}).(*DedupHandler)

	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	if !dedupHandle(t, h, &buf, "shared", base) {
		t.Fatal("first record must pass")
	}
	if dedupHandle(t, clone, &buf, "shared", base) {
		t.Error("clone must share dedup state with the original")
	}
}

func TestDedupHandler_ConcurrentNoRace(t *testing.T) {
	t.Parallel()

	h := NewDedupHandler(slog.NewJSONHandler(io.Discard, nil), WithDedupWindow(10*time.Millisecond))
	log := slog.New(h)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				log.Info("concurrent dedup flood")
			}
		}(g)
	}
	wg.Wait()

	stats := h.Stats()
	if stats.Passed+stats.Suppressed != 16*500 {
		t.Errorf("passed+suppressed must equal total records: %+v", stats)
	}
}
