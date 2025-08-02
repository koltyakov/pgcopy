package utils // nolint:revive // utils is an acceptable name for internal utility package

import (
	"path/filepath"
	"strings"
)

// MatchesPattern checks if a string matches a wildcard pattern
// Supports * wildcard for matching any sequence of characters
func MatchesPattern(text, pattern string) bool {
	// Use filepath.Match which supports * wildcards
	matched, _ := filepath.Match(pattern, text)
	return matched
}

// MatchesAnyPattern checks if a string matches any of the given patterns
func MatchesAnyPattern(text string, patterns []string) bool {
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
		if MatchesPattern(text, pattern) {
			return true
		}
	}
	return false
}
