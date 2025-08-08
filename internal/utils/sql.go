package utils // nolint:revive // utils is an acceptable name for internal utility package

import "strings"

// QuoteIdent returns a PostgreSQL-quoted identifier with embedded quotes escaped.
// Example: My"Tbl -> "My""Tbl"
func QuoteIdent(s string) string {
	replaced := strings.ReplaceAll(s, "\"", "\"\"")
	return "\"" + replaced + "\""
}

// QuoteTable returns a fully-qualified, quoted table name: "schema"."table".
func QuoteTable(schema, table string) string {
	return QuoteIdent(schema) + "." + QuoteIdent(table)
}

// QuoteJoinIdents quotes each identifier and joins them with comma+space.
func QuoteJoinIdents(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = QuoteIdent(c)
	}
	return strings.Join(q, ", ")
}

// QuoteLiteral returns a single-quoted SQL string literal with embedded quotes escaped.
func QuoteLiteral(s string) string {
	replaced := strings.ReplaceAll(s, "'", "''")
	return "'" + replaced + "'"
}
