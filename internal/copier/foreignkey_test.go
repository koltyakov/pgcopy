package copier

import (
	"sync"
	"testing"

	"github.com/koltyakov/pgcopy/internal/utils"
)

func TestForeignKeyManager_sortKeysByDependency(t *testing.T) {
	fkm := &ForeignKeyManager{}

	keys := []ForeignKey{
		{
			ConstraintName:   "fk_self_ref",
			Schema:           "public",
			Table:            "users",
			ReferencedSchema: "public",
			ReferencedTable:  "users", // Self-referencing
		},
		{
			ConstraintName:   "fk_normal",
			Schema:           "public",
			Table:            "orders",
			ReferencedSchema: "public",
			ReferencedTable:  "users",
		},
		{
			ConstraintName:   "fk_another",
			Schema:           "public",
			Table:            "items",
			ReferencedSchema: "public",
			ReferencedTable:  "orders",
		},
	}

	sorted := fkm.sortKeysByDependency(keys)

	// Self-referencing FK should be last
	if sorted[len(sorted)-1].ConstraintName != "fk_self_ref" {
		t.Errorf("Expected self-referencing FK to be last, got %s", sorted[len(sorted)-1].ConstraintName)
	}

	// Verify we have the same number of keys
	if len(sorted) != len(keys) {
		t.Errorf("Expected %d keys, got %d", len(keys), len(sorted))
	}
}

func TestForeignKeyManager_buildConstraintDefinition(t *testing.T) {
	fkm := &ForeignKeyManager{}

	fk := &ForeignKey{
		ConstraintName:    "fk_test",
		Schema:            "public",
		Table:             "orders",
		Columns:           []string{"user_id", "status_id"},
		ReferencedSchema:  "public",
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id", "status"},
		OnDelete:          "CASCADE",
		OnUpdate:          "RESTRICT",
	}

	definition := fkm.buildConstraintDefinition(fk)

	expected := `ALTER TABLE "public"."orders" ADD CONSTRAINT "fk_test" FOREIGN KEY ("user_id", "status_id") REFERENCES "public"."users" ("id", "status") ON DELETE CASCADE ON UPDATE RESTRICT NOT VALID`

	if definition != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, definition)
	}
}

func TestForeignKeyManager_buildConstraintDefinition_NoActions(t *testing.T) {
	fkm := &ForeignKeyManager{}

	fk := &ForeignKey{
		ConstraintName:    "fk_simple",
		Schema:            "public",
		Table:             "orders",
		Columns:           []string{"user_id"},
		ReferencedSchema:  "public",
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id"},
		OnDelete:          "NO ACTION",
		OnUpdate:          "NO ACTION",
	}

	definition := fkm.buildConstraintDefinition(fk)

	expected := `ALTER TABLE "public"."orders" ADD CONSTRAINT "fk_simple" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") NOT VALID`

	if definition != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, definition)
	}
}

func TestForeignKeyManager_GetForeignKeyStats(t *testing.T) {
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{{}, {}, {}}, // 3 total keys
		droppedKeys: []ForeignKey{{}, {}},     // 2 dropped keys
	}

	total, dropped := fkm.GetForeignKeyStats()

	if total != 3 {
		t.Errorf("Expected 3 total keys, got %d", total)
	}

	if dropped != 2 {
		t.Errorf("Expected 2 dropped keys, got %d", dropped)
	}
}

func TestForeignKeyManager_ConcurrentAccess(t *testing.T) {
	fkm := &ForeignKeyManager{
		foreignKeys:          make([]ForeignKey, 0),
		droppedKeys:          make([]ForeignKey, 0),
		processedConstraints: make(map[string]bool),
	}

	// Create test foreign keys
	testFKs := []ForeignKey{
		{
			ConstraintName:   "fk_test_1",
			Schema:           "public",
			Table:            "table1",
			ReferencedSchema: "public",
			ReferencedTable:  "ref_table",
		},
		{
			ConstraintName:   "fk_test_2",
			Schema:           "public",
			Table:            "table2",
			ReferencedSchema: "public",
			ReferencedTable:  "ref_table",
		},
		{
			ConstraintName:   "fk_test_3",
			Schema:           "public",
			Table:            "table3",
			ReferencedSchema: "public",
			ReferencedTable:  "ref_table",
		},
	}

	// Simulate concurrent access to the processedConstraints map
	var wg sync.WaitGroup
	numGoroutines := 10

	// Function to simulate dropping FKs
	dropFKs := func(fks []ForeignKey) {
		defer wg.Done()
		for _, fk := range fks {
			// Simulate the processing logic from dropForeignKey
			constraintKey := fk.Schema + "." + fk.Table + "." + fk.ConstraintName

			fkm.mu.Lock()
			if !fkm.processedConstraints[constraintKey] {
				fkm.processedConstraints[constraintKey] = true
				fkm.droppedKeys = append(fkm.droppedKeys, fk)
			}
			fkm.mu.Unlock()
		}
	}

	// Function to simulate restoring FKs
	restoreFKs := func() {
		defer wg.Done()

		fkm.mu.RLock()
		droppedCopy := make([]ForeignKey, len(fkm.droppedKeys))
		copy(droppedCopy, fkm.droppedKeys)
		fkm.mu.RUnlock()

		for _, fk := range droppedCopy {
			constraintKey := fk.Schema + "." + fk.Table + "." + fk.ConstraintName

			fkm.mu.RLock()
			wasProcessed := fkm.processedConstraints[constraintKey]
			fkm.mu.RUnlock()

			if wasProcessed {
				fkm.mu.Lock()
				delete(fkm.processedConstraints, constraintKey)
				fkm.mu.Unlock()
			}
		}
	}

	// Start concurrent operations
	for range numGoroutines {
		wg.Add(2)
		go dropFKs(testFKs)
		go restoreFKs()
	}

	// Wait for all operations to complete
	wg.Wait()

	// Verify final state
	fkm.mu.RLock()
	defer fkm.mu.RUnlock()

	// The exact final counts depend on timing, but the operations should complete without panics
	t.Logf("Final state: %d dropped keys, %d processed constraints",
		len(fkm.droppedKeys), len(fkm.processedConstraints))
}

func TestForeignKeyManager_GetStatsThreadSafe(t *testing.T) {
	fkm := &ForeignKeyManager{
		foreignKeys:          make([]ForeignKey, 5),
		droppedKeys:          make([]ForeignKey, 3),
		processedConstraints: make(map[string]bool),
	}

	// Test concurrent reads of stats
	var wg sync.WaitGroup
	numReads := 100

	for range numReads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			total, dropped := fkm.GetForeignKeyStats()
			if total != 5 || dropped != 3 {
				t.Errorf("Expected stats (5, 3), got (%d, %d)", total, dropped)
			}
		}()
	}

	wg.Wait()
}

func TestForeignKeyManager_DeduplicationLogic(t *testing.T) {
	fkm := &ForeignKeyManager{
		foreignKeys:          make([]ForeignKey, 0),
		droppedKeys:          make([]ForeignKey, 0),
		processedConstraints: make(map[string]bool),
	}

	// Create duplicate foreign keys
	fk1 := ForeignKey{
		ConstraintName:   "fk_duplicate",
		Schema:           "public",
		Table:            "test_table",
		ReferencedSchema: "public",
		ReferencedTable:  "ref_table",
	}

	fk2 := ForeignKey{
		ConstraintName:   "fk_duplicate",
		Schema:           "public",
		Table:            "test_table",
		ReferencedSchema: "public",
		ReferencedTable:  "ref_table",
	}

	// Add duplicates to dropped keys
	fkm.droppedKeys = append(fkm.droppedKeys, fk1, fk2)

	// Test deduplication
	fkm.deduplicateDroppedKeys()

	if len(fkm.droppedKeys) != 1 {
		t.Errorf("Expected 1 unique FK after deduplication, got %d", len(fkm.droppedKeys))
	}

	if fkm.droppedKeys[0].ConstraintName != "fk_duplicate" {
		t.Errorf("Expected constraint name 'fk_duplicate', got %s", fkm.droppedKeys[0].ConstraintName)
	}
}

func TestForeignKeyManager_IsUsingReplicaMode(t *testing.T) {
	fkm := &ForeignKeyManager{
		useReplica: false,
	}

	if fkm.IsUsingReplicaMode() {
		t.Error("Expected replica mode to be false")
	}

	fkm.useReplica = true
	if !fkm.IsUsingReplicaMode() {
		t.Error("Expected replica mode to be true")
	}
}

func TestNewForeignKeyManager(t *testing.T) {
	fkm := NewForeignKeyManager(nil, nil, false)

	if fkm == nil {
		t.Fatal("Expected non-nil ForeignKeyManager")
	}

	if fkm.foreignKeys == nil {
		t.Error("Expected foreignKeys to be initialized")
	}
	if fkm.droppedKeys == nil {
		t.Error("Expected droppedKeys to be initialized")
	}
	if fkm.restoredKeys == nil {
		t.Error("Expected restoredKeys to be initialized")
	}
	if fkm.processedConstraints == nil {
		t.Error("Expected processedConstraints to be initialized")
	}
	if fkm.backupFile != ".fk_backup.sql" {
		t.Errorf("Expected backupFile to be '.fk_backup.sql', got %s", fkm.backupFile)
	}
	if fkm.initialBackupDone {
		t.Error("Expected initialBackupDone to be false")
	}
}

func TestNewForeignKeyManager_NoTimeouts(t *testing.T) {
	fkm := NewForeignKeyManager(nil, nil, true)

	if !fkm.noTimeouts {
		t.Error("Expected noTimeouts to be true")
	}
}

func TestForeignKeyManager_SetLogger(t *testing.T) {
	fkm := &ForeignKeyManager{}

	if fkm.logger != nil {
		t.Error("Expected logger to be nil initially")
	}

	logger := &utils.SimpleLogger{}
	fkm.SetLogger(logger)

	if fkm.logger == nil {
		t.Error("Expected logger to be set")
	}
}

func TestForeignKeyManager_sortKeysByDependency_Empty(t *testing.T) {
	fkm := &ForeignKeyManager{}

	sorted := fkm.sortKeysByDependency(nil)
	if len(sorted) != 0 {
		t.Errorf("Expected empty slice for nil input, got %d", len(sorted))
	}

	sorted = fkm.sortKeysByDependency([]ForeignKey{})
	if len(sorted) != 0 {
		t.Errorf("Expected empty slice for empty input, got %d", len(sorted))
	}
}

func TestForeignKeyManager_sortKeysByDependency_AllSelfRef(t *testing.T) {
	fkm := &ForeignKeyManager{}

	keys := []ForeignKey{
		{
			ConstraintName:   "fk_self1",
			Schema:           "public",
			Table:            "a",
			ReferencedSchema: "public",
			ReferencedTable:  "a",
		},
		{
			ConstraintName:   "fk_self2",
			Schema:           "public",
			Table:            "b",
			ReferencedSchema: "public",
			ReferencedTable:  "b",
		},
	}

	sorted := fkm.sortKeysByDependency(keys)
	if len(sorted) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(sorted))
	}
}

func TestForeignKeyManager_buildConstraintDefinition_OnlyDelete(t *testing.T) {
	fkm := &ForeignKeyManager{}

	fk := &ForeignKey{
		ConstraintName:    "fk_cascade",
		Schema:            "public",
		Table:             "orders",
		Columns:           []string{"user_id"},
		ReferencedSchema:  "public",
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id"},
		OnDelete:          "CASCADE",
		OnUpdate:          "NO ACTION",
	}

	definition := fkm.buildConstraintDefinition(fk)

	expected := `ALTER TABLE "public"."orders" ADD CONSTRAINT "fk_cascade" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON DELETE CASCADE NOT VALID`

	if definition != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, definition)
	}
}

func TestForeignKeyManager_buildConstraintDefinition_OnlyUpdate(t *testing.T) {
	fkm := &ForeignKeyManager{}

	fk := &ForeignKey{
		ConstraintName:    "fk_update",
		Schema:            "public",
		Table:             "orders",
		Columns:           []string{"user_id"},
		ReferencedSchema:  "public",
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id"},
		OnDelete:          "NO ACTION",
		OnUpdate:          "SET NULL",
	}

	definition := fkm.buildConstraintDefinition(fk)

	expected := `ALTER TABLE "public"."orders" ADD CONSTRAINT "fk_update" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE SET NULL NOT VALID`

	if definition != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, definition)
	}
}

func TestForeignKeyManager_DeduplicationLogic_NoDuplicates(t *testing.T) {
	fkm := &ForeignKeyManager{
		droppedKeys: []ForeignKey{
			{ConstraintName: "fk_1", Schema: "public", Table: "t1"},
			{ConstraintName: "fk_2", Schema: "public", Table: "t2"},
			{ConstraintName: "fk_3", Schema: "public", Table: "t3"},
		},
	}

	fkm.deduplicateDroppedKeys()

	if len(fkm.droppedKeys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(fkm.droppedKeys))
	}
}

func TestForeignKeyManager_GetForeignKeyStats_Empty(t *testing.T) {
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{},
		droppedKeys: []ForeignKey{},
	}

	total, dropped := fkm.GetForeignKeyStats()
	if total != 0 || dropped != 0 {
		t.Errorf("Expected (0, 0), got (%d, %d)", total, dropped)
	}
}
