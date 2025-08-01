package copier

import (
	"testing"
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

	expected := `ALTER TABLE "public"."orders" ADD CONSTRAINT "fk_test" FOREIGN KEY ("user_id", "status_id") REFERENCES "public"."users" ("id", "status") ON DELETE CASCADE ON UPDATE RESTRICT`

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

	expected := `ALTER TABLE "public"."orders" ADD CONSTRAINT "fk_simple" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id")`

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
