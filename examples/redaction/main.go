// Package main demonstrates the RedactionHandler for protecting sensitive data.
//
// This example shows three redaction strategies:
// 1. Type-based: using logger.Redacted for sensitive values
// 2. Key-based: exact key names or dotted group paths
// 3. Pattern-based: regular expressions matching key names
package main

import (
	"log/slog"
	"os"

	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

func main() {
	// Create a RedactionHandler with multiple redaction strategies.
	redactH := handler.NewRedactionHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		// Key-based: exact key names.
		handler.WithRedactKeys("password", "ssn", "credit_card"),
		// Pattern-based: any key containing "secret" or "token".
		handler.WithRedactPatterns(`(?i)secret`, `(?i)token`),
	)

	log := slog.New(redactH)

	// --- Key-based redaction ---

	log.Info("user login",
		slog.String("username", "alice"),
		slog.String("password", "super-secret-p@ss!"), // Will be [REDACTED]
		slog.String("ip", "10.0.0.42"),                // Not redacted
	)

	// --- Pattern-based redaction ---

	log.Info("API call",
		slog.String("api_token", "eyJhbGciOiJIUzI1NiJ9"), // Matches (?i)token → [REDACTED]
		slog.String("auth_secret", "sk-1234567890"),      // Matches (?i)secret → [REDACTED]
		slog.String("endpoint", "/api/v1/users"),         // Not redacted
	)

	// --- Type-based redaction using logger.Redacted ---

	// The Redacted type implements slog.LogValuer and always resolves to [REDACTED].
	// This works even WITHOUT the RedactionHandler because it's built into the type.
	log.Info("payment processing",
		slog.String("order_id", "ORD-12345"),
		slog.Any("credit_card", logger.Redacted("4111-1111-1111-1111")), // Type-based
		slog.String("ssn", "123-45-6789"),                               // Key-based
		slog.Float64("amount", 99.99),                                   // Not redacted
	)

	// --- Redaction with groups ---

	log.Info("nested data",
		slog.Group("user",
			slog.String("name", "Bob"),
			slog.String("password", "bob-secret"), // Nested key still redacted
			slog.String("email", "bob@example.com"),
		),
	)

	// --- Sensitive bytes ---

	log.Info("crypto operation",
		slog.Any("private_key", logger.SensitiveBytes([]byte{0xDE, 0xAD, 0xBE, 0xEF})),
		slog.String("algorithm", "ed25519"),
	)
}
