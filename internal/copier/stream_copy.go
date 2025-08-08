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

	// If using replica mode FK strategy, set session_replication_role=replica so inserts bypass FK checks.
	if c.fkManager != nil && c.fkManager.IsUsingReplicaMode() {
		if _, err := dstConn.Exec(ctx, "SET session_replication_role = replica"); err != nil {
			c.logger.Warn("Failed to set replica mode on streaming destination connection: %v", err)
		}
	}

	cols := table.Columns
	columnList := utils.QuoteJoinIdents(cols)
	qt := utils.QuoteTable(table.Schema, table.Name)
	copyOutSQL := fmt.Sprintf("COPY %s (%s) TO STDOUT (FORMAT binary)", qt, columnList)
	copyInSQL := fmt.Sprintf("COPY %s (%s) FROM STDIN (FORMAT binary)", qt, columnList)

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

	startIn := time.Now()
	if _, err := dstConn.PgConn().CopyFrom(ctx, r, copyInSQL); err != nil {
		return fmt.Errorf("copy in failed: %w", err)
	}
	c.logger.Info("Applied streamed data to destination for %s in %s", utils.HighlightTableName(table.Schema, table.Name), utils.FormatDuration(time.Since(startIn)))

	// We cannot easily update per-row progress in binary streaming mode without decoding.
	// Mark table progress as complete here; higher-level code will log completion.
	c.updateTableProgress(table.Schema, table.Name, table.TotalRows)
	c.updateProgress(table.TotalRows)

	return nil
}

// quoteJoin joins identifiers with quoting.
// moved to utils.QuoteJoinIdents
