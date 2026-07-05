package handler

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"unicode"
)

// ANSI escape sequences used for level coloring.
const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// groupOrAttrs holds one WithGroup name or one WithAttrs attribute list, in
// the order the calls were made (the pattern from the official slog handler
// guide).
type groupOrAttrs struct {
	group string
	attrs []slog.Attr
}

// ConsoleHandler is a human-readable [slog.Handler] for development:
//
//	15:04:05.000 INF request handled method=GET status=200 auth.user=alice
//
// Compared to [slog.TextHandler] it drops the key= prefixes on the built-in
// fields, shortens the level to a colored three-letter tag, and renders
// group paths as dotted keys — optimized for scanning by eye, not for
// machine parsing. For production output use a JSON handler.
//
// Color is enabled by default unless the NO_COLOR environment variable is
// set (see https://no-color.org); override with [WithConsoleColor]. Note
// there is no TTY detection: when writing to a file with color enabled, ANSI
// codes are included.
//
// ConsoleHandler implements [slog.Handler] and is safe for concurrent use.
type ConsoleHandler struct {
	w     io.Writer
	mu    *sync.Mutex // shared across clones; guards w
	level slog.Leveler
	color bool
	goas  []groupOrAttrs
}

// NewConsoleHandler creates a [ConsoleHandler] writing to w.
//
// Defaults: level=Info, color on unless NO_COLOR is set.
func NewConsoleHandler(w io.Writer, opts ...ConsoleOption) *ConsoleHandler {
	o := applyConsoleOptions(opts)
	return &ConsoleHandler{
		w:     w,
		mu:    &sync.Mutex{},
		level: o.level,
		color: o.color,
	}
}

// Enabled reports whether the handler logs at the given level.
func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// WithAttrs returns a new ConsoleHandler whose subsequent records include
// the given attributes.
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{attrs: attrs})
}

// WithGroup returns a new ConsoleHandler that qualifies subsequent attribute
// keys with the given group name.
func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

func (h *ConsoleHandler) withGroupOrAttrs(goa groupOrAttrs) *ConsoleHandler {
	h2 := *h
	h2.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(h2.goas, h.goas)
	h2.goas[len(h.goas)] = goa
	return &h2
}

// Handle formats the record as a single human-readable line.
func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	buf := make([]byte, 0, 256)

	if !r.Time.IsZero() {
		buf = append(buf, h.dim(r.Time.Format("15:04:05.000"))...)
		buf = append(buf, ' ')
	}
	buf = append(buf, h.levelTag(r.Level)...)
	buf = append(buf, ' ')
	buf = append(buf, r.Message...)

	// Pre-applied groups and attrs, in registration order.
	prefix := ""
	for _, goa := range h.goas {
		if goa.group != "" {
			prefix += goa.group + "."
			continue
		}
		for _, a := range goa.attrs {
			buf = h.appendAttr(buf, prefix, a)
		}
	}

	// Record attrs.
	r.Attrs(func(a slog.Attr) bool {
		buf = h.appendAttr(buf, prefix, a)
		return true
	})

	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

// appendAttr renders one attribute (recursing into groups) with the given
// dotted prefix.
func (h *ConsoleHandler) appendAttr(buf []byte, prefix string, a slog.Attr) []byte {
	a.Value = a.Value.Resolve()

	// Ignore empty attrs, per the slog.Handler contract.
	if a.Equal(slog.Attr{}) {
		return buf
	}

	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		if len(attrs) == 0 {
			return buf // elide empty groups
		}
		childPrefix := prefix
		if a.Key != "" {
			childPrefix = prefix + a.Key + "."
		}
		for _, ga := range attrs {
			buf = h.appendAttr(buf, childPrefix, ga)
		}
		return buf
	}

	buf = append(buf, ' ')
	buf = append(buf, h.dim(prefix+a.Key+"=")...)
	buf = append(buf, consoleValue(a.Value)...)
	return buf
}

// consoleValue renders a resolved non-group value, quoting strings that
// would be ambiguous to read back.
func consoleValue(v slog.Value) string {
	s := v.String()
	if v.Kind() == slog.KindString || v.Kind() == slog.KindAny {
		if consoleNeedsQuote(s) {
			return strconv.Quote(s)
		}
	}
	return s
}

func consoleNeedsQuote(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r == ' ' || r == '"' || r == '=' || !unicode.IsPrint(r) {
			return true
		}
	}
	return false
}

// levelTag returns the (optionally colored) three-letter level tag.
func (h *ConsoleHandler) levelTag(level slog.Level) string {
	var tag, color string
	switch {
	case level < slog.LevelInfo:
		tag, color = "DBG", ansiCyan
	case level < slog.LevelWarn:
		tag, color = "INF", ansiGreen
	case level < slog.LevelError:
		tag, color = "WRN", ansiYellow
	default:
		tag, color = "ERR", ansiRed
	}
	if !h.color {
		return tag
	}
	return color + tag + ansiReset
}

// dim wraps s in the dim ANSI style when color is enabled.
func (h *ConsoleHandler) dim(s string) string {
	if !h.color {
		return s
	}
	return ansiDim + s + ansiReset
}

// ConsoleOption configures a [ConsoleHandler].
type ConsoleOption func(*consoleOptions)

type consoleOptions struct {
	level slog.Leveler
	color bool
}

func applyConsoleOptions(opts []ConsoleOption) *consoleOptions {
	o := &consoleOptions{
		level: slog.LevelInfo,
		color: os.Getenv("NO_COLOR") == "",
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithConsoleLevel sets the minimum level. Any [slog.Leveler] works,
// including a [*slog.LevelVar] for runtime changes. Default: Info.
func WithConsoleLevel(level slog.Leveler) ConsoleOption {
	return func(o *consoleOptions) {
		if level != nil {
			o.level = level
		}
	}
}

// WithConsoleColor enables or disables ANSI colors explicitly, overriding
// the NO_COLOR-based default.
func WithConsoleColor(enabled bool) ConsoleOption {
	return func(o *consoleOptions) {
		o.color = enabled
	}
}
