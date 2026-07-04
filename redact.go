package logger

import (
	"log/slog"
	"strconv"
)

// Redacted is a string type that always logs as "[REDACTED]".
//
// Implementing [slog.LogValuer], any value of this type will automatically
// have its contents hidden when logged, regardless of the handler or
// redaction middleware in use.
//
// This provides compile-time safety: sensitive types redact themselves,
// and callers cannot accidentally bypass the redaction.
//
//	type Config struct {
//	    APIKey logger.Redacted
//	    Host   string
//	}
//	// When logged: {"APIKey":"[REDACTED]","Host":"example.com"}
type Redacted string

// LogValue implements [slog.LogValuer]. It always returns "[REDACTED]",
// hiding the underlying string value.
func (r Redacted) LogValue() slog.Value {
	return slog.StringValue("[REDACTED]")
}

// SensitiveBytes is a []byte type that logs its length but not its content.
//
// This is useful for binary secrets (encryption keys, raw tokens, etc.)
// where knowing the size is helpful for debugging but the content must
// never appear in logs.
//
//	slog.Info("key loaded", "key", logger.SensitiveBytes(privateKeyBytes))
//	// Output: {"msg":"key loaded","key":"[REDACTED:32 bytes]"}
type SensitiveBytes []byte

// LogValue implements [slog.LogValuer]. It returns a string indicating
// the byte length without exposing the content.
func (s SensitiveBytes) LogValue() slog.Value {
	return slog.StringValue("[REDACTED:" + strconv.Itoa(len(s)) + " bytes]")
}
