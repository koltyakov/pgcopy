package copier

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/koltyakov/pgcopy/internal/state"
	"github.com/koltyakov/pgcopy/internal/utils"
)

// Default operation timeouts.
//
// These timeouts balance responsiveness with allowing large operations to complete.
// They can be overridden by setting config.NoTimeouts = true for operations that
// require extended time (e.g., very large tables, slow networks).
//
// Each timeout is chosen based on operational experience:
//   - truncateTimeout: 30s allows for large table cleanup with index maintenance
//   - selectTimeout: 2min allows for complex queries with sequential scans
//   - txnTimeout: 2min allows for large batch inserts within a transaction
const (
	// truncateTimeout is the maximum time allowed for TRUNCATE TABLE operations.
	// Larger tables may take longer if triggers or FK checks are involved.
	truncateTimeout = 30 * time.Second

	// selectTimeout is the maximum time for SELECT queries during batch reads.
	// Complex queries or cold caches may require the full timeout.
	selectTimeout = 2 * time.Minute

	// txnTimeout is the maximum time for transaction-scoped operations.
	// This includes batch inserts and related constraint checking.
	txnTimeout = 2 * time.Minute
)

// rowBufferPool provides reusable row buffers to reduce GC pressure during scanning.
//
// During high-throughput data copying, the scanner allocates a []any slice for each
// row to receive column values. Without pooling, this creates significant garbage
// collection overhead.
//
// This pool stores *[]any pointers (pointer to slice) because:
//  1. sync.Pool stores interface{} values - storing slices directly would cause allocations
//  2. Storing pointers allows us to resize the underlying slice without re-pooling
//
// Usage pattern:
//
//	buf := getRowBuffer(numCols)
//	defer putRowBuffer(buf)
//	err := rows.Scan(buf...)
//
// Thread Safety: sync.Pool is safe for concurrent use.
var rowBufferPool = sync.Pool{
	New: func() any {
		// Pre-allocate a reasonable initial capacity to reduce early reallocations.
		// 64 columns covers most table schemas; larger tables will resize as needed.
		slice := make([]any, 0, 64)
		return &slice
	},
}

// getRowBuffer retrieves a row buffer from the pool, resized to numCols.
//
// The returned slice is guaranteed to have length == numCols with all elements
// set to nil (safe for sql.Rows.Scan).
//
// Parameters:
//   - numCols: Number of columns to scan (must be >= 0)
//
// Returns: A slice of length numCols ready for scanning.
//
// The caller MUST call putRowBuffer when done to return the buffer to the pool.
func getRowBuffer(numCols int) []any {
	// Defensive: handle invalid input
	if numCols < 0 {
		numCols = 0
	}

	buf := rowBufferPool.Get().(*[]any)
	if cap(*buf) < numCols {
		// Grow capacity if needed - allocate with some headroom
		*buf = make([]any, numCols, numCols*2)
	} else {
		*buf = (*buf)[:numCols]
	}

	// Clear the slice - critical for correct scanning behavior.
	// sql.Rows.Scan expects nil interface{} values or properly typed pointers.
	for i := range *buf {
		(*buf)[i] = nil
	}
	return *buf
}

// putRowBuffer returns a row buffer to the pool.
//
// The buffer is cleared before returning to allow GC of referenced values.
// This prevents memory leaks from large scanned values being retained in pooled buffers.
//
// Parameters:
//   - buf: The buffer slice to return (may be nil - no-op)
func putRowBuffer(buf []any) {
	if buf == nil {
		return
	}

	// Clear references to allow GC of scanned values.
	// This is critical - without clearing, large values could be retained indefinitely.
	for i := range buf {
		buf[i] = nil
	}
	rowBufferPool.Put(&buf)
}

// stringBuilderPool provides reusable string builders to reduce GC pressure during INSERT statement construction.
//
// Building INSERT statements involves significant string concatenation. Without pooling, each batch
// creates and discards a strings.Builder, causing GC overhead.
//
// Thread Safety: sync.Pool is safe for concurrent use.
var stringBuilderPool = sync.Pool{
	New: func() any {
		// Pre-allocate reasonable capacity for typical INSERT statements.
		// Average INSERT: ~50 bytes header + (numCols * 5 bytes per placeholder) * numRows
		sb := &strings.Builder{}
		sb.Grow(65536) // 64KB initial capacity covers most batches
		return sb
	},
}

// getStringBuilder retrieves a string builder from the pool, reset and ready to use.
func getStringBuilder() *strings.Builder {
	sb := stringBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	return sb
}

// putStringBuilder returns a string builder to the pool.
func putStringBuilder(sb *strings.Builder) {
	if sb == nil {
		return
	}
	// Only pool builders that haven't grown too large (avoid memory hoarding)
	if sb.Cap() <= 1<<20 { // 1MB limit
		stringBuilderPool.Put(sb)
	}
}

// placeholderCache stores pre-computed placeholder integer strings for fast lookup.
// Avoids strconv.Itoa allocations for common parameter numbers.
var placeholderInts = func() []string {
	// Pre-compute strings for parameters 1-65535 (PostgreSQL max)
	// This uses ~1MB of memory but eliminates allocations in the hot path.
	const maxParams = 65536
	strs := make([]string, maxParams)
	for i := range strs {
		strs[i] = strconv.Itoa(i)
	}
	return strs
}()

// itoa returns a string representation of n without allocation for common values.
func itoa(n int) string {
	if n >= 0 && n < len(placeholderInts) {
		return placeholderInts[n]
	}
	return strconv.Itoa(n)
}

// buildValuesClause constructs the VALUES clause for a multi-row INSERT efficiently.
// Uses pre-allocated integer strings to avoid allocations in the hot loop.
//
// Parameters:
//   - sb: string builder to write to (should be reset before calling)
//   - numCols: number of columns per row
//   - numRows: number of rows to include
//   - startParam: starting parameter number (usually 1)
//
// Returns the next parameter number after the last placeholder.
func buildValuesClause(sb *strings.Builder, numCols, numRows, startParam int) int {
	param := startParam
	for row := 0; row < numRows; row++ {
		if row > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('(')
		for col := 0; col < numCols; col++ {
			if col > 0 {
				sb.WriteString(", ")
			}
			sb.WriteByte('$')
			sb.WriteString(itoa(param))
			param++
		}
		sb.WriteByte(')')
	}
	return param
}

// copyTablesParallel copies tables using parallel workers.
//
// This function implements a worker pool pattern:
//  1. Creates N worker goroutines (N = config.Parallel)
//  2. Distributes tables to workers via a buffered channel
//  3. Workers process tables independently, reporting errors via errChan
//  4. Waits for all workers to complete, then collects and returns errors
//
// # Error Handling
//
// Individual table failures don't stop other workers. All errors are collected
// and returned as a combined error using errors.Join. This allows maximum
// data migration even with partial failures.
//
// # Cancellation
//
// Workers check ctx.Done() before each table and abort promptly on cancellation.
// In-progress table copies will complete their current batch before stopping.
//
// # Panic Recovery
//
// Each worker has panic recovery to prevent one misbehaving table from
// crashing the entire operation. Panics are converted to errors.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - tables: Slice of tables to copy (must not be nil)
//
// Returns:
//   - error: nil on success, or combined errors from failed tables
//
// Thread Safety: Safe to call once per Copier instance (not concurrent-safe).
func (c *Copier) copyTablesParallel(ctx context.Context, tables []*TableInfo) error {
	// Defensive: handle nil or empty input
	if len(tables) == 0 {
		return nil
	}

	// Invariant check: parallel workers must be positive
	if c.config.Parallel <= 0 {
		c.config.Parallel = 1 // Safe default
	}

	// Create a channel for work distribution
	tableChan := make(chan *TableInfo, len(tables))
	errChan := make(chan error, c.config.Parallel)
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < c.config.Parallel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer func() {
				if r := recover(); r != nil {
					c.logger.Error("Worker %d panicked: %v", workerID, r)
					errChan <- fmt.Errorf("worker %d panic: %v", workerID, r)
				}
			}()
			c.worker(ctx, tableChan, errChan, &wg)
		}(i)
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
	var collectedErrors []error
	for err := range errChan {
		if err != nil {
			collectedErrors = append(collectedErrors, err)
			// Use state system for error tracking
			c.state.AddError("copy_error", err.Error(), "copier", false, nil)
		}
	}

	if len(collectedErrors) > 0 {
		// Use errors.Join to preserve all error details for debugging
		return errors.Join(append([]error{fmt.Errorf("%w: encountered %d errors during copy operation", utils.ErrCopyFailures, len(collectedErrors))}, collectedErrors...)...)
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
			// Interactive display reads progress from state; no direct call needed
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

	// Clear destination table first. For streaming pipeline we perform TRUNCATE inside
	// the destination connection transaction that also does COPY so we can rollback
	// on failure and avoid leaving the table empty.
	if !c.config.UseCopyPipe {
		if err := c.clearDestinationTable(ctx, table); err != nil {
			return fmt.Errorf("failed to clear destination table: %w", err)
		}
	}

	var err error
	if c.config.UseCopyPipe {
		// Use streaming COPY pipeline
		// Derive a generous timeout from provided ctx to avoid infinite runs
		var sctx context.Context
		var cancel context.CancelFunc
		if c.config.NoTimeouts {
			sctx, cancel = ctx, func() {}
		} else {
			sctx, cancel = context.WithTimeout(ctx, 24*time.Hour) // configurable in the future
		}
		defer cancel()
		err = c.copyTableViaPipe(sctx, table)
	} else {
		// Copy data in batches (legacy path)
		err = c.copyTableData(ctx, table)
	}

	if err != nil {
		return err
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
		for attempt := range 3 {
			var tctx context.Context
			var cancel context.CancelFunc
			if c.config.NoTimeouts {
				tctx, cancel = ctx, func() {}
			} else {
				tctx, cancel = context.WithTimeout(ctx, truncateTimeout)
			}
			tx, err := c.destDB.BeginTx(tctx, &sql.TxOptions{})
			if err != nil {
				cancel()
				return fmt.Errorf("failed to begin transaction for truncate: %w", err)
			}

			committed := false
			defer func(tx *sql.Tx) {
				if !committed {
					if err := tx.Rollback(); err != nil {
						c.logger.Error("Failed to rollback truncate transaction: %v", err)
					}
				}
				cancel()
			}(tx)

			if _, err := tx.ExecContext(tctx, "SET LOCAL session_replication_role = replica"); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("failed to set replica mode for truncate: %w", err)
			}

			// No CASCADE here: FK drop/replica mode should permit truncate without cascading
			query := fmt.Sprintf("TRUNCATE TABLE %s", utils.QuoteTable(table.Schema, table.Name))
			if _, err := tx.ExecContext(tctx, query); err != nil {
				lastErr = err
				_ = tx.Rollback()
				// deadlock code 40P01
				if strings.Contains(err.Error(), "deadlock detected") || strings.Contains(err.Error(), "40P01") {
					time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
					continue
				}
				if errors.Is(err, context.Canceled) {
					return fmt.Errorf("%w: truncate canceled: %w", utils.ErrCanceled, err)
				}
				if errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("%w: truncate deadline exceeded: %w", utils.ErrDeadlineExceeded, err)
				}
				return fmt.Errorf("failed to truncate %s: %w", utils.QuoteTable(table.Schema, table.Name), err)
			}

			if err := tx.Commit(); err != nil {
				lastErr = err
				if strings.Contains(err.Error(), "deadlock detected") || strings.Contains(err.Error(), "40P01") {
					time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
					continue
				}
				if errors.Is(err, context.Canceled) {
					return fmt.Errorf("%w: commit canceled: %w", utils.ErrCanceled, err)
				}
				if errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("%w: commit deadline exceeded: %w", utils.ErrDeadlineExceeded, err)
				}
				return fmt.Errorf("failed to commit truncate transaction: %w", err)
			}
			committed = true
			return nil
		}
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("truncate failed for %s.\"%s\" for unknown reason", table.Schema, table.Name)
	}
	// No CASCADE here: FK drop/replica mode should permit truncate without cascading
	query := fmt.Sprintf("TRUNCATE TABLE %s", utils.QuoteTable(table.Schema, table.Name))
	var tctx context.Context
	var cancel context.CancelFunc
	if c.config.NoTimeouts {
		tctx, cancel = ctx, func() {}
	} else {
		tctx, cancel = context.WithTimeout(ctx, truncateTimeout)
	}
	defer cancel()
	_, err := c.destDB.ExecContext(tctx, query)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("%w: truncate canceled: %w", utils.ErrCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: truncate deadline exceeded: %w", utils.ErrDeadlineExceeded, err)
		}
		return fmt.Errorf("failed to clear destination table: %w", err)
	}
	return nil
}

// copyTableData copies table data in batches
func (c *Copier) copyTableData(ctx context.Context, table *TableInfo) error { //nolint:funlen,gocognit
	columnList := strings.Join(func() []string {
		quotedColumns := make([]string, len(table.Columns))
		for i, col := range table.Columns {
			quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
		}
		return quotedColumns
	}(), ", ")

	// Snapshot transaction (optional). We create a dedicated connection/tx to ensure
	// REPEATABLE READ isolation so pagination isn't affected by concurrent inserts.
	var snapshotTx *sql.Tx
	var snapshotCancel context.CancelFunc = func() {}
	if c.config.Snapshot {
		var sctx context.Context
		if c.config.NoTimeouts {
			sctx, snapshotCancel = context.WithCancel(ctx)
		} else {
			sctx, snapshotCancel = context.WithTimeout(ctx, 24*time.Hour)
		}
		// Use REPEATABLE READ for a stable view
		tx, err := c.sourceDB.BeginTx(sctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			snapshotCancel()
			return fmt.Errorf("failed to start snapshot tx: %w", err)
		}
		snapshotTx = tx
		defer func() {
			_ = snapshotTx.Rollback() // safe if already committed/rolled back
			snapshotCancel()
		}()
	}

	// Helper to run queries either inside snapshot tx or base DB
	queryContext := func(qctx context.Context, query string, args ...any) (*sql.Rows, error) {
		if snapshotTx != nil {
			return snapshotTx.QueryContext(qctx, query, args...)
		}
		return c.sourceDB.QueryContext(qctx, query, args...)
	}

	rowsCopied := int64(0)

	// If table has PK(s), use keyset pagination; else fall back to OFFSET on ctid
	if len(table.PKColumns) == 0 {
		// Fallback: existing OFFSET pagination using ctid
		selectBase := fmt.Sprintf("SELECT %s FROM %s ORDER BY ctid LIMIT %d OFFSET $1", columnList, utils.QuoteTable(table.Schema, table.Name), c.config.BatchSize)
		offset := 0
		for {
			var sctx context.Context
			var cancel context.CancelFunc
			if c.config.NoTimeouts {
				sctx, cancel = ctx, func() {}
			} else {
				sctx, cancel = context.WithTimeout(ctx, selectTimeout)
			}
			rows, err := queryContext(sctx, selectBase, offset)
			if err != nil {
				cancel()
				return fmt.Errorf("failed to select batch: %w", err)
			}
			batchRowsCopied, err := c.processBatchBulk(ctx, rows, table, columnList)
			if err := rows.Close(); err != nil {
				c.logger.Error("Failed to close batch rows: %v", err)
			}
			cancel()
			if err != nil {
				return fmt.Errorf("failed to process batch: %w", err)
			}
			rowsCopied += batchRowsCopied
			c.updateProgress(batchRowsCopied)
			c.updateTableProgress(table.Schema, table.Name, rowsCopied)
			if batchRowsCopied < int64(c.config.BatchSize) {
				break
			}
			offset += c.config.BatchSize
		}
		return nil
	}

	// Keyset pagination
	pkCols := table.PKColumns
	quotedPK := utils.QuoteJoinIdents(pkCols)

	// Build WHERE clause template for keyset (multi-column lexicographic)
	// For single column: WHERE pk > $X ORDER BY pk
	// For composite: (pk1,pk2,...) > (last1,last2,...) using tuple comparison
	var whereClause string
	orderBy := quotedPK
	if len(pkCols) == 1 {
		whereClause = fmt.Sprintf("%s > $1", utils.QuoteIdent(pkCols[0]))
	} else {
		// Build ($1,$2,...) tuple
		placeholders := make([]string, len(pkCols))
		for i := range pkCols {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}
		whereClause = fmt.Sprintf("(%s) > (%s)", quotedPK, strings.Join(placeholders, ","))
	}

	buildSelect := func(hasCursor bool) string {
		if hasCursor {
			return fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT %d", columnList, utils.QuoteTable(table.Schema, table.Name), whereClause, orderBy, c.config.BatchSize)
		}
		return fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT %d", columnList, utils.QuoteTable(table.Schema, table.Name), orderBy, c.config.BatchSize)
	}

	// Track last seen PK values
	var lastPK []any
	for {
		var sctx context.Context
		var cancel context.CancelFunc
		if c.config.NoTimeouts {
			sctx, cancel = ctx, func() {}
		} else {
			sctx, cancel = context.WithTimeout(ctx, selectTimeout)
		}
		var rows *sql.Rows
		var err error
		if len(lastPK) == 0 {
			rows, err = queryContext(sctx, buildSelect(false))
		} else {
			rows, err = queryContext(sctx, buildSelect(true), lastPK...)
		}
		if err != nil {
			cancel()
			return fmt.Errorf("failed to select keyset batch: %w", err)
		}

		// We'll read rows twice: first capture PK values to prepare next cursor, then bulk insert
		// So we buffer rows similarly to processBatchBulk (can't reuse it directly to peek PKs first)
		// Instead we scan all rows into memory (like processBatchBulk does) then insert via existing helper by re-querying? Simpler: extend processBatchBulk to return lastPK? Avoid major refactor: replicate the buffering logic here then do insert manually (duplicating some logic) but to limit duplication we'll call a new internal helper.
		batchData, pkValues, err := c.scanBatch(rows, table)
		if cerr := rows.Close(); cerr != nil {
			c.logger.Error("Failed to close batch rows: %v", cerr)
		}
		cancel()
		if err != nil {
			return fmt.Errorf("failed to scan keyset batch: %w", err)
		}
		if len(batchData) == 0 { // done
			break
		}
		inserted, err := c.insertScannedBatch(ctx, table, columnList, batchData)
		if err != nil {
			return fmt.Errorf("failed to insert keyset batch: %w", err)
		}
		rowsCopied += inserted
		c.updateProgress(inserted)
		c.updateTableProgress(table.Schema, table.Name, rowsCopied)
		if int(inserted) < c.config.BatchSize { // last batch
			break
		}
		lastPK = pkValues
	}
	return nil
}

// scanBatch reads all rows of a keyset batch capturing both full row data and last PK tuple.
// Uses pooled pointer buffers to reduce allocations in the hot path.
func (c *Copier) scanBatch(rows *sql.Rows, table *TableInfo) (data [][]any, lastPK []any, err error) {
	numCols := len(table.Columns)
	pkIndex := make([]int, len(table.PKColumns))
	for i, pk := range table.PKColumns {
		for j, col := range table.Columns {
			if col == pk {
				pkIndex[i] = j
				break
			}
		}
	}

	// Pre-allocate data slice with estimated capacity to reduce reallocations
	data = make([][]any, 0, c.config.BatchSize)

	// Get a pooled pointer buffer for scanning
	ptrs := getRowBuffer(numCols)
	defer putRowBuffer(ptrs)

	for rows.Next() {
		// Each row needs its own storage (can't reuse since we keep the data)
		row := make([]any, numCols)
		for i := range ptrs {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return data, lastPK, err
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return data, lastPK, err
	}
	if len(data) > 0 {
		last := data[len(data)-1]
		lastPK = make([]any, len(pkIndex))
		for i, idx := range pkIndex {
			lastPK[i] = last[idx]
		}
	}
	return data, lastPK, nil
}

// insertScannedBatch inserts an already-scanned batch (mirrors processBatchBulk logic without scanning)
func (c *Copier) insertScannedBatch(ctx context.Context, table *TableInfo, columnList string, scanned [][]any) (int64, error) { //nolint:funlen
	if len(scanned) == 0 {
		return 0, nil
	}
	var bctx context.Context
	var cancel context.CancelFunc
	if c.config.NoTimeouts {
		bctx, cancel = ctx, func() {}
	} else {
		bctx, cancel = context.WithTimeout(ctx, txnTimeout)
	}
	defer cancel()
	tx, err := c.destDB.BeginTx(bctx, &sql.TxOptions{})
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
	if c.fkManager.IsUsingReplicaMode() {
		if _, err := tx.ExecContext(bctx, "SET LOCAL session_replication_role = replica"); err != nil {
			return 0, fmt.Errorf("failed to set replica mode for transaction: %w", err)
		}
	}
	if _, err := tx.ExecContext(bctx, "SET LOCAL synchronous_commit = OFF"); err != nil {
		c.logger.Warn("Could not set synchronous_commit=OFF for bulk insert tx: %v", err)
	}
	numCols := len(table.Columns)
	const maxParams = 65000
	maxRowsPerStmt := maxParams / numCols
	if maxRowsPerStmt <= 0 {
		maxRowsPerStmt = 1
	}
	totalInserted := int64(0)

	// Pre-compute the INSERT header (reused for all chunks)
	insertHeader := "INSERT INTO " + utils.QuoteTable(table.Schema, table.Name) + " (" + columnList + ") VALUES "

	// Get a pooled string builder for statement construction
	sb := getStringBuilder()
	defer putStringBuilder(sb)

	for start := 0; start < len(scanned); start += maxRowsPerStmt {
		end := min(start+maxRowsPerStmt, len(scanned))
		chunk := scanned[start:end]

		// Reset builder and construct INSERT statement
		sb.Reset()
		sb.WriteString(insertHeader)

		// Build VALUES clause efficiently using pre-allocated integer strings
		buildValuesClause(sb, numCols, len(chunk), 1)

		// Flatten args: pre-allocate with exact capacity
		args := make([]any, 0, len(chunk)*numCols)
		for _, row := range chunk {
			args = append(args, row...)
		}

		stmt := sb.String()
		c.logger.Debug("INSERT (keyset) for %s: %d rows", utils.HighlightTableName(table.Schema, table.Name), len(chunk))
		if _, err := tx.ExecContext(bctx, stmt, args...); err != nil {
			return totalInserted, fmt.Errorf("failed to insert batch (%d rows): %w", len(chunk), err)
		}
		totalInserted += int64(len(chunk))
	}
	if err := tx.Commit(); err != nil {
		return totalInserted, fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return totalInserted, nil
}

// processBatchBulk processes a batch using a single or few multi-row INSERT statements
func (c *Copier) processBatchBulk(ctx context.Context, rows *sql.Rows, table *TableInfo, columnList string) (int64, error) { //nolint:funlen,gocognit
	var bctx context.Context
	var cancel context.CancelFunc
	if c.config.NoTimeouts {
		bctx, cancel = ctx, func() {}
	} else {
		bctx, cancel = context.WithTimeout(ctx, txnTimeout)
	}
	defer cancel()
	tx, err := c.destDB.BeginTx(bctx, &sql.TxOptions{})
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
		if _, err := tx.ExecContext(bctx, "SET LOCAL session_replication_role = replica"); err != nil {
			return 0, fmt.Errorf("failed to set replica mode for transaction: %w", err)
		}
	}
	// Speed up bulk inserts by disabling synchronous commits for this transaction
	if _, err := tx.ExecContext(bctx, "SET LOCAL synchronous_commit = OFF"); err != nil {
		c.logger.Warn("Could not set synchronous_commit=OFF for bulk insert tx: %v", err)
	}

	// Pull rows into memory for this batch using pooled pointer buffer
	numCols := len(table.Columns)
	scanned := make([][]any, 0, c.config.BatchSize) // Pre-allocate with expected capacity

	// Get a pooled pointer buffer for scanning
	ptrs := getRowBuffer(numCols)
	defer putRowBuffer(ptrs)

	for rows.Next() {
		row := make([]any, numCols)
		for i := range ptrs {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, fmt.Errorf("failed to scan row: %w", err)
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows iteration error: %w", err)
	}

	if len(scanned) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("failed to commit empty batch transaction: %w", err)
		}
		committed = true
		return 0, nil
	}

	// Parameter safety: PostgreSQL max parameters ~65535
	const maxParams = 65000
	maxRowsPerStmt := maxParams / numCols
	if maxRowsPerStmt <= 0 {
		maxRowsPerStmt = 1
	}

	// Pre-compute the INSERT header (reused for all chunks)
	insertHeader := "INSERT INTO " + utils.QuoteTable(table.Schema, table.Name) + " (" + columnList + ") VALUES "

	// Get a pooled string builder for statement construction
	sb := getStringBuilder()
	defer putStringBuilder(sb)

	totalInserted := int64(0)
	// Insert in chunks if necessary
	for start := 0; start < len(scanned); start += maxRowsPerStmt {
		end := min(start+maxRowsPerStmt, len(scanned))
		chunk := scanned[start:end]

		// Reset builder and construct INSERT statement
		sb.Reset()
		sb.WriteString(insertHeader)

		// Build VALUES clause efficiently using pre-allocated integer strings
		buildValuesClause(sb, numCols, len(chunk), 1)

		// Flatten args: pre-allocate with exact capacity
		args := make([]any, 0, len(chunk)*numCols)
		for _, row := range chunk {
			args = append(args, row...)
		}

		stmt := sb.String()
		// Debug: log the actual INSERT and number of rows per chunk
		c.logger.Debug("INSERT for %s: %d rows, SQL: %s", utils.HighlightTableName(table.Schema, table.Name), len(chunk), stmt)
		if _, err := tx.ExecContext(bctx, stmt, args...); err != nil {
			return totalInserted, fmt.Errorf("failed to insert batch (%d rows): %w", len(chunk), err)
		}
		totalInserted += int64(len(chunk))
	}

	if err := tx.Commit(); err != nil {
		return totalInserted, fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return totalInserted, nil
}
