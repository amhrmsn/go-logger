package handler

import (
	"encoding/json"
	"expvar"
	"io"
	"log/slog"
	"testing"
)

func TestPublishAsyncStats(t *testing.T) {
	// Not parallel: expvar registry is process-global.
	h := NewAsyncHandler(slog.NewJSONHandler(io.Discard, nil))
	defer func() { _ = h.Close() }()

	PublishAsyncStats("test.expvar.async", h)

	log := slog.New(h)
	log.Error("bypass write") // synchronous → counted immediately

	v := expvar.Get("test.expvar.async")
	if v == nil {
		t.Fatal("expvar variable not registered")
	}
	var stats AsyncStats
	if err := json.Unmarshal([]byte(v.String()), &stats); err != nil {
		t.Fatalf("expvar output is not valid JSON: %v", err)
	}
	if stats.Written != 1 {
		t.Errorf("expected Written=1 via expvar snapshot, got %+v", stats)
	}
}

func TestPublishSampleAndDedupStats(t *testing.T) {
	// Not parallel: expvar registry is process-global.
	sh := NewSamplingHandler(slog.NewJSONHandler(io.Discard, nil))
	dh := NewDedupHandler(slog.NewJSONHandler(io.Discard, nil))

	PublishSampleStats("test.expvar.sample", sh)
	PublishDedupStats("test.expvar.dedup", dh)

	slog.New(sh).Info("one")
	slog.New(dh).Info("two")

	for _, name := range []string{"test.expvar.sample", "test.expvar.dedup"} {
		v := expvar.Get(name)
		if v == nil {
			t.Fatalf("expvar %q not registered", name)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(v.String()), &m); err != nil {
			t.Fatalf("expvar %q output not valid JSON: %v", name, err)
		}
		if m["Passed"] != float64(1) {
			t.Errorf("expvar %q: expected Passed=1, got %v", name, m["Passed"])
		}
	}
}
