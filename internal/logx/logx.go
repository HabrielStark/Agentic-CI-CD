// Package logx is a tiny structured-ish logger used by the CLI.
// It supports levels and JSON output. We avoid external deps so the binary
// stays small and deterministic.
package logx

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level is a log level.
type Level int

// Levels.
const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ParseLevel parses a level string (case-insensitive).
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// Logger writes leveled log lines.
type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	level  Level
	json   bool
	fields map[string]any
}

// New returns a Logger writing to out.
func New(out io.Writer, level Level, asJSON bool) *Logger {
	if out == nil {
		out = os.Stderr
	}
	return &Logger{out: out, level: level, json: asJSON, fields: map[string]any{}}
}

// Default returns a logger reading REPROFORGE_LOG and REPROFORGE_LOG_JSON env vars.
func Default() *Logger {
	lvl := ParseLevel(os.Getenv("REPROFORGE_LOG"))
	asJSON := os.Getenv("REPROFORGE_LOG_JSON") == "1"
	return New(os.Stderr, lvl, asJSON)
}

// With returns a child logger with the given fields merged in.
func (l *Logger) With(kv ...any) *Logger {
	child := &Logger{out: l.out, level: l.level, json: l.json, fields: map[string]any{}}
	for k, v := range l.fields {
		child.fields[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		key, _ := kv[i].(string)
		if key != "" {
			child.fields[key] = kv[i+1]
		}
	}
	return child
}

// SetLevel sets the minimum level.
func (l *Logger) SetLevel(lv Level) { l.level = lv }

// Level returns the current level.
func (l *Logger) Level() Level { return l.level }

// Debug logs at debug level.
func (l *Logger) Debug(msg string, kv ...any) { l.log(LevelDebug, msg, kv...) }

// Info logs at info level.
func (l *Logger) Info(msg string, kv ...any) { l.log(LevelInfo, msg, kv...) }

// Warn logs at warn level.
func (l *Logger) Warn(msg string, kv ...any) { l.log(LevelWarn, msg, kv...) }

// Error logs at error level.
func (l *Logger) Error(msg string, kv ...any) { l.log(LevelError, msg, kv...) }

// Fatalf logs at error level and exits 1.
func (l *Logger) Fatalf(format string, args ...any) {
	l.log(LevelError, fmt.Sprintf(format, args...))
	os.Exit(1)
}

func (l *Logger) log(level Level, msg string, kv ...any) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	if l.json {
		entry := map[string]any{"ts": now, "level": level.String(), "msg": msg}
		for k, v := range l.fields {
			entry[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			key, _ := kv[i].(string)
			if key != "" {
				entry[key] = kv[i+1]
			}
		}
		_ = json.NewEncoder(l.out).Encode(entry)
		return
	}
	var b strings.Builder
	b.WriteString(now)
	b.WriteString(" ")
	b.WriteString(strings.ToUpper(level.String()))
	b.WriteString(" ")
	b.WriteString(msg)
	for k, v := range l.fields {
		fmt.Fprintf(&b, " %s=%v", k, v)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		key, _ := kv[i].(string)
		if key != "" {
			fmt.Fprintf(&b, " %s=%v", key, kv[i+1])
		}
	}
	b.WriteString("\n")
	_, _ = l.out.Write([]byte(b.String()))
}
