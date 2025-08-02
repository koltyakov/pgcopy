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
