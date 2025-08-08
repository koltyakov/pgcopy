package copier

import (
	"database/sql"
	"fmt"
)

// resetSequencesForTable detects columns backed by sequences and adjusts the sequence's last value
// so that nextval() continues from MAX(column) (or from the sequence start if the table is empty).
func (c *Copier) resetSequencesForTable(table *TableInfo) error { //nolint:funlen
	// Discover sequence-backed columns using pg_get_serial_sequence
	// Then read start_value from pg_sequence (OID join via regclass)
	const seqQuery = `
WITH cols AS (
  SELECT a.attname AS column_name,
         pg_get_serial_sequence(format('%I.%I', ns.nspname, c.relname), a.attname) AS seq_reg
  FROM pg_class c
  JOIN pg_namespace ns ON ns.oid = c.relnamespace
  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
  WHERE ns.nspname = $1 AND c.relname = $2
),
seqs AS (
  SELECT column_name,
         seq_reg::regclass AS seq_regclass
  FROM cols
  WHERE seq_reg IS NOT NULL
)
SELECT s.column_name,
			 s.seq_regclass::text AS seq_fullname,
			 ps.start_value
FROM seqs s
JOIN pg_sequences ps
	ON (ps.schemaname || '.' || ps.sequencename) = s.seq_regclass::text`

	rows, err := c.destDB.Query(seqQuery, table.Schema, table.Name)
	if err != nil {
		return fmt.Errorf("failed to query sequences for %s.%s: %w", table.Schema, table.Name, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			c.logger.Error("Failed to close sequence rows: %v", err)
		}
	}()

	type seqInfo struct {
		columnName  string
		seqFullname string
		startValue  int64
	}

	var seqs []seqInfo
	for rows.Next() {
		var col, seqFull string
		var start sql.NullInt64
		if err := rows.Scan(&col, &seqFull, &start); err != nil {
			return fmt.Errorf("failed to scan sequence info: %w", err)
		}
		sv := int64(1)
		if start.Valid {
			sv = start.Int64
		}
		seqs = append(seqs, seqInfo{columnName: col, seqFullname: seqFull, startValue: sv})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sequence rows iteration error: %w", err)
	}

	// Nothing to do
	if len(seqs) == 0 {
		return nil
	}

	// For each sequence-backed column, compute MAX(column) on the destination and set sequence
	for _, s := range seqs {
		// Compute max value for the column
		maxQuery := fmt.Sprintf( // #nosec G201 - identifiers originate from system catalogs and are safely double-quoted
			`SELECT MAX("%s") FROM %s."%s"`, s.columnName, table.Schema, table.Name)
		var maxVal sql.NullInt64
		if err := c.destDB.QueryRow(maxQuery).Scan(&maxVal); err != nil {
			return fmt.Errorf("failed to compute MAX(%s) for %s.%s: %w", s.columnName, table.Schema, table.Name, err)
		}

		var setTo int64
		var isCalled bool
		if maxVal.Valid {
			// Set last_value to MAX(col), with is_called=true so nextval() returns MAX+1
			setTo = maxVal.Int64
			isCalled = true
		} else {
			// Table empty: set to start_value-1 and is_called=false so nextval() returns start_value
			setTo = s.startValue - 1
			if setTo < 0 {
				setTo = 0
			}
			isCalled = false
		}

		// Use setval(text, bigint, boolean) with parameters to avoid quoting issues
		if _, err := c.destDB.Exec(`SELECT setval($1::regclass, $2, $3)`, s.seqFullname, setTo, isCalled); err != nil {
			return fmt.Errorf("failed to set sequence %s to %d (is_called=%t): %w", s.seqFullname, setTo, isCalled, err)
		}
		c.logger.Info("Sequence %s aligned to %d (is_called=%t) for %s.%s.%s", s.seqFullname, setTo, isCalled, table.Schema, table.Name, s.columnName)
	}

	return nil
}
