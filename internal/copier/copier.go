// Package copier provides functionality for copying data between PostgreSQL databases.
// It handles table discovery, foreign key management, parallel copying, and progress tracking.
package copier

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/koltyakov/pgcopy/internal/utils"
	_ "github.com/lib/pq" // PostgreSQL driver
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

// DisplayMode represents different output modes for progress tracking
type DisplayMode string

const (
	// DisplayModeRaw shows minimal output, suitable for headless/CI environments (default)
	// Note: Internal name remains "raw" for backwards compatibility in code
	DisplayModeRaw DisplayMode = "raw"
	// DisplayModeProgress shows a progress bar
	DisplayModeProgress DisplayMode = "progress"
	// DisplayModeInteractive shows an interactive live display with table details
	DisplayModeInteractive DisplayMode = "interactive"
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
	SkipBackup    bool
	Mode          DisplayMode
}

// Copier handles the data copying operation
type Copier struct {
	config             *Config
	sourceDB           *sql.DB
	destDB             *sql.DB
	fkManager          *ForeignKeyManager
	mu                 sync.Mutex
	stats              *CopyStats
	lastUpdate         time.Time
	progressBar        *progressbar.ProgressBar
	fileLogger         *log.Logger         // Logger for copy.log file
	logFile            *os.File            // File handle for copy.log
	logger             *utils.SimpleLogger // Utils logger for colorized output
	tablesInProgress   map[string]bool     // Track which tables are currently being processed
	interactiveMode    bool                // Whether to use interactive display
	interactiveDisplay *InteractiveDisplay // Interactive progress display
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
		tablesInProgress: make(map[string]bool),
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

	// Initialize utils logger first
	c.logger = utils.NewSilentLogger()

	// Initialize foreign key manager
	c.fkManager = NewForeignKeyManager(c.destDB, c.logger)

	// Initialize file logger for copy.log
	if err := c.initFileLogger(); err != nil {
		return nil, fmt.Errorf("failed to initialize file logger: %w", err)
	}

	return c, nil
}

// Close closes database connections
func (c *Copier) Close() {
	if c.sourceDB != nil {
		if err := c.sourceDB.Close(); err != nil {
			c.logger.LogError("Failed to close source database connection: %v", err)
		}
	}
	if c.destDB != nil {
		if err := c.destDB.Close(); err != nil {
			c.logger.LogError("Failed to close destination database connection: %v", err)
		}
	}
	if c.logFile != nil {
		if err := c.logFile.Close(); err != nil {
			// Can't use c.logger.LogError here since we're closing the log file
			fmt.Fprintf(os.Stderr, "Failed to close copy.log file: %v\n", err)
		}
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
	totalRows := c.calculateTotalRows(tables)
	c.stats.TotalRows = totalRows

	// Show confirmation dialog unless skip-backup is enabled or dry-run mode
	if !c.config.SkipBackup && !c.config.DryRun {
		if !c.confirmDataOverwrite(tables, totalRows) {
			fmt.Println("Operation cancelled by user.")
			return nil
		}
	}

	// Log the start of copy operation
	if c.fileLogger != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		c.fileLogger.Printf("%s | COPY_START | %d tables | %d total rows", timestamp, len(tables), totalRows)
	}

	if c.config.DryRun {
		return c.dryRun(tables)
	}

	// Initialize progress tracking
	c.lastUpdate = time.Now()

	// Initialize display mode based on configuration
	c.initializeDisplayMode(tables, totalRows)

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
		c.logger.LogWarn("Failed to recover FKs from backup file: %v", recoveryErr)
	}

	// Clean up backup file on successful completion
	if cleanupErr := c.fkManager.CleanupBackupFile(); cleanupErr != nil {
		c.logger.LogWarn("Failed to cleanup FK backup file: %v", cleanupErr)
	}

	// Finish progress displays
	if c.interactiveMode && c.interactiveDisplay != nil {
		c.interactiveDisplay.Stop()
	} else if c.config.Mode == DisplayModeProgress && c.progressBar != nil {
		_ = c.progressBar.Finish()
		fmt.Println() // Add a newline after progress bar
		// Restore the original logger output
		log.SetOutput(os.Stderr)
	}

	// Print final statistics
	c.printStats()

	// Log the completion of copy operation
	if c.fileLogger != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		totalDuration := time.Since(c.stats.StartTime)
		c.fileLogger.Printf("%s | COPY_COMPLETE | %d tables | %d rows | %s",
			timestamp, c.stats.TablesProcessed, c.stats.RowsCopied, utils.FormatLogDuration(totalDuration))
	}

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
	defer func() {
		if err := rows.Close(); err != nil {
			c.logger.LogError("Failed to close rows: %v", err)
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
			c.logger.LogWarn("Failed to get columns for %s: %v", utils.HighlightTableName(schema, name), err)
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
			if utils.MatchesPattern(table, includePattern) {
				found = true
				break
			}

			// Check wildcard pattern match for full name (schema.table)
			if utils.MatchesPattern(fullName, includePattern) {
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
		if utils.MatchesPattern(table, excludePattern) {
			return true
		}

		// Check wildcard pattern match for full name (schema.table)
		if utils.MatchesPattern(fullName, excludePattern) {
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
	defer func() {
		if err := rows.Close(); err != nil {
			c.logger.LogError("Failed to close rows: %v", err)
		}
	}()

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
	defer func() {
		if err := pkRows.Close(); err != nil {
			c.logger.LogError("Failed to close primary key rows: %v", err)
		}
	}()

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
func (c *Copier) calculateTotalRows(tables []*TableInfo) int64 {
	var total int64
	for _, table := range tables {
		// Get actual row count for better accuracy
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s.\"%s\"", table.Schema, table.Name) // #nosec G201 - schema and table names from trusted database query
		var count int64
		if err := c.sourceDB.QueryRow(query).Scan(&count); err != nil {
			// Use approximate count if exact count fails
			count = table.RowCount
		}
		table.RowCount = count
		total += count
	}
	return total
}

// dryRun shows what would be copied without actually copying
func (c *Copier) dryRun(tables []*TableInfo) error {
	fmt.Println("DRY RUN - The following tables would be copied:")
	fmt.Println("==========================================")

	var totalRows int64
	for _, table := range tables {
		fmt.Printf("  %s.%s (%s rows)\n", table.Schema, table.Name, utils.FormatNumber(table.RowCount))
		totalRows += table.RowCount
	}

	fmt.Println("==========================================")
	fmt.Printf("Total: %d tables, %s rows\n", len(tables), utils.FormatNumber(totalRows))
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

	content, err := os.ReadFile(absPath) // #nosec G304 - reading user-specified configuration file
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

	// Handle different display modes
	c.handleProgressUpdate(rowsAdded)
}

// setTableInProgress marks a table as currently being processed
func (c *Copier) setTableInProgress(schema, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tableKey := fmt.Sprintf("%s.%s", schema, name)
	c.tablesInProgress[tableKey] = true
}

// removeTableFromProgress removes a table from the in-progress list
func (c *Copier) removeTableFromProgress(schema, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tableKey := fmt.Sprintf("%s.%s", schema, name)
	delete(c.tablesInProgress, tableKey)

	// Update interactive display if enabled
	if c.interactiveMode && c.interactiveDisplay != nil {
		c.interactiveDisplay.CompleteTable(schema, name, true)
	}
}

// startTableInInteractive starts tracking a table in interactive mode
func (c *Copier) startTableInInteractive(table *TableInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tableKey := fmt.Sprintf("%s.%s", table.Schema, table.Name)
	c.tablesInProgress[tableKey] = true

	// Update interactive display if enabled
	if c.interactiveMode && c.interactiveDisplay != nil {
		c.interactiveDisplay.StartTable(table.Schema, table.Name, table.RowCount)
	}
}

// updateTableProgress updates the progress of a specific table
func (c *Copier) updateTableProgress(schema, name string, copiedRows int64) {
	if c.interactiveMode && c.interactiveDisplay != nil {
		c.interactiveDisplay.UpdateTableProgress(schema, name, copiedRows)
	}
}

// getTablesInProgress returns a slice of table names currently being processed
func (c *Copier) getTablesInProgress() []string {
	tables := make([]string, 0, len(c.tablesInProgress))
	for table := range c.tablesInProgress {
		tables = append(tables, utils.HighlightTableName(strings.Split(table, ".")[0], strings.Split(table, ".")[1]))
	}
	return tables
}

// ValidateConfig validates the configuration
func ValidateConfig(config *Config) error {
	// Check for existing FK backup file that indicates unrestored foreign keys
	if err := checkForFKBackupFile(); err != nil {
		return err
	}

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

// checkForFKBackupFile checks if there's an existing FK backup file that indicates
// unrestored foreign keys from a previous run, which could cause data integrity issues
func checkForFKBackupFile() error {
	// Get the current working directory where the binary is running
	cwd, err := os.Getwd()
	if err != nil {
		return nil // If we can't get working directory, proceed anyway
	}

	backupFile := filepath.Join(cwd, ".fk_backup.sql")

	// Check if the backup file exists
	if _, err := os.Stat(backupFile); err == nil {
		return fmt.Errorf("found existing FK backup file '%s' - this indicates unrestored foreign keys from a previous run.\n"+
			"Please restore foreign keys manually by executing: psql -f %s <connection_string>\n"+
			"Or remove the file if you're certain all foreign keys are properly restored.\n"+
			"This safety check prevents potential data integrity issues", backupFile, backupFile)
	}

	return nil
}

// initFileLogger initializes the file logger for copy.log
func (c *Copier) initFileLogger() error {
	var err error
	c.logFile, err = os.OpenFile("copy.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open copy.log file: %w", err)
	}

	c.fileLogger = log.New(c.logFile, "", 0) // No prefix, we'll format our own timestamps
	return nil
}

// logTableCopy logs table copy operation to the copy.log file
func (c *Copier) logTableCopy(tableName string, rowCount int64, duration time.Duration) {
	if c.fileLogger != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		c.fileLogger.Printf("%s | %s | %d rows | %s", timestamp, tableName, rowCount, utils.FormatLogDuration(duration))
	}
}

// initializeDisplayMode initializes the appropriate display mode based on configuration
func (c *Copier) initializeDisplayMode(tables []*TableInfo, totalRows int64) {
	switch c.config.Mode {
	case DisplayModeInteractive:
		// Use interactive mode
		c.interactiveMode = true
		c.interactiveDisplay = NewInteractiveDisplay(len(tables))
		c.interactiveDisplay.SetTotalRows(totalRows) // Set total rows for overall progress
		c.interactiveDisplay.Start()
		c.logger = utils.NewSilentLogger() // Silent logger for interactive mode
		c.fkManager.SetLogger(c.logger)    // Update foreign key manager logger too

	case DisplayModeProgress:
		if totalRows > 0 {
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
			c.logger = utils.NewSimpleLogger(log.New(writer, "", log.LstdFlags))

			// Set the global logger to use our custom writer when progress bar is active
			log.SetOutput(writer)
		}

	case DisplayModeRaw:
		// Plain mode - minimal output suitable for headless/CI environments
		if totalRows > 0 {
			fmt.Printf("Starting copy of %d tables (%s rows) with %d workers\n",
				len(tables), utils.FormatNumber(totalRows), c.config.Parallel)
		}
		c.logger = utils.NewSimpleLogger(log.New(os.Stderr, "", log.LstdFlags))

	default:
		// Default to plain mode
		if totalRows > 0 {
			fmt.Printf("Starting copy of %d tables (%s rows) with %d workers\n",
				len(tables), utils.FormatNumber(totalRows), c.config.Parallel)
		}
		c.logger = utils.NewSimpleLogger(log.New(os.Stderr, "", log.LstdFlags))
	}
}

// handleProgressUpdate handles progress updates for different display modes
func (c *Copier) handleProgressUpdate(rowsAdded int64) {
	if c.interactiveMode && c.interactiveDisplay != nil {
		// Interactive mode doesn't need periodic updates here
		// Progress is updated via the interactive display methods
		return
	}

	if c.config.Mode == DisplayModeProgress && c.progressBar != nil {
		// Calculate speed
		elapsed := time.Since(c.stats.StartTime)
		var speedStr string
		if elapsed.Seconds() > 0 {
			rowsPerSecond := float64(c.stats.RowsCopied) / elapsed.Seconds()
			speedStr = fmt.Sprintf(" (%s/s)", utils.FormatNumber(int64(rowsPerSecond)))
		}

		// Update progress bar description with remaining tables, formatted row counts, and speed
		description := fmt.Sprintf("Copying rows (%d/%d tables) %s/%s%s",
			c.stats.TablesProcessed, c.stats.TotalTables,
			utils.FormatNumber(c.stats.RowsCopied), utils.FormatNumber(c.stats.TotalRows), speedStr)
		c.progressBar.Describe(description)

		// Update progress bar by the number of rows added
		_ = c.progressBar.Add64(rowsAdded)
		return
	}

	// Plain mode: text-based progress updates (only for non-headless scenarios)
	now := time.Now()
	if now.Sub(c.lastUpdate) > 5*time.Second { // Update every 5 seconds
		c.lastUpdate = now
		percentage := float64(c.stats.RowsCopied) / float64(c.stats.TotalRows) * 100
		if c.stats.TotalRows > 0 {
			fmt.Printf("Progress: %s/%s rows (%.1f%%) - %d/%d tables processed\n",
				utils.FormatNumber(c.stats.RowsCopied), utils.FormatNumber(c.stats.TotalRows), percentage, c.stats.TablesProcessed, c.stats.TotalTables)
			fmt.Printf("Syncing: %s\n", strings.Join(c.getTablesInProgress(), ", "))
		}
	}
}

// confirmDataOverwrite shows a confirmation dialog for data overwrite operation
func (c *Copier) confirmDataOverwrite(tables []*TableInfo, totalRows int64) bool {
	// Filter out tables with no rows for the warning display
	nonEmptyTables := make([]*TableInfo, 0)
	for _, table := range tables {
		if table.RowCount > 0 {
			nonEmptyTables = append(nonEmptyTables, table)
		}
	}

	// If all tables are empty, skip the warning entirely
	if len(nonEmptyTables) == 0 {
		return true // Proceed without warning for empty tables
	}

	// Sort tables by row count in descending order (largest first)
	sort.Slice(nonEmptyTables, func(i, j int) bool {
		return nonEmptyTables[i].RowCount > nonEmptyTables[j].RowCount
	})

	fmt.Printf("\n⚠️  WARNING: Data Overwrite Operation\n")
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("This operation will OVERWRITE data in the destination database:\n\n")

	// Show connection info (mask passwords)
	destConn := c.config.DestConn
	if c.config.DestFile != "" {
		destConn = fmt.Sprintf("(from file: %s)", c.config.DestFile)
	} else {
		// Mask password in connection string for display
		destConn = utils.MaskPassword(destConn)
	}
	fmt.Printf("🎯 Destination: %s\n", destConn)
	fmt.Printf("📊 Tables to overwrite: %d (with data)\n", len(nonEmptyTables))
	fmt.Printf("📈 Total rows to copy: %s\n", utils.FormatNumber(totalRows))

	fmt.Printf("\n⚠️  ALL EXISTING DATA in these tables will be DELETED:\n")
	for i, table := range nonEmptyTables {
		if i >= 10 { // Show only first 10 tables to avoid overwhelming output
			fmt.Printf("   ... and %d more tables\n", len(nonEmptyTables)-10)
			break
		}
		fmt.Printf("   • %s.%s (%s rows)\n", table.Schema, table.Name, utils.FormatNumber(table.RowCount))
	}

	fmt.Printf("\n═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("This action CANNOT be undone. Are you sure you want to proceed?\n")
	fmt.Printf("Type 'yes' to confirm, or anything else to cancel: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("\nError reading input: %v\n", err)
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "yes"
}
