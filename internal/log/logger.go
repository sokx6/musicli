// Package log provides a structured logger for musicli.
//
// Format: 2006-01-02T15:04:05.000-07:00[LEVEL][module][func] message k=v
//
// Four levels: debug, info, warning, error. Output to a file truncated on
// each startup (no rotation in v1). Every error log should wrap the original
// error with fmt.Errorf("...: %w", err) and pass it as the "err" attribute
// so the full error chain is recorded.
package log

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Level strings accepted in config.
const (
	LevelDebug   = "debug"
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"
)

// Logger wraps slog.Logger with the underlying file so callers can close it.
type Logger struct {
	*slog.Logger
	file  *os.File
	Attrs map[string]string // module + func tags for this logger
}

// New creates a logger writing to path (truncated on open). level is one of
// the Level* constants; unknown levels default to info.
func New(level, path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", path, err)
	}
	h := &handler{
		w:     f,
		level: parseLevel(level),
	}
	return &Logger{
		Logger: slog.New(h),
		file:   f,
	}, nil
}

// WithModule returns a child logger that tags every record with [module].
func (l *Logger) WithModule(name string) *Logger {
	nl := l.clone()
	if nl.Attrs == nil {
		nl.Attrs = map[string]string{}
	}
	nl.Attrs["module"] = name
	nl.Logger = slog.New(&handler{
		w:     l.file,
		level: l.Logger.Handler().(*handler).level,
		attrs: nl.Attrs,
	})
	return nl
}

// WithFunc returns a child logger that tags every record with [func].
// Usage: log.WithModule("audio").WithFunc("startFFmpeg").Error("...", "err", err)
func (l *Logger) WithFunc(name string) *Logger {
	nl := l.clone()
	if nl.Attrs == nil {
		nl.Attrs = map[string]string{}
	}
	nl.Attrs["func"] = name
	nl.Logger = slog.New(&handler{
		w:     l.file,
		level: l.Logger.Handler().(*handler).level,
		attrs: nl.Attrs,
	})
	return nl
}

func (l *Logger) clone() *Logger {
	attrs := map[string]string{}
	for k, v := range l.Attrs {
		attrs[k] = v
	}
	return &Logger{
		Logger: l.Logger,
		file:   l.file,
		Attrs:  attrs,
	}
}

// Close flushes and closes the underlying log file.
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

func parseLevel(s string) slog.Level {
	switch s {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarning:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Discard returns a logger that drops all output, for tests.
func Discard() *Logger {
	h := &handler{w: io.Discard, level: slog.LevelDebug}
	return &Logger{Logger: slog.New(h), Attrs: map[string]string{}}
}

// handler is a custom slog.Handler that formats as:
// time[LEVEL][module][func] message k=v
type handler struct {
	mu    sync.Mutex
	w     io.Writer
	level slog.Leveler
	attrs map[string]string // pre-set module/func from WithModule/WithFunc
}

func (h *handler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.level.Level()
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	var b []byte
	// time
	b = r.Time.AppendFormat(b, "2006-01-02T15:04:05.000-07:00")
	// level
	b = append(b, '[')
	b = appendLevel(b, r.Level)
	b = append(b, ']')

	// merge pre-set attrs with record attrs; record attrs override
	mod := h.attrs["module"]
	fnc := h.attrs["func"]
	var extra []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "module":
			mod = a.Value.String()
		case "func":
			fnc = a.Value.String()
		default:
			extra = append(extra, a)
		}
		return true
	})

	// [module]
	if mod != "" {
		b = append(b, '[')
		b = append(b, mod...)
		b = append(b, ']')
	}
	// [func]
	if fnc != "" {
		b = append(b, '[')
		b = append(b, fnc...)
		b = append(b, ']')
	}

	// message
	b = append(b, ' ')
	b = append(b, r.Message...)

	// remaining attrs as k=v (err included)
	for _, a := range extra {
		b = append(b, ' ')
		b = append(b, a.Key...)
		b = append(b, '=')
		b = appendAttrValue(b, a.Value)
		if err, ok := attrError(a); ok {
			if chain := errorChain(err); chain != "" {
				b = append(b, ' ')
				b = append(b, a.Key...)
				b = append(b, "_chain="...)
				b = appendQuoted(b, chain)
			}
		}
	}
	b = append(b, '\n')

	h.mu.Lock()
	_, err := h.w.Write(b)
	h.mu.Unlock()
	return err
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := map[string]string{}
	for k, v := range h.attrs {
		newAttrs[k] = v
	}
	for _, a := range attrs {
		newAttrs[a.Key] = a.Value.String()
	}
	return &handler{w: h.w, level: h.level, attrs: newAttrs}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return h // flat structure, no groups
}

func appendLevel(b []byte, lvl slog.Level) []byte {
	switch {
	case lvl >= slog.LevelError:
		return append(b, "ERROR"...)
	case lvl >= slog.LevelWarn:
		return append(b, "WARN"...)
	case lvl >= slog.LevelInfo:
		return append(b, "INFO"...)
	default:
		return append(b, "DEBUG"...)
	}
}

func appendAttrValue(b []byte, v slog.Value) []byte {
	if err, ok := valueError(v); ok {
		return appendQuoted(b, err.Error())
	}
	switch v.Kind() {
	case slog.KindString:
		return appendQuoted(b, v.String())
	case slog.KindInt64:
		return strconv.AppendInt(b, v.Int64(), 10)
	case slog.KindUint64:
		return strconv.AppendUint(b, v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.AppendFloat(b, v.Float64(), 'f', -1, 64)
	case slog.KindBool:
		if v.Bool() {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	default:
		return appendQuoted(b, fmt.Sprint(v.Any()))
	}
}

func appendQuoted(b []byte, s string) []byte {
	// quote if contains spaces or special chars
	if s == "" {
		return append(b, `""`...)
	}
	needsQuote := false
	for _, c := range s {
		if c == ' ' || c == '=' || c == '"' || c == '\n' || c == '\t' {
			needsQuote = true
			break
		}
	}
	if needsQuote {
		return strconv.AppendQuote(b, s)
	}
	return append(b, s...)
}

func attrError(a slog.Attr) (error, bool) {
	return valueError(a.Value)
}

func valueError(v slog.Value) (error, bool) {
	if v.Kind() != slog.KindAny {
		return nil, false
	}
	err, ok := v.Any().(error)
	return err, ok
}

func errorChain(err error) string {
	if err == nil {
		return ""
	}
	parts := []string{}
	for err != nil {
		parts = append(parts, errorMessageWithoutWrappedSuffix(err))
		err = errors.Unwrap(err)
	}
	return strings.Join(parts, " -> ")
}

func errorMessageWithoutWrappedSuffix(err error) string {
	msg := err.Error()
	wrapped := errors.Unwrap(err)
	if wrapped == nil {
		return msg
	}
	suffix := ": " + wrapped.Error()
	if strings.HasSuffix(msg, suffix) {
		return strings.TrimSuffix(msg, suffix)
	}
	return msg
}
