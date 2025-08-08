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

// MatchesTablePattern checks if a pattern matches either the simple table name
// or the fully-qualified name schema.table. Patterns support * wildcards.
// Examples:
//
//	users          -> matches table name "users"
//	public.users   -> matches full name "public.users"
//	public.*       -> matches any table in schema public
//	*.users        -> matches table "users" in any schema
func MatchesTablePattern(schema, table, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	name := table
	full := schema + "." + table

	// Exact match first
	if pattern == name || pattern == full {
		return true
	}

	// Wildcard matches
	if ok, _ := filepath.Match(pattern, name); ok {
		return true
	}
	if ok, _ := filepath.Match(pattern, full); ok {
		return true
	}
	return false
}

// MatchesAnyTablePattern returns true if any pattern matches either table or schema.table.
func MatchesAnyTablePattern(schema, table string, patterns []string) bool {
	for _, p := range patterns {
		if MatchesTablePattern(schema, table, p) {
			return true
		}
	}
	return false
}
