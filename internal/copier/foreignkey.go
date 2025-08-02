package copier

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/koltyakov/pgcopy/internal/utils"
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
	foreignKeys          []ForeignKey
	droppedKeys          []ForeignKey
	useReplica           bool
	backupFile           string
	mu                   sync.RWMutex    // Protects all shared state operations
	processedConstraints map[string]bool // Track processed constraints to avoid duplicates
}

// NewForeignKeyManager creates a new foreign key manager
func NewForeignKeyManager(db *sql.DB, logger *utils.SimpleLogger) *ForeignKeyManager {
	return &ForeignKeyManager{
		db:                   db,
		logger:               logger,
		foreignKeys:          make([]ForeignKey, 0),
		droppedKeys:          make([]ForeignKey, 0),
		backupFile:           ".fk_backup.sql",
		processedConstraints: make(map[string]bool),
	}
}

// DetectForeignKeys discovers all foreign keys in the database
func (fkm *ForeignKeyManager) DetectForeignKeys(tables []*TableInfo) error {
	fkm.logger.LogProgress("Detecting foreign key constraints...")

	// Build table filter for IN clause
	tableFilters := make([]string, 0, len(tables))
	for _, table := range tables {
		tableFilters = append(tableFilters, fmt.Sprintf("'%s.%s'", table.Schema, table.Name))
	}

	if len(tableFilters) == 0 {
		return nil
	}

	// #nosec G202 - SQL concatenation is safe here as tableFilters contains sanitized schema.table names from database
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
			AND (tc.table_schema || '.' || tc.table_name) IN (` + strings.Join(tableFilters, ",") + `)
		GROUP BY tc.constraint_name, tc.table_schema, tc.table_name, 
			ccu.table_schema, ccu.table_name, rc.delete_rule, rc.update_rule
		ORDER BY tc.table_schema, tc.table_name, tc.constraint_name`

	rows, err := fkm.db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query foreign keys: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fkm.logger.LogError("Failed to close foreign key rows: %v", err)
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

	fkm.logger.LogSuccess("Found %s foreign key constraints", utils.HighlightNumber(len(fkm.foreignKeys)))
	return rows.Err()
}

// buildConstraintDefinition creates the ALTER TABLE statement to recreate the FK
func (fkm *ForeignKeyManager) buildConstraintDefinition(fk *ForeignKey) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("ALTER TABLE \"%s\".\"%s\" ADD CONSTRAINT \"%s\" ",
		fk.Schema, fk.Table, fk.ConstraintName))

	builder.WriteString("FOREIGN KEY (")
	for i, col := range fk.Columns {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(fmt.Sprintf("\"%s\"", col))
	}
	builder.WriteString(") REFERENCES ")

	builder.WriteString(fmt.Sprintf("\"%s\".\"%s\" (", fk.ReferencedSchema, fk.ReferencedTable))
	for i, col := range fk.ReferencedColumns {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(fmt.Sprintf("\"%s\"", col))
	}
	builder.WriteString(")")

	if fk.OnDelete != "NO ACTION" {
		builder.WriteString(fmt.Sprintf(" ON DELETE %s", fk.OnDelete))
	}
	if fk.OnUpdate != "NO ACTION" {
		builder.WriteString(fmt.Sprintf(" ON UPDATE %s", fk.OnUpdate))
	}

	return builder.String()
}

// TryUseReplicaMode attempts to use replica mode for FK handling
func (fkm *ForeignKeyManager) TryUseReplicaMode() error {
	_, err := fkm.db.Exec("SET session_replication_role = replica")
	if err != nil {
		fkm.logger.LogWarn("Cannot use replica mode (requires superuser), will drop/recreate FKs: %v", err)
		fkm.logger.LogProgress("FK definitions will be backed up to: %s", fkm.backupFile)
		fkm.useReplica = false
		return nil
	}

	fkm.logger.LogSuccess("Using replica mode for foreign key handling")
	fkm.useReplica = true
	return nil
}

// DisableReplicaMode disables replica mode
func (fkm *ForeignKeyManager) DisableReplicaMode() error {
	if !fkm.useReplica {
		return nil
	}

	_, err := fkm.db.Exec("SET session_replication_role = default")
	if err != nil {
		fkm.logger.LogWarn("Failed to restore session_replication_role: %v", err)
	}
	return err
}

// DropForeignKeysForTable drops all foreign keys that reference or are referenced by a table
func (fkm *ForeignKeyManager) DropForeignKeysForTable(table *TableInfo) error {
	// Skip FK management for empty tables that won't be copied
	if table.RowCount == 0 {
		fkm.logger.LogProgress("Skipping FK management for empty table %s", utils.HighlightTableName(table.Schema, table.Name))
		return nil
	}

	// Find all FKs that need to be handled for this table
	var relatedFKs []ForeignKey
	tableFullName := fmt.Sprintf("%s.%s", table.Schema, table.Name)

	for _, fk := range fkm.foreignKeys {
		fkTableName := fmt.Sprintf("%s.%s", fk.Schema, fk.Table)
		refTableName := fmt.Sprintf("%s.%s", fk.ReferencedSchema, fk.ReferencedTable)

		// Include if this table has FK constraints or is referenced by FK constraints
		if fkTableName == tableFullName || refTableName == tableFullName {
			relatedFKs = append(relatedFKs, fk)
		}
	}

	if len(relatedFKs) > 0 {
		if fkm.useReplica {
			// fkm.logger.logSuccess("Foreign keys for %s handled via replica mode (%s constraints)",
			// 	highlightTableName(table.Schema, table.Name), highlightNumber(len(relatedFKs)))
			return nil
		}
		fkm.logger.LogProgress("Managing %s foreign key constraint(s) for table %s",
			utils.HighlightNumber(len(relatedFKs)), utils.HighlightTableName(table.Schema, table.Name))
	}

	// Drop the foreign keys if not using replica mode
	for _, fk := range relatedFKs {
		if err := fkm.dropForeignKey(&fk); err != nil {
			return fmt.Errorf("failed to drop FK %s on %s.%s: %w",
				fk.ConstraintName, fk.Schema, fk.Table, err)
		}
	}

	return nil
}

// RestoreForeignKeysForTable restores foreign keys related to a specific table
func (fkm *ForeignKeyManager) RestoreForeignKeysForTable(table *TableInfo) error {
	if fkm.useReplica {
		// No need to restore in replica mode
		return nil
	}

	fkm.mu.RLock()
	if len(fkm.droppedKeys) == 0 {
		fkm.mu.RUnlock()
		return nil
	}

	tableFullName := fmt.Sprintf("%s.%s", table.Schema, table.Name)
	var toRestore []ForeignKey
	var remaining []ForeignKey

	// Find FKs related to this table
	for _, fk := range fkm.droppedKeys {
		fkTableName := fmt.Sprintf("%s.%s", fk.Schema, fk.Table)
		refTableName := fmt.Sprintf("%s.%s", fk.ReferencedSchema, fk.ReferencedTable)

		if fkTableName == tableFullName || refTableName == tableFullName {
			toRestore = append(toRestore, fk)
		} else {
			remaining = append(remaining, fk)
		}
	}
	fkm.mu.RUnlock()

	if len(toRestore) == 0 {
		return nil
	}

	fkm.logger.LogProgress("Restoring %s foreign key constraint(s) for table %s", utils.HighlightNumber(len(toRestore)), utils.HighlightTableName(table.Schema, table.Name))

	// Sort by dependency order
	sortedKeys := fkm.sortKeysByDependency(toRestore)
	restored := 0

	for _, fk := range sortedKeys {
		constraintKey := fmt.Sprintf("%s.%s.%s", fk.Schema, fk.Table, fk.ConstraintName)

		fkm.mu.RLock()
		wasProcessed := fkm.processedConstraints[constraintKey]
		fkm.mu.RUnlock()

		// Skip if already restored
		if !wasProcessed {
			fkm.logger.LogWarn("FK %s on %s was not in dropped list", utils.HighlightFKName(fk.ConstraintName), utils.HighlightTableName(fk.Schema, fk.Table))
			continue
		}

		fkm.logger.LogProgress("Restoring FK constraint %s on %s", utils.HighlightFKName(fk.ConstraintName), utils.HighlightTableName(fk.Schema, fk.Table))

		_, err := fkm.db.Exec(fk.Definition)
		if err != nil {
			fkm.logger.LogWarn("Failed to restore FK %s on %s: %v", utils.HighlightFKName(fk.ConstraintName), utils.HighlightTableName(fk.Schema, fk.Table), err)
			// Keep it in the dropped list for later retry
			remaining = append(remaining, fk)
		} else {
			restored++
			// Mark as restored (remove from processed constraints)
			fkm.mu.Lock()
			delete(fkm.processedConstraints, constraintKey)
			fkm.mu.Unlock()
		}
	}

	// Update the dropped keys list
	fkm.mu.Lock()
	fkm.droppedKeys = remaining
	fkm.mu.Unlock()

	// Update backup file snapshot
	if backupErr := fkm.writeBackupSnapshot(); backupErr != nil {
		fkm.logger.LogWarn("Failed to update FK backup file: %v", backupErr)
	}

	if restored > 0 {
		fkm.logger.LogSuccess("Successfully restored %s/%s foreign key constraints for table %s",
			utils.HighlightNumber(restored), utils.HighlightNumber(len(toRestore)), utils.HighlightTableName(table.Schema, table.Name))
	}

	return nil
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

	query := fmt.Sprintf("ALTER TABLE \"%s\".\"%s\" DROP CONSTRAINT IF EXISTS \"%s\"",
		fk.Schema, fk.Table, fk.ConstraintName)

	fkm.logger.LogProgress("Dropping FK constraint %s on %s", utils.HighlightFKName(fk.ConstraintName), utils.HighlightTableName(fk.Schema, fk.Table))

	// Release lock temporarily for database operation
	fkm.mu.Unlock()
	_, err := fkm.db.Exec(query)
	fkm.mu.Lock()

	if err != nil {
		return err
	}

	// Mark as processed and add to dropped list
	fkm.processedConstraints[constraintKey] = true
	fkm.droppedKeys = append(fkm.droppedKeys, *fk)

	// Clean up duplicates in droppedKeys
	fkm.deduplicateDroppedKeysUnsafe()

	// Update backup file snapshot (will acquire its own lock)
	fkm.mu.Unlock()
	backupErr := fkm.writeBackupSnapshot()
	fkm.mu.Lock()
	if backupErr != nil {
		fkm.logger.LogWarn("Failed to update FK backup file: %v", backupErr)
	}

	return nil
}

// deduplicateDroppedKeys removes duplicate entries from droppedKeys slice
func (fkm *ForeignKeyManager) deduplicateDroppedKeys() {
	fkm.mu.Lock()
	defer fkm.mu.Unlock()
	fkm.deduplicateDroppedKeysUnsafe()
}

// deduplicateDroppedKeysUnsafe removes duplicate entries from droppedKeys slice (must hold mu.Lock)
func (fkm *ForeignKeyManager) deduplicateDroppedKeysUnsafe() {
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
func (fkm *ForeignKeyManager) writeBackupSnapshot() error {
	fkm.mu.Lock()
	defer fkm.mu.Unlock()

	if len(fkm.droppedKeys) == 0 {
		// No FKs to backup, remove backup file if it exists
		if _, err := os.Stat(fkm.backupFile); err == nil {
			return os.Remove(fkm.backupFile)
		}
		return nil
	}

	// Deduplicate droppedKeys using a map
	uniqueFKs := make(map[string]ForeignKey)
	for _, fk := range fkm.droppedKeys {
		constraintKey := fmt.Sprintf("%s.%s.%s", fk.Schema, fk.Table, fk.ConstraintName)
		uniqueFKs[constraintKey] = fk
	}

	// Convert back to slice for sorting
	var deduplicatedFKs []ForeignKey
	for _, fk := range uniqueFKs {
		deduplicatedFKs = append(deduplicatedFKs, fk)
	}

	// Sort for consistent output
	sort.Slice(deduplicatedFKs, func(i, j int) bool {
		iKey := fmt.Sprintf("%s.%s.%s", deduplicatedFKs[i].Schema, deduplicatedFKs[i].Table, deduplicatedFKs[i].ConstraintName)
		jKey := fmt.Sprintf("%s.%s.%s", deduplicatedFKs[j].Schema, deduplicatedFKs[j].Table, deduplicatedFKs[j].ConstraintName)
		return iKey < jKey
	})

	// Create backup file content
	var content strings.Builder
	content.WriteString("-- Foreign Key Backup File\n")
	content.WriteString("-- Generated by pgcopy - contains unrestored FK constraints\n")
	content.WriteString("-- Execute this file manually if pgcopy process was interrupted\n\n")

	for _, fk := range deduplicatedFKs {
		content.WriteString(fmt.Sprintf("-- FK: %s.%s.%s\n", fk.Schema, fk.Table, fk.ConstraintName))
		content.WriteString(fmt.Sprintf("%s;\n\n", fk.Definition))
	}

	// Remove trailing empty line
	contentStr := strings.TrimSuffix(content.String(), "\n")

	return os.WriteFile(fkm.backupFile, []byte(contentStr), 0600)
}

// CleanupBackupFile removes the backup file on successful completion
func (fkm *ForeignKeyManager) CleanupBackupFile() error {
	fkm.mu.Lock()
	defer fkm.mu.Unlock()

	if _, err := os.Stat(fkm.backupFile); os.IsNotExist(err) {
		return nil // File doesn't exist
	}

	fkm.logger.LogSuccess("All foreign keys restored successfully, cleaning up backup file: %s", fkm.backupFile)
	return os.Remove(fkm.backupFile)
}

// RecoverFromBackupFile attempts to restore FKs from backup file if they exist but weren't tracked
func (fkm *ForeignKeyManager) RecoverFromBackupFile() error {
	if fkm.useReplica {
		return nil
	}

	// Check if backup file exists
	if _, err := os.Stat(fkm.backupFile); os.IsNotExist(err) {
		return nil // No backup file to recover from
	}

	fkm.logger.LogProgress("Found FK backup file, attempting to recover any untracked foreign keys...")

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

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ALTER TABLE") && strings.HasSuffix(line, ";") {
			// Remove trailing semicolon
			statement := strings.TrimSuffix(line, ";")

			_, err := fkm.db.Exec(statement)
			if err != nil {
				// Ignore "already exists" errors, but log others
				if !strings.Contains(err.Error(), "already exists") {
					errors = append(errors, fmt.Errorf("failed to restore FK from backup: %w", err))
					fkm.logger.LogWarn("Failed to restore FK from backup: %v", err)
				}
			} else {
				restored++
				fkm.logger.LogSuccess("Successfully restored FK from backup file")
			}
		}
	}

	if restored > 0 {
		fkm.logger.LogSuccess("Recovered %s foreign key constraints from backup file", utils.HighlightNumber(restored))
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during backup recovery", len(errors))
	}

	return nil
}
