package handler

import (
	"context"
	"log/slog"
	"slices"
)

// ModuleHandler filters log records based on per-component log level
// configuration.
//
// The component name is resolved from the "component" attribute, which can be
// set via [logger.Component]. Resolution checks record attributes first, then
// falls back to pre-applied attributes from [slog.Handler.WithAttrs].
//
// This allows code like:
//
//	log := slog.New(handler).With(logger.Component("networking"))
//	log.Debug("low-level detail") // filtered based on "networking" level config
//
// ModuleHandler implements [slog.Handler] and follows the immutable clone
// pattern for [slog.Handler.WithAttrs] and [slog.Handler.WithGroup].
type ModuleHandler struct {
	inner           slog.Handler
	config          *ModuleConfig
	preAttrs        []slog.Attr // attrs from WithAttrs for component resolution
	groups          []string    // current group path from WithGroup calls
	cachedComponent string      // component name resolved from preAttrs for Enabled() optimization
}

// NewModuleHandler creates a [ModuleHandler] that wraps the given inner handler
// with per-component log level filtering.
//
// The [ModuleConfig] is shared across all clones created by [WithAttrs] and
// [WithGroup], enabling runtime level changes to take effect immediately.
//
// If config is nil, a default [ModuleConfig] with [slog.LevelInfo] is used.
// Note that such a config is not reachable by the caller, so per-component
// levels cannot be changed later; pass an explicit config to control levels
// at runtime.
func NewModuleHandler(inner slog.Handler, config *ModuleConfig) *ModuleHandler {
	if config == nil {
		config = NewModuleConfig(slog.LevelInfo)
	}
	return &ModuleHandler{
		inner:  inner,
		config: config,
	}
}

// Enabled reports whether this handler would log a record at the given level.
//
// When a component name has been resolved via [WithAttrs] (e.g., from
// log.With(logger.Component("database"))), Enabled uses the cached component
// to perform an efficient per-module level check. This avoids creating a
// record that will be filtered in Handle.
//
// When no component is cached, Enabled uses the most permissive level across
// all configured modules (via [ModuleConfig.minLevel]). This ensures that
// log records where the component is only provided as a log-call attribute
// (e.g., log.Debug("msg", "component", "database")) can still reach Handle
// for proper component-level filtering.
//
// For best performance, attach the component via .With(logger.Component(...))
// so that Enabled can perform an exact module-level check.
func (h *ModuleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.cachedComponent != "" {
		cachedLevel := h.config.levelFor(h.cachedComponent).Level()
		minLevel := h.config.minLevel()

		// If any configured module allows a lower level than the cached component,
		// let the record reach Handle() so call-site attrs can override the cached
		// component.
		if minLevel < cachedLevel {
			return level >= minLevel
		}

		return level >= cachedLevel
	}

	// No cached component: use the most permissive level so potentially
	// valid records reach Handle() for proper component resolution.
	return level >= h.config.minLevel()
}

// Unwrap returns the inner handler, enabling lifecycle traversal.
func (h *ModuleHandler) Unwrap() slog.Handler { return h.inner }

// Handle resolves the component name from the record's attributes and applies
// per-component level filtering. If the record passes, it is forwarded to the
// inner handler.
func (h *ModuleHandler) Handle(ctx context.Context, r slog.Record) error {
	component := h.resolveComponent(r)
	lv := h.config.levelFor(component)

	if r.Level < lv.Level() {
		return nil // Filtered out by component level.
	}

	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new [ModuleHandler] where the inner handler has been
// cloned with the given attributes.
//
// If a "component" attribute is found (either in the new attrs or in the
// accumulated preAttrs), its value is cached in the clone for efficient
// Enabled() checks. This makes log.With(logger.Component("database"))
// the recommended pattern for module filtering.
func (h *ModuleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// If we're inside a group, the "component" attr won't be at the top
	// level, so we only store pre-attrs when not in a group context.
	var newPreAttrs []slog.Attr
	if len(h.groups) == 0 {
		newPreAttrs = make([]slog.Attr, 0, len(h.preAttrs)+len(attrs))
		newPreAttrs = append(newPreAttrs, h.preAttrs...)
		newPreAttrs = append(newPreAttrs, attrs...)
	} else {
		newPreAttrs = h.preAttrs // Don't accumulate grouped attrs for component resolution.
	}

	// Resolve cached component from the accumulated preAttrs.
	// Last occurrence wins to match resolveComponent semantics.
	cached := h.cachedComponent
	if len(h.groups) == 0 {
		for _, a := range attrs {
			if a.Key == "component" {
				cached = a.Value.String()
			}
		}
	}

	return &ModuleHandler{
		inner:           h.inner.WithAttrs(attrs),
		config:          h.config,
		preAttrs:        newPreAttrs,
		groups:          h.groups,
		cachedComponent: cached,
	}
}

// WithGroup returns a new [ModuleHandler] where the inner handler has been
// cloned with the given group name.
//
// The cached component is preserved across group boundaries.
func (h *ModuleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &ModuleHandler{
		inner:           h.inner.WithGroup(name),
		config:          h.config,
		preAttrs:        h.preAttrs,
		groups:          append(slices.Clone(h.groups), name),
		cachedComponent: h.cachedComponent,
	}
}

// resolveComponent extracts the "component" value from the record's attributes.
// It checks record attrs first (from Handle's record), then falls back to
// pre-applied attrs (from WithAttrs).
//
// Returns "" if no "component" attribute is found, which causes the handler
// to use the default level.
func (h *ModuleHandler) resolveComponent(r slog.Record) string {
	// 1. Check record attrs (set at log call site). Last occurrence wins.
	var component string
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			component = a.Value.String()
			found = true
		}
		return true // continue iteration to find the last occurrence
	})
	if found {
		return component
	}

	// 2. Check pre-applied attrs (set via WithAttrs / .With()). Last occurrence wins.
	for i := len(h.preAttrs) - 1; i >= 0; i-- {
		a := h.preAttrs[i]
		if a.Key == "component" {
			return a.Value.String()
		}
	}

	return ""
}
