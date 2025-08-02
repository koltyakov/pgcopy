package utils // nolint:revive // utils is an acceptable name for internal utility package

import (
	"fmt"
	"log"
	"os"
	"time"
)

// Logger interface for colorized logging
type Logger interface {
	Logf(format string, args ...interface{})
	LogSuccess(format string, args ...interface{})
	LogWarn(format string, args ...interface{})
	LogError(format string, args ...interface{})
	LogProgress(format string, args ...interface{})
}

// SimpleLogger provides basic logging functionality
type SimpleLogger struct {
	logger *log.Logger
}

// NewSimpleLogger creates a new simple logger
func NewSimpleLogger(logger *log.Logger) *SimpleLogger {
	return &SimpleLogger{logger: logger}
}

// Logf logs a message using the custom logger when progress bar is active,
// or the standard logger otherwise
func (l *SimpleLogger) Logf(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		Colorize(ColorDim, timestamp),
		FormatLogLevel("INFO"),
		message)

	if l.logger != nil {
		// For progress bar mode, just write the formatted message
		l.logger.SetFlags(0) // Remove default timestamp since we add our own
		l.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// LogSuccess logs a success message with green color and checkmark
func (l *SimpleLogger) LogSuccess(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		Colorize(ColorDim, timestamp),
		FormatLogLevel("SUCCESS"),
		message)

	if l.logger != nil {
		l.logger.SetFlags(0)
		l.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// LogWarn logs a warning message with yellow color and warning icon
func (l *SimpleLogger) LogWarn(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		Colorize(ColorDim, timestamp),
		FormatLogLevel("WARN"),
		message)

	if l.logger != nil {
		l.logger.SetFlags(0)
		l.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// LogError logs an error message with red color and error icon
func (l *SimpleLogger) LogError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		Colorize(ColorDim, timestamp),
		FormatLogLevel("ERROR"),
		message)

	if l.logger != nil {
		l.logger.SetFlags(0)
		l.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// LogProgress logs a progress message with cyan color and progress icon
func (l *SimpleLogger) LogProgress(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		Colorize(ColorDim, timestamp),
		FormatLogLevel("INFO"),
		message)

	if l.logger != nil {
		l.logger.SetFlags(0)
		l.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}
