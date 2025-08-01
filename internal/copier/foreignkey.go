package copier

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
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
	db          *sql.DB
	foreignKeys []ForeignKey
	droppedKeys []ForeignKey
	useReplica  bool
}

// NewForeignKeyManager creates a new foreign key manager
func NewForeignKeyManager(db *sql.DB) *ForeignKeyManager {
	return &ForeignKeyManager{
		db:          db,
		foreignKeys: make([]ForeignKey, 0),
		droppedKeys: make([]ForeignKey, 0),
	}
}

// DetectForeignKeys discovers all foreign keys in the database
func (fkm *ForeignKeyManager) DetectForeignKeys(tables []*TableInfo) error {
	log.Printf("Detecting foreign key constraints...")

	// Build table filter for IN clause
	tableFilters := make([]string, 0, len(tables))
	for _, table := range tables {
		tableFilters = append(tableFilters, fmt.Sprintf("'%s.%s'", table.Schema, table.Name))
	}

	if len(tableFilters) == 0 {
		return nil
	}

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
	defer rows.Close()

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

	log.Printf("Found %d foreign key constraints", len(fkm.foreignKeys))
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
		log.Printf("Cannot use replica mode (requires superuser), will drop/recreate FKs: %v", err)
		fkm.useReplica = false
		return nil
	}

	log.Printf("Using replica mode for foreign key handling")
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
		log.Printf("Warning: failed to restore session_replication_role: %v", err)
	}
	return err
}

// DropForeignKeysForTable drops all foreign keys that reference or are referenced by a table
func (fkm *ForeignKeyManager) DropForeignKeysForTable(table *TableInfo) error {
	// Skip FK management for empty tables that won't be copied
	if table.RowCount == 0 {
		log.Printf("Skipping FK management for empty table %s.%s", table.Schema, table.Name)
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
			log.Printf("Foreign keys for table %s.%s automatically handled via replica mode (%d constraints)",
				table.Schema, table.Name, len(relatedFKs))
			return nil
		} else {
			log.Printf("Managing %d foreign key constraint(s) for table %s.%s",
				len(relatedFKs), table.Schema, table.Name)
		}
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

// dropForeignKey drops a specific foreign key constraint
func (fkm *ForeignKeyManager) dropForeignKey(fk *ForeignKey) error {
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

	log.Printf("Dropping FK constraint %s on %s.%s", fk.ConstraintName, fk.Schema, fk.Table)
	_, err := fkm.db.Exec(query)
	if err != nil {
		return err
	}

	fkm.droppedKeys = append(fkm.droppedKeys, *fk)
	return nil
}

// RestoreAllForeignKeys restores all dropped foreign keys
func (fkm *ForeignKeyManager) RestoreAllForeignKeys() error {
	if fkm.useReplica {
		// Just disable replica mode
		return fkm.DisableReplicaMode()
	}

	if len(fkm.droppedKeys) == 0 {
		log.Printf("No foreign keys to restore")
		return nil
	}

	log.Printf("Restoring %d foreign key constraints...", len(fkm.droppedKeys))

	// Sort by dependency order to avoid restoration issues
	sortedKeys := fkm.sortKeysByDependency(fkm.droppedKeys)

	var errors []error
	restored := 0

	for _, fk := range sortedKeys {
		log.Printf("Restoring FK constraint %s on %s.%s", fk.ConstraintName, fk.Schema, fk.Table)

		_, err := fkm.db.Exec(fk.Definition)
		if err != nil {
			errMsg := fmt.Errorf("failed to restore FK %s on %s.%s: %w",
				fk.ConstraintName, fk.Schema, fk.Table, err)
			errors = append(errors, errMsg)
			log.Printf("Warning: %v", errMsg)
		} else {
			restored++
		}
	}

	log.Printf("Successfully restored %d/%d foreign key constraints", restored, len(fkm.droppedKeys))

	if len(errors) > 0 {
		return fmt.Errorf("failed to restore %d foreign keys: %v", len(errors), errors[0])
	}

	return nil
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
	return len(fkm.foreignKeys), len(fkm.droppedKeys)
}

// IsUsingReplicaMode returns whether replica mode is enabled
func (fkm *ForeignKeyManager) IsUsingReplicaMode() bool {
	return fkm.useReplica
}
