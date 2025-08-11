package copier

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/koltyakov/pgcopy/internal/utils"
)

const noAction = "NO ACTION"

// Timeouts for FK operations
const (
	fkDetectTimeout     = 2 * time.Minute
	fkDropTimeout       = 1 * time.Minute
	fkRestoreTimeout    = 2 * time.Minute
	fkReplicaSetTimeout = 10 * time.Second
	fkBackupExecTimeout = 30 * time.Second
)

// ForeignKey represents a foreign key constraint
type ForeignKey struct {
	ConstraintName    string
	Schema            string
	Table             string
	Columns           []string
	ReferencedSchema  string
	ReferencedTable   string
	ReferencedColumns []string
	OnDelete          string
	OnUpdate          string
	Definition        string // Full CREATE CONSTRAINT statement
}

// ForeignKeyManager handles foreign key operations
type ForeignKeyManager struct {
	db                   *sql.DB
	logger               *utils.SimpleLogger
	noTimeouts           bool
	foreignKeys          []ForeignKey
	droppedKeys          []ForeignKey
	restoredKeys         []ForeignKey
	useReplica           bool
	backupFile           string
	initialBackupDone    bool
	mu                   sync.RWMutex    // Protects all shared state operations
	processedConstraints map[string]bool // Track processed constraints to avoid duplicates
}

// Detect discovers foreign key constraints for provided tables (ForeignKeyStrategy implementation).
func (fkm *ForeignKeyManager) Detect(tables []*TableInfo) error { return fkm.DetectForeignKeys(tables) }

// Cleanup removes residual FK backup artifacts after successful run.
func (fkm *ForeignKeyManager) Cleanup() error { return fkm.CleanupBackupFile() }

// DropAllForeignKeys drops all incoming foreign keys referencing any of the planned tables.
// This prepares the database for TRUNCATE without CASCADE across the whole plan.
func (fkm *ForeignKeyManager) DropAllForeignKeys(tables []*TableInfo) error {
	if len(tables) == 0 {
		return nil
	}
	// Build a set of planned full table names for fast lookup
	planned := make(map[string]struct{}, len(tables))
	for _, t := range tables {
		planned[fmt.Sprintf("%s.%s", t.Schema, t.Name)] = struct{}{}
	}

	// Collect incoming FKs referencing any planned table
	toDrop := make([]ForeignKey, 0)
	for _, fk := range fkm.foreignKeys {
		refFull := fmt.Sprintf("%s.%s", fk.ReferencedSchema, fk.ReferencedTable)
		if _, ok := planned[refFull]; ok {
			// Drop all constraints that reference any planned table, including self-referencing,
			// so TRUNCATE can proceed without CASCADE.
			toDrop = append(toDrop, fk)
		}
	}

	if len(toDrop) == 0 {
		return nil
	}

	// One-time initial backup of all constraints slated for drop
	if !fkm.initialBackupDone {
		if err := fkm.writeInitialBackupSnapshot(toDrop); err != nil {
			return fmt.Errorf("failed to write initial FK backup: %w", err)
		}
	}

	fkm.logger.Info("Dropping %s foreign key constraint(s) globally before copy", utils.HighlightNumber(len(toDrop)))
	for _, fk := range toDrop {
		if err := fkm.dropForeignKey(&fk); err != nil {
			return fmt.Errorf("failed to drop FK %s on %s.%s: %w", fk.ConstraintName, fk.Schema, fk.Table, err)
		}
	}
	return nil
}

// RestoreAllForeignKeys attempts to restore all previously dropped FKs.
func (fkm *ForeignKeyManager) RestoreAllForeignKeys() error {
	fkm.mu.RLock()
	if len(fkm.droppedKeys) == 0 {
		fkm.mu.RUnlock()
		return nil
	}
	// Work on a snapshot to avoid mutation while iterating
	snapshot := make([]ForeignKey, len(fkm.droppedKeys))
	copy(snapshot, fkm.droppedKeys)
	fkm.mu.RUnlock()

	// Sort by dependency for safer recreation
	sorted := fkm.sortKeysByDependency(snapshot)
	restored := 0
	for _, fk := range sorted {
		constraintKey := fmt.Sprintf("%s.%s.%s", fk.Schema, fk.Table, fk.ConstraintName)
		// Mark as processed so per-table restore won't redo it
		fkm.mu.Lock()
		already := fkm.processedConstraints[constraintKey]
		if !already {
			fkm.processedConstraints[constraintKey] = true
		}
		fkm.mu.Unlock()

		// Recreate with retries similar to per-table restore
		maxRetries := 3
		var lastErr error
		expectedSig := fkm.computeFKSignature(&fk)
		for attempt := 1; attempt <= maxRetries; attempt++ {
			ctx, cancel := fkm.newCtx(fkRestoreTimeout)
			_, err := fkm.db.ExecContext(ctx, fk.Definition)
			cancel()
			if err == nil {
				lastErr = nil
				break
			}
			// If already exists and matches, consider restored
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "already exists") {
				if sig, ok, _ := fkm.getExistingFKSignature(fk.Schema, fk.Table, fk.ConstraintName); ok && sig == expectedSig {
					lastErr = nil
					break
				}
			}
			lastErr = err
			if attempt < maxRetries && fkm.shouldRetryFKError(err) {
				time.Sleep(fkm.retryDelay(attempt))
				continue
			}
			break
		}
		if lastErr == nil {
			restored++
			// Remove from droppedKeys and processed map
			fkm.mu.Lock()
			delete(fkm.processedConstraints, constraintKey)
			// Rebuild droppedKeys without this fk
			newDropped := make([]ForeignKey, 0, len(fkm.droppedKeys))
			for _, dk := range fkm.droppedKeys {
				same := dk.ConstraintName == fk.ConstraintName && dk.Schema == fk.Schema && dk.Table == fk.Table
				if !same {
					newDropped = append(newDropped, dk)
				}
			}
			fkm.droppedKeys = newDropped
			// Track restored for later validation
			fkm.restoredKeys = append(fkm.restoredKeys, fk)
			fkm.mu.Unlock()
		} else {
			fkm.logger.Warn("Failed to restore FK %s on %s: %v", utils.HighlightFKName(fk.ConstraintName), utils.HighlightTableName(fk.Schema, fk.Table), lastErr)
		}
	}

	if restored > 0 {
		fkm.logger.Info("Restored %s foreign key constraints globally", utils.HighlightNumber(restored))
	}
	return nil
}

// writeInitialBackupSnapshot writes the initial full list of FKs slated for drop to the backup file.
// This snapshot is created once up front and not mutated during the run.
func (fkm *ForeignKeyManager) writeInitialBackupSnapshot(fks []ForeignKey) error {
	// Deduplicate
	unique := make(map[string]ForeignKey)
	for _, fk := range fks {
		key := fmt.Sprintf("%s.%s.%s", fk.Schema, fk.Table, fk.ConstraintName)
		unique[key] = fk
	}
	// To slice
	list := make([]ForeignKey, 0, len(unique))
	for _, fk := range unique {
		list = append(list, fk)
	}
	// Sort for stable output
	sort.Slice(list, func(i, j int) bool {
		iKey := fmt.Sprintf("%s.%s.%s", list[i].Schema, list[i].Table, list[i].ConstraintName)
		jKey := fmt.Sprintf("%s.%s.%s", list[j].Schema, list[j].Table, list[j].ConstraintName)
		return iKey < jKey
	})
	var b strings.Builder
	b.WriteString("-- Foreign Key Backup File\n")
	b.WriteString("-- Generated by pgcopy - contains FK constraints to recreate\n")
	b.WriteString("-- Execute this file manually if pgcopy process was interrupted\n\n")
	for _, fk := range list {
		b.WriteString(fmt.Sprintf("%s;\n", fk.Definition))
	}
	content := strings.TrimSuffix(b.String(), "\n")
	if err := os.WriteFile(fkm.backupFile, []byte(content), 0600); err != nil {
		return err
	}
	fkm.initialBackupDone = true
	return nil
}

// NewForeignKeyManager creates a new foreign key manager
func NewForeignKeyManager(db *sql.DB, logger *utils.SimpleLogger, noTimeouts bool) *ForeignKeyManager {
	return &ForeignKeyManager{
		db:                   db,
		logger:               logger,
		noTimeouts:           noTimeouts,
		foreignKeys:          make([]ForeignKey, 0),
		droppedKeys:          make([]ForeignKey, 0),
		restoredKeys:         make([]ForeignKey, 0),
		backupFile:           ".fk_backup.sql",
		initialBackupDone:    false,
		processedConstraints: make(map[string]bool),
	}
}

// newCtx returns a context with timeout unless unlimited mode is enabled.
func (fkm *ForeignKeyManager) newCtx(d time.Duration) (context.Context, context.CancelFunc) {
	if fkm.noTimeouts {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), d)
}

// SetLogger updates the logger for the foreign key manager
func (fkm *ForeignKeyManager) SetLogger(logger *utils.SimpleLogger) {
	fkm.mu.Lock()
	defer fkm.mu.Unlock()
	fkm.logger = logger
}

// DetectForeignKeys discovers all foreign keys in the database
func (fkm *ForeignKeyManager) DetectForeignKeys(tables []*TableInfo) error {
	fkm.logger.Info("Detecting foreign key constraints...")

	// Build table filter values and parameterize with ANY($1) to avoid SQL concatenation
	tableFilters := make([]string, 0, len(tables))
	for _, table := range tables {
		tableFilters = append(tableFilters, fmt.Sprintf("%s.%s", table.Schema, table.Name))
	}

	if len(tableFilters) == 0 {
		return nil
	}

	// Build a parameterized IN clause to avoid array encoding dependency.
	// We only interpolate the placeholder markers ($1,$2,...) which are not user input.
	// #nosec G201
	placeholders := make([]string, len(tableFilters))
	args := make([]any, len(tableFilters))
	for i, tf := range tableFilters {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = tf
	}

	inClause := strings.Join(placeholders, ",")
	// #nosec G202 - safe: only placeholder markers are concatenated, values are passed as parameters
	// Include FKs where either the referencing table (tc.table_schema.table_name) OR
	// the referenced table (ccu.table_schema.table_name) is in scope. This ensures we
	// also drop constraints on tables not included in the copy set but referencing
	// tables we are about to truncate.
	query := `
		SELECT 
			tc.constraint_name,
			tc.table_schema,
			tc.table_name,
			string_agg(kcu.column_name, ',' ORDER BY kcu.ordinal_position) as columns,
			ccu.table_schema AS referenced_schema,
			ccu.table_name AS referenced_table,
			string_agg(ccu.column_name, ',' ORDER BY kcu.ordinal_position) as referenced_columns,
			rc.delete_rule,
			rc.update_rule
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu 
			ON tc.constraint_name = kcu.constraint_name 
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu 
			ON ccu.constraint_name = tc.constraint_name 
			AND ccu.table_schema = tc.table_schema
		JOIN information_schema.referential_constraints rc 
			ON tc.constraint_name = rc.constraint_name 
			AND tc.table_schema = rc.constraint_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND ((tc.table_schema || '.' || tc.table_name) IN (` + inClause + `)
				 OR (ccu.table_schema || '.' || ccu.table_name) IN (` + inClause + `))
		GROUP BY tc.constraint_name, tc.table_schema, tc.table_name, 
			ccu.table_schema, ccu.table_name, rc.delete_rule, rc.update_rule
		ORDER BY tc.table_schema, tc.table_name, tc.constraint_name`

	ctx, cancel := fkm.newCtx(fkDetectTimeout)
	defer cancel()
	rows, err := fkm.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to query foreign keys: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fkm.logger.Error("Failed to close foreign key rows: %v", err)
		}
	}()

	fkm.foreignKeys = make([]ForeignKey, 0)
	for rows.Next() {
		var fk ForeignKey
		var columns, referencedColumns string

		err := rows.Scan(
			&fk.ConstraintName,
			&fk.Schema,
			&fk.Table,
			&columns,
			&fk.ReferencedSchema,
			&fk.ReferencedTable,
			&referencedColumns,
			&fk.OnDelete,
			&fk.OnUpdate,
		)
		if err != nil {
			return fmt.Errorf("failed to scan foreign key: %w", err)
		}

		fk.Columns = strings.Split(columns, ",")
		fk.ReferencedColumns = strings.Split(referencedColumns, ",")

		// Build the constraint definition for recreation
		fk.Definition = fkm.buildConstraintDefinition(&fk)

		fkm.foreignKeys = append(fkm.foreignKeys, fk)
	}

	fkm.logger.Info("Found %s foreign key constraints", utils.HighlightNumber(len(fkm.foreignKeys)))
	return rows.Err()
}

// buildConstraintDefinition creates the ALTER TABLE statement to recreate the FK
func (fkm *ForeignKeyManager) buildConstraintDefinition(fk *ForeignKey) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s ",
		utils.QuoteTable(fk.Schema, fk.Table), utils.QuoteIdent(fk.ConstraintName)))

	builder.WriteString("FOREIGN KEY (")
	for i, col := range fk.Columns {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(utils.QuoteIdent(col))
	}
	builder.WriteString(") REFERENCES ")

	builder.WriteString(fmt.Sprintf("%s (", utils.QuoteTable(fk.ReferencedSchema, fk.ReferencedTable)))
	for i, col := range fk.ReferencedColumns {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(utils.QuoteIdent(col))
	}
	builder.WriteString(")")

	if fk.OnDelete != noAction {
		builder.WriteString(fmt.Sprintf(" ON DELETE %s", fk.OnDelete))
	}
	if fk.OnUpdate != noAction {
		builder.WriteString(fmt.Sprintf(" ON UPDATE %s", fk.OnUpdate))
	}
	// Create as NOT VALID to avoid heavy validation during load; validate after copy
	builder.WriteString(" NOT VALID")

	return builder.String()
}

// TryUseReplicaMode attempts to use replica mode for FK handling
func (fkm *ForeignKeyManager) TryUseReplicaMode() error {
	ctx, cancel := fkm.newCtx(fkReplicaSetTimeout)
	defer cancel()
	_, err := fkm.db.ExecContext(ctx, "SET session_replication_role = replica")
	if err != nil {
		fkm.logger.Warn("Cannot use replica mode (requires superuser), will drop/recreate FKs: %v", err)
		fkm.logger.Info("FK definitions will be backed up to: %s", fkm.backupFile)
		fkm.useReplica = false
		return nil
	}

	fkm.logger.Info("Using replica mode for foreign key handling")
	fkm.useReplica = true
	return nil
}

// DisableReplicaMode disables replica mode
func (fkm *ForeignKeyManager) DisableReplicaMode() error {
	if !fkm.IsUsingReplicaMode() {
		return nil
	}

	ctx, cancel := fkm.newCtx(fkReplicaSetTimeout)
	defer cancel()
	_, err := fkm.db.ExecContext(ctx, "SET session_replication_role = default")
	if err != nil {
		fkm.logger.Warn("Failed to restore session_replication_role: %v", err)
	}
	return err
}

// DropForeignKeysForTable drops all foreign keys that reference or are referenced by a table
func (fkm *ForeignKeyManager) DropForeignKeysForTable(table *TableInfo) error {
	// Replica mode no longer prevents drops: per request, allow FK drop/restore even in replica mode
	// Skip FK management for empty tables that won't be copied
	if table.TotalRows == 0 {
		fkm.logger.Info("Skipping FK management for empty table %s", utils.HighlightTableName(table.Schema, table.Name))
		return nil
	}

	// Find incoming FKs that reference this table (these block TRUNCATE)
	var incomingFKs []ForeignKey
	tableFullName := fmt.Sprintf("%s.%s", table.Schema, table.Name)

	for _, fk := range fkm.foreignKeys {
		fkTableName := fmt.Sprintf("%s.%s", fk.Schema, fk.Table)
		refTableName := fmt.Sprintf("%s.%s", fk.ReferencedSchema, fk.ReferencedTable)

		// Include incoming and self-referencing FKs that reference this table.
		if refTableName == tableFullName {
			incomingFKs = append(incomingFKs, fk)
		}
		_ = fkTableName // retained for clarity; outgoing FKs still not needed here
	}

	if len(incomingFKs) > 0 {
		fkm.logger.Info("Dropping %s incoming foreign key constraint(s) referencing %s",
			utils.HighlightNumber(len(incomingFKs)), utils.HighlightTableName(table.Schema, table.Name))
	}

	// Drop the incoming foreign keys that reference this table to allow TRUNCATE without CASCADE
	for _, fk := range incomingFKs {
		if err := fkm.dropForeignKey(&fk); err != nil {
			return fmt.Errorf("failed to drop FK %s on %s.%s: %w",
				fk.ConstraintName, fk.Schema, fk.Table, err)
		}
	}

	return nil
}

// shouldRetryFKError returns true for errors that are likely transient.
func (fkm *ForeignKeyManager) shouldRetryFKError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "deadlock detected"),
		strings.Contains(s, "lock not available"),
		strings.Contains(s, "could not obtain lock"),
		strings.Contains(s, "timeout"),
		strings.Contains(s, "canceling statement due to"),
		strings.Contains(s, "connection reset"),
		strings.Contains(s, "connection refused"),
		strings.Contains(s, "server closed the connection"),
		strings.Contains(s, "terminating connection due to administrator command"),
		strings.Contains(s, "serialization failure"),
		strings.Contains(s, "too many connections"):
		return true
	default:
		return false
	}
}

// retryDelay computes an exponential backoff delay with a cap.
func (fkm *ForeignKeyManager) retryDelay(attempt int) time.Duration {
	base := 200 * time.Millisecond
	d := base << (attempt - 1) // 200ms, 400ms, 800ms
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}

// computeFKSignature creates a deterministic signature string for an FK definition.
// It is suitable for hashing or direct equality comparison.
func (fkm *ForeignKeyManager) computeFKSignature(fk *ForeignKey) string {
	// Normalize case for actions and join lists with commas
	onDelete := strings.ToUpper(strings.TrimSpace(fk.OnDelete))
	if onDelete == "" {
		onDelete = noAction
	}
	onUpdate := strings.ToUpper(strings.TrimSpace(fk.OnUpdate))
	if onUpdate == "" {
		onUpdate = noAction
	}

	cols := strings.Join(fk.Columns, ",")
	refCols := strings.Join(fk.ReferencedColumns, ",")

	// Build a canonical signature
	base := fmt.Sprintf("%s.%s|%s|%s.%s|%s|del:%s|upd:%s",
		fk.Schema, fk.Table, cols, fk.ReferencedSchema, fk.ReferencedTable, refCols, onDelete, onUpdate,
	)
	// Hash to compact and stabilize the signature length
	sum := sha256.Sum256([]byte(base))
	return hex.EncodeToString(sum[:])
}

// getExistingFKSignature returns the signature of an existing FK in the DB if present.
func (fkm *ForeignKeyManager) getExistingFKSignature(schema, table, constraint string) (string, bool, error) {
	// Query current FK definition parts for the given constraint
	// #nosec G202 - identifiers are used via parameters
	query := `
		SELECT 
			string_agg(kcu.column_name, ',' ORDER BY kcu.ordinal_position) as columns,
			ccu.table_schema AS referenced_schema,
			ccu.table_name AS referenced_table,
			string_agg(ccu.column_name, ',' ORDER BY kcu.ordinal_position) as referenced_columns,
			rc.delete_rule,
			rc.update_rule
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu 
			ON tc.constraint_name = kcu.constraint_name 
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu 
			ON ccu.constraint_name = tc.constraint_name 
			AND ccu.table_schema = tc.table_schema
		JOIN information_schema.referential_constraints rc 
			ON tc.constraint_name = rc.constraint_name 
			AND tc.table_schema = rc.constraint_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = $1 AND tc.table_name = $2 AND tc.constraint_name = $3
		GROUP BY ccu.table_schema, ccu.table_name, rc.delete_rule, rc.update_rule`

	ctx, cancel := fkm.newCtx(fkDetectTimeout)
	defer cancel()
	row := fkm.db.QueryRowContext(ctx, query, schema, table, constraint)
	var cols, refSchema, refTable, refCols, del, upd string
	if err := row.Scan(&cols, &refSchema, &refTable, &refCols, &del, &upd); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}

	// Build a temporary FK object to reuse signature computation
	tmp := ForeignKey{
		ConstraintName:    constraint,
		Schema:            schema,
		Table:             table,
		Columns:           strings.Split(cols, ","),
		ReferencedSchema:  refSchema,
		ReferencedTable:   refTable,
		ReferencedColumns: strings.Split(refCols, ","),
		OnDelete:          del,
		OnUpdate:          upd,
	}
	return fkm.computeFKSignature(&tmp), true, nil
}

// dropForeignKey drops a specific foreign key constraint
func (fkm *ForeignKeyManager) dropForeignKey(fk *ForeignKey) error {
	fkm.mu.Lock()
	defer fkm.mu.Unlock()

	constraintKey := fmt.Sprintf("%s.%s.%s", fk.Schema, fk.Table, fk.ConstraintName)

	// Check if already processed
	if fkm.processedConstraints[constraintKey] {
		return nil // Already processed
	}

	// Check if already dropped
	for _, dropped := range fkm.droppedKeys {
		if dropped.ConstraintName == fk.ConstraintName &&
			dropped.Schema == fk.Schema &&
			dropped.Table == fk.Table {
			return nil // Already dropped
		}
	}

	query := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s",
		utils.QuoteTable(fk.Schema, fk.Table), utils.QuoteIdent(fk.ConstraintName))

	fkm.logger.Info("Dropping FK constraint %s on %s", utils.HighlightFKName(fk.ConstraintName), utils.HighlightTableName(fk.Schema, fk.Table))

	// Release lock temporarily for database operation
	fkm.mu.Unlock()
	ctx, cancel := fkm.newCtx(fkDropTimeout)
	_, err := fkm.db.ExecContext(ctx, query)
	cancel()
	fkm.mu.Lock()

	if err != nil {
		return err
	}

	// Mark as processed and add to dropped list
	fkm.processedConstraints[constraintKey] = true
	fkm.droppedKeys = append(fkm.droppedKeys, *fk)

	// Clean up duplicates in droppedKeys
	fkm.deduplicateDroppedKeysLocked()

	// Do not mutate backup file mid-run; initial snapshot was created up-front.

	return nil
}

// deduplicateDroppedKeys removes duplicate entries from droppedKeys slice
func (fkm *ForeignKeyManager) deduplicateDroppedKeys() {
	fkm.mu.Lock()
	defer fkm.mu.Unlock()
	fkm.deduplicateDroppedKeysLocked()
}

// deduplicateDroppedKeysLocked removes duplicate entries from droppedKeys slice (must hold mu.Lock)
func (fkm *ForeignKeyManager) deduplicateDroppedKeysLocked() {
	uniqueFKs := make(map[string]ForeignKey)
	for _, fk := range fkm.droppedKeys {
		constraintKey := fmt.Sprintf("%s.%s.%s", fk.Schema, fk.Table, fk.ConstraintName)
		uniqueFKs[constraintKey] = fk
	}

	// Rebuild the slice with unique entries
	fkm.droppedKeys = make([]ForeignKey, 0, len(uniqueFKs))
	for _, fk := range uniqueFKs {
		fkm.droppedKeys = append(fkm.droppedKeys, fk)
	}
}

// sortKeysByDependency sorts foreign keys to minimize restoration conflicts
func (fkm *ForeignKeyManager) sortKeysByDependency(keys []ForeignKey) []ForeignKey {
	// Create a copy to sort
	sorted := make([]ForeignKey, len(keys))
	copy(sorted, keys)

	// Simple sort: self-referencing tables last, then by table name
	sort.Slice(sorted, func(i, j int) bool {
		iSelfRef := sorted[i].Schema == sorted[i].ReferencedSchema &&
			sorted[i].Table == sorted[i].ReferencedTable
		jSelfRef := sorted[j].Schema == sorted[j].ReferencedSchema &&
			sorted[j].Table == sorted[j].ReferencedTable

		// Self-referencing FKs go last
		if iSelfRef != jSelfRef {
			return jSelfRef
		}

		// Then sort by table name for consistency
		iTable := fmt.Sprintf("%s.%s", sorted[i].Schema, sorted[i].Table)
		jTable := fmt.Sprintf("%s.%s", sorted[j].Schema, sorted[j].Table)
		return iTable < jTable
	})

	return sorted
}

// GetForeignKeyStats returns statistics about foreign keys
func (fkm *ForeignKeyManager) GetForeignKeyStats() (total, dropped int) {
	fkm.mu.RLock()
	defer fkm.mu.RUnlock()
	return len(fkm.foreignKeys), len(fkm.droppedKeys)
}

// IsUsingReplicaMode returns whether replica mode is enabled
func (fkm *ForeignKeyManager) IsUsingReplicaMode() bool {
	return fkm.useReplica
}

// writeBackupSnapshot writes a complete snapshot of unrestored FKs to the backup file
// Note: mid-run backup snapshot updates were intentionally removed to keep the backup immutable.

// CleanupBackupFile removes the backup file on successful completion
func (fkm *ForeignKeyManager) CleanupBackupFile() error {
	fkm.mu.Lock()
	defer fkm.mu.Unlock()

	if _, err := os.Stat(fkm.backupFile); os.IsNotExist(err) {
		return nil // File doesn't exist
	}

	fkm.logger.Info("All foreign keys restored successfully, cleaning up backup file: %s", fkm.backupFile)
	return os.Remove(fkm.backupFile)
}

// RecoverFromBackupFile attempts to restore FKs from backup file if they exist but weren't tracked
func (fkm *ForeignKeyManager) RecoverFromBackupFile() error {
	if fkm.IsUsingReplicaMode() {
		return nil
	}

	// Check if backup file exists
	if _, err := os.Stat(fkm.backupFile); os.IsNotExist(err) {
		return nil // No backup file to recover from
	}

	fkm.logger.Info("Found FK backup file, attempting to recover any untracked foreign keys...")

	// Try to execute the backup file to restore any remaining FKs
	content, err := os.ReadFile(fkm.backupFile)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	contentStr := string(content)
	if len(contentStr) == 0 {
		return nil
	}

	// Split into individual FK statements and execute them
	lines := strings.Split(contentStr, "\n")
	restored := 0
	var errors []error
	// Regex to parse: ALTER TABLE "schema"."table" ADD CONSTRAINT "constraint" ...;
	re := regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+"([^"]+)"\."([^"]+)"\s+ADD\s+CONSTRAINT\s+"([^"]+)"\b`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "ALTER TABLE") && strings.HasSuffix(line, ";") {
			// Remove trailing semicolon
			statement := strings.TrimSuffix(line, ";")

			ctx, cancel := fkm.newCtx(fkBackupExecTimeout)
			_, err := fkm.db.ExecContext(ctx, statement)
			cancel()
			if err != nil {
				// Ignore "already exists" errors, but log others
				if !strings.Contains(err.Error(), "already exists") {
					errors = append(errors, fmt.Errorf("failed to restore FK from backup: %w", err))
					fkm.logger.Warn("Failed to restore FK from backup: %v", err)
				}
			} else {
				restored++
				fkm.logger.Info("Successfully restored FK from backup file")
				// Record for validation stage by parsing the ALTER statement
				if m := re.FindStringSubmatch(line); len(m) == 4 {
					fkm.mu.Lock()
					fkm.restoredKeys = append(fkm.restoredKeys, ForeignKey{Schema: m[1], Table: m[2], ConstraintName: m[3]})
					fkm.mu.Unlock()
				}
			}
		}
	}

	if restored > 0 {
		fkm.logger.Info("Recovered %s foreign key constraints from backup file", utils.HighlightNumber(restored))
	}

	if len(errors) > 0 {
		return fmt.Errorf("%w: encountered %d errors during backup recovery", utils.ErrFKRecoveryFailed, len(errors))
	}

	return nil
}

// ValidateRestoredForeignKeys validates constraints created as NOT VALID during restoration.
func (fkm *ForeignKeyManager) ValidateRestoredForeignKeys() error {
	fkm.mu.RLock()
	if len(fkm.restoredKeys) == 0 {
		fkm.mu.RUnlock()
		return nil
	}
	// Snapshot
	toValidate := make([]ForeignKey, len(fkm.restoredKeys))
	copy(toValidate, fkm.restoredKeys)
	fkm.mu.RUnlock()

	// Validate each
	validated := 0
	for _, fk := range toValidate {
		stmt := fmt.Sprintf("ALTER TABLE %s VALIDATE CONSTRAINT %s", utils.QuoteTable(fk.Schema, fk.Table), utils.QuoteIdent(fk.ConstraintName))
		ctx, cancel := fkm.newCtx(fkRestoreTimeout)
		_, err := fkm.db.ExecContext(ctx, stmt)
		cancel()
		if err != nil {
			// Log and continue; these may be already valid or conflicting
			fkm.logger.Warn("Failed to validate FK %s on %s: %v", utils.HighlightFKName(fk.ConstraintName), utils.HighlightTableName(fk.Schema, fk.Table), err)
			continue
		}
		validated++
	}
	if validated > 0 {
		fkm.logger.Info("Validated %s foreign key constraints", utils.HighlightNumber(validated))
	}
	// Clear list
	fkm.mu.Lock()
	fkm.restoredKeys = fkm.restoredKeys[:0]
	fkm.mu.Unlock()
	return nil
}
