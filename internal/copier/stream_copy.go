package copier

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
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

	dstConn, err := pgx.Connect(ctx, c.config.DestConn)
	if err != nil {
		return fmt.Errorf("pgx connect dest: %w", err)
	}
	defer func() {
		if cerr := dstConn.Close(ctx); cerr != nil {
			c.logger.Warn("Error closing destination pgx connection: %v", cerr)
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

	// Writer goroutine: source -> (optional gzip) -> pipe
	go func() {
		defer func() { _ = pw.Close() }()
		var w io.Writer = pw
		var gz *gzip.Writer
		if c.config.CompressPipe {
			gz = gzip.NewWriter(pw)
			w = gz
		}

		start := time.Now()
		// CopyTo returns a CommandTag (which includes row count for COPY TO, not bytes).
		tag, err := srcConn.PgConn().CopyTo(ctx, w, copyOutSQL)
		if gz != nil {
			_ = gz.Close()
		}
		if err != nil {
			pw.CloseWithError(fmt.Errorf("copy out failed: %w", err))
			return
		}
		c.logger.Info("Streamed %s rows from source for %s in %s", utils.FormatNumber(tag.RowsAffected()), utils.HighlightTableName(table.Schema, table.Name), utils.FormatDuration(time.Since(start)))
	}()

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
	stopPoll := make(chan struct{})
	go func(schema, name string, pid uint32, _ int64) {
		progCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		conn, err := pgx.Connect(progCtx, c.config.DestConn)
		if err != nil {
			// Can't poll, just return silently
			return
		}
		defer func() { _ = conn.Close(progCtx) }()

		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()

		var last int64
		for {
			select {
			case <-stopPoll:
				return
			case <-progCtx.Done():
				return
			case <-t.C:
				var processed int64
				// Note: pg_stat_progress_copy exists in PG14+. For STDIN streams, bytes_total may be NULL/0.
				// We rely on tuples_processed and our planned TotalRows as denominator.
				err = conn.QueryRow(progCtx, `
					select coalesce(tuples_processed, 0)
					from pg_stat_progress_copy
					where pid = $1
					limit 1`, int(pid)).Scan(&processed) //nolint:gosec // PID narrowing to int for query parameter is safe on supported platforms
				if err != nil {
					// If the view isn't present or row not visible, stop polling.
					return
				}
				if processed > last {
					inc := processed - last
					last = processed
					c.updateTableProgress(schema, name, inc)
					c.updateProgress(inc)
				}
			}
		}
	}(table.Schema, table.Name, dstConn.PgConn().PID(), table.TotalRows)

	startIn := time.Now()
	if _, err := tx.Conn().PgConn().CopyFrom(ctx, r, copyInSQL); err != nil {
		close(stopPoll)
		return fmt.Errorf("copy in failed: %w", err)
	}
	close(stopPoll)
	// Commit the transaction so TRUNCATE + COPY are atomic
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dest tx failed: %w", err)
	}
	committed = true
	c.logger.Info("Applied streamed data to destination for %s in %s", utils.HighlightTableName(table.Schema, table.Name), utils.FormatDuration(time.Since(startIn)))

	// We cannot easily update per-row progress in binary streaming mode without decoding.
	// Mark table progress as complete here; higher-level code will log completion.
	c.updateTableProgress(table.Schema, table.Name, table.TotalRows)
	c.updateProgress(table.TotalRows)

	return nil
}
