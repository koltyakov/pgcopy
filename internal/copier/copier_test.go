package copier

import (
	"testing"

	"github.com/koltyakov/pgcopy/internal/state"
)

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "complete config",
			config: &Config{
				SourceConn:    "source connection",
				DestConn:      "dest connection",
				Parallel:      4,
				BatchSize:     1000,
				IncludeTables: []string{"users", "orders"},
				ExcludeTables: []string{"logs", "temp"},
				DryRun:        true,
				SkipBackup:    false,
				OutputMode:    "progress",
			},
		},
		{
			name: "minimal config",
			config: &Config{
				SourceConn: "source connection",
				DestConn:   "dest connection",
				Parallel:   2,
				BatchSize:  500,
				OutputMode: "raw",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that the config values are properly accessible
			if tt.config.Parallel <= 0 {
				t.Errorf("Expected positive parallel value, got %d", tt.config.Parallel)
			}
			if tt.config.BatchSize <= 0 {
				t.Errorf("Expected positive batch size, got %d", tt.config.BatchSize)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config with connections",
			config: &Config{
				SourceConn: "source connection string",
				DestConn:   "dest connection string",
				Parallel:   2,
				BatchSize:  1000,
			},
			wantErr: false,
		},
		{
			name: "valid config with files",
			config: &Config{
				SourceFile: "source.conn",
				DestFile:   "dest.conn",
				Parallel:   1,
				BatchSize:  100,
			},
			wantErr: false,
		},
		{
			name: "missing source",
			config: &Config{
				DestConn:  "dest connection string",
				Parallel:  2,
				BatchSize: 1000,
			},
			wantErr: true,
		},
		{
			name: "missing destination",
			config: &Config{
				SourceConn: "source connection string",
				Parallel:   2,
				BatchSize:  1000,
			},
			wantErr: true,
		},
		{
			name: "invalid parallel workers",
			config: &Config{
				SourceConn: "source connection string",
				DestConn:   "dest connection string",
				Parallel:   0,
				BatchSize:  1000,
			},
			wantErr: true,
		},
		{
			name: "invalid batch size",
			config: &Config{
				SourceConn: "source connection string",
				DestConn:   "dest connection string",
				Parallel:   2,
				BatchSize:  50,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDisplayMode_StringConversion(t *testing.T) {
	tests := []struct {
		name string
		mode DisplayMode
		want string
	}{
		{"raw mode", DisplayModeRaw, "raw"},
		{"progress mode", DisplayModeProgress, "progress"},
		{"interactive mode", DisplayModeInteractive, "interactive"},
		{"web mode", DisplayModeWeb, "web"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.mode) != tt.want {
				t.Errorf("DisplayMode string = %v, want %v", string(tt.mode), tt.want)
			}
		})
	}
}

func TestShouldSkipTable_WildcardPatterns(t *testing.T) {
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
		{
			name: "wildcard include - matches",
			config: &Config{
				IncludeTables: []string{"user_*", "order_*"},
			},
			schema:       "public",
			table:        "user_profiles",
			expectedSkip: false,
		},
		{
			name: "wildcard include - doesn't match",
			config: &Config{
				IncludeTables: []string{"user_*", "order_*"},
			},
			schema:       "public",
			table:        "logs",
			expectedSkip: true,
		},
		{
			name: "wildcard exclude - matches",
			config: &Config{
				ExcludeTables: []string{"temp_*", "*_logs"},
			},
			schema:       "public",
			table:        "temp_data",
			expectedSkip: true,
		},
		{
			name: "wildcard exclude - doesn't match",
			config: &Config{
				ExcludeTables: []string{"temp_*", "*_logs"},
			},
			schema:       "public",
			table:        "users",
			expectedSkip: false,
		},
		{
			name: "schema wildcard - matches",
			config: &Config{
				IncludeTables: []string{"auth.*", "public.users"},
			},
			schema:       "auth",
			table:        "sessions",
			expectedSkip: false,
		},
		{
			name: "schema wildcard - doesn't match",
			config: &Config{
				IncludeTables: []string{"auth.*", "public.users"},
			},
			schema:       "logs",
			table:        "error_logs",
			expectedSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copier with the test config
			copyState := state.NewCopyState("test", *tt.config)
			copier := &Copier{
				config: tt.config,
				state:  copyState,
			}

			result := copier.shouldSkipTable(tt.schema, tt.table)
			if result != tt.expectedSkip {
				t.Errorf("shouldSkipTable(%q, %q) = %v, want %v", tt.schema, tt.table, result, tt.expectedSkip)
			}
		})
	}
}

func TestCopier_StateIntegration(t *testing.T) {
	config := &Config{
		SourceConn: "test source",
		DestConn:   "test dest",
		Parallel:   2,
		BatchSize:  500,
		OutputMode: "raw",
	}

	copyState := state.NewCopyState("test-operation", *config)
	copier := &Copier{
		config:           config,
		state:            copyState,
		tablesInProgress: make(map[string]bool),
	}

	// Test initial state
	if copier.state.Summary.TotalTables != 0 {
		t.Errorf("Expected TotalTables = 0, got %d", copier.state.Summary.TotalTables)
	}

	if copier.state.Summary.SyncedRows != 0 {
		t.Errorf("Expected SyncedRows = 0, got %d", copier.state.Summary.SyncedRows)
	}

	// Test progress updates
	copier.updateProgress(100)
	if copier.state.Summary.SyncedRows != 100 {
		t.Errorf("Expected SyncedRows = 100, got %d", copier.state.Summary.SyncedRows)
	}

	copier.updateProgress(50)
	if copier.state.Summary.SyncedRows != 150 {
		t.Errorf("Expected SyncedRows = 150, got %d", copier.state.Summary.SyncedRows)
	}

	// Test display mode
	if copier.getDisplayMode() != DisplayModeRaw {
		t.Errorf("Expected DisplayModeRaw, got %v", copier.getDisplayMode())
	}
}

func TestValidateConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "negative parallel",
			config: &Config{
				SourceConn: "source",
				DestConn:   "dest",
				Parallel:   -1,
				BatchSize:  1000,
			},
			wantErr: true,
		},
		{
			name: "zero batch size",
			config: &Config{
				SourceConn: "source",
				DestConn:   "dest",
				Parallel:   1,
				BatchSize:  0,
			},
			wantErr: true,
		},
		{
			name: "empty include and exclude lists",
			config: &Config{
				SourceConn:    "source",
				DestConn:      "dest",
				Parallel:      1,
				BatchSize:     1000,
				IncludeTables: []string{},
				ExcludeTables: []string{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCopier_WebModeIntegration(t *testing.T) {
config := &Config{
SourceConn: "test source",
DestConn:   "test dest",
Parallel:   2,
BatchSize:  500,
OutputMode: "web",
}

// Test that NewWithWebPort creates a copier for web mode
// Note: We cannot actually start the web server in tests without a real port,
// but we can test the configuration
copyState := state.NewCopyState("test-web-operation", *config)
copier := &Copier{
config:           config,
state:            copyState,
tablesInProgress: make(map[string]bool),
}

// Test that the display mode is correctly identified as web
if copier.getDisplayMode() != DisplayModeWeb {
t.Errorf("Expected DisplayModeWeb, got %v", copier.getDisplayMode())
}

// Test that config has web output mode
if config.OutputMode != "web" {
t.Errorf("Expected OutputMode = 'web', got %v", config.OutputMode)
}
}
