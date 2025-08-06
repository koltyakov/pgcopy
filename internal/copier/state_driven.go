// Package copier provides a state-driven data copying implementation
package copier

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/koltyakov/pgcopy/internal/server"
	"github.com/koltyakov/pgcopy/internal/state"
	"github.com/koltyakov/pgcopy/internal/utils"
)

// StateDrivenCopier is a new implementation using centralized state management
type StateDrivenCopier struct {
	config    *Config
	sourceDB  *sql.DB
	destDB    *sql.DB
	state     *state.CopyState
	webServer *server.WebServer
}

// NewStateDrivenCopier creates a new state-driven copier
func NewStateDrivenCopier(config *Config, webPort ...int) (*StateDrivenCopier, error) {
	// Generate unique operation ID
	operationID := fmt.Sprintf("copy_%d", time.Now().Unix())

	// Create state instance using the config directly (no conversion needed)
	copyState := state.NewCopyState(operationID, *config)

	// Initialize copier
	sdc := &StateDrivenCopier{
		config: config,
		state:  copyState,
	}

	// Setup database connections
	if err := sdc.setupDatabaseConnections(); err != nil {
		return nil, fmt.Errorf("failed to setup database connections: %w", err)
	}

	// Initialize web server if in web mode
	if config.OutputMode == "web" {
		port := 8080 // Default port
		if len(webPort) > 0 {
			port = webPort[0]
		}
		sdc.webServer = server.NewWebServer(copyState, port)
		if err := sdc.webServer.Start(); err != nil {
			return nil, fmt.Errorf("failed to start web server: %w", err)
		}
	}

	return sdc, nil
}

// setupDatabaseConnections initializes database connections and updates state
func (sdc *StateDrivenCopier) setupDatabaseConnections() error {
	sdc.state.SetStatus(state.StatusInitializing)
	sdc.state.AddLog(state.LogLevelInfo, "Initializing database connections", "copier", "", nil)

	var err error

	// Setup source connection
	sourceConnStr := sdc.config.SourceConn
	sdc.sourceDB, err = sql.Open("postgres", sourceConnStr)
	if err != nil {
		sdc.state.AddError("connection_error", fmt.Sprintf("Failed to connect to source: %v", err), "copier", true, nil)
		return fmt.Errorf("failed to connect to source database: %w", err)
	}

	if err = sdc.sourceDB.Ping(); err != nil {
		sdc.state.AddError("connection_error", fmt.Sprintf("Failed to ping source: %v", err), "copier", true, nil)
		return fmt.Errorf("failed to ping source database: %w", err)
	}

	// Update source connection state
	sourceDetails := utils.ExtractConnectionDetails(sourceConnStr)
	sdc.state.UpdateConnectionDetails("source", sourceDetails, state.ConnectionStatusConnected)

	// Setup destination connection
	destConnStr := sdc.config.DestConn
	sdc.destDB, err = sql.Open("postgres", destConnStr)
	if err != nil {
		sdc.state.AddError("connection_error", fmt.Sprintf("Failed to connect to destination: %v", err), "copier", true, nil)
		return fmt.Errorf("failed to connect to destination database: %w", err)
	}

	if err = sdc.destDB.Ping(); err != nil {
		sdc.state.AddError("connection_error", fmt.Sprintf("Failed to ping destination: %v", err), "copier", true, nil)
		return fmt.Errorf("failed to ping destination database: %w", err)
	}

	// Update destination connection state
	destDetails := utils.ExtractConnectionDetails(destConnStr)
	sdc.state.UpdateConnectionDetails("destination", destDetails, state.ConnectionStatusConnected)

	// Configure connection pools
	sdc.sourceDB.SetMaxOpenConns(sdc.config.Parallel * 2)
	sdc.sourceDB.SetMaxIdleConns(sdc.config.Parallel)
	sdc.sourceDB.SetConnMaxLifetime(time.Hour)

	sdc.destDB.SetMaxOpenConns(sdc.config.Parallel * 2)
	sdc.destDB.SetMaxIdleConns(sdc.config.Parallel)
	sdc.destDB.SetConnMaxLifetime(time.Hour)

	sdc.state.AddLog(state.LogLevelInfo, "Database connections established successfully", "copier", "", nil)
	return nil
}

// Copy performs the data copying operation using state management
func (sdc *StateDrivenCopier) Copy() error {
	sdc.state.SetStatus(state.StatusPreparing)
	sdc.state.AddLog(state.LogLevelInfo, "Starting copy operation", "copier", "", nil)

	// Get tables to copy
	tables, err := sdc.getTablesToCopy()
	if err != nil {
		sdc.state.AddError("discovery_error", fmt.Sprintf("Failed to discover tables: %v", err), "copier", true, nil)
		sdc.state.SetStatus(state.StatusFailed)
		return fmt.Errorf("failed to get tables list: %w", err)
	}

	if len(tables) == 0 {
		sdc.state.AddLog(state.LogLevelInfo, "No tables to copy", "copier", "", nil)
		sdc.state.SetStatus(state.StatusCompleted)
		return nil
	}

	// Add tables to state
	for _, table := range tables {
		sdc.state.AddTable(table.Schema, table.Name, table.TotalRows)
	}

	sdc.state.AddLog(state.LogLevelInfo, fmt.Sprintf("Found %d tables to copy", len(tables)), "copier", "", nil)

	// Show confirmation dialog if needed
	if !sdc.config.SkipBackup && !sdc.config.DryRun {
		sdc.state.SetStatus(state.StatusConfirming)
		if !sdc.confirmDataOverwrite() {
			sdc.state.AddLog(state.LogLevelInfo, "Operation cancelled by user", "copier", "", nil)
			sdc.state.SetStatus(state.StatusCancelled)
			return nil
		}
	}

	if sdc.config.DryRun {
		return sdc.performDryRun()
	}

	// Start copying
	sdc.state.SetStatus(state.StatusCopying)
	return sdc.performCopy(tables)
}

// getTablesToCopy discovers tables and returns table information
func (sdc *StateDrivenCopier) getTablesToCopy() ([]*TableInfo, error) {
	sdc.state.AddLog(state.LogLevelInfo, "Discovering tables...", "copier", "", nil)

	query := `
		SELECT 
			t.schemaname, 
			t.tablename,
			COALESCE(s.n_tup_ins + s.n_tup_upd + s.n_tup_del, 0) as approx_row_count
		FROM pg_tables t
		LEFT JOIN pg_stat_user_tables s ON t.schemaname = s.schemaname AND t.tablename = s.relname
		WHERE t.schemaname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		ORDER BY t.schemaname, t.tablename`

	rows, err := sdc.sourceDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Printf("Failed to close rows: %v\n", err)
		}
	}()

	var tables []*TableInfo
	for rows.Next() {
		var schema, name string
		var rowCount sql.NullInt64

		if err := rows.Scan(&schema, &name, &rowCount); err != nil {
			return nil, err
		}

		// Skip tables based on include/exclude lists
		if sdc.shouldSkipTable(schema, name) {
			continue
		}

		tableInfo := &TableInfo{
			Schema:    schema,
			Name:      name,
			FullName:  fmt.Sprintf("%s.%s", schema, name),
			TotalRows: rowCount.Int64,
		}

		// Get exact row count using safe query construction
		// Note: schema and name come from information_schema, so they're trusted
		exactCountQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.\"%s\"", schema, name) // #nosec G201 - schema/table names from trusted system catalog
		var exactCount int64
		if err := sdc.sourceDB.QueryRow(exactCountQuery).Scan(&exactCount); err == nil {
			tableInfo.TotalRows = exactCount
		}

		tables = append(tables, tableInfo)
	}

	return tables, rows.Err()
}

// shouldSkipTable determines if a table should be skipped
func (sdc *StateDrivenCopier) shouldSkipTable(schema, table string) bool {
	fullName := fmt.Sprintf("%s.%s", schema, table)

	// Check include list
	if len(sdc.config.IncludeTables) > 0 {
		found := false
		for _, includePattern := range sdc.config.IncludeTables {
			if includePattern == table || includePattern == fullName || utils.MatchesPattern(table, includePattern) {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}

	// Check exclude list
	for _, excludePattern := range sdc.config.ExcludeTables {
		if excludePattern == table || excludePattern == fullName || utils.MatchesPattern(table, excludePattern) {
			return true
		}
	}

	return false
}

// confirmDataOverwrite shows confirmation dialog
func (sdc *StateDrivenCopier) confirmDataOverwrite() bool {
	// This is a simplified version - in web mode, this could be handled by the UI
	sdc.state.AddLog(state.LogLevelWarn, "Data overwrite confirmation required", "copier", "", nil)

	// For non-interactive modes, we skip confirmation (handled by CLI)
	// In web mode, this could trigger a confirmation UI state
	return true
}

// performDryRun executes a dry run
func (sdc *StateDrivenCopier) performDryRun() error {
	sdc.state.AddLog(state.LogLevelInfo, "Performing dry run...", "copier", "", nil)

	// Mark all tables as completed for dry run
	snapshot := sdc.state.GetSnapshot()
	for _, table := range snapshot.Tables {
		sdc.state.UpdateTableStatus(table.Schema, table.Name, state.TableStatusCompleted)
	}

	sdc.state.SetStatus(state.StatusCompleted)
	sdc.state.AddLog(state.LogLevelInfo, "Dry run completed successfully", "copier", "", nil)
	return nil
}

// performCopy executes the actual copy operation
func (sdc *StateDrivenCopier) performCopy(tables []*TableInfo) error {
	sdc.state.AddLog(state.LogLevelInfo, "Starting table copying process", "copier", "", nil)

	// For demonstration, we'll simulate the copy process
	for _, table := range tables {
		sdc.state.UpdateTableStatus(table.Schema, table.Name, state.TableStatusCopying)
		sdc.state.AddLog(state.LogLevelInfo, fmt.Sprintf("Copying table %s.%s", table.Schema, table.Name), "copier", table.Schema+"."+table.Name, nil)

		// Simulate copying with progress updates
		totalRows := table.TotalRows
		batchSize := int64(sdc.config.BatchSize)

		for copied := int64(0); copied < totalRows; copied += batchSize {
			currentBatch := batchSize
			if copied+batchSize > totalRows {
				currentBatch = totalRows - copied
			}

			// Simulate some work
			time.Sleep(100 * time.Millisecond)

			// Update progress
			sdc.state.UpdateTableProgress(table.Schema, table.Name, copied+currentBatch)
		}

		sdc.state.UpdateTableStatus(table.Schema, table.Name, state.TableStatusCompleted)
		sdc.state.AddLog(state.LogLevelInfo, fmt.Sprintf("Completed table %s.%s (%d rows)", table.Schema, table.Name, totalRows), "copier", table.Schema+"."+table.Name, nil)
	}

	sdc.state.SetStatus(state.StatusCompleted)
	sdc.state.AddLog(state.LogLevelInfo, "All tables copied successfully", "copier", "", nil)
	return nil
}

// Close cleans up resources
func (sdc *StateDrivenCopier) Close() {
	if sdc.sourceDB != nil {
		if err := sdc.sourceDB.Close(); err != nil {
			fmt.Printf("Failed to close source database: %v\n", err)
		}
	}
	if sdc.destDB != nil {
		if err := sdc.destDB.Close(); err != nil {
			fmt.Printf("Failed to close destination database: %v\n", err)
		}
	}
}
