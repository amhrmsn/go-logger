package handler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// FuzzModuleConfigSetLevels ensures the level-spec parser never panics and
// never half-applies an invalid spec.
func FuzzModuleConfigSetLevels(f *testing.F) {
	f.Add("database=debug,auth=warn,*=info")
	f.Add("=,==,a=b=c")
	f.Add("  spaced = WARN+2 ,")
	f.Add("*=error")
	f.Add("component=")
	f.Add(",,,")

	f.Fuzz(func(t *testing.T, spec string) {
		c := NewModuleConfig(slog.LevelInfo)
		if err := c.SetLevels(spec); err != nil {
			// Invalid spec must leave the config untouched.
			if c.DefaultLevel() != slog.LevelInfo {
				t.Errorf("invalid spec %q changed the default level", spec)
			}
			if len(c.Levels()) != 0 {
				t.Errorf("invalid spec %q half-applied component levels", spec)
			}
		}
	})
}

// FuzzRedactionHandlerHandle ensures redaction never panics on arbitrary
// keys, values, and group names — including dotted paths, empty strings,
// and non-UTF-8 byte sequences.
func FuzzRedactionHandlerHandle(f *testing.F) {
	f.Add("password", "secret", "auth")
	f.Add("", "", "")
	f.Add("auth.token", "value", "deep.nested.group")
	f.Add("__proto__", "\x00\xff", "*")

	inner := slog.NewJSONHandler(io.Discard, nil)
	h := NewRedactionHandler(inner,
		WithRedactKeys("password", "auth.token"),
		WithRedactPatterns(`(?i)secret`),
	)

	f.Fuzz(func(t *testing.T, key, value, group string) {
		r := slog.NewRecord(time.Now(), slog.LevelInfo, "fuzz", 0)
		r.AddAttrs(
			slog.String(key, value),
			slog.Group(group, slog.String(key, value)),
		)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Errorf("Handle returned error: %v", err)
		}
	})
}
