package copier

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// copyTablesParallel copies tables using parallel workers
func (c *Copier) copyTablesParallel(tables []*TableInfo) error {
	// Create a channel for work distribution
	tableChan := make(chan *TableInfo, len(tables))
	errChan := make(chan error, c.config.Parallel)
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < c.config.Parallel; i++ {
		wg.Add(1)
		go c.worker(tableChan, errChan, &wg)
	}

	// Send tables to workers
	for _, table := range tables {
		tableChan <- table
	}
	close(tableChan)

	// Wait for all workers to complete
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Collect any errors
	var errors []error
	for err := range errChan {
		if err != nil {
			errors = append(errors, err)
			c.mu.Lock()
			c.stats.Errors = append(c.stats.Errors, err)
			c.mu.Unlock()
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during copy operation", len(errors))
	}

	return nil
}

// worker processes tables from the channel
func (c *Copier) worker(tableChan <-chan *TableInfo, errChan chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()

	for table := range tableChan {
		if err := c.copyTable(table); err != nil {
			c.logError("Error copying table %s: %v", highlightTableName(table.Schema, table.Name), err)
			errChan <- fmt.Errorf("failed to copy table %s.%s: %w", table.Schema, table.Name, err)
		} else {
			c.mu.Lock()
			c.stats.TablesProcessed++
			// Update progress bar description when a table is completed
			if c.config.ProgressBar && c.progressBar != nil {
				description := fmt.Sprintf("Copying rows (%d/%d tables)",
					c.stats.TablesProcessed, c.stats.TotalTables)
				c.progressBar.Describe(description)
			}
			c.mu.Unlock()
		}
	}
}

// copyTable copies a single table from source to destination
func (c *Copier) copyTable(table *TableInfo) error {
	startTime := time.Now()

	if table.RowCount == 0 {
		c.logProgress("Skipping empty table %s", highlightTableName(table.Schema, table.Name))
		return nil
	}

	c.logProgress("Copying table %s (%s rows)", highlightTableName(table.Schema, table.Name), highlightNumber(formatNumber(table.RowCount)))

	// Drop foreign keys for this table if not using replica mode
	if err := c.fkManager.DropForeignKeysForTable(table); err != nil {
		return fmt.Errorf("failed to drop foreign keys for table: %w", err)
	}

	// Clear destination table first
	if err := c.clearDestinationTable(table); err != nil {
		return fmt.Errorf("failed to clear destination table: %w", err)
	}

	// Copy data in batches
	err := c.copyTableData(table)

	if err != nil {
		return err
	}

	// Restore foreign keys for this table immediately after copying
	if restoreErr := c.fkManager.RestoreForeignKeysForTable(table); restoreErr != nil {
		c.logWarn("Failed to restore foreign keys for %s: %v", highlightTableName(table.Schema, table.Name), restoreErr)
	}

	duration := time.Since(startTime)
	c.logSuccess("Completed copying %s (%s rows) in %s", highlightTableName(table.Schema, table.Name), highlightNumber(formatNumber(table.RowCount)), colorize(ColorDim, formatDuration(duration)))
	return nil
}

// clearDestinationTable truncates the destination table
func (c *Copier) clearDestinationTable(table *TableInfo) error {
	if c.fkManager.IsUsingReplicaMode() {
		// Use a transaction with replica mode for the truncate
		tx, err := c.destDB.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for truncate: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec("SET LOCAL session_replication_role = replica"); err != nil {
			return fmt.Errorf("failed to set replica mode for truncate: %w", err)
		}

		query := fmt.Sprintf("TRUNCATE TABLE %s.\"%s\" CASCADE", table.Schema, table.Name)
		if _, err := tx.Exec(query); err != nil {
			return err
		}

		return tx.Commit()
	} else {
		query := fmt.Sprintf("TRUNCATE TABLE %s.\"%s\" CASCADE", table.Schema, table.Name)
		_, err := c.destDB.Exec(query)
		return err
	}
}

// copyTableData copies table data in batches
func (c *Copier) copyTableData(table *TableInfo) error {
	columnList := strings.Join(func() []string {
		quotedColumns := make([]string, len(table.Columns))
		for i, col := range table.Columns {
			quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
		}
		return quotedColumns
	}(), ", ")
	placeholders := make([]string, len(table.Columns))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	placeholderList := strings.Join(placeholders, ", ")

	// Prepare insert statement for destination
	insertQuery := fmt.Sprintf(
		"INSERT INTO %s.\"%s\" (%s) VALUES (%s)",
		table.Schema, table.Name, columnList, placeholderList,
	)

	insertStmt, err := c.destDB.Prepare(insertQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer insertStmt.Close()

	// Build select query with ordering for consistent pagination
	var selectQuery string
	if len(table.PKColumns) > 0 {
		// Use primary key for ordering if available
		orderBy := strings.Join(table.PKColumns, ", ")
		selectQuery = fmt.Sprintf(
			"SELECT %s FROM %s.\"%s\" ORDER BY \"%s\" LIMIT %d OFFSET $1",
			columnList, table.Schema, table.Name, orderBy, c.config.BatchSize,
		)
	} else {
		// Use ctid for ordering if no primary key (less efficient but works)
		selectQuery = fmt.Sprintf(
			"SELECT %s FROM %s.\"%s\" ORDER BY ctid LIMIT %d OFFSET $1",
			columnList, table.Schema, table.Name, c.config.BatchSize,
		)
	}

	offset := 0
	rowsCopied := int64(0)

	for {
		// Select batch from source
		rows, err := c.sourceDB.Query(selectQuery, offset)
		if err != nil {
			return fmt.Errorf("failed to select batch: %w", err)
		}

		batchRowsCopied, err := c.processBatch(rows, insertStmt, table)
		rows.Close()

		if err != nil {
			return fmt.Errorf("failed to process batch: %w", err)
		}

		rowsCopied += batchRowsCopied

		// Update progress periodically
		c.updateProgress(batchRowsCopied)

		// If we got fewer rows than batch size, we're done
		if batchRowsCopied < int64(c.config.BatchSize) {
			break
		}

		offset += c.config.BatchSize
	}

	return nil
}

// processBatch processes a batch of rows
func (c *Copier) processBatch(rows *sql.Rows, insertStmt *sql.Stmt, table *TableInfo) (int64, error) {
	tx, err := c.destDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Set replica mode for this transaction if FK manager is using it
	if c.fkManager.IsUsingReplicaMode() {
		if _, err := tx.Exec("SET LOCAL session_replication_role = replica"); err != nil {
			return 0, fmt.Errorf("failed to set replica mode for transaction: %w", err)
		}
	}

	txStmt := tx.Stmt(insertStmt)
	defer txStmt.Close()

	var batchSize int64
	values := make([]interface{}, len(table.Columns))
	valuePtrs := make([]interface{}, len(table.Columns))

	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return batchSize, fmt.Errorf("failed to scan row: %w", err)
		}

		if _, err := txStmt.Exec(values...); err != nil {
			return batchSize, fmt.Errorf("failed to insert row: %w", err)
		}

		batchSize++
	}

	if err := rows.Err(); err != nil {
		return batchSize, fmt.Errorf("rows iteration error: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return batchSize, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return batchSize, nil
}
