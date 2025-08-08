package copier

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/koltyakov/pgcopy/internal/state"
	"github.com/koltyakov/pgcopy/internal/utils"
)

// copyTablesParallel copies tables using parallel workers
func (c *Copier) copyTablesParallel(ctx context.Context, tables []*TableInfo) error {
	// Create a channel for work distribution
	tableChan := make(chan *TableInfo, len(tables))
	errChan := make(chan error, c.config.Parallel)
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < c.config.Parallel; i++ {
		wg.Add(1)
		go c.worker(ctx, tableChan, errChan, &wg)
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
			// Use state system for error tracking
			c.state.AddError("copy_error", err.Error(), "copier", false, nil)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during copy operation", len(errors))
	}

	return nil
}

// worker processes tables from the channel
func (c *Copier) worker(ctx context.Context, tableChan <-chan *TableInfo, errChan chan<- error, wg *sync.WaitGroup) { //nolint:funlen
	defer wg.Done()

	// Helper for processing a single table (no batching logic here)
	processOne := func(table *TableInfo) {
		if table == nil {
			return
		}
		// Check for cancellation before starting
		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
			return
		default:
		}

		if c.interactiveMode {
			c.startTableInInteractive(table)
		} else {
			c.setTableInProgress(table.Schema, table.Name)
		}

		c.state.UpdateTableStatus(table.Schema, table.Name, state.TableStatusCopying)
		c.state.AddLog(state.LogLevelInfo, fmt.Sprintf("Starting copy of table %s.%s", table.Schema, table.Name), "worker", table.Schema+"."+table.Name, nil)

		if err := c.copyTable(ctx, table); err != nil {
			c.logger.Error("Error copying table %s: %v", utils.HighlightTableName(table.Schema, table.Name), err)
			errChan <- fmt.Errorf("failed to copy table %s.%s: %w", table.Schema, table.Name, err)

			c.state.UpdateTableStatus(table.Schema, table.Name, state.TableStatusFailed)
			c.state.AddTableError(table.Schema, table.Name, err.Error(), nil)
			if c.interactiveMode && c.interactiveDisplay != nil {
				c.interactiveDisplay.CompleteTable(table.Schema, table.Name, false)
			}
		} else {
			c.state.UpdateTableStatus(table.Schema, table.Name, state.TableStatusCompleted)
			c.state.AddLog(state.LogLevelInfo, fmt.Sprintf("Completed copy of table %s.%s", table.Schema, table.Name), "worker", table.Schema+"."+table.Name, nil)
			if c.getDisplayMode() == DisplayModeProgress && c.progressBar != nil {
				description := fmt.Sprintf("Copying rows (%d/%d tables)", c.state.Summary.CompletedTables, c.state.Summary.TotalTables)
				c.progressBar.Describe(description)
			}
		}
		c.removeTableFromProgress(table.Schema, table.Name)
	}

	smallThreshold := int64(c.config.BatchSize) // heuristic: table smaller than one batch considered "small"

	for {
		var table *TableInfo
		var ok bool
		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
			return
		case table, ok = <-tableChan:
			if !ok { // channel closed
				return
			}
		}

		// Process the first (possibly large) table
		processOne(table)

		// If it's not small, loop to fetch next table normally
		if table == nil || table.TotalRows >= smallThreshold {
			continue
		}

		// Table was small: opportunistically drain additional small tables (and maybe one large) without letting the worker go idle
		// This reduces worker churn for many tiny tables
	drainLoop:
		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			case next, ok2 := <-tableChan:
				if !ok2 { // channel exhausted
					return
				}
				processOne(next)
				// Stop batching if we hit a large table; large table processed already, proceed to next outer iteration
				if next.TotalRows >= smallThreshold {
					break drainLoop
				}
				// Otherwise continue draining more small tables (non-blocking try again)
				continue
			default:
				// No immediately available tables; yield back to outer loop to allow fair scheduling
				break drainLoop
			}
		}
	}
}

// copyTable copies a single table from source to destination
func (c *Copier) copyTable(ctx context.Context, table *TableInfo) error {
	startTime := time.Now()

	if table.TotalRows == 0 {
		c.logger.Info("Skipping empty table %s", utils.HighlightTableName(table.Schema, table.Name))
		return nil
	}

	c.logger.Info("Copying table %s (%s rows)", utils.HighlightTableName(table.Schema, table.Name), utils.HighlightNumber(utils.FormatNumber(table.TotalRows)))

	// Drop foreign keys for this table if not using replica mode
	if c.fkStrategy != nil {
		if err := c.fkStrategy.Prepare(table); err != nil {
			return fmt.Errorf("failed to drop foreign keys for table: %w", err)
		}
	}

	// Clear destination table first
	if err := c.clearDestinationTable(ctx, table); err != nil {
		return fmt.Errorf("failed to clear destination table: %w", err)
	}

	var err error
	if c.config.UseCopyPipe {
		// Use streaming COPY pipeline
		// Derive a generous timeout from provided ctx to avoid infinite runs
		sctx, cancel := context.WithTimeout(ctx, 24*time.Hour) // configurable in the future
		defer cancel()
		err = c.copyTableViaPipe(sctx, table)
	} else {
		// Copy data in batches (legacy path)
		err = c.copyTableData(ctx, table)
	}

	if err != nil {
		return err
	}

	// Restore foreign keys for this table immediately after copying
	if c.fkStrategy != nil {
		if restoreErr := c.fkStrategy.Restore(table); restoreErr != nil {
			c.logger.Warn("Failed to restore foreign keys for %s: %v", utils.HighlightTableName(table.Schema, table.Name), restoreErr)
		}
	}

	// After data load, ensure any sequences backing columns on this table are set correctly
	if err := c.resetSequencesForTable(ctx, table); err != nil {
		c.logger.Warn("Failed to reset sequences for %s: %v", utils.HighlightTableName(table.Schema, table.Name), err)
	}

	duration := time.Since(startTime)

	// Log to copy.log file
	tableFullName := fmt.Sprintf("%s.%s", table.Schema, table.Name)
	c.logTableCopy(tableFullName, table.TotalRows, duration)

	c.logger.Info("Completed copying %s (%s rows) in %s", utils.HighlightTableName(table.Schema, table.Name), utils.HighlightNumber(utils.FormatNumber(table.TotalRows)), utils.Colorize(utils.ColorDim, utils.FormatDuration(duration)))
	return nil
}

// clearDestinationTable truncates the destination table
func (c *Copier) clearDestinationTable(ctx context.Context, table *TableInfo) error {
	if c.fkManager.IsUsingReplicaMode() {
		// Use a transaction with replica mode for the truncate
		// retry loop to mitigate deadlocks
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			tx, err := c.destDB.BeginTx(ctx, &sql.TxOptions{})
			if err != nil {
				return fmt.Errorf("failed to begin transaction for truncate: %w", err)
			}

			committed := false
			defer func(tx *sql.Tx) {
				if !committed {
					if err := tx.Rollback(); err != nil {
						c.logger.Error("Failed to rollback truncate transaction: %v", err)
					}
				}
			}(tx)

			if _, err := tx.ExecContext(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("failed to set replica mode for truncate: %w", err)
			}

			query := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", utils.QuoteTable(table.Schema, table.Name))
			if _, err := tx.ExecContext(ctx, query); err != nil {
				lastErr = err
				_ = tx.Rollback()
				// deadlock code 40P01
				if strings.Contains(err.Error(), "deadlock detected") || strings.Contains(err.Error(), "40P01") {
					time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
					continue
				}
				return err
			}

			if err := tx.Commit(); err != nil {
				lastErr = err
				if strings.Contains(err.Error(), "deadlock detected") || strings.Contains(err.Error(), "40P01") {
					time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
					continue
				}
				return err
			}
			committed = true
			return nil
		}
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("truncate failed for %s.\"%s\" for unknown reason", table.Schema, table.Name)
	}
	query := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", utils.QuoteTable(table.Schema, table.Name))
	_, err := c.destDB.ExecContext(ctx, query)
	return err
}

// copyTableData copies table data in batches
func (c *Copier) copyTableData(ctx context.Context, table *TableInfo) error {
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
	insertQuery := fmt.Sprintf( // #nosec G201 - identifiers are quoted safely
		"INSERT INTO %s (%s) VALUES (%s)",
		utils.QuoteTable(table.Schema, table.Name), columnList, placeholderList,
	)

	insertStmt, err := c.destDB.PrepareContext(ctx, insertQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer func() {
		if err := insertStmt.Close(); err != nil {
			c.logger.Error("Failed to close insert statement: %v", err)
		}
	}()

	// Build select query with ordering for consistent pagination
	var selectQuery string
	if len(table.PKColumns) > 0 {
		// Use primary key for ordering if available
		orderBy := utils.QuoteJoinIdents(table.PKColumns)
		selectQuery = fmt.Sprintf(
			"SELECT %s FROM %s ORDER BY %s LIMIT %d OFFSET $1",
			columnList, utils.QuoteTable(table.Schema, table.Name), orderBy, c.config.BatchSize,
		)
	} else {
		// Use ctid for ordering if no primary key (less efficient but works)
		selectQuery = fmt.Sprintf(
			"SELECT %s FROM %s ORDER BY ctid LIMIT %d OFFSET $1",
			columnList, utils.QuoteTable(table.Schema, table.Name), c.config.BatchSize,
		)
	}

	offset := 0
	rowsCopied := int64(0)

	for {
		// Select batch from source
		rows, err := c.sourceDB.QueryContext(ctx, selectQuery, offset)
		if err != nil {
			return fmt.Errorf("failed to select batch: %w", err)
		}

		batchRowsCopied, err := c.processBatch(ctx, rows, insertStmt, table)
		if err := rows.Close(); err != nil {
			c.logger.Error("Failed to close batch rows: %v", err)
		}

		if err != nil {
			return fmt.Errorf("failed to process batch: %w", err)
		}

		rowsCopied += batchRowsCopied

		// Update progress periodically
		c.updateProgress(batchRowsCopied)

		// Update table-specific progress for state tracking
		c.updateTableProgress(table.Schema, table.Name, rowsCopied)

		// If we got fewer rows than batch size, we're done
		if batchRowsCopied < int64(c.config.BatchSize) {
			break
		}

		offset += c.config.BatchSize
	}

	return nil
}

// processBatch processes a batch of rows
func (c *Copier) processBatch(ctx context.Context, rows *sql.Rows, insertStmt *sql.Stmt, table *TableInfo) (int64, error) {
	tx, err := c.destDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				c.logger.Error("Failed to rollback batch transaction: %v", err)
			}
		}
	}()

	// Set replica mode for this transaction if FK manager is using it
	if c.fkManager.IsUsingReplicaMode() {
		if _, err := tx.ExecContext(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
			return 0, fmt.Errorf("failed to set replica mode for transaction: %w", err)
		}
	}

	txStmt := tx.Stmt(insertStmt)
	defer func() {
		if err := txStmt.Close(); err != nil {
			c.logger.Error("Failed to close transaction statement: %v", err)
		}
	}()

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

		if _, err := txStmt.ExecContext(ctx, values...); err != nil {
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
	committed = true

	return batchSize, nil
}
