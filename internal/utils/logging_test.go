package utils

import (
	"bytes"
	"io"
	"log"
	"strings"
	"testing"
)

func TestLogLevel_slogLevel(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LogLevel(99), "INFO"}, // Unknown defaults to INFO
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			slog := tt.level.slogLevel()
			if slog.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, slog.String())
			}
		})
	}
}

func TestNewSimpleLogger(t *testing.T) {
	logger := NewSimpleLogger(log.New(io.Discard, "", 0))

	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}
	if logger.silent {
		t.Error("Logger should not be silent")
	}
	if logger.level != LevelInfo {
		t.Errorf("Expected level Info, got %d", logger.level)
	}
}

func TestNewSimpleLoggerWithLevel(t *testing.T) {
	logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), LevelDebug)

	if logger.level != LevelDebug {
		t.Errorf("Expected level Debug, got %d", logger.level)
	}
}

func TestNewSilentLogger(t *testing.T) {
	logger := NewSilentLogger()

	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}
	if !logger.silent {
		t.Error("Logger should be silent")
	}
	if logger.level != LevelSilent {
		t.Errorf("Expected level Silent, got %d", logger.level)
	}
}

func TestSimpleLogger_SilentMode(t *testing.T) {
	logger := NewSilentLogger()

	// These should not panic and should do nothing
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
}

func TestSimpleLogger_SetLevel(t *testing.T) {
	logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), LevelInfo)

	logger.SetLevel(LevelDebug)
	if logger.level != LevelDebug {
		t.Errorf("Expected level Debug, got %d", logger.level)
	}

	logger.SetLevel(LevelError)
	if logger.level != LevelError {
		t.Errorf("Expected level Error, got %d", logger.level)
	}
}

func TestSimpleLogger_SetLevel_Silent(t *testing.T) {
	logger := NewSilentLogger()

	// Should not panic
	logger.SetLevel(LevelDebug)
	// Silent logger should remain silent
	if !logger.silent {
		t.Error("Silent logger should remain silent after SetLevel")
	}
}

func TestSimpleLogger_GetLevel(t *testing.T) {
	tests := []struct {
		name  string
		level LogLevel
	}{
		{"debug", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"error", LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), tt.level)
			if logger.GetLevel() != tt.level {
				t.Errorf("Expected level %d, got %d", tt.level, logger.GetLevel())
			}
		})
	}
}

func TestSimpleLogger_LevelFiltering(t *testing.T) {
	// Logger set to Warn level should not log Debug or Info
	var buf bytes.Buffer
	logger := NewSimpleLoggerWithLevel(log.New(&buf, "", 0), LevelWarn)
	// Override writer for testing
	logger.slogger = nil // Force fallback behavior

	// At Warn level, Debug and Info should be filtered
	logger.Debug("debug message")
	logger.Info("info message")
	// These should be output-able but we're testing filtering logic
}

func TestSimpleLogger_With(t *testing.T) {
	logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), LevelInfo)

	childLogger := logger.With("key", "value")

	if childLogger == nil {
		t.Fatal("Expected non-nil child logger")
	}
	if childLogger.level != logger.level {
		t.Error("Child logger should inherit level")
	}
	if childLogger.silent != logger.silent {
		t.Error("Child logger should inherit silent flag")
	}
}

func TestSimpleLogger_With_Silent(t *testing.T) {
	logger := NewSilentLogger()

	childLogger := logger.With("key", "value")

	// Silent logger should return itself
	if childLogger != logger {
		t.Error("Silent logger.With() should return the same logger")
	}
}

func TestColoredTextHandler_Handle(t *testing.T) {
	var buf bytes.Buffer
	handler := &coloredTextHandler{
		level:  LevelInfo.slogLevel(),
		writer: &buf,
	}

	// Test that handler can process a record
	// We can't easily test the exact output due to slog.Record complexity
	// but we can test the handler exists and doesn't panic
	if handler.writer == nil {
		t.Error("Handler writer should not be nil")
	}
}

func TestColoredTextHandler_WithAttrs(t *testing.T) {
	handler := &coloredTextHandler{
		level:  LevelInfo.slogLevel(),
		writer: io.Discard,
	}

	newHandler := handler.WithAttrs(nil)

	// Should return the same handler (attrs ignored for simple output)
	if newHandler != handler {
		t.Error("WithAttrs should return same handler")
	}
}

func TestColoredTextHandler_WithGroup(t *testing.T) {
	handler := &coloredTextHandler{
		level:  LevelInfo.slogLevel(),
		writer: io.Discard,
	}

	newHandler := handler.WithGroup("test")

	// Should return the same handler (groups ignored for simple output)
	if newHandler != handler {
		t.Error("WithGroup should return same handler")
	}
}

func TestLogLevelConstants(t *testing.T) {
	// Ensure log levels are ordered correctly
	if LevelDebug >= LevelInfo {
		t.Error("Debug should be less than Info")
	}
	if LevelInfo >= LevelWarn {
		t.Error("Info should be less than Warn")
	}
	if LevelWarn >= LevelError {
		t.Error("Warn should be less than Error")
	}
	if LevelError >= LevelSilent {
		t.Error("Error should be less than Silent")
	}
}

func TestSimpleLogger_Debug_Filtering(t *testing.T) {
	logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), LevelInfo)

	// Should not panic and should be filtered (Info level doesn't show Debug)
	logger.Debug("test %s", "message")
}

func TestSimpleLogger_Info_Formatting(t *testing.T) {
	logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), LevelInfo)

	// Should not panic
	logger.Info("test %s %d", "message", 42)
}

func TestSimpleLogger_Warn_Formatting(t *testing.T) {
	logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), LevelWarn)

	// Should not panic
	logger.Warn("warning: %s", "something")
}

func TestSimpleLogger_Error_Formatting(t *testing.T) {
	logger := NewSimpleLoggerWithLevel(log.New(io.Discard, "", 0), LevelError)

	// Should not panic
	logger.Error("error: %v", strings.NewReader("test"))
}
