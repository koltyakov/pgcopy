package copier

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/schollz/progressbar/v3"
)

// progressBarWriter is a custom writer that ensures log messages
// appear below the progress bar without interfering with it
type progressBarWriter struct {
	progressBar *progressbar.ProgressBar
}

func (w *progressBarWriter) Write(p []byte) (n int, err error) {
	if w.progressBar != nil {
		// Clear the progress bar temporarily, write the message, then redraw
		fmt.Fprint(os.Stderr, "\r\033[K") // Clear current line
		n, err = os.Stderr.Write(p)
		// The progress bar will redraw itself on the next update
		return n, err
	}
	return os.Stderr.Write(p)
}

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
	ProgressBar   bool
}

// Copier handles the data copying operation
type Copier struct {
	config      *Config
	sourceDB    *sql.DB
	destDB      *sql.DB
	fkManager   *ForeignKeyManager
	mu          sync.Mutex
	stats       *CopyStats
	lastUpdate  time.Time
	progressBar *progressbar.ProgressBar
	logger      *log.Logger
}

// CopyStats tracks copying statistics
type CopyStats struct {
	TablesProcessed     int64
	RowsCopied          int64
	TotalTables         int64
	TotalRows           int64
	ForeignKeysDetected int64
	ForeignKeysDropped  int64
	StartTime           time.Time
	Errors              []error
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

	// Initialize foreign key manager
	c.fkManager = NewForeignKeyManager(c.destDB, c)

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
		c.logWarn("Failed to calculate total rows: %v", err)
		totalRows = 0
	}
	c.stats.TotalRows = totalRows

	if c.config.DryRun {
		return c.dryRun(tables)
	}

	// Initialize progress tracking
	c.lastUpdate = time.Now()
	if c.config.ProgressBar && totalRows > 0 {
		// Create a custom progress bar that stays at the top with modern styling
		c.progressBar = progressbar.NewOptions64(totalRows,
			progressbar.OptionSetDescription(fmt.Sprintf("Copying %d tables", len(tables))),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionSetPredictTime(true),
			progressbar.OptionFullWidth(),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "█",
				SaucerHead:    "█",
				SaucerPadding: "░",
				BarStart:      "▐",
				BarEnd:        "▌",
			}),
		)

		// Initialize custom logger that works with progress bar
		writer := &progressBarWriter{progressBar: c.progressBar}
		c.logger = log.New(writer, "", log.LstdFlags)

		// Set the global logger to use our custom writer when progress bar is active
		log.SetOutput(writer)
	} else if totalRows > 0 {
		fmt.Printf("Starting copy of %d tables (%s rows) with %d workers\n",
			len(tables), formatNumber(totalRows), c.config.Parallel)
		c.logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	// Detect and handle foreign keys
	if err := c.fkManager.DetectForeignKeys(tables); err != nil {
		return fmt.Errorf("failed to detect foreign keys: %w", err)
	}

	// Try to use replica mode first, fall back to dropping FKs if needed
	if err := c.fkManager.TryUseReplicaMode(); err != nil {
		return fmt.Errorf("failed to setup foreign key handling: %w", err)
	}

	// Copy tables in parallel
	err = c.copyTablesParallel(tables)

	if err != nil {
		return err
	}

	// Try to recover any remaining FKs from backup file before cleanup
	if recoveryErr := c.fkManager.RecoverFromBackupFile(); recoveryErr != nil {
		c.logWarn("Failed to recover FKs from backup file: %v", recoveryErr)
	}

	// Clean up backup file on successful completion
	if cleanupErr := c.fkManager.CleanupBackupFile(); cleanupErr != nil {
		c.logWarn("Failed to cleanup FK backup file: %v", cleanupErr)
	}

	// Finish progress bar if enabled
	if c.config.ProgressBar && c.progressBar != nil {
		c.progressBar.Finish()
		fmt.Println() // Add a newline after progress bar
		// Restore the original logger output
		log.SetOutput(os.Stderr)
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
			c.logWarn("Failed to get columns for %s: %v", highlightTableName(schema, name), err)
			continue
		}

		tables = append(tables, tableInfo)
	}

	return tables, rows.Err()
}

// shouldSkipTable determines if a table should be skipped based on include/exclude lists
// Supports wildcard patterns using * for any sequence of characters
func (c *Copier) shouldSkipTable(schema, table string) bool {
	fullName := fmt.Sprintf("%s.%s", schema, table)

	// If include list is specified, only include tables in the list
	if len(c.config.IncludeTables) > 0 {
		found := false
		for _, includePattern := range c.config.IncludeTables {
			includePattern = strings.TrimSpace(includePattern)
			if includePattern == "" {
				continue
			}

			// Check exact match first
			if includePattern == table || includePattern == fullName {
				found = true
				break
			}

			// Check wildcard pattern match for table name
			if matchesPattern(table, includePattern) {
				found = true
				break
			}

			// Check wildcard pattern match for full name (schema.table)
			if matchesPattern(fullName, includePattern) {
				found = true
				break
			}
		}
		if !found {
			return true // Not in include list
		}
	}

	// Check exclude list
	for _, excludePattern := range c.config.ExcludeTables {
		excludePattern = strings.TrimSpace(excludePattern)
		if excludePattern == "" {
			continue
		}

		// Check exact match first
		if excludePattern == table || excludePattern == fullName {
			return true
		}

		// Check wildcard pattern match for table name
		if matchesPattern(table, excludePattern) {
			return true
		}

		// Check wildcard pattern match for full name (schema.table)
		if matchesPattern(fullName, excludePattern) {
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
		fmt.Printf("  %s.%s (%s rows)\n", table.Schema, table.Name, formatNumber(table.RowCount))
		totalRows += table.RowCount
	}

	fmt.Println("==========================================")
	fmt.Printf("Total: %d tables, %s rows\n", len(tables), formatNumber(totalRows))
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

// updateProgress prints progress updates periodically
func (c *Copier) updateProgress(rowsAdded int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.RowsCopied += rowsAdded

	if c.config.ProgressBar && c.progressBar != nil {
		// Calculate speed
		elapsed := time.Since(c.stats.StartTime)
		var speedStr string
		if elapsed.Seconds() > 0 {
			rowsPerSecond := float64(c.stats.RowsCopied) / elapsed.Seconds()
			speedStr = fmt.Sprintf(" (%s/s)", formatNumber(int64(rowsPerSecond)))
		}

		// Update progress bar description with remaining tables, formatted row counts, and speed
		description := fmt.Sprintf("Copying rows (%d/%d tables) %s/%s%s",
			c.stats.TablesProcessed, c.stats.TotalTables,
			formatNumber(c.stats.RowsCopied), formatNumber(c.stats.TotalRows), speedStr)
		c.progressBar.Describe(description)

		// Update progress bar by the number of rows added
		c.progressBar.Add64(rowsAdded)
	} else {
		now := time.Now()
		if now.Sub(c.lastUpdate) > 5*time.Second { // Update every 5 seconds
			c.lastUpdate = now
			percentage := float64(c.stats.RowsCopied) / float64(c.stats.TotalRows) * 100
			if c.stats.TotalRows > 0 {
				fmt.Printf("Progress: %s/%s rows (%.1f%%) - %d tables processed\n",
					formatNumber(c.stats.RowsCopied), formatNumber(c.stats.TotalRows), percentage, c.stats.TablesProcessed)
			}
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
