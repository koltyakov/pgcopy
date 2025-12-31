package state

import (
	"testing"
)

func TestIsValidOperationTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     OperationStatus
		to       OperationStatus
		expected bool
	}{
		// Valid transitions from Initializing
		{"initializing->preparing", StatusInitializing, StatusPreparing, true},
		{"initializing->confirming", StatusInitializing, StatusConfirming, true},
		{"initializing->failed", StatusInitializing, StatusFailed, true},
		{"initializing->cancelled", StatusInitializing, StatusCancelled, true},
		{"initializing->completed", StatusInitializing, StatusCompleted, false},
		{"initializing->copying", StatusInitializing, StatusCopying, false},

		// Valid transitions from Confirming
		{"confirming->preparing", StatusConfirming, StatusPreparing, true},
		{"confirming->cancelled", StatusConfirming, StatusCancelled, true},
		{"confirming->completed", StatusConfirming, StatusCompleted, false},

		// Valid transitions from Preparing
		{"preparing->copying", StatusPreparing, StatusCopying, true},
		{"preparing->completed", StatusPreparing, StatusCompleted, true},
		{"preparing->failed", StatusPreparing, StatusFailed, true},
		{"preparing->cancelled", StatusPreparing, StatusCancelled, true},

		// Valid transitions from Copying
		{"copying->completed", StatusCopying, StatusCompleted, true},
		{"copying->failed", StatusCopying, StatusFailed, true},
		{"copying->cancelled", StatusCopying, StatusCancelled, true},
		{"copying->preparing", StatusCopying, StatusPreparing, false},

		// Terminal states - no transitions allowed
		{"completed->copying", StatusCompleted, StatusCopying, false},
		{"completed->failed", StatusCompleted, StatusFailed, false},
		{"failed->completed", StatusFailed, StatusCompleted, false},
		{"failed->copying", StatusFailed, StatusCopying, false},
		{"cancelled->completed", StatusCancelled, StatusCompleted, false},
		{"cancelled->copying", StatusCancelled, StatusCopying, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidOperationTransition(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("isValidOperationTransition(%s, %s) = %v, want %v",
					tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestIsValidTableTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     TableStatus
		to       TableStatus
		expected bool
	}{
		// Valid transitions from Pending
		{"pending->queued", TableStatusPending, TableStatusQueued, true},
		{"pending->copying", TableStatusPending, TableStatusCopying, true},
		{"pending->skipped", TableStatusPending, TableStatusSkipped, true},
		{"pending->cancelled", TableStatusPending, TableStatusCancelled, true},
		{"pending->completed", TableStatusPending, TableStatusCompleted, false},

		// Valid transitions from Queued
		{"queued->copying", TableStatusQueued, TableStatusCopying, true},
		{"queued->skipped", TableStatusQueued, TableStatusSkipped, true},
		{"queued->cancelled", TableStatusQueued, TableStatusCancelled, true},
		{"queued->completed", TableStatusQueued, TableStatusCompleted, false},

		// Valid transitions from Copying
		{"copying->completed", TableStatusCopying, TableStatusCompleted, true},
		{"copying->failed", TableStatusCopying, TableStatusFailed, true},
		{"copying->retrying", TableStatusCopying, TableStatusRetrying, true},
		{"copying->cancelled", TableStatusCopying, TableStatusCancelled, true},
		{"copying->pending", TableStatusCopying, TableStatusPending, false},

		// Valid transitions from Retrying
		{"retrying->copying", TableStatusRetrying, TableStatusCopying, true},
		{"retrying->failed", TableStatusRetrying, TableStatusFailed, true},
		{"retrying->cancelled", TableStatusRetrying, TableStatusCancelled, true},
		{"retrying->completed", TableStatusRetrying, TableStatusCompleted, false},

		// Valid transitions from Failed (can retry)
		{"failed->retrying", TableStatusFailed, TableStatusRetrying, true},
		{"failed->completed", TableStatusFailed, TableStatusCompleted, false},
		{"failed->copying", TableStatusFailed, TableStatusCopying, false},

		// Terminal states - no transitions allowed
		{"completed->copying", TableStatusCompleted, TableStatusCopying, false},
		{"completed->failed", TableStatusCompleted, TableStatusFailed, false},
		{"skipped->copying", TableStatusSkipped, TableStatusCopying, false},
		{"cancelled->copying", TableStatusCancelled, TableStatusCopying, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidTableTransition(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("isValidTableTransition(%s, %s) = %v, want %v",
					tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestIsValidTableTransition_UnknownStatus(t *testing.T) {
	unknownStatus := TableStatus("unknown")

	result := isValidTableTransition(unknownStatus, TableStatusCopying)
	if result != false {
		t.Error("Expected false for unknown source status")
	}

	result = isValidTableTransition(TableStatusPending, unknownStatus)
	if result != false {
		t.Error("Expected false for unknown target status")
	}
}

func TestIsValidOperationTransition_UnknownStatus(t *testing.T) {
	unknownStatus := OperationStatus("unknown")

	result := isValidOperationTransition(unknownStatus, StatusCopying)
	if result != false {
		t.Error("Expected false for unknown source status")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("Expected non-empty ID")
	}
	if len(id1) != 16 {
		t.Errorf("Expected ID length 16, got %d", len(id1))
	}
	if id1 == id2 {
		t.Error("Expected unique IDs")
	}
}

func TestCopyState_AddTableError(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})
	state.AddTable("public", "users", 1000)

	ctx := map[string]any{"row": 42}
	state.AddTableError("public", "users", "Test error", ctx)

	table := state.Tables[0]
	if len(table.Errors) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(table.Errors))
	}

	err := table.Errors[0]
	if err.Message != "Test error" {
		t.Errorf("Expected message 'Test error', got '%s'", err.Message)
	}
	if err.Context["row"] != 42 {
		t.Error("Expected context to contain row=42")
	}
	if table.ErrorRows != 1 {
		t.Errorf("Expected error rows 1, got %d", table.ErrorRows)
	}
}

func TestCopyState_AddTableError_NonExistent(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	// Should not panic
	state.AddTableError("public", "nonexistent", "Test error", nil)
}

func TestCopyState_UpdateTableStatus_Skipped(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})
	state.AddTable("public", "users", 1000)

	state.UpdateTableStatus("public", "users", TableStatusSkipped)

	if state.Tables[0].Status != TableStatusSkipped {
		t.Errorf("Expected status Skipped, got '%s'", state.Tables[0].Status)
	}
	if state.Summary.SkippedTables != 1 {
		t.Errorf("Expected skipped tables 1, got %d", state.Summary.SkippedTables)
	}
}

func TestCopyState_UpdateTableStatus_Failed(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})
	state.AddTable("public", "users", 1000)
	state.UpdateTableStatus("public", "users", TableStatusCopying)

	state.UpdateTableStatus("public", "users", TableStatusFailed)

	if state.Tables[0].Status != TableStatusFailed {
		t.Errorf("Expected status Failed, got '%s'", state.Tables[0].Status)
	}
	if state.Summary.FailedTables != 1 {
		t.Errorf("Expected failed tables 1, got %d", state.Summary.FailedTables)
	}
	if state.Tables[0].EndTime == nil {
		t.Error("Expected EndTime to be set for failed status")
	}
}

func TestCopyState_AddError(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	ctx := map[string]any{"key": "value"}
	state.AddError("connection_error", "Failed to connect", "copier", true, ctx)

	if len(state.Errors) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(state.Errors))
	}

	err := state.Errors[0]
	if err.Type != "connection_error" {
		t.Errorf("Expected type 'connection_error', got '%s'", err.Type)
	}
	if err.Message != "Failed to connect" {
		t.Errorf("Expected message 'Failed to connect', got '%s'", err.Message)
	}
	if err.Component != "copier" {
		t.Errorf("Expected component 'copier', got '%s'", err.Component)
	}
	if !err.IsFatal {
		t.Error("Expected IsFatal to be true")
	}
	if err.Context["key"] != "value" {
		t.Error("Expected context key=value")
	}
}

func TestCopyState_UpdateForeignKeys(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.UpdateForeignKeys("drop_restore", false, 10)

	if state.ForeignKeys.Mode != "drop_restore" {
		t.Errorf("Expected mode 'drop_restore', got '%s'", state.ForeignKeys.Mode)
	}
	if state.ForeignKeys.IsUsingReplicaMode {
		t.Error("Expected IsUsingReplicaMode to be false")
	}
	if state.ForeignKeys.TotalFKs != 10 {
		t.Errorf("Expected TotalFKs 10, got %d", state.ForeignKeys.TotalFKs)
	}

	// Test replica mode
	state.UpdateForeignKeys("replica", true, 5)

	if state.ForeignKeys.Mode != "replica" {
		t.Errorf("Expected mode 'replica', got '%s'", state.ForeignKeys.Mode)
	}
	if !state.ForeignKeys.IsUsingReplicaMode {
		t.Error("Expected IsUsingReplicaMode to be true")
	}
}

func TestCopyState_AddForeignKey(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	// Add tables first
	state.AddTable("public", "orders", 1000)
	state.AddTable("public", "users", 500)

	fk := ForeignKey{
		ConstraintName: "fk_orders_user",
		SourceTable:    "public.orders",
		SourceColumns:  []string{"user_id"},
		TargetTable:    "public.users",
		TargetColumns:  []string{"id"},
		OnDelete:       "CASCADE",
		OnUpdate:       "NO ACTION",
	}

	state.AddForeignKey(fk)

	if len(state.ForeignKeys.ForeignKeys) != 1 {
		t.Fatalf("Expected 1 foreign key, got %d", len(state.ForeignKeys.ForeignKeys))
	}

	// Check that the source table has the FK
	ordersTable := state.Tables[0]
	if !ordersTable.HasForeignKey {
		t.Error("Expected orders table to have HasForeignKey=true")
	}
	if len(ordersTable.ForeignKeys) != 1 {
		t.Errorf("Expected 1 FK on orders, got %d", len(ordersTable.ForeignKeys))
	}
	if len(ordersTable.Dependencies) != 1 || ordersTable.Dependencies[0] != "public.users" {
		t.Errorf("Expected orders to depend on public.users")
	}

	// Check that the target table has dependents
	usersTable := state.Tables[1]
	if len(usersTable.Dependents) != 1 || usersTable.Dependents[0] != "public.orders" {
		t.Errorf("Expected users to have public.orders as dependent")
	}
}

func TestCopyState_UpdateMetrics(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	metrics := Metrics{
		RowsPerSecond:     1000,
		TablesPerMinute:   10,
		AverageTableTime:  30,
		PeakRowsPerSecond: 1500,
		ActiveWorkers:     4,
		QueuedTables:      10,
	}

	state.UpdateMetrics(metrics)

	if state.Metrics.RowsPerSecond != 1000 {
		t.Errorf("Expected RowsPerSecond 1000, got %.1f", state.Metrics.RowsPerSecond)
	}
	if state.Metrics.ActiveWorkers != 4 {
		t.Errorf("Expected ActiveWorkers 4, got %d", state.Metrics.ActiveWorkers)
	}

	// Check that history was updated
	if len(state.Metrics.RowsPerSecondHistory) != 1 {
		t.Errorf("Expected 1 history point, got %d", len(state.Metrics.RowsPerSecondHistory))
	}
}

func TestCopyState_UpdateMetrics_HistoryLimit(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	// Add more than 60 metrics updates
	for i := 0; i < 70; i++ {
		metrics := Metrics{RowsPerSecond: float64(i * 100)}
		state.UpdateMetrics(metrics)
	}

	// Should only keep last 60 points
	if len(state.Metrics.RowsPerSecondHistory) > 60 {
		t.Errorf("Expected max 60 history points, got %d", len(state.Metrics.RowsPerSecondHistory))
	}
}

func TestCopyState_UpdateConnectionDetails(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.UpdateConnectionDetails("source", "localhost:5432/mydb", ConnectionStatusConnected)

	if state.Connections.Source.Display != "localhost:5432/mydb" {
		t.Errorf("Expected source display 'localhost:5432/mydb', got '%s'", state.Connections.Source.Display)
	}
	if state.Connections.Source.Status != ConnectionStatusConnected {
		t.Errorf("Expected source status Connected, got '%s'", state.Connections.Source.Status)
	}
	if state.Connections.Source.LastPing == nil {
		t.Error("Expected LastPing to be set")
	}

	state.UpdateConnectionDetails("destination", "remote:5432/otherdb", ConnectionStatusConnecting)

	if state.Connections.Destination.Display != "remote:5432/otherdb" {
		t.Errorf("Expected dest display 'remote:5432/otherdb', got '%s'", state.Connections.Destination.Display)
	}
	if state.Connections.Destination.Status != ConnectionStatusConnecting {
		t.Errorf("Expected dest status Connecting, got '%s'", state.Connections.Destination.Status)
	}
}

func TestCopyState_GetTablesByStatus(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.AddTable("public", "users", 100)
	state.AddTable("public", "orders", 200)
	state.AddTable("public", "products", 300)

	state.UpdateTableStatus("public", "users", TableStatusCompleted)
	state.UpdateTableStatus("public", "orders", TableStatusCopying)
	// products stays pending

	completedTables := state.GetTablesByStatus(TableStatusCompleted)
	if len(completedTables) != 1 {
		t.Errorf("Expected 1 completed table, got %d", len(completedTables))
	}
	if completedTables[0] != "public.users" {
		t.Errorf("Expected 'public.users', got '%s'", completedTables[0])
	}

	pendingTables := state.GetTablesByStatus(TableStatusPending)
	if len(pendingTables) != 1 {
		t.Errorf("Expected 1 pending table, got %d", len(pendingTables))
	}

	copyingTables := state.GetTablesByStatus(TableStatusCopying)
	if len(copyingTables) != 1 {
		t.Errorf("Expected 1 copying table, got %d", len(copyingTables))
	}
}

func TestCopyState_GetAllTables(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.AddTable("public", "users", 100)
	state.AddTable("public", "orders", 200)

	tables := state.GetAllTables()

	if len(tables) != 2 {
		t.Errorf("Expected 2 tables, got %d", len(tables))
	}

	// Verify it's a copy (modifying doesn't affect original)
	tables[0].Name = "modified"
	originalTables := state.GetAllTables()
	if originalTables[0].Name == "modified" {
		t.Error("GetAllTables should return a copy, not the original")
	}
}

func TestCopyState_FindTableIndex(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.AddTable("public", "users", 100)
	state.AddTable("auth", "sessions", 200)

	// Test finding existing table
	idx := state.findTableIndex("public", "users")
	if idx != 0 {
		t.Errorf("Expected index 0, got %d", idx)
	}

	idx = state.findTableIndex("auth", "sessions")
	if idx != 1 {
		t.Errorf("Expected index 1, got %d", idx)
	}

	// Test finding non-existent table
	idx = state.findTableIndex("public", "nonexistent")
	if idx != -1 {
		t.Errorf("Expected index -1 for non-existent table, got %d", idx)
	}
}

func TestCopyState_AssignWorker(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.AddTable("public", "table1", 100)
	state.AddTable("public", "table2", 100)
	state.AddTable("public", "table3", 100)
	state.AddTable("public", "table4", 100)
	state.AddTable("public", "table5", 100)

	// No tables copying - should assign worker 1
	worker := state.assignWorker()
	if worker != 1 {
		t.Errorf("Expected worker 1 with no active workers, got %d", worker)
	}

	// Set one table to copying
	state.UpdateTableStatus("public", "table1", TableStatusCopying)
	worker = state.assignWorker()
	// Now 1 active worker, next should be worker 2
	if worker != 2 {
		t.Errorf("Expected worker 2 with 1 active worker, got %d", worker)
	}
}

func TestCopyState_UpdateSummary(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.AddTable("public", "users", 1000)
	state.AddTable("public", "orders", 2000)

	// Update progress
	state.UpdateTableProgress("public", "users", 500)
	state.UpdateTableProgress("public", "orders", 1000)

	// Check summary calculations
	if state.Summary.TotalRows != 3000 {
		t.Errorf("Expected TotalRows 3000, got %d", state.Summary.TotalRows)
	}
	if state.Summary.SyncedRows != 1500 {
		t.Errorf("Expected SyncedRows 1500, got %d", state.Summary.SyncedRows)
	}

	expectedProgress := float64(1500) / float64(3000) * 100
	if state.Summary.OverallProgress != expectedProgress {
		t.Errorf("Expected OverallProgress %.1f, got %.1f", expectedProgress, state.Summary.OverallProgress)
	}
}
