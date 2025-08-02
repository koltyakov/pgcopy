// Package utils provides utility functions for pgcopy
package utils // nolint:revive // utils is an acceptable name for internal utility package

import (
	"fmt"
	"os"
	"runtime"
)

// ANSI color codes
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
	ColorBold    = "\033[1m"
	ColorDim     = "\033[2m"
)

// Colorize adds color to text if output supports it
func Colorize(color, text string) string {
	if IsColorSupported() {
		return color + text + ColorReset
	}
	return text
}

// IsColorSupported checks if the terminal supports colors
func IsColorSupported() bool {
	// Disable colors on Windows
	if runtime.GOOS == "windows" {
		return false
	}

	// Check if we're outputting to a terminal and not being redirected
	if fi, err := os.Stderr.Stat(); err == nil {
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// FormatLogLevel formats log levels with colors and icons
func FormatLogLevel(level string) string {
	switch level {
	case "INFO":
		return Colorize(ColorBlue, "INFO")
	case "WARN":
		return Colorize(ColorYellow, "WARN")
	case "ERROR":
		return Colorize(ColorRed, "ERROR")
	case "SUCCESS":
		return Colorize(ColorGreen, "DONE")
	default:
		return Colorize(ColorWhite, "📝 "+level)
	}
}

// HighlightTableName highlights table names in log messages
func HighlightTableName(schema, table string) string {
	return fmt.Sprintf("%s.%s",
		Colorize(ColorBlue, schema),
		Colorize(ColorBlue+ColorBold, table))
}

// HighlightFKName highlights foreign key constraint names
func HighlightFKName(fkName string) string {
	return Colorize(ColorMagenta+ColorBold, fkName)
}

// HighlightNumber highlights numbers in brackets and standalone numbers
func HighlightNumber(number any) string {
	return Colorize(ColorYellow+ColorBold, fmt.Sprintf("%v", number))
}
