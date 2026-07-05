package handler

import (
	"bytes"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/slogtest"
)

var consoleTimeRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3}$`)

var consoleLevelNames = map[string]string{
	"DBG": "DEBUG", "INF": "INFO", "WRN": "WARN", "ERR": "ERROR",
}

// splitConsoleTokens splits a console line on spaces, keeping quoted
// sections (anywhere within a token) intact.
func splitConsoleTokens(line string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"' && (i == 0 || line[i-1] != '\\'):
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == ' ' && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func isConsoleAttrToken(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	return !strings.ContainsRune(tok[:eq], '"')
}

// parseConsoleLine reconstructs a result map (with nested groups) from one
// formatted console line, for slogtest.
func parseConsoleLine(t *testing.T, line string) map[string]any {
	t.Helper()
	tokens := splitConsoleTokens(line)
	if len(tokens) == 0 {
		t.Fatalf("empty console line")
	}

	m := map[string]any{}
	i := 0
	if consoleTimeRe.MatchString(tokens[0]) {
		m[slog.TimeKey] = tokens[0]
		i++
	}
	name, ok := consoleLevelNames[tokens[i]]
	if !ok {
		t.Fatalf("unexpected level tag %q in line %q", tokens[i], line)
	}
	m[slog.LevelKey] = name
	i++

	// Attrs are the trailing key=value tokens; everything between the level
	// tag and the first attr is the message.
	j := len(tokens)
	for j > i && isConsoleAttrToken(tokens[j-1]) {
		j--
	}
	m[slog.MessageKey] = strings.Join(tokens[i:j], " ")

	for _, tok := range tokens[j:] {
		key, val, _ := strings.Cut(tok, "=")
		if strings.HasPrefix(val, `"`) {
			if unq, err := strconv.Unquote(val); err == nil {
				val = unq
			}
		}
		putNested(m, key, val)
	}
	return m
}

// putNested inserts a dotted key as nested maps: "a.b.c" → m[a][b][c].
func putNested(m map[string]any, key, val string) {
	parts := strings.Split(key, ".")
	for len(parts) > 1 {
		sub, ok := m[parts[0]].(map[string]any)
		if !ok {
			sub = map[string]any{}
			m[parts[0]] = sub
		}
		m = sub
		parts = parts[1:]
	}
	m[parts[0]] = val
}

func TestConsoleHandler_SlogtestCompliance(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf,
		WithConsoleColor(false),
		WithConsoleLevel(slog.LevelDebug),
	)

	err := slogtest.TestHandler(h, func() []map[string]any {
		var results []map[string]any
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			results = append(results, parseConsoleLine(t, line))
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsoleHandler_Format(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, WithConsoleColor(false))
	log := slog.New(h)

	log.Info("request handled", "method", "GET", "status", 200)

	line := strings.TrimSpace(buf.String())
	// 15:04:05.000 INF request handled method=GET status=200
	if !regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} INF request handled method=GET status=200$`).MatchString(line) {
		t.Errorf("unexpected format: %q", line)
	}
}

func TestConsoleHandler_GroupsDottedKeys(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, WithConsoleColor(false))
	log := slog.New(h).WithGroup("auth").With("user", "alice")

	log.Info("login", slog.Group("session", slog.String("id", "s1")))

	line := buf.String()
	if !strings.Contains(line, "auth.user=alice") {
		t.Errorf("expected dotted pre-attr key, got %q", line)
	}
	if !strings.Contains(line, "auth.session.id=s1") {
		t.Errorf("expected dotted group key, got %q", line)
	}
}

func TestConsoleHandler_QuotesAmbiguousValues(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, WithConsoleColor(false))
	slog.New(h).Info("m", "note", "has spaces", "eq", "a=b", "empty", "")

	line := buf.String()
	for _, want := range []string{`note="has spaces"`, `eq="a=b"`, `empty=""`} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %s in %q", want, line)
		}
	}
}

func TestConsoleHandler_ColorOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, WithConsoleColor(true))
	slog.New(h).Error("boom")

	if !strings.Contains(buf.String(), ansiRed+"ERR"+ansiReset) {
		t.Errorf("expected colored ERR tag, got %q", buf.String())
	}
}

func TestConsoleHandler_LevelFiltering(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, WithConsoleColor(false), WithConsoleLevel(slog.LevelWarn))
	log := slog.New(h)

	log.Info("hidden")
	log.Warn("visible")

	out := buf.String()
	if strings.Contains(out, "hidden") || !strings.Contains(out, "visible") {
		t.Errorf("level filtering broken: %q", out)
	}
}
