// Package handler provides composable [slog.Handler] middleware for the
// go-logger library.
//
// All handlers in this package implement the [slog.Handler] interface and
// follow the immutable clone pattern: [slog.Handler.WithAttrs] and
// [slog.Handler.WithGroup] always return new handler instances without
// modifying the receiver.
//
// Handlers can be composed in any order to build a processing pipeline:
//
//	base := slog.NewJSONHandler(os.Stdout, nil)
//	h := handler.NewRedactionHandler(
//	    handler.NewAsyncHandler(base),
//	    handler.WithRedactKeys("password"),
//	)
//	log := slog.New(h)
package handler
