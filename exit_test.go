package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/amhrmsn/go-logger/handler"
)

// interceptExit replaces osExit for the duration of a test and returns a
// pointer to the captured exit code (-1 if never called).
func interceptExit(t *testing.T) *int {
	t.Helper()
	code := -1
	orig := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = orig })
	return &code
}

// syncBuffer is a mutex-guarded bytes.Buffer, safe for the async worker and
// the test goroutine to share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var lines []string
	for _, l := range strings.Split(b.buf.String(), "\n") {
		if len(l) > 0 {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestExit_FlushesAsyncBufferBeforeTerminating(t *testing.T) {
	code := interceptExit(t)

	var buf syncBuffer
	// Bypass level raised so every record goes through the async buffer.
	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(&buf, nil),
		handler.WithAsyncBypassLevel(slog.Level(100)),
	)
	log := slog.New(h)

	const records = 50
	for i := 0; i < records; i++ {
		log.Info("buffered", "i", i)
	}

	Exit(h, 3)

	if *code != 3 {
		t.Errorf("expected exit code 3, got %d", *code)
	}
	if got := len(buf.Lines()); got != records {
		t.Errorf("expected all %d buffered records flushed before exit, got %d", records, got)
	}
}

func TestExit_ClosesHandlerChain(t *testing.T) {
	_ = interceptExit(t)

	var buf syncBuffer
	async := handler.NewAsyncHandler(slog.NewJSONHandler(&buf, nil))
	redact := handler.NewRedactionHandler(async, handler.WithRedactKeys("password"))

	Exit(redact, 0)

	// The async handler wrapped by middleware must have been closed.
	err := async.Handle(context.Background(), slog.Record{})
	if err != handler.ErrHandlerClosed {
		t.Errorf("expected wrapped AsyncHandler to be closed by Exit, Handle returned %v", err)
	}
}

func TestFatal_LogsMessageAndExitsWithCode1(t *testing.T) {
	code := interceptExit(t)

	var buf syncBuffer
	h := handler.NewAsyncHandler(
		slog.NewJSONHandler(&buf, nil),
		handler.WithAsyncBypassLevel(slog.Level(100)),
	)
	log := slog.New(h)

	Fatal(log, "boot failed", "component", "db")

	if *code != 1 {
		t.Errorf("expected exit code 1, got %d", *code)
	}

	lines := buf.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 record, got %d", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if rec["level"] != "ERROR" {
		t.Errorf("expected ERROR level, got %v", rec["level"])
	}
	if rec["msg"] != "boot failed" {
		t.Errorf("expected msg %q, got %v", "boot failed", rec["msg"])
	}
	if rec["component"] != "db" {
		t.Errorf("expected component attr, got %v", rec["component"])
	}
}

func TestExit_PlainHandlerNoLifecycle(t *testing.T) {
	code := interceptExit(t)

	// A handler with no Flusher/Closer anywhere in the chain must still exit.
	Exit(slog.NewJSONHandler(&bytes.Buffer{}, nil), 7)

	if *code != 7 {
		t.Errorf("expected exit code 7, got %d", *code)
	}
}
