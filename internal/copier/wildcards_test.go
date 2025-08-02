package copier

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
			result := matchesPattern(tt.text, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesPattern(%s, %s) = %v, want %v", tt.text, tt.pattern, result, tt.expected)
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
			result := matchesAnyPattern(tt.text, tt.patterns)
			if result != tt.expected {
				t.Errorf("matchesAnyPattern(%s, %v) = %v, want %v", tt.text, tt.patterns, result, tt.expected)
			}
		})
	}
}

func TestShouldSkipTable_Wildcards(t *testing.T) {
	tests := []struct {
		name          string
		schema        string
		table         string
		includeTables []string
		excludeTables []string
		expectedSkip  bool
	}{
		{
			name:          "include wildcard match",
			schema:        "public",
			table:         "temp_users",
			includeTables: []string{"temp_*"},
			expectedSkip:  false,
		},
		{
			name:          "include wildcard no match",
			schema:        "public",
			table:         "users",
			includeTables: []string{"temp_*"},
			expectedSkip:  true,
		},
		{
			name:          "exclude wildcard match",
			schema:        "public",
			table:         "temp_users",
			excludeTables: []string{"temp_*"},
			expectedSkip:  true,
		},
		{
			name:          "exclude wildcard no match",
			schema:        "public",
			table:         "users",
			excludeTables: []string{"temp_*"},
			expectedSkip:  false,
		},
		{
			name:          "mixed exact and wildcard include",
			schema:        "public",
			table:         "users",
			includeTables: []string{"users", "temp_*", "*_logs"},
			expectedSkip:  false,
		},
		{
			name:          "mixed exact and wildcard exclude",
			schema:        "public",
			table:         "temp_cache",
			excludeTables: []string{"logs", "temp_*", "*_cache"},
			expectedSkip:  true,
		},
		{
			name:          "full name wildcard match",
			schema:        "temp",
			table:         "users",
			excludeTables: []string{"temp.*"},
			expectedSkip:  true,
		},
		{
			name:          "suffix wildcard",
			schema:        "public",
			table:         "user_logs",
			excludeTables: []string{"*_logs"},
			expectedSkip:  true,
		},
		{
			name:          "contains wildcard",
			schema:        "public",
			table:         "user_cache_data",
			excludeTables: []string{"*cache*"},
			expectedSkip:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Copier{
				config: &Config{
					IncludeTables: tt.includeTables,
					ExcludeTables: tt.excludeTables,
				},
			}

			result := c.shouldSkipTable(tt.schema, tt.table)
			if result != tt.expectedSkip {
				t.Errorf("shouldSkipTable() = %v, want %v", result, tt.expectedSkip)
			}
		})
	}
}
