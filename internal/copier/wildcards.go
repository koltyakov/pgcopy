package copier

import (
	"path/filepath"
	"strings"
)

// matchesPattern checks if a string matches a wildcard pattern
// Supports * wildcard for matching any sequence of characters
func matchesPattern(text, pattern string) bool {
	// Use filepath.Match which supports * wildcards
	matched, _ := filepath.Match(pattern, text)
	return matched
}

// matchesAnyPattern checks if a string matches any of the given patterns
func matchesAnyPattern(text string, patterns []string) bool {
	for _, pattern := range patterns {
		// Clean up the pattern (remove extra spaces)
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		// Check exact match first (for backward compatibility)
		if pattern == text {
			return true
		}

		// Check wildcard pattern match
		if matchesPattern(text, pattern) {
			return true
		}
	}
	return false
}
