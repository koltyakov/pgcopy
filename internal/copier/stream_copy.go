package copier

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/koltyakov/pgcopy/internal/utils"
)

// copyTableViaPipe streams data using COPY ... TO STDOUT / FROM STDIN (binary) optionally gzip-compressed.
func (c *Copier) copyTableViaPipe(ctx context.Context, table *TableInfo) error { //nolint:funlen
	// Establish pgx connections (separate from existing *sql.DB pool) per table for now.
	srcConn, err := pgx.Connect(ctx, c.config.SourceConn)
	if err != nil {
		return fmt.Errorf("pgx connect source: %w", err)
	}
	defer func() {
		if cerr := srcConn.Close(ctx); cerr != nil {
			c.logger.Warn("Error closing source pgx connection: %v", cerr)
		}
	}()

	dstConn, err := pgx.Connect(ctx, c.config.TargetConn)
	if err != nil {
		return fmt.Errorf("pgx connect target: %w", err)
	}
	defer func() {
		if cerr := dstConn.Close(ctx); cerr != nil {
			c.logger.Warn("Error closing target pgx connection: %v", cerr)
		}
	}()

	// We'll use a transaction on destination for TRUNCATE + COPY, with optional replica mode set locally.
	tx, err := dstConn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dest tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rerr := tx.Rollback(ctx); rerr != nil {
				c.logger.Warn("Failed to rollback destination tx: %v", rerr)
			}
		}
	}()

	if c.fkManager != nil && c.fkManager.IsUsingReplicaMode() {
		if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
			c.logger.Warn("Failed to set LOCAL replica mode on streaming tx: %v", err)
		}
	}

	cols := table.Columns
	columnList := utils.QuoteJoinIdents(cols)
	qt := utils.QuoteTable(table.Schema, table.Name)
	copyOutSQL := fmt.Sprintf("COPY %s (%s) TO STDOUT (FORMAT binary)", qt, columnList)
	copyInSQL := fmt.Sprintf("COPY %s (%s) FROM STDIN (FORMAT binary)", qt, columnList)

	// Ensure destination table is empty within the same tx so failures rollback.
	// No CASCADE: FK handling is managed outside or via replica mode.
	if _, err := tx.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", qt)); err != nil {
		return fmt.Errorf("truncate in streaming tx failed: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	defer func() { _ = pr.Close() }()
	go c.writeCopyStream(ctx, srcConn, pw, copyOutSQL, table)

	// Reader: pipe -> (optional gunzip) -> dest COPY IN
	var r io.Reader = pr
	if c.config.CompressPipe {
		gzr, err := gzip.NewReader(pr)
		if err != nil {
			return fmt.Errorf("gzip reader: %w", err)
		}
		defer func() {
			if cerr := gzr.Close(); cerr != nil {
				c.logger.Warn("Error closing gzip reader: %v", cerr)
			}
		}()
		r = gzr
	}

	// Optional progress poller (PostgreSQL 14+): sample pg_stat_progress_copy for this backend PID.
	// Uses a separate connection because dstConn is busy with COPY FROM. If the view isn't available,
	// or no row is visible, the poller exits quietly.
	progressRows, stopProgressPoll := c.startCopyProgressPoller(ctx, table, dstConn.PgConn().PID())

	startIn := time.Now()
	tag, err := tx.Conn().PgConn().CopyFrom(ctx, r, copyInSQL)
	if err != nil {
		stopProgressPoll()
		return fmt.Errorf("copy in failed: %w", err)
	}
	stopProgressPoll()
	// Commit the transaction so TRUNCATE + COPY are atomic
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dest tx failed: %w", err)
	}
	committed = true
	c.logger.Info("Applied streamed data to destination for %s in %s", utils.HighlightTableName(table.Schema, table.Name), utils.FormatDuration(time.Since(startIn)))

	actualRows := tag.RowsAffected()
	trackedRows := progressRows.Load()
	if actualRows < trackedRows {
		actualRows = trackedRows
	}
	c.updateTableProgress(table.Schema, table.Name, actualRows)
	if actualRows > trackedRows {
		c.updateProgress(actualRows - trackedRows)
	}

	return nil
}

func (c *Copier) writeCopyStream(ctx context.Context, srcConn *pgx.Conn, pw *io.PipeWriter, copyOutSQL string, table *TableInfo) {
	var streamErr error
	defer func() {
		if recovered := recover(); recovered != nil {
			c.logger.Error("Stream copy writer goroutine panicked: %v", recovered)
			streamErr = fmt.Errorf("writer panic: %v", recovered)
		}
		_ = pw.CloseWithError(streamErr)
	}()

	var writer io.Writer = pw
	var gz *gzip.Writer
	if c.config.CompressPipe {
		gz = gzip.NewWriter(pw)
		writer = gz
	}

	start := time.Now()
	// CopyTo returns a CommandTag whose row count describes copied rows, not bytes.
	tag, err := srcConn.PgConn().CopyTo(ctx, writer, copyOutSQL)
	if err != nil {
		streamErr = fmt.Errorf("copy out failed: %w", err)
		return
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			streamErr = fmt.Errorf("close gzip writer: %w", err)
			return
		}
	}

	c.logger.Info("Streamed %s rows from source for %s in %s", utils.FormatNumber(tag.RowsAffected()), utils.HighlightTableName(table.Schema, table.Name), utils.FormatDuration(time.Since(start)))
}

func (c *Copier) startCopyProgressPoller(ctx context.Context, table *TableInfo, pid uint32) (*atomic.Int64, func()) {
	progressRows := &atomic.Int64{}
	pollCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil {
				c.logger.Error("Progress poller goroutine panicked: %v", recovered)
			}
		}()
		c.pollCopyProgress(pollCtx, table, pid, progressRows)
	}()

	stop := func() {
		cancel()
		<-done
	}
	return progressRows, stop
}

func (c *Copier) pollCopyProgress(ctx context.Context, table *TableInfo, pid uint32, progressRows *atomic.Int64) {
	conn, err := pgx.Connect(ctx, c.config.TargetConn)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(ctx) }()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var last int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var processed int64
			// pg_stat_progress_copy exists in PostgreSQL 14+. Missing rows or views stop polling quietly.
			err = conn.QueryRow(ctx, `
				select coalesce(tuples_processed, 0)
				from pg_stat_progress_copy
				where pid = $1
				limit 1`, int(pid)).Scan(&processed) //nolint:gosec // PID narrowing to int is safe on supported platforms
			if err != nil {
				return
			}
			if processed > last {
				increment := processed - last
				last = processed
				progressRows.Store(processed)
				c.updateTableProgress(table.Schema, table.Name, processed)
				c.updateProgress(increment)
			}
		}
	}
}
