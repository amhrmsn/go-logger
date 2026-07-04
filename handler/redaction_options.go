package handler

import "log/slog"

// RedactOption configures a [RedactionHandler].
type RedactOption func(*redactOptions)

// redactOptions holds the collected configuration for [RedactionHandler].
type redactOptions struct {
	keys       []string
	patterns   []string
	redactFunc func(groups []string, key string, value slog.Value) slog.Value
}

func applyRedactOptions(opts []RedactOption) *redactOptions {
	o := &redactOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithRedactKeys specifies attribute keys whose values should be replaced
// with "[REDACTED]".
//
// Keys can be simple names ("password", "token") or dotted paths for nested
// groups ("auth.token", "db.password", "request.headers.authorization").
//
// Matching is case-sensitive and exact.
func WithRedactKeys(keys ...string) RedactOption {
	return func(o *redactOptions) {
		o.keys = append(o.keys, keys...)
	}
}

// WithRedactPatterns specifies regular expression patterns for key matching.
//
// Any attribute whose full key path (e.g., "auth.token") matches any pattern
// will have its value replaced with "[REDACTED]".
//
// Matching uses substring semantics ([regexp.Regexp.MatchString]): the pattern
// "token" also redacts "token_count" and "auth.tokens". This errs on the side
// of over-redaction; anchor the pattern (e.g., `(^|\.)token$`) to match a key
// exactly.
//
// Patterns are compiled once at handler construction time. Invalid patterns
// cause a panic.
func WithRedactPatterns(patterns ...string) RedactOption {
	return func(o *redactOptions) {
		o.patterns = append(o.patterns, patterns...)
	}
}

// WithRedactFunc sets a custom function for redacting attribute values.
//
// The function receives the current group path, the attribute key, and its
// value. It should return the (possibly modified) value. To redact, return
// slog.StringValue("[REDACTED]").
//
// The function is called after key-based and pattern-based checks, so it
// acts as an additional layer of redaction.
func WithRedactFunc(fn func(groups []string, key string, value slog.Value) slog.Value) RedactOption {
	return func(o *redactOptions) {
		o.redactFunc = fn
	}
}
