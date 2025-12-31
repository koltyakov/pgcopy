package copier

import (
	"testing"
)

func TestDefaultPlanner_PlanTables(t *testing.T) {
	planner := &defaultPlanner{}

	tables := []*TableInfo{
		{Schema: "public", Name: "users"},
		{Schema: "public", Name: "orders"},
		{Schema: "public", Name: "products"},
	}

	result, err := planner.PlanTables(tables)
	if err != nil {
		t.Fatalf("PlanTables failed: %v", err)
	}

	// Default planner returns input as-is
	if len(result) != len(tables) {
		t.Errorf("Expected %d tables, got %d", len(tables), len(result))
	}

	for i, table := range result {
		if table != tables[i] {
			t.Errorf("Table at index %d does not match", i)
		}
	}
}

func TestDefaultPlanner_PlanTables_Empty(t *testing.T) {
	planner := &defaultPlanner{}

	result, err := planner.PlanTables([]*TableInfo{})
	if err != nil {
		t.Fatalf("PlanTables failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 tables, got %d", len(result))
	}
}

func TestDefaultPlanner_PlanTables_Nil(t *testing.T) {
	planner := &defaultPlanner{}

	result, err := planner.PlanTables(nil)
	if err != nil {
		t.Fatalf("PlanTables failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result for nil input")
	}
}

func TestDefaultPlanner_PlanLayers_EmptyInput(t *testing.T) {
	p := &defaultPlanner{c: &Copier{}}
	layers, err := p.PlanLayers(nil)
	if err != nil {
		t.Errorf("PlanLayers should not error on nil input: %v", err)
	}
	if layers != nil {
		t.Errorf("Expected nil layers for nil input, got %v", layers)
	}
}

func TestDefaultPlanner_PlanLayers_SingleTable(t *testing.T) {
	c := &Copier{}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "public", Name: "users", TotalRows: 100},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}
	if len(layers) != 1 {
		t.Errorf("Expected 1 layer, got %d", len(layers))
	}
	if len(layers[0]) != 1 {
		t.Errorf("Expected 1 table in layer, got %d", len(layers[0]))
	}
}

func TestDefaultPlanner_PlanLayers_MultipleTables_NoFK(t *testing.T) {
	c := &Copier{}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "public", Name: "users", TotalRows: 100},
		{Schema: "public", Name: "products", TotalRows: 200},
		{Schema: "public", Name: "categories", TotalRows: 50},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}
	if len(layers) != 1 {
		t.Errorf("Expected 1 layer when no FKs, got %d", len(layers))
	}
	if len(layers[0]) != 3 {
		t.Errorf("Expected 3 tables in layer, got %d", len(layers[0]))
	}
}

func TestDefaultPlanner_PlanLayers_WithFK(t *testing.T) {
	// Create a copier with an FK manager that has some foreign keys
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{
			{
				Schema:           "public",
				Table:            "orders",
				ReferencedSchema: "public",
				ReferencedTable:  "users",
				ConstraintName:   "orders_user_id_fkey",
			},
		},
	}
	c := &Copier{fkManager: fkm}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "public", Name: "users", TotalRows: 100},
		{Schema: "public", Name: "orders", TotalRows: 200},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}

	// Users should be in first layer, orders in second
	if len(layers) != 2 {
		t.Errorf("Expected 2 layers with FK dependency, got %d", len(layers))
		return
	}

	// First layer should have users (the parent)
	if layers[0][0].Name != "users" {
		t.Errorf("Expected 'users' in first layer, got '%s'", layers[0][0].Name)
	}

	// Second layer should have orders (the child)
	if layers[1][0].Name != "orders" {
		t.Errorf("Expected 'orders' in second layer, got '%s'", layers[1][0].Name)
	}
}

func TestDefaultPlanner_PlanLayers_ChainedFK(t *testing.T) {
	// Create a chain: categories -> products -> order_items
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{
			{
				Schema:           "public",
				Table:            "products",
				ReferencedSchema: "public",
				ReferencedTable:  "categories",
				ConstraintName:   "products_category_id_fkey",
			},
			{
				Schema:           "public",
				Table:            "order_items",
				ReferencedSchema: "public",
				ReferencedTable:  "products",
				ConstraintName:   "order_items_product_id_fkey",
			},
		},
	}
	c := &Copier{fkManager: fkm}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "public", Name: "order_items", TotalRows: 500},
		{Schema: "public", Name: "products", TotalRows: 200},
		{Schema: "public", Name: "categories", TotalRows: 50},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}

	if len(layers) != 3 {
		t.Errorf("Expected 3 layers for chained FK, got %d", len(layers))
		return
	}

	// First layer should have categories
	if layers[0][0].Name != "categories" {
		t.Errorf("Expected 'categories' in first layer, got '%s'", layers[0][0].Name)
	}

	// Second layer should have products
	if layers[1][0].Name != "products" {
		t.Errorf("Expected 'products' in second layer, got '%s'", layers[1][0].Name)
	}

	// Third layer should have order_items
	if layers[2][0].Name != "order_items" {
		t.Errorf("Expected 'order_items' in third layer, got '%s'", layers[2][0].Name)
	}
}

func TestDefaultPlanner_PlanLayers_SelfReference(t *testing.T) {
	// Self-referencing FKs should be ignored in ordering
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{
			{
				Schema:           "public",
				Table:            "categories",
				ReferencedSchema: "public",
				ReferencedTable:  "categories",
				ConstraintName:   "categories_parent_id_fkey",
			},
		},
	}
	c := &Copier{fkManager: fkm}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "public", Name: "categories", TotalRows: 50},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}

	// Should still be one layer since self-reference doesn't create dependency
	if len(layers) != 1 {
		t.Errorf("Expected 1 layer with self-reference, got %d", len(layers))
	}
}

func TestDefaultPlanner_PlanLayers_ExternalFK(t *testing.T) {
	// FK to table not in the plan should be ignored
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{
			{
				Schema:           "public",
				Table:            "orders",
				ReferencedSchema: "public",
				ReferencedTable:  "customers", // not in tables list
				ConstraintName:   "orders_customer_id_fkey",
			},
		},
	}
	c := &Copier{fkManager: fkm}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "public", Name: "orders", TotalRows: 200},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}

	// Should be one layer since referenced table is external
	if len(layers) != 1 {
		t.Errorf("Expected 1 layer when FK is external, got %d", len(layers))
	}
}

func TestDefaultPlanner_PlanLayers_DiamondDependency(t *testing.T) {
	// Diamond pattern: A <- B, A <- C, B <- D, C <- D
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{
			{Schema: "public", Table: "b", ReferencedSchema: "public", ReferencedTable: "a"},
			{Schema: "public", Table: "c", ReferencedSchema: "public", ReferencedTable: "a"},
			{Schema: "public", Table: "d", ReferencedSchema: "public", ReferencedTable: "b"},
			{Schema: "public", Table: "d", ReferencedSchema: "public", ReferencedTable: "c"},
		},
	}
	c := &Copier{fkManager: fkm}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "public", Name: "d"},
		{Schema: "public", Name: "c"},
		{Schema: "public", Name: "b"},
		{Schema: "public", Name: "a"},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}

	if len(layers) != 3 {
		t.Errorf("Expected 3 layers for diamond dependency, got %d", len(layers))
		return
	}

	// Layer 1: a
	// Layer 2: b, c
	// Layer 3: d
	if layers[0][0].Name != "a" {
		t.Errorf("Expected 'a' in first layer")
	}
	if len(layers[1]) != 2 {
		t.Errorf("Expected 2 tables in second layer, got %d", len(layers[1]))
	}
	if layers[2][0].Name != "d" {
		t.Errorf("Expected 'd' in third layer")
	}
}

func TestDefaultPlanner_PlanLayers_MultipleSchemas(t *testing.T) {
	fkm := &ForeignKeyManager{
		foreignKeys: []ForeignKey{
			{
				Schema:           "orders",
				Table:            "items",
				ReferencedSchema: "inventory",
				ReferencedTable:  "products",
			},
		},
	}
	c := &Copier{fkManager: fkm}
	p := &defaultPlanner{c: c}

	tables := []*TableInfo{
		{Schema: "orders", Name: "items", TotalRows: 500},
		{Schema: "inventory", Name: "products", TotalRows: 200},
	}

	layers, err := p.PlanLayers(tables)
	if err != nil {
		t.Errorf("PlanLayers should not error: %v", err)
	}

	if len(layers) != 2 {
		t.Errorf("Expected 2 layers for cross-schema FK, got %d", len(layers))
		return
	}

	// First layer should have products
	if layers[0][0].Schema != "inventory" || layers[0][0].Name != "products" {
		t.Errorf("Expected 'inventory.products' in first layer")
	}

	// Second layer should have items
	if layers[1][0].Schema != "orders" || layers[1][0].Name != "items" {
		t.Errorf("Expected 'orders.items' in second layer")
	}
}

func TestDefaultDiscovery_Interface(t *testing.T) {
	// Test that defaultDiscovery implements Discovery interface
	var _ Discovery = &defaultDiscovery{}
}

func TestDefaultPlanner_Interface(t *testing.T) {
	// Test that defaultPlanner implements Planner interface
	var _ Planner = &defaultPlanner{}
}

func TestDefaultExecutor_Interface(t *testing.T) {
	// Test that defaultExecutor implements Executor interface
	var _ Executor = &defaultExecutor{}
}
