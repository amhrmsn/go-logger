package record

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

// TestCloneRecord_ConcurrentSafety verifies that cloning a record from
// multiple goroutines simultaneously does not cause data races.
func TestCloneRecord_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	original := slog.NewRecord(time.Now(), slog.LevelInfo, "concurrent clone", 0)
	original.AddAttrs(
		slog.String("key1", "value1"),
		slog.Int("key2", 42),
		slog.Bool("key3", true),
		slog.Group("nested",
			slog.String("inner", "data"),
		),
	)

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clone := CloneRecord(original)

			// Verify the clone has the correct number of attrs.
			attrs := collectAttrs(clone)
			if len(attrs) != 4 {
				t.Errorf("expected 4 attrs, got %d", len(attrs))
			}
		}()
	}

	wg.Wait()
}

// TestCloneRecord_ConcurrentModify verifies that modifying a clone does not
// affect other clones or the original.
func TestCloneRecord_ConcurrentModify(t *testing.T) {
	t.Parallel()

	original := slog.NewRecord(time.Now(), slog.LevelInfo, "modify test", 0)
	original.AddAttrs(slog.String("shared", "value"))

	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			clone := CloneRecord(original)
			// Each goroutine adds different attrs to its clone.
			clone.AddAttrs(slog.Int("unique", n))

			attrs := collectAttrs(clone)
			if len(attrs) != 2 {
				t.Errorf("goroutine %d: expected 2 attrs, got %d", n, len(attrs))
			}
		}(i)
	}

	wg.Wait()

	// Original should be unmodified.
	origAttrs := collectAttrs(original)
	if len(origAttrs) != 1 {
		t.Errorf("original should still have 1 attr, got %d", len(origAttrs))
	}
}
