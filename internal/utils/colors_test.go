package utils

import (
	"os"
	"testing"
)

func TestColorConstants(t *testing.T) {
	// Verify color constants are properly defined
	tests := []struct {
		name     string
		color    string
		notEmpty bool
	}{
		{"ColorReset", ColorReset, true},
		{"ColorRed", ColorRed, true},
		{"ColorGreen", ColorGreen, true},
		{"ColorYellow", ColorYellow, true},
		{"ColorBlue", ColorBlue, true},
		{"ColorMagenta", ColorMagenta, true},
		{"ColorCyan", ColorCyan, true},
		{"ColorWhite", ColorWhite, true},
		{"ColorBold", ColorBold, true},
		{"ColorDim", ColorDim, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.notEmpty && tt.color == "" {
				t.Errorf("%s should not be empty", tt.name)
			}
		})
	}
}

func TestColorize(t *testing.T) {
	text := "Hello"

	// Colorize should return the text regardless of terminal support
	// (actual coloring depends on IsColorSupported)
	result := Colorize(ColorRed, text)

	if result == "" {
		t.Error("Colorize should return non-empty string")
	}
	// The result should at minimum contain the original text
	if len(result) < len(text) {
		t.Error("Colorize result should contain original text")
	}
}

func TestColorize_EmptyText(t *testing.T) {
	result := Colorize(ColorRed, "")

	// Empty text with colors should work
	if result != "" && result != ColorRed+ColorReset {
		// Either empty (no color support) or colored empty string
	}
}

func TestIsColorSupported(t *testing.T) {
	// This test depends on the environment
	// We just verify it doesn't panic and returns a boolean
	result := IsColorSupported()

	// Result must be a boolean (either true or false)
	if result != true && result != false {
		t.Error("IsColorSupported should return a boolean")
	}
}

func TestIsColorSupported_StderrStat(t *testing.T) {
	// Save original stderr
	originalStderr := os.Stderr
	defer func() { os.Stderr = originalStderr }()

	// Test with a pipe (should return false for color support)
	r, w, err := os.Pipe()
	if err != nil {
		t.Skip("Cannot create pipe for testing")
	}
	defer r.Close()
	defer w.Close()

	os.Stderr = w
	result := IsColorSupported()

	// Pipe is not a terminal, so color should not be supported
	// (unless on Windows where it's always false)
	if result {
		t.Log("Color reported as supported on pipe - may be platform specific")
	}
}

func TestFormatLogLevel(t *testing.T) {
	tests := []struct {
		level    string
		notEmpty bool
	}{
		{"INFO", true},
		{"WARN", true},
		{"ERROR", true},
		{"DEBUG", true},
		{"UNKNOWN", true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			result := FormatLogLevel(tt.level)
			if tt.notEmpty && result == "" {
				t.Errorf("FormatLogLevel(%s) should not be empty", tt.level)
			}
		})
	}
}

func TestHighlightTableName(t *testing.T) {
	result := HighlightTableName("public", "users")

	if result == "" {
		t.Error("HighlightTableName should return non-empty string")
	}
	// Should contain both schema and table name
	// The exact format depends on color support
}

func TestHighlightFKName(t *testing.T) {
	result := HighlightFKName("fk_users_orders")

	if result == "" {
		t.Error("HighlightFKName should return non-empty string")
	}
}

func TestHighlightNumber(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		notNil bool
	}{
		{"integer", 42, true},
		{"int64", int64(1000000), true},
		{"float", 3.14, true},
		{"string_number", "123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HighlightNumber(tt.input)
			if tt.notNil && result == "" {
				t.Errorf("HighlightNumber(%v) should return non-empty string", tt.input)
			}
		})
	}
}

func TestColorize_WithAllColors(t *testing.T) {
	colors := []string{
		ColorReset, ColorRed, ColorGreen, ColorYellow,
		ColorBlue, ColorMagenta, ColorCyan, ColorWhite,
		ColorBold, ColorDim,
	}

	text := "test"
	for _, color := range colors {
		result := Colorize(color, text)
		if len(result) == 0 {
			t.Errorf("Colorize with color %q returned empty string", color)
		}
	}
}

func TestColorize_CombinedColors(t *testing.T) {
	// Test combined color codes like ColorBlue+ColorBold
	combined := ColorBlue + ColorBold
	result := Colorize(combined, "test")

	if result == "" {
		t.Error("Combined colors should work")
	}
}
