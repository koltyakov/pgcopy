package utils // nolint:revive // utils is an acceptable name for internal utility package

import "testing"

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		text     string
		pattern  string
		expected bool
	}{
		// Exact matches
		{"users", "users", true},
		{"users", "orders", false},

		// Wildcard patterns
		{"temp_users", "temp_*", true},
		{"temp_orders", "temp_*", true},
		{"users", "temp_*", false},

		{"user_logs", "*_logs", true},
		{"system_logs", "*_logs", true},
		{"logs", "*_logs", false},

		{"cache_users", "*cache*", true},
		{"user_cache_data", "*cache*", true},
		{"users", "*cache*", false},

		{"test_user_data", "test_*_data", true},
		{"test_order_data", "test_*_data", true},
		{"test_data", "test_*_data", false},
		{"user_data", "test_*_data", false},

		// Multiple wildcards
		{"temp_cache_data", "*cache*", true},
		{"temp_cache_data", "temp_*", true},
		{"temp_cache_data", "*_data", true},
	}

	for _, tt := range tests {
		t.Run(tt.text+"_"+tt.pattern, func(t *testing.T) {
			result := MatchesPattern(tt.text, tt.pattern)
			if result != tt.expected {
				t.Errorf("MatchesPattern(%s, %s) = %v, want %v", tt.text, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestMatchesAnyPattern(t *testing.T) {
	tests := []struct {
		text     string
		patterns []string
		expected bool
	}{
		{"users", []string{"users", "orders"}, true},
		{"users", []string{"orders", "products"}, false},
		{"temp_users", []string{"temp_*", "cache_*"}, true},
		{"cache_data", []string{"temp_*", "cache_*"}, true},
		{"user_logs", []string{"*_logs", "*_cache"}, true},
		{"normal_table", []string{"temp_*", "*_logs", "*_cache"}, false},
		{"", []string{"", "users"}, false},
		{"users", []string{""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			result := MatchesAnyPattern(tt.text, tt.patterns)
			if result != tt.expected {
				t.Errorf("MatchesAnyPattern(%s, %v) = %v, want %v", tt.text, tt.patterns, result, tt.expected)
			}
		})
	}
}

func TestMatchesTablePattern(t *testing.T) {
	tests := []struct {
		schema   string
		table    string
		pattern  string
		expected bool
	}{
		{"public", "users", "users", true},
		{"public", "users", "public.users", true},
		{"auth", "sessions", "public.users", false},
		{"auth", "sessions", "auth.*", true},
		{"auth", "sessions", "*.sessions", true},
		{"auth", "sessions", "*.*", true},
		{"sales", "order_items", "order_*", true},
		{"sales", "order_items", "sales.order_*", true},
		{"sales", "order_items", "prod.*", false},
		{"sales", "order_items", "*.users", false},
		{"sales", "order_items", "", false},
	}

	for _, tt := range tests {
		if got := MatchesTablePattern(tt.schema, tt.table, tt.pattern); got != tt.expected {
			t.Errorf("MatchesTablePattern(%q,%q,%q) = %v, want %v", tt.schema, tt.table, tt.pattern, got, tt.expected)
		}
	}
}

func TestMatchesAnyTablePattern(t *testing.T) {
	tests := []struct {
		schema   string
		table    string
		patterns []string
		expected bool
	}{
		{"public", "users", []string{"orders", "public.users"}, true},
		{"public", "users", []string{"orders", "auth.*"}, false},
		{"auth", "sessions", []string{"*.sessions", "public.*"}, true},
		{"sales", "order_items", []string{"inventory.*", "prod.*"}, false},
	}

	for _, tt := range tests {
		if got := MatchesAnyTablePattern(tt.schema, tt.table, tt.patterns); got != tt.expected {
			t.Errorf("MatchesAnyTablePattern(%q,%q,%v) = %v, want %v", tt.schema, tt.table, tt.patterns, got, tt.expected)
		}
	}
}
