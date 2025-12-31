package utils

import (
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "users", `"users"`},
		{"with_space", "my table", `"my table"`},
		{"with_quotes", `my"table`, `"my""table"`},
		{"with_multiple_quotes", `a"b"c`, `"a""b""c"`},
		{"empty", "", `""`},
		{"reserved_word", "select", `"select"`},
		{"mixed_case", "MyTable", `"MyTable"`},
		{"with_number", "table1", `"table1"`},
		{"underscore", "my_table", `"my_table"`},
		{"hyphen", "my-table", `"my-table"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QuoteIdent(tt.input)
			if result != tt.expected {
				t.Errorf("QuoteIdent(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestQuoteTable(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		table    string
		expected string
	}{
		{"simple", "public", "users", `"public"."users"`},
		{"with_spaces", "my schema", "my table", `"my schema"."my table"`},
		{"with_quotes", `my"schema`, `my"table`, `"my""schema"."my""table"`},
		{"empty_schema", "", "users", `""."users"`},
		{"empty_table", "public", "", `"public".""`},
		{"reserved_words", "select", "from", `"select"."from"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QuoteTable(tt.schema, tt.table)
			if result != tt.expected {
				t.Errorf("QuoteTable(%q, %q) = %q, want %q", tt.schema, tt.table, result, tt.expected)
			}
		})
	}
}

func TestQuoteJoinIdents(t *testing.T) {
	tests := []struct {
		name     string
		cols     []string
		expected string
	}{
		{"single", []string{"id"}, `"id"`},
		{"multiple", []string{"id", "name", "email"}, `"id", "name", "email"`},
		{"empty", []string{}, ""},
		{"with_quotes", []string{`my"col`}, `"my""col"`},
		{"mixed", []string{"id", `name"1`, "email"}, `"id", "name""1", "email"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QuoteJoinIdents(tt.cols)
			if result != tt.expected {
				t.Errorf("QuoteJoinIdents(%v) = %q, want %q", tt.cols, result, tt.expected)
			}
		})
	}
}

func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "hello", "'hello'"},
		{"with_single_quote", "it's", "'it''s'"},
		{"with_multiple_quotes", "a'b'c", "'a''b''c'"},
		{"empty", "", "''"},
		{"with_spaces", "hello world", "'hello world'"},
		{"with_numbers", "123", "'123'"},
		{"sql_injection_attempt", "'; DROP TABLE users; --", "'''; DROP TABLE users; --'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QuoteLiteral(tt.input)
			if result != tt.expected {
				t.Errorf("QuoteLiteral(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestQuoteIdent_SpecialCharacters(t *testing.T) {
	// Test various special characters
	specials := []string{
		"table.name",
		"table\nname",
		"table\tname",
		"table\\name",
		"table/name",
		"@#$%",
	}

	for _, s := range specials {
		result := QuoteIdent(s)
		// Result should start and end with double quotes
		if len(result) < 2 || result[0] != '"' || result[len(result)-1] != '"' {
			t.Errorf("QuoteIdent(%q) = %q, should be quoted", s, result)
		}
	}
}

func TestQuoteLiteral_SpecialCharacters(t *testing.T) {
	// Test various special characters
	specials := []string{
		"string\nwith\nnewlines",
		"string\twith\ttabs",
		"string\\with\\backslashes",
		"unicode: 中文",
		"emoji: 🎉",
	}

	for _, s := range specials {
		result := QuoteLiteral(s)
		// Result should start and end with single quotes
		if len(result) < 2 || result[0] != '\'' || result[len(result)-1] != '\'' {
			t.Errorf("QuoteLiteral(%q) = %q, should be quoted", s, result)
		}
	}
}

func TestQuoteIdent_SQLSafety(t *testing.T) {
	// These should all be safely quoted to prevent SQL injection
	dangerous := []struct {
		input string
	}{
		{`"; DROP TABLE users; --`},
		{`" OR "1"="1`},
		{`users"; DELETE FROM users; --`},
	}

	for _, tt := range dangerous {
		result := QuoteIdent(tt.input)
		// All double quotes in input should be doubled
		// The result should be a valid quoted identifier
		if result[0] != '"' || result[len(result)-1] != '"' {
			t.Errorf("QuoteIdent(%q) = %q, not properly quoted", tt.input, result)
		}
	}
}

func TestQuoteLiteral_SQLSafety(t *testing.T) {
	// These should all be safely quoted to prevent SQL injection
	dangerous := []struct {
		input string
	}{
		{`'; DROP TABLE users; --`},
		{`' OR '1'='1`},
		{`admin'--`},
	}

	for _, tt := range dangerous {
		result := QuoteLiteral(tt.input)
		// All single quotes in input should be doubled
		// The result should be a valid quoted literal
		if result[0] != '\'' || result[len(result)-1] != '\'' {
			t.Errorf("QuoteLiteral(%q) = %q, not properly quoted", tt.input, result)
		}
	}
}
