package copier

import (
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config with connection strings",
			config: &Config{
				SourceConn: "postgres://user:pass@localhost:5432/sourcedb",
				DestConn:   "postgres://user:pass@localhost:5433/destdb",
				Parallel:   4,
				BatchSize:  1000,
			},
			wantErr: false,
		},
		{
			name: "valid config with files",
			config: &Config{
				SourceFile: "source.conf",
				DestFile:   "dest.conf",
				Parallel:   2,
				BatchSize:  500,
			},
			wantErr: false,
		},
		{
			name: "missing source",
			config: &Config{
				DestConn:  "postgres://user:pass@localhost:5433/destdb",
				Parallel:  4,
				BatchSize: 1000,
			},
			wantErr: true,
		},
		{
			name: "missing destination",
			config: &Config{
				SourceConn: "postgres://user:pass@localhost:5432/sourcedb",
				Parallel:   4,
				BatchSize:  1000,
			},
			wantErr: true,
		},
		{
			name: "invalid parallel workers",
			config: &Config{
				SourceConn: "postgres://user:pass@localhost:5432/sourcedb",
				DestConn:   "postgres://user:pass@localhost:5433/destdb",
				Parallel:   0,
				BatchSize:  1000,
			},
			wantErr: true,
		},
		{
			name: "invalid batch size",
			config: &Config{
				SourceConn: "postgres://user:pass@localhost:5432/sourcedb",
				DestConn:   "postgres://user:pass@localhost:5433/destdb",
				Parallel:   4,
				BatchSize:  50,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCopier_shouldSkipTable(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		schema       string
		table        string
		expectedSkip bool
	}{
		{
			name:         "no filters - should not skip",
			config:       &Config{},
			schema:       "public",
			table:        "users",
			expectedSkip: false,
		},
		{
			name: "include list - table in list",
			config: &Config{
				IncludeTables: []string{"users", "orders"},
			},
			schema:       "public",
			table:        "users",
			expectedSkip: false,
		},
		{
			name: "include list - table not in list",
			config: &Config{
				IncludeTables: []string{"users", "orders"},
			},
			schema:       "public",
			table:        "logs",
			expectedSkip: true,
		},
		{
			name: "exclude list - table in list",
			config: &Config{
				ExcludeTables: []string{"logs", "temp"},
			},
			schema:       "public",
			table:        "logs",
			expectedSkip: true,
		},
		{
			name: "exclude list - table not in list",
			config: &Config{
				ExcludeTables: []string{"logs", "temp"},
			},
			schema:       "public",
			table:        "users",
			expectedSkip: false,
		},
		{
			name: "full table name in include list",
			config: &Config{
				IncludeTables: []string{"public.users", "auth.sessions"},
			},
			schema:       "public",
			table:        "users",
			expectedSkip: false,
		},
		{
			name: "full table name not in include list",
			config: &Config{
				IncludeTables: []string{"public.users", "auth.sessions"},
			},
			schema:       "public",
			table:        "orders",
			expectedSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Copier{config: tt.config}
			result := c.shouldSkipTable(tt.schema, tt.table)
			if result != tt.expectedSkip {
				t.Errorf("shouldSkipTable() = %v, want %v", result, tt.expectedSkip)
			}
		})
	}
}
