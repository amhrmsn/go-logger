package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNewContext_FromContext_Roundtrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	ctx := NewContext(context.Background(), log)
	got := FromContext(ctx)

	if got != log {
		t.Fatal("FromContext did not return the logger stored by NewContext")
	}

	got.Info("via context")
	if !strings.Contains(buf.String(), "via context") {
		t.Error("logger retrieved from context did not write to the expected handler")
	}
}

func TestFromContext_EmptyContext_ReturnsDefault(t *testing.T) {
	t.Parallel()

	if got := FromContext(context.Background()); got != slog.Default() {
		t.Error("FromContext without a stored logger should return slog.Default()")
	}
}

func TestFromContext_NilContext_ReturnsDefault(t *testing.T) {
	t.Parallel()

	//lint:ignore SA1012 deliberately passing a nil context to exercise the guard
	if got := FromContext(nil); got != slog.Default() {
		t.Error("FromContext(nil) should return slog.Default(), not panic")
	}
}

func TestNewContext_ChildOverridesParent(t *testing.T) {
	t.Parallel()

	parentLog := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	childLog := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	parent := NewContext(context.Background(), parentLog)
	child := NewContext(parent, childLog)

	if FromContext(parent) != parentLog {
		t.Error("parent context lost its logger")
	}
	if FromContext(child) != childLog {
		t.Error("child context should return the overriding logger")
	}
}
