package logger_test

import (
	"log/slog"
	"os"

	logger "github.com/amhrmsn/go-logger"
	"github.com/amhrmsn/go-logger/handler"
)

// removeTime strips the time attribute so example output is deterministic.
func removeTime(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey && len(groups) == 0 {
		return slog.Attr{}
	}
	return a
}

func ExampleNewJSON() {
	log := logger.NewJSON(os.Stdout, logger.WithReplaceAttr(removeTime))
	log.Info("server started", "port", 8080)
	// Output: {"level":"INFO","msg":"server started","port":8080}
}

func ExampleRedacted() {
	log := logger.NewJSON(os.Stdout, logger.WithReplaceAttr(removeTime))
	log.Info("config loaded",
		"api_key", logger.Redacted("sk-1234-secret"),
		"host", "example.com",
	)
	// Output: {"level":"INFO","msg":"config loaded","api_key":"[REDACTED]","host":"example.com"}
}

func ExampleSensitiveBytes() {
	log := logger.NewJSON(os.Stdout, logger.WithReplaceAttr(removeTime))
	log.Info("key loaded", "key", logger.SensitiveBytes([]byte{0xDE, 0xAD, 0xBE, 0xEF}))
	// Output: {"level":"INFO","msg":"key loaded","key":"[REDACTED:4 bytes]"}
}

func ExampleNewBuilder() {
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: removeTime})

	log := logger.NewBuilder(base).
		WithRedaction(handler.WithRedactKeys("password")).
		BuildLogger()

	log.Info("user login", "user", "alice", "password", "s3cret")
	// Output: {"level":"INFO","msg":"user login","user":"alice","password":"[REDACTED]"}
}

func ExampleComponent() {
	config := handler.NewModuleConfig(slog.LevelInfo)
	config.SetLevel("database", slog.LevelDebug)

	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: removeTime,
	})
	log := slog.New(handler.NewModuleHandler(base, config))

	dbLog := log.With(logger.Component("database"))
	dbLog.Debug("query executed") // logged: database is set to Debug

	apiLog := log.With(logger.Component("api"))
	apiLog.Debug("parsing request") // filtered: api uses the Info default
	apiLog.Info("request handled")

	// Output:
	// {"level":"DEBUG","msg":"query executed","component":"database"}
	// {"level":"INFO","msg":"request handled","component":"api"}
}
