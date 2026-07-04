package logger

import "log/slog"

// Find walks the middleware chain starting at h (following [Unwrapper]) and
// returns the first handler that is of type T.
//
// T can be a concrete handler pointer or an interface. This is the ergonomic
// way to reach a specific handler inside a chain built by [Builder], e.g. to
// read runtime statistics:
//
//	log := logger.NewBuilder(base).WithAsync().WithSampling().BuildLogger()
//	if async, ok := logger.Find[*handler.AsyncHandler](log.Handler()); ok {
//	    fmt.Println("dropped:", async.Stats().Dropped)
//	}
//
// Find does not descend into the children of a fan-out handler such as
// [handler.MultiHandler], because a fan-out has no single inner handler;
// it only follows the linear Unwrap chain.
func Find[T any](h slog.Handler) (T, bool) {
	for depth := 0; h != nil && depth < maxUnwrapDepth; depth++ {
		if t, ok := h.(T); ok {
			return t, true
		}
		u, ok := h.(Unwrapper)
		if !ok {
			break
		}
		h = u.Unwrap()
	}
	var zero T
	return zero, false
}
