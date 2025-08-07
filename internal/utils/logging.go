package utils // nolint:revive // utils is an acceptable name for internal utility package

import (
	"fmt"
	"log"
	"os"
	"time"
)

// Logger interface for colorized logging
type Logger interface {
	Debug(format string, args ...any)
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

// SimpleLogger provides basic logging functionality
type SimpleLogger struct {
	logger *log.Logger
	silent bool // When true, all logging is suppressed
	level  LogLevel
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

// NewSimpleLogger creates a new simple logger
func NewSimpleLogger(logger *log.Logger) *SimpleLogger {
	return &SimpleLogger{logger: logger, silent: false, level: LevelInfo}
}

// NewSilentLogger creates a new silent logger that suppresses all output
func NewSilentLogger() *SimpleLogger {
	return &SimpleLogger{logger: nil, silent: true, level: LevelSilent}
}

// Logf logs a message using the custom logger when progress bar is active,
// or the standard logger otherwise
func (l *SimpleLogger) log(level LogLevel, tag, format string, args ...any) {
	if l.silent || level < l.level || l.level == LevelSilent {
		return
	}
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")
	formatted := fmt.Sprintf("%s %s %s", Colorize(ColorDim, timestamp), FormatLogLevel(tag), message)
	if l.logger != nil {
		l.logger.SetFlags(0)
		l.logger.Print(formatted)
	} else {
		fmt.Fprintln(os.Stderr, formatted)
	}
}

// Debug logs verbose diagnostic information.
func (l *SimpleLogger) Debug(format string, args ...any) {
	l.log(LevelDebug, "DEBUG", format, args...)
}

// Info logs informational messages (general progress).
func (l *SimpleLogger) Info(format string, args ...any) {
	l.log(LevelInfo, "INFO", format, args...)
}

// Warn logs warnings.
func (l *SimpleLogger) Warn(format string, args ...any) {
	l.log(LevelWarn, "WARN", format, args...)
}

// Error logs errors.
func (l *SimpleLogger) Error(format string, args ...any) {
	l.log(LevelError, "ERROR", format, args...)
}

// Backward-compatible alias methods and Success removed. Use Debug/Info/Warn/Error.
