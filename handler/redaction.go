package handler

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"
)

// RedactionHandler inspects and redacts sensitive attributes from log records
// before passing them to the inner handler.
//
// It supports three complementary redaction strategies:
//
//  1. Key-based: exact key names or dotted group paths (e.g., "password", "auth.token")
//  2. Pattern-based: regular expression matching against full key paths
//  3. Function-based: custom logic for context-dependent redaction
//
// RedactionHandler correctly handles nested [slog.Group] attributes by
// recursively inspecting group values and tracking the current group path.
//
// # Limitations
//
// RedactionHandler inspects attribute keys only. Sensitive data can still
// leak through paths it does not see:
//
//   - The record message is never inspected. Secrets interpolated into the
//     message (e.g., fmt.Sprintf) are logged as-is.
//   - Values inside [slog.Any] attributes (maps, structs, slices) are not
//     recursed into — only [slog.Group] attributes are. An attribute like
//     slog.Any("creds", map[string]string{"password": "x"}) passes through
//     unredacted unless its top-level key matches.
//
// For values like these, prefer self-redacting types such as Redacted and
// SensitiveBytes from the root go-logger package, which hide their contents
// regardless of handler configuration.
//
// RedactionHandler implements [slog.Handler] and follows the immutable clone
// pattern for [slog.Handler.WithAttrs] and [slog.Handler.WithGroup].
type RedactionHandler struct {
	inner      slog.Handler
	keys       map[string]bool  // exact key or dotted-path matches
	patterns   []*regexp.Regexp // compiled regex patterns
	redactFunc func(groups []string, key string, value slog.Value) slog.Value
	groups     []string // current group path from WithGroup calls
	noop       bool     // true when no keys, patterns, or redactFunc are configured
}

// NewRedactionHandler creates a [RedactionHandler] that wraps the given inner
// handler with the specified redaction configuration.
//
// Panics if any pattern in [WithRedactPatterns] is not a valid regular expression.
func NewRedactionHandler(inner slog.Handler, opts ...RedactOption) *RedactionHandler {
	o := applyRedactOptions(opts)

	keys := make(map[string]bool, len(o.keys))
	for _, k := range o.keys {
		keys[k] = true
	}

	patterns := make([]*regexp.Regexp, len(o.patterns))
	for i, p := range o.patterns {
		patterns[i] = regexp.MustCompile(p)
	}

	return &RedactionHandler{
		inner:      inner,
		keys:       keys,
		patterns:   patterns,
		redactFunc: o.redactFunc,
		noop:       len(keys) == 0 && len(patterns) == 0 && o.redactFunc == nil,
	}
}

// Enabled reports whether the inner handler is enabled for the given level.
func (h *RedactionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Unwrap returns the inner handler, enabling lifecycle traversal.
func (h *RedactionHandler) Unwrap() slog.Handler { return h.inner }

// Handle creates a new [slog.Record] with redacted attributes and passes it
// to the inner handler.
//
// All attributes on the record are inspected and potentially redacted.
// Group attributes are recursively inspected with proper path tracking.
//
// When no keys, patterns, or redact function are configured, the record is
// forwarded unchanged.
func (h *RedactionHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.noop {
		return h.inner.Handle(ctx, r)
	}

	// Build a new record with redacted attributes.
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newRecord.AddAttrs(h.redactAttr(h.groups, a))
		return true
	})
	return h.inner.Handle(ctx, newRecord)
}

// WithAttrs returns a new [RedactionHandler] where the inner handler has been
// cloned with the given attributes (after redacting them).
//
// The original handler is not modified.
func (h *RedactionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Redact the pre-applied attributes before passing to inner.
	redacted := attrs
	if !h.noop {
		redacted = make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			redacted[i] = h.redactAttr(h.groups, a)
		}
	}
	return &RedactionHandler{
		inner:      h.inner.WithAttrs(redacted),
		keys:       h.keys,
		patterns:   h.patterns,
		redactFunc: h.redactFunc,
		groups:     h.groups,
		noop:       h.noop,
	}
}

// WithGroup returns a new [RedactionHandler] with the given group name
// appended to the current group path. The inner handler is also cloned
// with the group.
//
// The original handler is not modified.
func (h *RedactionHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &RedactionHandler{
		inner:      h.inner.WithGroup(name),
		keys:       h.keys,
		patterns:   h.patterns,
		redactFunc: h.redactFunc,
		groups:     append(slices.Clone(h.groups), name),
		noop:       h.noop,
	}
}

// redactAttr inspects a single attribute and redacts it if necessary.
// For group attributes, it recurses into the group's children with an
// updated group path.
func (h *RedactionHandler) redactAttr(groups []string, a slog.Attr) slog.Attr {
	// Resolve LogValuer first — this ensures types like logger.Redacted
	// are resolved before we inspect them.
	a.Value = a.Value.Resolve()

	// Recurse into groups.
	if a.Value.Kind() == slog.KindGroup {
		childGroups := groups
		if a.Key != "" {
			childGroups = append(slices.Clone(groups), a.Key)
		}
		groupAttrs := a.Value.Group()
		redacted := make([]any, len(groupAttrs))
		for i, ga := range groupAttrs {
			redacted[i] = h.redactAttr(childGroups, ga)
		}
		return slog.Group(a.Key, redacted...)
	}

	// Check if this key should be redacted.
	if h.shouldRedact(groups, a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}

	// Apply custom redaction function.
	if h.redactFunc != nil {
		newVal := h.redactFunc(groups, a.Key, a.Value)
		return slog.Attr{Key: a.Key, Value: newVal}
	}

	return a
}

// shouldRedact checks whether the given key (at the given group path) should
// be redacted based on exact keys or regex patterns.
func (h *RedactionHandler) shouldRedact(groups []string, key string) bool {
	// Check simple key match (no group prefix).
	if h.keys[key] {
		return true
	}

	// Build the full dotted path for group-aware matching.
	var fullKey string
	if len(groups) > 0 {
		fullKey = strings.Join(groups, ".") + "." + key
	} else {
		fullKey = key
	}

	// Check dotted path match.
	if len(groups) > 0 && h.keys[fullKey] {
		return true
	}

	// Check regex patterns against the full key path.
	for _, pat := range h.patterns {
		if pat.MatchString(fullKey) {
			return true
		}
	}

	return false
}
