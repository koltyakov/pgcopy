// Package utils provides internal utility functions for pgcopy.
//
// This package contains common utility functions including:
//   - SQL identifier quoting for injection prevention
//   - Connection string parsing and masking
//   - Logging infrastructure
//   - Formatting helpers
//
// # SQL Injection Prevention
//
// The quoting functions (QuoteIdent, QuoteTable, QuoteLiteral) implement
// PostgreSQL's identifier and literal quoting rules to prevent SQL injection.
// Always use these functions when constructing dynamic SQL with user-provided
// or database-provided identifiers.
//
// # Thread Safety
//
// All functions in this package are stateless and safe for concurrent use.
package utils // nolint:revive // utils is an acceptable name for internal utility package

import "strings"

// QuoteIdent returns a PostgreSQL-quoted identifier with embedded quotes escaped.
//
// PostgreSQL identifiers are quoted by surrounding them with double quotes.
// Embedded double quotes are escaped by doubling them.
//
// Examples:
//
//	QuoteIdent("users")        -> "\"users\""
//	QuoteIdent("My\"Table")    -> "\"My\"\"Table\""
//	QuoteIdent("public.users") -> "\"public.users\"" (dot is literal, not schema separator)
//
// Security: This function is critical for SQL injection prevention.
// Never construct SQL with unquoted identifiers from external sources.
func QuoteIdent(s string) string {
	if s == "" {
		return "\"\""
	}
	replaced := strings.ReplaceAll(s, "\"", "\"\"")
	return "\"" + replaced + "\""
}

// QuoteTable returns a fully-qualified, quoted table name: "schema"."table".
//
// This function properly handles schema and table names containing special
// characters, including dots, quotes, and spaces.
//
// Examples:
//
//	QuoteTable("public", "users")     -> "\"public\".\"users\""
//	QuoteTable("my schema", "my.tbl") -> "\"my schema\".\"my.tbl\""
//
// Security: Always use this function when constructing table references
// in dynamic SQL to prevent SQL injection.
func QuoteTable(schema, table string) string {
	return QuoteIdent(schema) + "." + QuoteIdent(table)
}

// QuoteJoinIdents quotes each identifier and joins them with comma+space.
//
// This is useful for constructing column lists in SELECT, INSERT, or UPDATE statements.
//
// Examples:
//
//	QuoteJoinIdents([]string{"id", "name"})     -> "\"id\", \"name\""
//	QuoteJoinIdents([]string{"user\"id"})       -> "\"user\"\"id\""
//	QuoteJoinIdents(nil)                         -> ""
//
// Security: Always use this function for column lists in dynamic SQL.
func QuoteJoinIdents(cols []string) string {
	if len(cols) == 0 {
		return ""
	}
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = QuoteIdent(c)
	}
	return strings.Join(q, ", ")
}

// QuoteLiteral returns a single-quoted SQL string literal with embedded quotes escaped.
//
// PostgreSQL string literals are quoted by surrounding them with single quotes.
// Embedded single quotes are escaped by doubling them.
//
// Examples:
//
//	QuoteLiteral("hello")       -> "'hello'"
//	QuoteLiteral("it's fine")   -> "'it''s fine'"
//	QuoteLiteral("")            -> "''"
//
// Note: This function does NOT handle backslash escaping (standard_conforming_strings).
// PostgreSQL's default (standard_conforming_strings = on) treats backslashes literally.
//
// Security: Use parameterized queries when possible. This function is for cases
// where dynamic SQL literals are unavoidable.
func QuoteLiteral(s string) string {
	replaced := strings.ReplaceAll(s, "'", "''")
	return "'" + replaced + "'"
}
