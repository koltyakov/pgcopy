package copier

import (
	"fmt"
	"os"
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

// colorize adds color to text if output supports it
func colorize(color, text string) string {
	if isColorSupported() {
		return color + text + ColorReset
	}
	return text
}

// isColorSupported checks if the terminal supports colors
func isColorSupported() bool {
	// Check if we're outputting to a terminal and not being redirected
	if fi, err := os.Stderr.Stat(); err == nil {
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// formatLogLevel formats log levels with colors and icons
func formatLogLevel(level string) string {
	switch level {
	case "INFO":
		return colorize(ColorBlue, "INFO")
	case "WARN":
		return colorize(ColorYellow, "WARN")
	case "ERROR":
		return colorize(ColorRed, "ERROR")
	case "SUCCESS":
		return colorize(ColorGreen, "DONE")
	default:
		return colorize(ColorWhite, "📝 "+level)
	}
}

// highlightTableName highlights table names in log messages
func highlightTableName(schema, table string) string {
	return fmt.Sprintf("%s.%s",
		colorize(ColorBlue, schema),
		colorize(ColorBlue+ColorBold, table))
}

// highlightFKName highlights foreign key constraint names
func highlightFKName(fkName string) string {
	return colorize(ColorMagenta+ColorBold, fkName)
}

// highlightNumber highlights numbers in brackets and standalone numbers
func highlightNumber(number interface{}) string {
	return colorize(ColorYellow+ColorBold, fmt.Sprintf("%v", number))
}
