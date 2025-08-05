package utils // nolint:revive // utils is an acceptable name for internal utility package

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// FormatDuration formats a duration without decimal parts
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
}

// FormatLogDuration formats duration for log files with rounded values
func FormatLogDuration(d time.Duration) string {
	if d < time.Second {
		// Round to nearest millisecond
		ms := d.Round(time.Millisecond)
		return fmt.Sprintf("%dms", ms.Milliseconds())
	}
	if d < time.Minute {
		// Round to nearest 100ms for seconds
		rounded := d.Round(100 * time.Millisecond)
		return fmt.Sprintf("%.1fs", rounded.Seconds())
	}
	if d < time.Hour {
		// Round to nearest second for minutes
		rounded := d.Round(time.Second)
		minutes := int(rounded.Minutes())
		seconds := int(rounded.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	// Round to nearest second for hours
	rounded := d.Round(time.Second)
	hours := int(rounded.Hours())
	minutes := int(rounded.Minutes()) % 60
	seconds := int(rounded.Seconds()) % 60
	return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
}

// FormatNumber formats large numbers with K/M suffixes
func FormatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dK", n/1000)
		}
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		if n%1000000 == 0 {
			return fmt.Sprintf("%dM", n/1000000)
		}
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n%1000000000 == 0 {
		return fmt.Sprintf("%dB", n/1000000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

// MaskPassword masks password in PostgreSQL connection strings for safe display
func MaskPassword(connStr string) string {
	// Handle postgres:// URLs
	if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
		parsed, err := url.Parse(connStr)
		if err != nil {
			return "***masked***"
		}
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword {
				parsed.User = url.UserPassword(parsed.User.Username(), "***")
			}
		}
		return parsed.String()
	}

	// Handle key=value format
	if strings.Contains(connStr, "password=") {
		parts := strings.Split(connStr, " ")
		for i, part := range parts {
			if strings.HasPrefix(strings.ToLower(part), "password=") {
				parts[i] = "password=***"
			}
		}
		return strings.Join(parts, " ")
	}

	return connStr
}
