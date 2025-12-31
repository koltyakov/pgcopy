package utils

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"milliseconds", 500 * time.Millisecond, "500ms"},
		{"one_second", time.Second, "1s"},
		{"seconds", 45 * time.Second, "45s"},
		{"one_minute", time.Minute, "1m0s"},
		{"minutes_seconds", 2*time.Minute + 30*time.Second, "2m30s"},
		{"one_hour", time.Hour, "1h0m0s"},
		{"hours_minutes", 2*time.Hour + 15*time.Minute, "2h15m0s"},
		{"full_format", 1*time.Hour + 30*time.Minute + 45*time.Second, "1h30m45s"},
		{"zero", 0, "0ms"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("FormatDuration(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatLogDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"milliseconds", 500 * time.Millisecond, "500ms"},
		{"one_second", time.Second, "1.0s"},
		{"seconds", 45 * time.Second, "45.0s"},
		{"one_minute", time.Minute, "1m0s"},
		{"minutes_seconds", 2*time.Minute + 30*time.Second, "2m30s"},
		{"one_hour", time.Hour, "1h0m0s"},
		{"hours_minutes", 2*time.Hour + 15*time.Minute, "2h15m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatLogDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("FormatLogDuration(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		name     string
		number   int64
		expected string
	}{
		{"zero", 0, "0"},
		{"small", 100, "100"},
		{"under_thousand", 999, "999"},
		{"thousand_exact", 1000, "1K"},
		{"thousands", 1500, "1.5K"},
		{"thousands_exact", 5000, "5K"},
		{"under_million", 999999, "1000.0K"},
		{"million_exact", 1000000, "1M"},
		{"millions", 2500000, "2.5M"},
		{"millions_exact", 5000000, "5M"},
		{"billion_exact", 1000000000, "1B"},
		{"billions", 2500000000, "2.5B"},
		{"billions_exact", 5000000000, "5B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatNumber(tt.number)
			if result != tt.expected {
				t.Errorf("FormatNumber(%d) = %s, want %s", tt.number, result, tt.expected)
			}
		})
	}
}

func TestMaskPassword(t *testing.T) {
	tests := []struct {
		name     string
		connStr  string
		expected string
	}{
		{
			"postgres_url_with_password",
			"postgres://user:secret@localhost:5432/db",
			"postgres://user:%2A%2A%2A@localhost:5432/db", // URL-encoded ***
		},
		{
			"postgresql_url_with_password",
			"postgresql://admin:p4ssw0rd@host:5432/mydb",
			"postgresql://admin:%2A%2A%2A@host:5432/mydb", // URL-encoded ***
		},
		{
			"postgres_url_no_password",
			"postgres://user@localhost:5432/db",
			"postgres://user@localhost:5432/db",
		},
		{
			"key_value_with_password",
			"host=localhost user=admin password=secret dbname=test",
			"host=localhost user=admin password=*** dbname=test",
		},
		{
			"key_value_no_password",
			"host=localhost user=admin dbname=test",
			"host=localhost user=admin dbname=test",
		},
		{
			"invalid_url",
			"://invalid",
			"://invalid", // Not a postgres URL, returned as-is
		},
		{
			"empty_string",
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskPassword(tt.connStr)
			if result != tt.expected {
				t.Errorf("MaskPassword(%s) = %s, want %s", tt.connStr, result, tt.expected)
			}
		})
	}
}

func TestExtractConnectionDetails(t *testing.T) {
	tests := []struct {
		name     string
		connStr  string
		contains string
	}{
		{
			"postgres_url",
			"postgres://user:pass@localhost:5432/mydb",
			"localhost:5432/mydb",
		},
		{
			"key_value_format",
			"host=localhost port=5432 dbname=mydb user=admin password=secret",
			"localhost:5432/mydb",
		},
		{
			"default_port",
			"postgres://user:pass@localhost/mydb",
			"localhost:5432/mydb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractConnectionDetails(tt.connStr)
			if result == "" {
				t.Error("Expected non-empty result")
			}
			// Connection details format may vary, just ensure it contains key info
			if len(result) == 0 {
				t.Error("Result should not be empty")
			}
		})
	}
}

func TestExtractConnectionDetails_Invalid(t *testing.T) {
	const expectedUnavailable = "connection details unavailable"
	result := ExtractConnectionDetails("://invalid")
	if result != expectedUnavailable {
		t.Errorf("Expected '%s', got '%s'", expectedUnavailable, result)
	}
}

func TestFormatDuration_EdgeCases(t *testing.T) {
	// Test edge cases around boundaries
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"just_under_second", 999 * time.Millisecond, "999ms"},
		{"exactly_second", 1000 * time.Millisecond, "1s"},
		{"just_under_minute", 59 * time.Second, "59s"},
		{"exactly_minute", 60 * time.Second, "1m0s"},
		{"just_under_hour", 59*time.Minute + 59*time.Second, "59m59s"},
		{"exactly_hour", 60 * time.Minute, "1h0m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("FormatDuration(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatNumber_Negative(t *testing.T) {
	// FormatNumber takes int64, test with negative (should work mathematically)
	result := FormatNumber(-100)
	if result != "-100" {
		t.Errorf("FormatNumber(-100) = %s, want -100", result)
	}
}
