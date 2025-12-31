package utils // nolint:revive // utils is an acceptable name for internal utility package

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
)

// Logger interface for colorized logging
type Logger interface {
	Debug(format string, args ...any)
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

// SimpleLogger provides basic logging functionality with slog backend.
// It maintains the original API for backward compatibility while using
// structured logging internally.
type SimpleLogger struct {
	logger  *log.Logger  // Legacy logger (deprecated, kept for compatibility)
	slogger *slog.Logger // Structured logger
	silent  bool         // When true, all logging is suppressed
	level   LogLevel
}

// LogLevel represents verbosity threshold.
type LogLevel int

const (
	// LevelDebug enables verbose diagnostic logs.
	LevelDebug LogLevel = iota
	// LevelInfo standard informational logs.
	LevelInfo
	// LevelWarn warning conditions.
	LevelWarn
	// LevelError error conditions.
	LevelError
	// LevelSilent disables all logging.
	LevelSilent
)

// slogLevel converts LogLevel to slog.Level
func (l LogLevel) slogLevel() slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// coloredTextHandler is a custom slog handler that outputs colored text
type coloredTextHandler struct {
	level  slog.Level
	writer io.Writer
}

func (h *coloredTextHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *coloredTextHandler) Handle(_ context.Context, r slog.Record) error {
	timestamp := r.Time.Format("15:04:05")
	levelStr := r.Level.String()
	formatted := fmt.Sprintf("%s %s %s\n", Colorize(ColorDim, timestamp), FormatLogLevel(levelStr), r.Message)
	_, err := h.writer.Write([]byte(formatted))
	return err
}

func (h *coloredTextHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h // Ignore attrs for simple colored output
}

func (h *coloredTextHandler) WithGroup(_ string) slog.Handler {
	return h // Ignore groups for simple colored output
}

// NewSimpleLogger creates a new simple logger with the specified level
func NewSimpleLogger(logger *log.Logger) *SimpleLogger {
	return NewSimpleLoggerWithLevel(logger, LevelInfo)
}

// NewSimpleLoggerWithLevel creates a new logger with explicit level control
func NewSimpleLoggerWithLevel(logger *log.Logger, level LogLevel) *SimpleLogger {
	handler := &coloredTextHandler{
		level:  level.slogLevel(),
		writer: os.Stderr,
	}
	return &SimpleLogger{
		logger:  logger,
		slogger: slog.New(handler),
		silent:  false,
		level:   level,
	}
}

// NewSilentLogger creates a new silent logger that suppresses all output
func NewSilentLogger() *SimpleLogger {
	// Use a discard handler for silent mode
	handler := slog.NewTextHandler(io.Discard, nil)
	return &SimpleLogger{
		logger:  nil,
		slogger: slog.New(handler),
		silent:  true,
		level:   LevelSilent,
	}
}

// SetLevel changes the logging level at runtime
func (l *SimpleLogger) SetLevel(level LogLevel) {
	l.level = level
	if !l.silent {
		handler := &coloredTextHandler{
			level:  level.slogLevel(),
			writer: os.Stderr,
		}
		l.slogger = slog.New(handler)
	}
}

// GetLevel returns the current logging level
func (l *SimpleLogger) GetLevel() LogLevel {
	return l.level
}

// Debug logs verbose diagnostic information.
func (l *SimpleLogger) Debug(format string, args ...any) {
	if l.silent || l.level > LevelDebug {
		return
	}
	l.slogger.Debug(fmt.Sprintf(format, args...))
}

// Info logs informational messages (general progress).
func (l *SimpleLogger) Info(format string, args ...any) {
	if l.silent || l.level > LevelInfo {
		return
	}
	l.slogger.Info(fmt.Sprintf(format, args...))
}

// Warn logs warnings.
func (l *SimpleLogger) Warn(format string, args ...any) {
	if l.silent || l.level > LevelWarn {
		return
	}
	l.slogger.Warn(fmt.Sprintf(format, args...))
}

// Error logs errors.
func (l *SimpleLogger) Error(format string, args ...any) {
	if l.silent || l.level > LevelError {
		return
	}
	l.slogger.Error(fmt.Sprintf(format, args...))
}

// With returns a new logger with additional structured attributes.
// This is useful for adding context like table names, worker IDs, etc.
func (l *SimpleLogger) With(args ...any) *SimpleLogger {
	if l.silent {
		return l
	}
	return &SimpleLogger{
		logger:  l.logger,
		slogger: l.slogger.With(args...),
		silent:  l.silent,
		level:   l.level,
	}
}
