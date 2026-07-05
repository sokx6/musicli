// Package log provides a structured logger for musicli using log/slog.
// Four levels: debug, info, warning, error. Output to a file that is
// truncated on each startup (no rotation in v1).
package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	file *os.File
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
	return &Logger{
		Logger: slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
			Level: parseLevel(level),
		})),
		file: f,
	}, nil
}

// WithModule returns a child logger that tags every record with module=<name>.
func (l *Logger) WithModule(name string) *Logger {
	return &Logger{Logger: l.With(slog.String("module", name)), file: l.file}
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
	return &Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}
