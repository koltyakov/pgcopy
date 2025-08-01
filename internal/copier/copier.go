package copier

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// Config holds the configuration for the data copy operation
type Config struct {
	SourceConn    string
	DestConn      string
	SourceFile    string
	DestFile      string
	Parallel      int
	BatchSize     int
	ExcludeTables []string
	IncludeTables []string
	Resume        bool
	DryRun        bool
}

// Copier handles the data copying operation
type Copier struct {
	config     *Config
	sourceDB   *sql.DB
	destDB     *sql.DB
	mu         sync.Mutex
	stats      *CopyStats
	lastUpdate time.Time
}

// CopyStats tracks copying statistics
type CopyStats struct {
	TablesProcessed int64
	RowsCopied      int64
	TotalTables     int64
	TotalRows       int64
	StartTime       time.Time
	Errors          []error
}

// TableInfo holds information about a table to be copied
type TableInfo struct {
	Schema    string
	Name      string
	RowCount  int64
	Columns   []string
	PKColumns []string
}

// New creates a new Copier instance
func New(config *Config) (*Copier, error) {
	c := &Copier{
		config: config,
		stats: &CopyStats{
			StartTime: time.Now(),
			Errors:    make([]error, 0),
		},
	}

	var err error

	// Connect to source database
	sourceConnStr := config.SourceConn
	if config.SourceFile != "" {
		sourceConnStr, err = readConnectionFromFile(config.SourceFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read source connection file: %w", err)
		}
	}

	c.sourceDB, err = sql.Open("postgres", sourceConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source database: %w", err)
	}

	if err = c.sourceDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping source database: %w", err)
	}

	// Connect to destination database
	destConnStr := config.DestConn
	if config.DestFile != "" {
		destConnStr, err = readConnectionFromFile(config.DestFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read destination connection file: %w", err)
		}
	}

	c.destDB, err = sql.Open("postgres", destConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to destination database: %w", err)
	}

	if err = c.destDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping destination database: %w", err)
	}

	// Configure connection pools for performance
	c.sourceDB.SetMaxOpenConns(config.Parallel * 2)
	c.sourceDB.SetMaxIdleConns(config.Parallel)
	c.sourceDB.SetConnMaxLifetime(time.Hour)

	c.destDB.SetMaxOpenConns(config.Parallel * 2)
	c.destDB.SetMaxIdleConns(config.Parallel)
	c.destDB.SetConnMaxLifetime(time.Hour)

	return c, nil
}

// Close closes database connections
func (c *Copier) Close() {
	if c.sourceDB != nil {
		c.sourceDB.Close()
	}
	if c.destDB != nil {
		c.destDB.Close()
	}
}

// Copy performs the data copying operation
func (c *Copier) Copy() error {
	// Get list of tables to copy
	tables, err := c.getTablesToCopy()
	if err != nil {
		return fmt.Errorf("failed to get tables list: %w", err)
	}

	if len(tables) == 0 {
		fmt.Println("No tables to copy")
		return nil
	}

	c.stats.TotalTables = int64(len(tables))

	// Calculate total rows for progress tracking
	totalRows, err := c.calculateTotalRows(tables)
	if err != nil {
		log.Printf("Warning: failed to calculate total rows: %v", err)
		totalRows = 0
	}
	c.stats.TotalRows = totalRows

	if c.config.DryRun {
		return c.dryRun(tables)
	}

	// Initialize progress tracking
	c.lastUpdate = time.Now()
	if totalRows > 0 {
		fmt.Printf("Starting copy of %d tables (%d rows) with %d workers\n",
			len(tables), totalRows, c.config.Parallel)
	}

	// Disable foreign key checks on destination to avoid constraint issues
	if err := c.disableForeignKeys(); err != nil {
		log.Printf("Warning: failed to disable foreign keys: %v", err)
	}

	// Copy tables in parallel
	err = c.copyTablesParallel(tables)

	// Re-enable foreign key checks
	if fkErr := c.enableForeignKeys(); fkErr != nil {
		log.Printf("Warning: failed to re-enable foreign keys: %v", fkErr)
	}

	if err != nil {
		return err
	}

	// Print final statistics
	c.printStats()

	return nil
}

// getTablesToCopy returns the list of tables to copy based on configuration
func (c *Copier) getTablesToCopy() ([]*TableInfo, error) {
	query := `
		SELECT 
			t.schemaname, 
			t.tablename,
			COALESCE(s.n_tup_ins + s.n_tup_upd + s.n_tup_del, 0) as approx_row_count
		FROM pg_tables t
		LEFT JOIN pg_stat_user_tables s ON t.schemaname = s.schemaname AND t.tablename = s.relname
		WHERE t.schemaname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		ORDER BY t.schemaname, t.tablename`

	rows, err := c.sourceDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []*TableInfo
	for rows.Next() {
		var schema, name string
		var rowCount sql.NullInt64

		if err := rows.Scan(&schema, &name, &rowCount); err != nil {
			return nil, err
		}

		// Skip tables based on include/exclude lists
		if c.shouldSkipTable(schema, name) {
			continue
		}

		tableInfo := &TableInfo{
			Schema:   schema,
			Name:     name,
			RowCount: rowCount.Int64,
		}

		// Get column information
		if err := c.getTableColumns(tableInfo); err != nil {
			log.Printf("Warning: failed to get columns for %s.%s: %v", schema, name, err)
			continue
		}

		tables = append(tables, tableInfo)
	}

	return tables, rows.Err()
}

// shouldSkipTable determines if a table should be skipped based on include/exclude lists
func (c *Copier) shouldSkipTable(schema, table string) bool {
	fullName := fmt.Sprintf("%s.%s", schema, table)

	// If include list is specified, only include tables in the list
	if len(c.config.IncludeTables) > 0 {
		found := false
		for _, includeTable := range c.config.IncludeTables {
			if includeTable == table || includeTable == fullName {
				found = true
				break
			}
		}
		if !found {
			return true // Not in include list
		}
	}

	// Check exclude list
	for _, excludeTable := range c.config.ExcludeTables {
		if excludeTable == table || excludeTable == fullName {
			return true
		}
	}

	return false
}

// getTableColumns gets column information for a table
func (c *Copier) getTableColumns(table *TableInfo) error {
	// Get all columns
	query := `
		SELECT column_name 
		FROM information_schema.columns 
		WHERE table_schema = $1 AND table_name = $2 
		ORDER BY ordinal_position`

	rows, err := c.sourceDB.Query(query, table.Schema, table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	table.Columns = make([]string, 0)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return err
		}
		table.Columns = append(table.Columns, column)
	}

	// Get primary key columns
	pkQuery := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = $1::regclass AND i.indisprimary
		ORDER BY a.attnum`

	fullTableName := fmt.Sprintf("%s.%s", table.Schema, table.Name)
	pkRows, err := c.sourceDB.Query(pkQuery, fullTableName)
	if err != nil {
		// Primary key is optional, so we continue without it
		table.PKColumns = make([]string, 0)
		return nil
	}
	defer pkRows.Close()

	table.PKColumns = make([]string, 0)
	for pkRows.Next() {
		var column string
		if err := pkRows.Scan(&column); err != nil {
			return err
		}
		table.PKColumns = append(table.PKColumns, column)
	}

	return nil
}

// calculateTotalRows calculates the total number of rows across all tables
func (c *Copier) calculateTotalRows(tables []*TableInfo) (int64, error) {
	var total int64
	for _, table := range tables {
		// Get actual row count for better accuracy
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s.\"%s\"", table.Schema, table.Name)
		var count int64
		if err := c.sourceDB.QueryRow(query).Scan(&count); err != nil {
			// Use approximate count if exact count fails
			count = table.RowCount
		}
		table.RowCount = count
		total += count
	}
	return total, nil
}

// dryRun shows what would be copied without actually copying
func (c *Copier) dryRun(tables []*TableInfo) error {
	fmt.Println("DRY RUN - The following tables would be copied:")
	fmt.Println("==========================================")

	var totalRows int64
	for _, table := range tables {
		fmt.Printf("  %s.%s (%d rows)\n", table.Schema, table.Name, table.RowCount)
		totalRows += table.RowCount
	}

	fmt.Println("==========================================")
	fmt.Printf("Total: %d tables, %d rows\n", len(tables), totalRows)
	fmt.Printf("Parallel workers: %d\n", c.config.Parallel)
	fmt.Printf("Batch size: %d\n", c.config.BatchSize)

	return nil
}

// Helper function to read connection string from file
func readConnectionFromFile(filename string) (string, error) {
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// disableForeignKeys disables foreign key checks on destination database
func (c *Copier) disableForeignKeys() error {
	_, err := c.destDB.Exec("SET session_replication_role = replica")
	return err
}

// enableForeignKeys re-enables foreign key checks on destination database
func (c *Copier) enableForeignKeys() error {
	_, err := c.destDB.Exec("SET session_replication_role = default")
	return err
}

// printStats prints final copy statistics
func (c *Copier) printStats() {
	duration := time.Since(c.stats.StartTime)
	fmt.Printf("\n=== Copy Statistics ===\n")
	fmt.Printf("Tables processed: %d/%d\n", c.stats.TablesProcessed, c.stats.TotalTables)
	fmt.Printf("Rows copied: %d\n", c.stats.RowsCopied)
	fmt.Printf("Duration: %v\n", duration)

	if c.stats.RowsCopied > 0 && duration.Seconds() > 0 {
		rowsPerSecond := float64(c.stats.RowsCopied) / duration.Seconds()
		fmt.Printf("Average speed: %.0f rows/second\n", rowsPerSecond)
	}

	if len(c.stats.Errors) > 0 {
		fmt.Printf("Errors encountered: %d\n", len(c.stats.Errors))
		for i, err := range c.stats.Errors {
			fmt.Printf("  %d: %v\n", i+1, err)
		}
	}
}

// updateProgress prints progress updates periodically
func (c *Copier) updateProgress(rowsAdded int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if now.Sub(c.lastUpdate) > 5*time.Second { // Update every 5 seconds
		c.lastUpdate = now
		percentage := float64(c.stats.RowsCopied) / float64(c.stats.TotalRows) * 100
		if c.stats.TotalRows > 0 {
			fmt.Printf("Progress: %d/%d rows (%.1f%%) - %d tables processed\n",
				c.stats.RowsCopied, c.stats.TotalRows, percentage, c.stats.TablesProcessed)
		}
	}
}

// ValidateConfig validates the configuration
func ValidateConfig(config *Config) error {
	if config.SourceConn == "" && config.SourceFile == "" {
		return fmt.Errorf("either --source connection string or --source-file must be provided")
	}
	if config.DestConn == "" && config.DestFile == "" {
		return fmt.Errorf("either --dest connection string or --dest-file must be provided")
	}
	if config.Parallel < 1 {
		return fmt.Errorf("parallel workers must be at least 1")
	}
	if config.BatchSize < 100 {
		return fmt.Errorf("batch size must be at least 100")
	}
	return nil
}
