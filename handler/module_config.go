package handler

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
)

// ModuleConfig holds per-component log level configuration for [ModuleHandler].
//
// Each component is identified by a string name (e.g., "networking", "storage",
// "consensus") and has its own [*slog.LevelVar] that can be changed at runtime.
//
// Components not explicitly configured use the default level.
//
// ModuleConfig is safe for concurrent use.
type ModuleConfig struct {
	mu           sync.RWMutex
	defaultLevel *slog.LevelVar
	modules      map[string]*slog.LevelVar

	// minCache holds the lowest (most permissive) level across the default
	// level and all configured modules. It is read on every Enabled() call
	// without a cached component, so it is kept in an atomic and recomputed
	// only when a level changes (SetLevel / SetDefaultLevel).
	minCache atomic.Int64
}

// NewModuleConfig creates a [ModuleConfig] with the given default log level.
//
// The default level applies to any component not explicitly configured via
// [ModuleConfig.SetLevel].
func NewModuleConfig(defaultLevel slog.Level) *ModuleConfig {
	lv := &slog.LevelVar{}
	lv.Set(defaultLevel)
	c := &ModuleConfig{
		defaultLevel: lv,
		modules:      make(map[string]*slog.LevelVar),
	}
	c.minCache.Store(int64(defaultLevel))
	return c
}

// SetLevel sets the log level for a specific component.
//
// If the component already has a configured level, it is updated in place
// (all loggers using this component will see the change immediately).
//
// If the component has not been configured before, a new [*slog.LevelVar]
// is created.
//
// This method is safe for concurrent use.
func (c *ModuleConfig) SetLevel(component string, level slog.Level) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if lv, ok := c.modules[component]; ok {
		lv.Set(level)
	} else {
		lv := &slog.LevelVar{}
		lv.Set(level)
		c.modules[component] = lv
	}
	c.recomputeMinLocked()
}

// SetDefaultLevel updates the default log level for components not explicitly
// configured.
//
// This method is safe for concurrent use.
func (c *ModuleConfig) SetDefaultLevel(level slog.Level) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.defaultLevel.Set(level)
	c.recomputeMinLocked()
}

// SetLevels parses and applies a comma-separated level specification:
//
//	"database=debug,auth=warn,*=info"
//
// Each segment is component=level. The special component "*" sets the
// default level. Level names are parsed case-insensitively via
// [slog.Level.UnmarshalText] ("debug", "info", "warn", "error", including
// offset forms like "info+2"). Whitespace around segments is trimmed and
// empty segments are ignored, so trailing commas are harmless.
//
// The whole spec is validated before anything is applied: on error, no
// levels are changed.
//
// This makes runtime level control easy to wire to an environment variable,
// config file, or admin endpoint:
//
//	if spec := os.Getenv("LOG_LEVELS"); spec != "" {
//	    if err := config.SetLevels(spec); err != nil { ... }
//	}
//
// This method is safe for concurrent use.
func (c *ModuleConfig) SetLevels(spec string) error {
	type pair struct {
		component string
		level     slog.Level
	}
	var pairs []pair
	for _, seg := range strings.Split(spec, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		name, levelStr, ok := strings.Cut(seg, "=")
		name = strings.TrimSpace(name)
		levelStr = strings.TrimSpace(levelStr)
		if !ok || name == "" || levelStr == "" {
			return fmt.Errorf("go-logger: invalid level spec segment %q (want component=level)", seg)
		}
		var lv slog.Level
		if err := lv.UnmarshalText([]byte(levelStr)); err != nil {
			return fmt.Errorf("go-logger: invalid level %q for component %q: %w", levelStr, name, err)
		}
		pairs = append(pairs, pair{component: name, level: lv})
	}

	for _, p := range pairs {
		if p.component == "*" {
			c.SetDefaultLevel(p.level)
		} else {
			c.SetLevel(p.component, p.level)
		}
	}
	return nil
}

// levelFor returns the [*slog.LevelVar] for the given component, or the
// default level if the component is not configured.
func (c *ModuleConfig) levelFor(component string) *slog.LevelVar {
	if component == "" {
		return c.defaultLevel
	}
	c.mu.RLock()
	lv, ok := c.modules[component]
	c.mu.RUnlock()
	if ok {
		return lv
	}
	return c.defaultLevel
}

// minLevel returns the lowest (most permissive) log level across the default
// level and all configured module levels.
//
// This is used by [ModuleHandler.Enabled] when no component is cached: it
// allows potentially valid log records to pass through to [Handle], where the
// component is resolved from record attributes. Without this, Enabled would
// fall back to the inner handler's level, which may reject logs that a
// specific module would accept.
//
// The value is served from an atomic cache maintained by [SetLevel] and
// [SetDefaultLevel], so this is a single atomic load on the hot path.
func (c *ModuleConfig) minLevel() slog.Level {
	return slog.Level(c.minCache.Load())
}

// recomputeMinLocked rescans all levels and updates the cached minimum.
// The caller must hold c.mu.
func (c *ModuleConfig) recomputeMinLocked() {
	min := c.defaultLevel.Level()
	for _, lv := range c.modules {
		if l := lv.Level(); l < min {
			min = l
		}
	}
	c.minCache.Store(int64(min))
}
